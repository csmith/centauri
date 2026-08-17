//go:build !noredis

package certificate

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// redisLockTTL is how long a certificate lock lasts without being refreshed. It exists so that a lock is
	// eventually released if a Centauri instance dies mid-renewal.
	redisLockTTL = time.Minute
	// redisLockRetryInterval is how long to wait between attempts to acquire a lock held elsewhere.
	redisLockRetryInterval = 500 * time.Millisecond
	// redisLockRenewInterval is how often a held lock is refreshed. Locks are refreshed so that lengthy
	// certificate obtaining operations don't outlive the lock.
	redisLockRenewInterval = redisLockTTL / 3
)

// RedisStore is responsible for storing and managing certificates in Redis. Unlike the JsonStore it allows multiple
// Centauri instances to share a single set of certificates, using distributed locks to co-ordinate obtaining and
// renewing them.
type RedisStore struct {
	client    *redis.Client
	keyPrefix string

	locksMu sync.Mutex
	locks   map[string]*redisLock
}

// NewRedisStore creates a new certificate store backed by the given Redis client. Keys are stored with the given
// prefix. An error is returned if the Redis server cannot be reached.
//
// Operations are bounded by redisOperationTimeout if the client is created with ContextTimeoutEnabled (go-redis
// bounds operations by its own ReadTimeout regardless).
func NewRedisStore(client *redis.Client, keyPrefix string) (*RedisStore, error) {
	ctx, cancel := operationContext()
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("unable to reach redis: %w", err)
	}

	return &RedisStore{
		client:    client,
		keyPrefix: keyPrefix,
		locks:     make(map[string]*redisLock),
	}, nil
}

// GetCertificate returns a previously stored certificate for the given provider, subject and alt names, or `nil` if
// none exists.
//
// A certificate with an empty provider (i.e. one loaded from a store created before providers were tracked) is
// treated as a legacy fallback: it will be returned for any provider. This allows existing certificates to continue
// to be served until they naturally expire and are replaced with provider-specific certificates on the next renewal.
//
// Returned certificates are not guaranteed to be valid.
func (r *RedisStore) GetCertificate(provider string, subjectName string, altNames []string) *Details {
	certificates, err := r.allCertificates()
	if err != nil {
		slog.Error("Unable to load certificates from redis", "error", err)
		return nil
	}

	var legacy *Details
	for i := range certificates {
		if !certificates[i].IsFor(subjectName, altNames) {
			continue
		}

		if certificates[i].Provider == provider {
			return certificates[i]
		}

		if certificates[i].Provider == "" {
			legacy = certificates[i]
		}
	}

	return legacy
}

// SaveCertificate adds the given certificate to the store, replacing any previously saved certificate for the same
// provider, subject and alt names. Certificates belonging to other providers are left untouched, and any certificates
// that are no longer valid are removed.
//
// Expired certificates are only removed if they're unchanged since we read them: we hold the lock for this
// certificate's names but not for theirs, and another instance may be concurrently renewing them.
//
// Callers should acquire a lock on the certificate by calling LockCertificate before saving it.
func (r *RedisStore) SaveCertificate(certificate *Details) error {
	stored, err := r.storedCertificates()
	if err != nil {
		return err
	}

	b, err := json.Marshal(certificate)
	if err != nil {
		return err
	}

	// Find entries to prune: anything expired (or too corrupt to decode). Their hash field and the value we read
	// are passed to the prune script, which only deletes them if they haven't been rewritten in the meantime.
	var pruneArgs []any
	for field, raw := range stored {
		var existing Details
		if err := json.Unmarshal([]byte(raw), &existing); err != nil || !existing.ValidFor(0) {
			pruneArgs = append(pruneArgs, field, raw)
		}
	}

	ctx, cancel := operationContext()
	defer cancel()

	pipe := r.client.TxPipeline()
	if len(pruneArgs) > 0 {
		pipe.Eval(ctx, pruneCertificatesScript, []string{r.certificatesKey()}, pruneArgs...)
	}
	pipe.HSet(ctx, r.certificatesKey(), certificateKey(certificate.Provider, certificate.Subject, certificate.AltNames), b)
	_, err = pipe.Exec(ctx)
	return err
}

// LockCertificate acquires a lock over the writing of the given certificate. The lock is distributed: other Centauri
// instances using the same Redis server will block until it is released. All calls to LockCertificate should be
// followed by calls to UnlockCertificate.
func (r *RedisStore) LockCertificate(subjectName string, altNames []string) {
	r.lockFor(subjectName, altNames).Lock()
}

// UnlockCertificate releases a previously acquired lock over the writing of the given certificate.
func (r *RedisStore) UnlockCertificate(subjectName string, altNames []string) {
	r.lockFor(subjectName, altNames).Unlock()
}

// lockFor provides the lock to use for locking access to the given certificate.
func (r *RedisStore) lockFor(subjectName string, altNames []string) *redisLock {
	key := r.keyPrefix + ":lock:" + namesKey(subjectName, altNames)

	r.locksMu.Lock()
	defer r.locksMu.Unlock()

	if lock, ok := r.locks[key]; ok {
		return lock
	}

	lock := &redisLock{client: r.client, key: key, ttl: redisLockTTL}
	r.locks[key] = lock
	return lock
}

// certificatesKey provides the Redis key of the hash holding all certificates.
func (r *RedisStore) certificatesKey() string {
	return r.keyPrefix + ":certificates"
}

// allCertificates loads and decodes all certificates stored in Redis. Entries that cannot be decoded are skipped.
func (r *RedisStore) allCertificates() ([]*Details, error) {
	stored, err := r.storedCertificates()
	if err != nil {
		return nil, err
	}

	certificates := make([]*Details, 0, len(stored))
	for key, raw := range stored {
		var certificate Details
		if err := json.Unmarshal([]byte(raw), &certificate); err != nil {
			slog.Warn("Skipping corrupt certificate in redis store", "error", err, "key", key)
			continue
		}
		certificates = append(certificates, &certificate)
	}

	return certificates, nil
}

// storedCertificates returns the raw JSON of all certificates stored in Redis, keyed by their hash field.
func (r *RedisStore) storedCertificates() (map[string]string, error) {
	ctx, cancel := operationContext()
	defer cancel()
	return r.client.HGetAll(ctx, r.certificatesKey()).Result()
}

// redisOperationTimeout bounds each individual Redis operation, so that an unresponsive server can't block callers
// indefinitely. Acquiring a lock retries forever (like a mutex would), but each attempt is bounded by this value.
var redisOperationTimeout = 5 * time.Second

// operationContext returns a context that bounds the lifetime of a single Redis operation.
func operationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), redisOperationTimeout)
}

// certificateKey builds the hash field that identifies a certificate from its provider, subject and alt names.
func certificateKey(provider string, subjectName string, altNames []string) string {
	return strings.Join(append([]string{provider, subjectName}, sortedCopy(altNames)...), ";")
}

// namesKey builds a stable identifier for a subject and its alt names, used for lock keys. Alt names are sorted so
// that routes specifying the same names in a different order still share a lock.
func namesKey(subjectName string, altNames []string) string {
	return strings.Join(append([]string{subjectName}, sortedCopy(altNames)...), ";")
}

// sortedCopy returns a sorted copy of the given names, so they can be joined into a key without mutating caller data.
func sortedCopy(names []string) []string {
	res := append([]string(nil), names...)
	sort.Strings(res)
	return res
}

// releaseLockScript releases the lock held by the given token, without affecting any lock acquired by someone else
// in the meantime (e.g. if our lock expired).
const releaseLockScript = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`

// extendLockScript extends the lock held by the given token, without affecting any lock acquired by someone else
// in the meantime (e.g. if our lock expired).
const extendLockScript = `if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("pexpire", KEYS[1], ARGV[2]) else return 0 end`

// pruneCertificatesScript deletes the given hash fields, but only if their values are unchanged since they were
// read. This stops us pruning a certificate that another instance has renewed between our read and our delete.
// Arguments are alternating hash fields and the value that was read for each.
const pruneCertificatesScript = `
for i = 1, #ARGV, 2 do
	if redis.call("hget", KEYS[1], ARGV[i]) == ARGV[i + 1] then
		redis.call("hdel", KEYS[1], ARGV[i])
	end
end
return 0`

// redisLock is a distributed lock over a single Redis key. While held, the lock is periodically refreshed so that it
// survives lengthy certificate obtaining operations, and it expires automatically if the process holding it dies.
type redisLock struct {
	client *redis.Client
	key    string
	ttl    time.Duration

	mu       sync.Mutex
	token    string
	stopChan chan struct{}
}

// Lock acquires the lock, blocking until it becomes available.
func (l *redisLock) Lock() {
	for {
		token := newLockToken()

		ctx, cancel := operationContext()
		acquired, err := l.client.SetNX(ctx, l.key, token, l.ttl).Result()
		cancel()

		if err != nil {
			slog.Error("Unable to acquire certificate lock, will retry", "error", err, "lock", l.key)
		} else if acquired {
			stop := make(chan struct{})
			l.mu.Lock()
			l.token = token
			l.stopChan = stop
			l.mu.Unlock()
			go l.refresh(token, stop)
			return
		}

		time.Sleep(redisLockRetryInterval)
	}
}

// Unlock releases a previously acquired lock. It is a no-op if the lock is not currently held. If the lock can't be
// released it will be left to expire on its own.
func (l *redisLock) Unlock() {
	l.mu.Lock()
	token := l.token
	stop := l.stopChan
	l.token = ""
	l.stopChan = nil
	l.mu.Unlock()

	if token == "" {
		return
	}

	close(stop)

	ctx, cancel := operationContext()
	defer cancel()
	if err := l.client.Eval(ctx, releaseLockScript, []string{l.key}, token).Err(); err != nil {
		slog.Warn("Unable to release certificate lock", "error", err, "lock", l.key)
	}
}

// refresh periodically extends the lock until the given channel is closed. It stops extending the lock if it detects
// that ownership has been lost (e.g. Redis was unavailable for longer than the lock's lifetime).
func (l *redisLock) refresh(token string, stop <-chan struct{}) {
	ticker := time.NewTicker(redisLockRenewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			ctx, cancel := operationContext()
			extended, err := l.client.Eval(ctx, extendLockScript, []string{l.key}, token, l.ttl.Milliseconds()).Int64()
			cancel()

			if err != nil || extended == 0 {
				// A failed extension is expected during unlock, when the lock has just been released: only warn
				// about losing the lock if we haven't been told to stop.
				select {
				case <-stop:
					return
				default:
					slog.Warn("Certificate lock could not be refreshed; it may have expired", "error", err, "lock", l.key)
				}
			}
		}
	}
}

// newLockToken generates a random token used to identify the holder of a lock.
func newLockToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

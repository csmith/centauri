//go:build !noredis

package certificate

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRedisStore(t *testing.T, keyPrefix string) *RedisStore {
	t.Helper()

	server := miniredis.RunT(t)
	store, err := NewRedisStore(redis.NewClient(&redis.Options{Addr: server.Addr()}), keyPrefix)
	require.NoError(t, err, "store should connect")

	return store
}

func Test_NewRedisStore_returnsErrorIfRedisUnreachable(t *testing.T) {
	// Grab a port that is guaranteed to be closed
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	listener.Close()

	_, err = NewRedisStore(redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1}), "test")
	assert.Error(t, err)
}

func Test_RedisStore_LoadSaveGet(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	store, err := NewRedisStore(client, "test")
	require.NoError(t, err, "store should load")

	timestamp := time.Now().Add(time.Hour).UTC()

	cert := &Details{
		Issuer:         "this is the issuer",
		PrivateKey:     "this is the private key",
		Certificate:    "this is the cert",
		Subject:        "subject.example.com",
		AltNames:       []string{"alt1.example.com", "alt2.example.com"},
		NotAfter:       timestamp,
		OcspResponse:   []byte("this is the ocsp response"),
		NextOcspUpdate: timestamp.Add(time.Minute),
	}

	require.NoError(t, store.SaveCertificate(cert), "store should save certificate")

	newStore, err := NewRedisStore(redis.NewClient(&redis.Options{Addr: server.Addr()}), "test")
	require.NoError(t, err, "second store should load")

	newCert := newStore.GetCertificate("", cert.Subject, cert.AltNames)
	assert.Equal(t, cert, newCert, "certificates should match")
}

func Test_RedisStore_saveCertificate_prunesExpiredCerts(t *testing.T) {
	store := newTestRedisStore(t, "test")

	certs := []*Details{
		{
			Subject:  "just-expired.example.com",
			NotAfter: time.Now().Add(-time.Hour),
		},
		{
			Subject:  "long-expired.example.com",
			NotAfter: time.Now().Add(-time.Hour * 24 * 365),
		},
		{
			Subject:  "zero-time.example.com",
			NotAfter: time.Time{},
		},
		{
			Subject:  "just-valid.example.com",
			NotAfter: time.Now().Add(time.Hour).UTC(),
		},
		{
			Subject:  "long-valid.example.com",
			NotAfter: time.Now().Add(time.Hour * 24 * 365),
		},
	}

	for i := range certs {
		require.NoError(t, store.SaveCertificate(certs[i]), "store should save certificate")
	}

	for i := range certs {
		t.Run(certs[i].Subject, func(t *testing.T) {
			hasCert := store.GetCertificate("", certs[i].Subject, certs[i].AltNames) != nil
			expectedCert := strings.Contains(certs[i].Subject, "-valid")
			assert.Equal(t, expectedCert, hasCert)
		})
	}
}

func Test_RedisStore_saveCertificate_removesDuplicates(t *testing.T) {
	store := newTestRedisStore(t, "test")

	certs := []*Details{
		{
			Subject:  "example.com",
			NotAfter: time.Now().Add(time.Hour).UTC(),
		},
		{
			Subject:  "example.com",
			NotAfter: time.Now().Add(time.Hour).UTC(),
		},
		{
			Subject:  "example.com",
			AltNames: []string{"example.net"},
			NotAfter: time.Now().Add(time.Hour).UTC(),
		},
		{
			Subject:  "example.com",
			AltNames: []string{"example.net"},
			NotAfter: time.Now().Add(time.Hour).UTC(),
		},
		{
			Subject:  "example.com",
			AltNames: []string{"example.org"},
			NotAfter: time.Now().Add(time.Hour).UTC(),
		},
	}

	for i := range certs {
		require.NoError(t, store.SaveCertificate(certs[i]), "store should save certificate")
	}

	stored, err := store.allCertificates()
	require.NoError(t, err)
	assert.Equal(t, 3, len(stored))
}

func Test_RedisStore_GetCertificate_returnsCertWithMatchingProvider(t *testing.T) {
	store := newTestRedisStore(t, "test")

	acmeCert := &Details{
		Provider: "acme",
		Subject:  "*.example.com",
		NotAfter: time.Now().Add(time.Hour).UTC(),
	}
	selfSignedCert := &Details{
		Provider: "selfsigned",
		Subject:  "*.example.com",
		NotAfter: time.Now().Add(time.Hour).UTC(),
	}

	require.NoError(t, store.SaveCertificate(acmeCert))
	require.NoError(t, store.SaveCertificate(selfSignedCert))

	assert.Equal(t, acmeCert, store.GetCertificate("acme", "*.example.com", nil))
	assert.Equal(t, selfSignedCert, store.GetCertificate("selfsigned", "*.example.com", nil))
}

func Test_RedisStore_GetCertificate_returnsLegacyCertAsFallback(t *testing.T) {
	store := newTestRedisStore(t, "test")

	legacyCert := &Details{
		Provider: "",
		Subject:  "*.example.com",
		NotAfter: time.Now().Add(time.Hour).UTC(),
	}

	require.NoError(t, store.SaveCertificate(legacyCert))

	assert.Equal(t, legacyCert, store.GetCertificate("acme", "*.example.com", nil))
	assert.Equal(t, legacyCert, store.GetCertificate("selfsigned", "*.example.com", nil))
}

func Test_RedisStore_GetCertificate_prefersExactProviderMatchOverLegacy(t *testing.T) {
	store := newTestRedisStore(t, "test")

	legacyCert := &Details{
		Provider: "",
		Subject:  "*.example.com",
		NotAfter: time.Now().Add(time.Hour).UTC(),
	}
	acmeCert := &Details{
		Provider: "acme",
		Subject:  "*.example.com",
		NotAfter: time.Now().Add(time.Hour).UTC(),
	}

	require.NoError(t, store.SaveCertificate(legacyCert))
	require.NoError(t, store.SaveCertificate(acmeCert))

	assert.Equal(t, acmeCert, store.GetCertificate("acme", "*.example.com", nil))
	assert.Equal(t, legacyCert, store.GetCertificate("selfsigned", "*.example.com", nil))
}

func Test_RedisStore_GetCertificate_returnsNilIfNoMatchingProvider(t *testing.T) {
	store := newTestRedisStore(t, "test")

	acmeCert := &Details{
		Provider: "acme",
		Subject:  "*.example.com",
		NotAfter: time.Now().Add(time.Hour).UTC(),
	}

	require.NoError(t, store.SaveCertificate(acmeCert))

	assert.Nil(t, store.GetCertificate("selfsigned", "*.example.com", nil))
}

func Test_RedisStore_SaveCertificate_doesNotEvictCertsFromOtherProviders(t *testing.T) {
	store := newTestRedisStore(t, "test")

	selfSignedCert := &Details{
		Provider: "selfsigned",
		Subject:  "*.example.com",
		NotAfter: time.Now().Add(time.Hour).UTC(),
	}
	acmeCert := &Details{
		Provider: "acme",
		Subject:  "*.example.com",
		NotAfter: time.Now().Add(time.Hour).UTC(),
	}

	require.NoError(t, store.SaveCertificate(selfSignedCert))
	require.NoError(t, store.SaveCertificate(acmeCert))

	stored, err := store.allCertificates()
	require.NoError(t, err)
	assert.Equal(t, 2, len(stored))
	assert.NotNil(t, store.GetCertificate("selfsigned", "*.example.com", nil))
	assert.NotNil(t, store.GetCertificate("acme", "*.example.com", nil))
}

func Test_RedisStore_storesAreIsolatedByKeyPrefix(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	storeOne, err := NewRedisStore(client, "one")
	require.NoError(t, err)
	storeTwo, err := NewRedisStore(client, "two")
	require.NoError(t, err)

	cert := &Details{
		Subject:  "example.com",
		NotAfter: time.Now().Add(time.Hour).UTC(),
	}
	require.NoError(t, storeOne.SaveCertificate(cert))

	assert.Equal(t, cert, storeOne.GetCertificate("", "example.com", nil))
	assert.Nil(t, storeTwo.GetCertificate("", "example.com", nil))
}

func Test_RedisStore_GetCertificate_skipsCorruptEntries(t *testing.T) {
	store := newTestRedisStore(t, "test")

	require.NoError(t, store.client.HSet(context.Background(), store.certificatesKey(), "corrupt", "not json").Err())

	cert := &Details{
		Subject:  "example.com",
		NotAfter: time.Now().Add(time.Hour).UTC(),
	}
	require.NoError(t, store.SaveCertificate(cert))

	assert.Equal(t, cert, store.GetCertificate("", "example.com", nil))
}

func Test_RedisStore_pruneScript_onlyDeletesUnchangedEntries(t *testing.T) {
	store := newTestRedisStore(t, "test")
	ctx := context.Background()

	expired := `{"subject":"example.com","notAfter":"2000-01-01T00:00:00Z"}`
	renewed := `{"subject":"example.com","notAfter":"2099-01-01T00:00:00Z"}`

	// Two certificates that have expired when we read the store...
	require.NoError(t, store.client.HSet(ctx, store.certificatesKey(),
		";expired.example.com", expired,
		";renewed.example.com", expired,
	).Err())

	// ...then, after our read but before our prune, another instance renews one of them.
	require.NoError(t, store.client.HSet(ctx, store.certificatesKey(), ";renewed.example.com", renewed).Err())

	require.NoError(t, store.client.Eval(ctx, pruneCertificatesScript, []string{store.certificatesKey()},
		";expired.example.com", expired,
		";renewed.example.com", expired,
	).Err())

	existsExpired, err := store.client.HExists(ctx, store.certificatesKey(), ";expired.example.com").Result()
	require.NoError(t, err)
	existsRenewed, err := store.client.HExists(ctx, store.certificatesKey(), ";renewed.example.com").Result()
	require.NoError(t, err)

	assert.False(t, existsExpired, "an unchanged expired certificate should be pruned")
	assert.True(t, existsRenewed, "a certificate renewed after our read should not be pruned")
}

func Test_RedisStore_operationsAreBounded(t *testing.T) {
	// A server that accepts connections but never responds to anything.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	connections := make([]net.Conn, 0, 4)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections = append(connections, conn)
		}
	}()

	redisOperationTimeout = 100 * time.Millisecond
	t.Cleanup(func() { redisOperationTimeout = 5 * time.Second })

	client := redis.NewClient(&redis.Options{Addr: listener.Addr().String(), MaxRetries: -1, ContextTimeoutEnabled: true})
	t.Cleanup(func() { _ = client.Close() })

	store := &RedisStore{client: client, keyPrefix: "test", locks: make(map[string]*redisLock)}

	start := time.Now()
	assert.Nil(t, store.GetCertificate("", "example.com", nil), "no certificate should be returned when redis is unresponsive")
	assert.Less(t, time.Since(start), 2*time.Second, "GetCertificate should be bounded by the operation timeout")

	start = time.Now()
	assert.Error(t, store.SaveCertificate(&Details{Subject: "example.com", NotAfter: time.Now().Add(time.Hour).UTC()}))
	assert.Less(t, time.Since(start), 2*time.Second, "SaveCertificate should be bounded by the operation timeout")
}

func Test_RedisStore_Lock_isExclusiveAcrossStores(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	storeA, err := NewRedisStore(client, "test")
	require.NoError(t, err)
	storeB, err := NewRedisStore(client, "test")
	require.NoError(t, err)

	storeA.LockCertificate("example.com", nil)

	acquired := make(chan struct{})
	go func() {
		storeB.LockCertificate("example.com", nil)
		close(acquired)
	}()

	select {
	case <-acquired:
		t.Fatal("store B should not acquire the lock while store A holds it")
	case <-time.After(10 * redisLockRetryInterval):
	}

	storeA.UnlockCertificate("example.com", nil)

	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("store B should acquire the lock after store A releases it")
	}

	storeB.UnlockCertificate("example.com", nil)
}

func Test_RedisStore_Lock_canBeReacquiredAfterUnlock(t *testing.T) {
	store := newTestRedisStore(t, "test")

	store.LockCertificate("example.com", nil)
	store.UnlockCertificate("example.com", nil)

	done := make(chan struct{})
	go func() {
		store.LockCertificate("example.com", nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("lock should be acquirable immediately after being released")
	}

	store.UnlockCertificate("example.com", nil)
}

func Test_RedisStore_Lock_expiresIfHolderDies(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	storeA, err := NewRedisStore(client, "test")
	require.NoError(t, err)
	storeB, err := NewRedisStore(client, "test")
	require.NoError(t, err)

	storeA.LockCertificate("example.com", nil)

	// Simulate the holder dying without releasing: fast-forward past the lock's TTL.
	server.FastForward(2 * redisLockTTL)

	done := make(chan struct{})
	go func() {
		storeB.LockCertificate("example.com", nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("lock should be acquirable after the previous holder's lock expired")
	}

	storeB.UnlockCertificate("example.com", nil)
	storeA.UnlockCertificate("example.com", nil)
}

func Test_RedisStore_Lock_ignoresUnlockWhenNotHeld(t *testing.T) {
	store := newTestRedisStore(t, "test")

	store.UnlockCertificate("example.com", nil)

	store.LockCertificate("example.com", nil)
	store.UnlockCertificate("example.com", nil)
}

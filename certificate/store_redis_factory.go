//go:build !noredis

package certificate

import (
	"crypto/tls"
	"net"

	"github.com/redis/go-redis/v9"
)

// NewRedisStoreFromOptions creates a certificate store backed by Redis, connecting with the given options.
// An error is returned if the Redis server cannot be reached.
func NewRedisStoreFromOptions(options RedisOptions) (Store, error) {
	opts := &redis.Options{
		Addr:     options.Addr,
		Username: options.Username,
		Password: options.Password,
		DB:       options.DB,
		// The store bounds each operation with a context; honour it when timing out reads and writes.
		ContextTimeoutEnabled: true,
	}

	if options.UseTLS {
		opts.TLSConfig = &tls.Config{ServerName: hostFromAddress(options.Addr)}
	}

	return NewRedisStore(redis.NewClient(opts), options.KeyPrefix)
}

// hostFromAddress returns the host part of a host:port address, or the address as-is if it has no port.
func hostFromAddress(address string) string {
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
}

//go:build !noredis

package main

import (
	"crypto/tls"
	"flag"
	"net"

	"github.com/csmith/centauri/certificate"
	"github.com/redis/go-redis/v9"
)

var (
	redisAddress   = flag.String("redis-address", "localhost:6379", "Address of the Redis server, when using the redis certificate store")
	redisUsername  = flag.String("redis-username", "", "Username for the Redis server, when using the redis certificate store")
	redisPassword  = flag.String("redis-password", "", "Password for the Redis server, when using the redis certificate store")
	redisDB        = flag.Int("redis-db", 0, "Redis database number, when using the redis certificate store")
	redisKeyPrefix = flag.String("redis-key-prefix", "centauri", "Prefix for keys, when using the redis certificate store")
	redisUseTLS    = flag.Bool("redis-tls", false, "Use TLS when connecting to the Redis server")
)

// createRedisStore creates a certificate store backed by Redis. Certificates are stored under the configured key
// prefix, allowing multiple Centauri instances to share a single certificate store.
func createRedisStore() (certificate.Store, error) {
	options := &redis.Options{
		Addr:     *redisAddress,
		Username: *redisUsername,
		Password: *redisPassword,
		DB:       *redisDB,
		// The store bounds each operation with a context; honour it when timing out reads and writes.
		ContextTimeoutEnabled: true,
	}

	if *redisUseTLS {
		options.TLSConfig = &tls.Config{ServerName: hostFromAddress(*redisAddress)}
	}

	return certificate.NewRedisStore(redis.NewClient(options), *redisKeyPrefix)
}

// hostFromAddress returns the host part of a host:port address, or the address as-is if it has no port.
func hostFromAddress(address string) string {
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
}

package certificate

// RedisOptions describes how to connect to a Redis server for use as a certificate store.
type RedisOptions struct {
	// Addr is the address of the Redis server.
	Addr string
	// Username and Password authenticate the connection, if required.
	Username string
	Password string
	// DB is the Redis database number to use.
	DB int
	// KeyPrefix is prepended to all keys written by the store, allowing multiple Centauri instances to
	// share a single Redis server.
	KeyPrefix string
	// UseTLS enables TLS for the connection. The server name is taken from the host part of Addr.
	UseTLS bool
}

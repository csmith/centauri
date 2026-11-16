//go:build noredis

package certificate

import "fmt"

// NewRedisStoreFromOptions always errors: the binary has been built without redis support.
func NewRedisStoreFromOptions(options RedisOptions) (Store, error) {
	return nil, fmt.Errorf("redis certificate store is not compiled in (built with the noredis tag)")
}

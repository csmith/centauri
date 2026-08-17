//go:build noredis

package main

import (
	"fmt"

	"github.com/csmith/centauri/certificate"
)

// createRedisStore always errors: the binary has been built without redis support.
func createRedisStore() (certificate.Store, error) {
	return nil, fmt.Errorf("redis certificate store is not compiled in (built with the noredis tag)")
}

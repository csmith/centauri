//go:build !noredis

package certificate

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_NewRedisStoreFromOptions_connectsToTheServer(t *testing.T) {
	server := miniredis.RunT(t)

	store, err := NewRedisStoreFromOptions(RedisOptions{
		Addr:      server.Addr(),
		KeyPrefix: "centauri-test",
	})
	require.NoError(t, err)
	require.NotNil(t, store)
}

func Test_NewRedisStoreFromOptions_errorsIfServerIsUnreachable(t *testing.T) {
	_, err := NewRedisStoreFromOptions(RedisOptions{
		Addr:      "127.0.0.1:1",
		KeyPrefix: "centauri-test",
	})
	assert.ErrorContains(t, err, "unable to reach redis")
}

func Test_hostFromAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
	}{
		{"host and port", "localhost:6379", "localhost"},
		{"ipv4 and port", "127.0.0.1:6379", "127.0.0.1"},
		{"ipv6 and port", "[::1]:6379", "::1"},
		{"no port", "localhost", "localhost"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hostFromAddress(tt.address))
		})
	}
}

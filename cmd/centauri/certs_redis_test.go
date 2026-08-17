//go:build integration && !noredis

package main

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/csmith/centauri/cmd/centauri/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Run_ErrorsIfRedisCertificateStoreUnreachable(t *testing.T) {
	err := runTest(
		make(chan os.Signal, 1),
		"CERTIFICATE_STORE_TYPE", "redis",
		"REDIS_ADDRESS", "127.0.0.1:1",
	)

	assert.ErrorContains(t, err, "certificate store error")
	assert.ErrorContains(t, err, "unable to reach redis")
}

func Test_Run_UsesRedisCertificateStore(t *testing.T) {
	server := miniredis.RunT(t)

	upstream := startStaticServer(8701)
	defer upstream.stop(context.Background())

	signalChan := make(chan os.Signal, 1)
	doneChan := make(chan struct{}, 1)

	go func() {
		err := runTest(
			signalChan,
			"CONFIG", testdata.Path("simple-proxy.conf"),
			"PROVIDER", "selfsigned",
			"CERTIFICATE_STORE_TYPE", "redis",
			"REDIS_ADDRESS", server.Addr(),
			"REDIS_KEY_PREFIX", "centauri-test",
			"FRONTEND", "tcp",
			"HTTP_PORT", "8702",
			"HTTPS_PORT", "8703",
		)
		assert.NoError(t, err)
		doneChan <- struct{}{}
	}()

	start := time.Now()
	for time.Since(start) < 30*time.Second {
		time.Sleep(500 * time.Millisecond)

		res, err := proxyGet(8703, "https://example.com/test")
		if err != nil {
			continue
		}

		assert.Equal(t, http.StatusOK, res.StatusCode)
		require.True(t, server.Exists("centauri-test:certificates"), "certificate should be stored in redis")

		signalChan <- os.Interrupt
		<-doneChan
		return
	}

	assert.Fail(t, "timeout exceeded")
}

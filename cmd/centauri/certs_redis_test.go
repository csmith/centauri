//go:build integration && !noredis

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/csmith/centauri/certificate"
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

func Test_Run_ObtainsCertificatesUsingAcmeWithRedisStore(t *testing.T) {
	server := miniredis.RunT(t)

	upstream := startStaticServer(8701)
	defer upstream.stop(context.Background())

	stopPebble := startPebble("pebble-config.json")
	defer stopPebble()

	signalChan := make(chan os.Signal, 1)
	doneChan := make(chan struct{}, 1)

	userPem, err := os.CreateTemp("", "centauri-integration-test-user-*.pem")
	assert.NoError(t, err)
	userPem.Close()
	os.Remove(userPem.Name())
	defer os.Remove(userPem.Name())

	go func() {
		err := runTest(
			signalChan,
			"CONFIG", testdata.Path("simple-proxy.conf"),
			"PROVIDER", "lego",
			"DNS_PROVIDER", "exec",
			"EXEC_PATH", testdata.Path("update.sh"),
			"ACME_EMAIL", "test@example.com",
			"ACME_DIRECTORY", "https://localhost:14000/dir",
			"ACME_DISABLE_PROPAGATION_CHECK", "true",
			"USER_DATA", userPem.Name(),
			"CERTIFICATE_STORE_TYPE", "redis",
			"REDIS_ADDRESS", server.Addr(),
			"REDIS_KEY_PREFIX", "centauri-test",
			"LEGO_CA_CERTIFICATES", testdata.Path("pebble.minica.pem"),
			"FRONTEND", "tcp",
			"HTTP_PORT", "8702",
			"HTTPS_PORT", "8703",
		)
		assert.NoError(t, err)
		doneChan <- struct{}{}
	}()

	start := time.Now()
	for time.Since(start) < time.Minute {
		time.Sleep(2 * time.Second)

		res, err := proxyGet(8703, "https://example.com/test")
		if err != nil && strings.Contains(err.Error(), "tls: unrecognized name") {
			slog.Warn("Centauri isn't serving a cert yet, waiting...")
			continue
		}

		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, res.StatusCode)

		servedCert := res.TLS.PeerCertificates[0]
		assert.True(t, strings.Contains(servedCert.Issuer.CommonName, "Pebble Intermediate CA"))

		// The certificate should have been saved in redis under a hash keyed by provider, subject and alt names.
		require.True(t, server.Exists("centauri-test:certificates"), "certificate should be stored in redis")
		raw := server.HGet("centauri-test:certificates", "lego;example.com")
		require.NotEmpty(t, raw, "certificate should be stored under the lego;example.com hash field")

		var details certificate.Details
		require.NoError(t, json.Unmarshal([]byte(raw), &details))
		assert.Equal(t, "lego", details.Provider)
		assert.Equal(t, "example.com", details.Subject)
		assert.Empty(t, details.AltNames)
		assert.True(t, details.ValidFor(0), "stored certificate should not be expired")

		// The stored certificate should be the one we were served, and pair up with the stored private key.
		block, _ := pem.Decode([]byte(details.Certificate))
		require.NotNil(t, block, "stored certificate should be valid PEM")
		storedCert, err := x509.ParseCertificate(block.Bytes)
		require.NoError(t, err)
		assert.Equal(t, servedCert.Raw, storedCert.Raw, "served certificate should match the one stored in redis")
		assert.Contains(t, storedCert.Issuer.CommonName, "Pebble Intermediate CA")

		_, err = tls.X509KeyPair([]byte(details.Certificate), []byte(details.PrivateKey))
		assert.NoError(t, err, "stored private key should match the stored certificate")

		signalChan <- os.Interrupt
		<-doneChan
		return
	}

	assert.Fail(t, "timeout exceeded")
}

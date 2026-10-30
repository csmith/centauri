//go:build integration

package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/csmith/centauri/cmd/centauri/testdata"
	"github.com/csmith/centauri/frontend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Run_ErrorsIfFrontendUndefined(t *testing.T) {
	err := runTest(
		make(chan os.Signal, 1),
		"FRONTEND", "not-good",
	)

	assert.ErrorContains(t, err, "unknown frontend: not-good")
}

func Test_Run_ErrorsIfConfigSourceUndefined(t *testing.T) {
	err := runTest(
		make(chan os.Signal, 1),
		"CONFIG_SOURCE", "invalid",
	)

	assert.ErrorContains(t, err, "invalid config source specified")
	assert.ErrorContains(t, err, "unknown config source: invalid")
}

func Test_Run_ErrorsIfCertificateStoreTypeUndefined(t *testing.T) {
	err := runTest(
		make(chan os.Signal, 1),
		"CERTIFICATE_STORE_TYPE", "invalid",
	)

	assert.ErrorContains(t, err, "error creating certificate providers")
	assert.ErrorContains(t, err, "unknown certificate store: invalid")
}

func Test_Run_ErrorsIfNetworkConfigSourceWithoutAddress(t *testing.T) {
	err := runTest(
		make(chan os.Signal, 1),
		"CONFIG_SOURCE", "network",
	)

	assert.ErrorContains(t, err, "failed to start config source")
	assert.ErrorContains(t, err, "address must be specified")
}

func Test_Run_NetworkConfigSource_AppliesUpdates(t *testing.T) {
	upstream1 := startStaticServer(t)
	upstream2 := startStaticServer(t)

	configListener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	defer configListener.Close()

	configUpdateChan := make(chan []byte, 1)
	configServerDone := make(chan struct{})
	go func() {
		defer close(configServerDone)
		conn, err := configListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		initialConfig := []byte("route example.com\n    upstream " + upstream1 + "\n")
		require.NoError(t, sendNetworkConfig(conn, initialConfig))

		newConfig := <-configUpdateChan
		require.NoError(t, sendNetworkConfig(conn, newConfig))

		<-configUpdateChan
	}()

	c := startCentauri(
		t,
		"CONFIG_SOURCE", "network",
		"CONFIG_NETWORK_ADDRESS", configListener.Addr().String(),
		"PROVIDER", "selfsigned",
	)

	time.Sleep(2 * time.Second)

	res, err := proxyGet(c.httpsPort, "https://example.com/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	assert.Contains(t, string(b), "This is the upstream on "+upstream1)

	updatedConfig := []byte("route example.com\n    upstream " + upstream2 + "\n")
	configUpdateChan <- updatedConfig

	time.Sleep(2 * time.Second)

	res, err = proxyGet(c.httpsPort, "https://example.com/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	b, _ = io.ReadAll(res.Body)
	res.Body.Close()
	assert.Contains(t, string(b), "This is the upstream on "+upstream2)

	c.stop(t)
	close(configUpdateChan)
	<-configServerDone
}

func Test_Run_ErrorsIfConfigNotFound(t *testing.T) {
	err := runTest(
		make(chan os.Signal, 1),
		"CONFIG", "/does/not/exist",
	)

	assert.ErrorContains(t, err, "failed to open config file")
}

func Test_Run_ErrorsIfConfigCantBeParsed(t *testing.T) {
	err := runTest(
		make(chan os.Signal, 1),
		"CONFIG", testdata.Path("badly-formatted.conf"),
	)

	assert.ErrorContains(t, err, "failed to parse config file")
}

func Test_Run_ProxiesToUpstream(t *testing.T) {
	upstream := startStaticServer(t)

	c := startCentauri(
		t,
		"CONFIG", writeConfig(t, "simple-proxy.conf", "UPSTREAM", upstream),
		"PROVIDER", "selfsigned",
	)

	time.Sleep(2 * time.Second)

	res, err := proxyGet(c.httpsPort, "https://example.com/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()
	assert.Contains(t, string(b), "This is the upstream on "+upstream)
}

func Test_Run_SendsXForwardedHeadersToUpstream(t *testing.T) {
	upstream := startStaticServer(t)

	c := startCentauri(
		t,
		"CONFIG", writeConfig(t, "simple-proxy.conf", "UPSTREAM", upstream),
		"PROVIDER", "selfsigned",
	)

	time.Sleep(2 * time.Second)

	res, err := proxyGet(c.httpsPort, "https://example.com/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()
	assert.Contains(t, string(b), "X-Forwarded-For: 127.0.0.1\n")
	assert.Contains(t, string(b), "X-Forwarded-Host: example.com\n")
	assert.Contains(t, string(b), "X-Forwarded-Proto: https\n")
}

func Test_Run_TrustedDownstreams_PreservesHeadersFromTrustedSource(t *testing.T) {
	upstream := startStaticServer(t)

	c := startCentauri(
		t,
		"CONFIG", writeConfig(t, "simple-proxy.conf", "UPSTREAM", upstream),
		"PROVIDER", "selfsigned",
		"TRUSTED_DOWNSTREAMS", "127.0.0.0/8",
	)

	time.Sleep(2 * time.Second)

	req, err := http.NewRequest(http.MethodGet, "https://example.com/test", nil)
	assert.NoError(t, err)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	req.Header.Set("X-Forwarded-Host", "original.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	res, err := proxyDo(c.httpsPort, req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()

	assert.Contains(t, string(b), "X-Forwarded-For: 10.0.0.1, 127.0.0.1\n")
	assert.Contains(t, string(b), "X-Forwarded-Host: original.example.com\n")
	assert.Contains(t, string(b), "X-Forwarded-Proto: https\n")
}

func Test_Run_TrustedDownstreams_ReplacesHeadersFromUntrustedSource(t *testing.T) {
	upstream := startStaticServer(t)

	c := startCentauri(
		t,
		"CONFIG", writeConfig(t, "simple-proxy.conf", "UPSTREAM", upstream),
		"PROVIDER", "selfsigned",
		"TRUSTED_DOWNSTREAMS", "192.168.1.0/24",
	)

	time.Sleep(2 * time.Second)

	req, err := http.NewRequest(http.MethodGet, "https://example.com/test", nil)
	assert.NoError(t, err)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	req.Header.Set("X-Forwarded-Host", "malicious.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	res, err := proxyDo(c.httpsPort, req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()

	assert.Contains(t, string(b), "X-Forwarded-For: 127.0.0.1\n")
	assert.Contains(t, string(b), "X-Forwarded-Host: example.com\n")
	assert.Contains(t, string(b), "X-Forwarded-Proto: https\n")

	assert.NotContains(t, string(b), "10.0.0.1")
	assert.NotContains(t, string(b), "malicious.example.com")
}

func Test_Run_IgnoresBadHeadersFromClients(t *testing.T) {
	upstream := startStaticServer(t)

	c := startCentauri(
		t,
		"CONFIG", writeConfig(t, "simple-proxy.conf", "UPSTREAM", upstream),
		"PROVIDER", "selfsigned",
	)

	time.Sleep(2 * time.Second)

	req, err := http.NewRequest(http.MethodGet, "https://example.com/test", nil)
	assert.NoError(t, err)
	req.Header.Set("X-Forwarded-For", "1.3.3.7")
	req.Header.Set("X-Forwarded-Host", "example.net")
	req.Header.Set("X-Forwarded-Proto", "tcp")
	req.Header.Set("X-Real-IP", "1.3.3.7")
	req.Header.Set("True-Client-IP", "1.3.3.7")
	req.Header.Set("Forwarded", "1.3.3.7")
	req.Header.Set("Tailscale-User-Login", "acidburn")
	req.Header.Set("Tailscale-User-Name", "Acid Burn")
	req.Header.Set("Tailscale-User-Profile-Pic", "http://example.net/...")

	res, err := proxyDo(c.httpsPort, req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()
	// None of the values should be passed
	assert.NotContains(t, string(b), "1.3.3.7")
	assert.NotContains(t, string(b), "example.net")
	assert.NotContains(t, string(b), "tcp")
	assert.NotContains(t, string(b), "acidburn")
	assert.NotContains(t, string(b), "Acid Burn")

	// None of the Tailscale headers should exist
	assert.NotContains(t, string(b), "Tailscale")
}

func Test_Run_SendsErrorToClientIfUpstreamUnreachable(t *testing.T) {
	c := startCentauri(
		t,
		"CONFIG", testdata.Path("simple-proxy-bad-port.conf"),
		"PROVIDER", "selfsigned",
	)

	time.Sleep(2 * time.Second)

	res, err := proxyGet(c.httpsPort, "https://example.com/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadGateway, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()
	assert.Contains(t, string(b), "The server was unable to complete your request")
}

func Test_Run_ReturnsBadGatewayForUnknownHosts(t *testing.T) {
	upstream := startStaticServer(t)

	var otherHit atomic.Bool
	other := startServer(
		t,
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				otherHit.Store(true)
				w.WriteHeader(http.StatusOK)
			},
		),
	)

	c := startCentauri(
		t,
		"CONFIG",
		writeConfig(t, "subject.conf", "UPSTREAM", upstream),
		"PROVIDER",
		"selfsigned",
	)

	time.Sleep(2 * time.Second)

	conn, err := tls.Dial(
		"tcp",
		fmt.Sprintf("127.0.0.1:%d", c.httpsPort),
		&tls.Config{
			ServerName:         "example.com",
			InsecureSkipVerify: true,
		},
	)
	require.NoError(t, err)
	defer conn.Close()

	_, err = fmt.Fprintf(conn, "GET http://%s/ HTTP/1.1\r\nHost: example.com\r\nConnection: close\r\n\r\n", other)
	require.NoError(t, err)

	b, err := io.ReadAll(conn)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(b), "HTTP/1.1 502 "), "unexpected response: %s", b)
	assert.False(t, otherHit.Load())
}

func Test_Run_OnError_ServesErrorPageFromUpstream(t *testing.T) {
	// The primary upstream (127.0.0.1:1) is unreachable, so the route's on_error 502 mapping
	// should cause the response to be served from the error upstream instead of the default page.
	errorUpstream := startStaticServer(t)

	c := startCentauri(
		t,
		"CONFIG", writeConfig(t, "on-error.conf", "ERROR_UPSTREAM", errorUpstream),
		"PROVIDER", "selfsigned",
	)

	time.Sleep(2 * time.Second)

	res, err := proxyGet(c.httpsPort, "https://example.com/test")
	assert.NoError(t, err)
	// The status code is always the original error (502); the body comes from the error upstream.
	assert.Equal(t, http.StatusBadGateway, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()
	assert.Contains(t, string(b), "This is the upstream on "+errorUpstream)
	assert.NotContains(t, string(b), "The server was unable to complete your request")
}

func Test_Run_OnError_FollowsRedirectFromErrorUpstream(t *testing.T) {
	// The error upstream responds with a redirect; Centauri should follow it itself and serve the
	// final response, rather than passing the redirect on to the client.
	errorUpstream := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("the error page after a redirect"))
			return
		}
		http.Redirect(w, r, "/final", http.StatusFound)
	}))

	c := startCentauri(
		t,
		"CONFIG", writeConfig(t, "on-error.conf", "ERROR_UPSTREAM", errorUpstream),
		"PROVIDER", "selfsigned",
	)

	time.Sleep(2 * time.Second)

	res, err := proxyGet(c.httpsPort, "https://example.com/test")
	assert.NoError(t, err)
	// The redirect is resolved by Centauri: the client sees the original 502 with the final body,
	// not a redirect status or a Location header. (The test client does not follow redirects.)
	assert.Equal(t, http.StatusBadGateway, res.StatusCode)
	assert.Empty(t, res.Header.Get("Location"))

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()
	assert.Contains(t, string(b), "the error page after a redirect")
}

func Test_Run_OnError_PassesHeadersFromErrorUpstream(t *testing.T) {
	// Headers set by the error upstream (such as Content-Type) should be served to the client.
	errorUpstream := startServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"error":"bad gateway"}`))
	}))

	c := startCentauri(
		t,
		"CONFIG", writeConfig(t, "on-error.conf", "ERROR_UPSTREAM", errorUpstream),
		"PROVIDER", "selfsigned",
	)

	time.Sleep(2 * time.Second)

	res, err := proxyGet(c.httpsPort, "https://example.com/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadGateway, res.StatusCode)
	assert.Equal(t, "application/json", res.Header.Get("Content-Type"))

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()
	assert.JSONEq(t, `{"error":"bad gateway"}`, string(b))
}

func Test_Run_ReloadsConfigOnHUP(t *testing.T) {
	upstream := startStaticServer(t)

	// Start with our bad port
	configPath := filepath.Join(t.TempDir(), "centauri.conf")
	b, err := os.ReadFile(testdata.Path("simple-proxy-bad-port.conf"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, b, os.FileMode(0600)))

	c := startCentauri(
		t,
		"CONFIG", configPath,
		"PROVIDER", "selfsigned",
	)

	time.Sleep(2 * time.Second)

	// Make sure we're using the "bad" config
	res, err := proxyGet(c.httpsPort, "https://example.com/test")
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadGateway, res.StatusCode)

	// Update the config
	b, err = os.ReadFile(writeConfig(t, "simple-proxy.conf", "UPSTREAM", upstream))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, b, os.FileMode(0600)))

	// Send a HUP
	c.signals <- syscall.SIGHUP
	time.Sleep(2 * time.Second)

	// Now the same request should work
	res, err = proxyGet(c.httpsPort, "https://example.com/test")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
}

func Test_Run_ObtainsCertificatesUsingAcme(t *testing.T) {
	upstream := startStaticServer(t)

	p := startPebble(t, "pebble-config.json")

	userPem, err := os.CreateTemp("", "centauri-integration-test-user-*.pem")
	assert.NoError(t, err)
	userPem.Close()
	os.Remove(userPem.Name())
	defer os.Remove(userPem.Name())

	certsJson, err := os.CreateTemp("", "centauri-integration-test-certs-*.json")
	assert.NoError(t, err)
	certsJson.Close()
	os.Remove(certsJson.Name())
	defer os.Remove(certsJson.Name())

	c := startCentauri(
		t,
		"CONFIG", writeConfig(t, "simple-proxy.conf", "UPSTREAM", upstream),
		"PROVIDER", "lego",
		"DNS_PROVIDER", "exec",
		"EXEC_PATH", testdata.Path("update.sh"),
		"ACME_EMAIL", "test@example.com",
		"ACME_DIRECTORY", p.directory,
		"CHALLTESTSRV_MANAGEMENT_ADDRESS", p.challengeManagementAddress,
		"ACME_DISABLE_PROPAGATION_CHECK", "true",
		"USER_DATA", userPem.Name(),
		"CERTIFICATE_STORE", certsJson.Name(),
		"LEGO_CA_CERTIFICATES", testdata.Path("pebble.minica.pem"),
	)

	start := time.Now()
	for time.Since(start) < time.Minute {
		time.Sleep(2 * time.Second)

		res, err := proxyGet(c.httpsPort, "https://example.com/test")
		if err != nil && strings.Contains(err.Error(), "tls: unrecognized name") {
			slog.Warn("Centauri isn't serving a cert yet, waiting...")
			continue
		}

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.True(t, strings.Contains(res.TLS.PeerCertificates[0].Issuer.CommonName, "Pebble Intermediate CA"))

		return
	}

	assert.Fail(t, "timeout exceeded")
}

func Test_Run_ObtainsCertificatesUsingAcmeWithExternalAccountBinding(t *testing.T) {
	upstream := startStaticServer(t)

	p := startPebble(t, "pebble-config-eab.json")

	userPem, err := os.CreateTemp("", "centauri-integration-test-user-*.pem")
	assert.NoError(t, err)
	userPem.Close()
	os.Remove(userPem.Name())
	defer os.Remove(userPem.Name())

	certsJson, err := os.CreateTemp("", "centauri-integration-test-certs-*.json")
	assert.NoError(t, err)
	certsJson.Close()
	os.Remove(certsJson.Name())
	defer os.Remove(certsJson.Name())

	c := startCentauri(
		t,
		"CONFIG", writeConfig(t, "simple-proxy.conf", "UPSTREAM", upstream),
		"PROVIDER", "lego",
		"DNS_PROVIDER", "exec",
		"EXEC_PATH", testdata.Path("update.sh"),
		"ACME_EMAIL", "test@example.com",
		"ACME_DIRECTORY", p.directory,
		"CHALLTESTSRV_MANAGEMENT_ADDRESS", p.challengeManagementAddress,
		"ACME_DISABLE_PROPAGATION_CHECK", "true",
		"ACME_EXTERNAL_KID", "kid-1",
		"ACME_EXTERNAL_HMAC", "w7rzMImDLQBdYxpKZLT9x9g2QU3_yrBH6ZxzzpZ4Jto",
		"USER_DATA", userPem.Name(),
		"CERTIFICATE_STORE", certsJson.Name(),
		"LEGO_CA_CERTIFICATES", testdata.Path("pebble.minica.pem"),
	)

	start := time.Now()
	for time.Since(start) < time.Minute {
		time.Sleep(2 * time.Second)

		res, err := proxyGet(c.httpsPort, "https://example.com/test")
		if err != nil && strings.Contains(err.Error(), "tls: unrecognized name") {
			slog.Warn("Centauri isn't serving a cert yet, waiting...")
			continue
		}

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.True(t, strings.Contains(res.TLS.PeerCertificates[0].Issuer.CommonName, "Pebble Intermediate CA"))

		return
	}

	assert.Fail(t, "timeout exceeded")
}

func Test_Run_ObtainsCertificatesUsingAcmeAfterInitialConnectionFails(t *testing.T) {
	upstream := startStaticServer(t)

	p := startPebble(t, "pebble-config.json")

	// Fail the first request for the directory, then pass subsequent ones through to pebble.
	var directoryRequested atomic.Bool
	pebbleClient := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	directory := httptest.NewTLSServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				if !directoryRequested.Swap(true) {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}

				res, err := pebbleClient.Get(p.directory)
				if err != nil {
					w.WriteHeader(http.StatusBadGateway)
					return
				}
				defer res.Body.Close()

				w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
				w.WriteHeader(res.StatusCode)
				_, _ = io.Copy(w, res.Body)
			},
		),
	)
	defer directory.Close()

	directoryCa := filepath.Join(t.TempDir(), "directory.pem")
	require.NoError(
		t,
		os.WriteFile(
			directoryCa,
			pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: directory.Certificate().Raw}),
			0o600,
		),
	)

	c := startCentauri(
		t,
		"CONFIG",
		writeConfig(t, "simple-proxy.conf", "UPSTREAM", upstream),
		"PROVIDER",
		"lego",
		"DNS_PROVIDER",
		"exec",
		"EXEC_PATH",
		testdata.Path("update.sh"),
		"ACME_EMAIL",
		"test@example.com",
		"ACME_DIRECTORY",
		directory.URL,
		"CHALLTESTSRV_MANAGEMENT_ADDRESS",
		p.challengeManagementAddress,
		"ACME_DISABLE_PROPAGATION_CHECK",
		"true",
		"USER_DATA",
		filepath.Join(t.TempDir(), "user.pem"),
		"CERTIFICATE_STORE",
		filepath.Join(t.TempDir(), "certs.json"),
		"LEGO_CA_CERTIFICATES",
		testdata.Path("pebble.minica.pem")+string(os.PathListSeparator)+directoryCa,
	)

	start := time.Now()
	for time.Since(start) < time.Minute {
		time.Sleep(2 * time.Second)

		res, err := proxyGet(c.httpsPort, "https://example.com/test")
		if err != nil && strings.Contains(err.Error(), "tls: unrecognized name") {
			slog.Warn("Centauri isn't serving a cert yet, waiting...")
			continue
		}

		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.True(t, strings.Contains(res.TLS.PeerCertificates[0].Issuer.CommonName, "Pebble Intermediate CA"))
		assert.True(t, directoryRequested.Load())

		return
	}

	assert.Fail(t, "timeout exceeded")
}

func Test_Run_ErrorsIfDnsProviderCannotBeCreated(t *testing.T) {
	err := runTest(
		make(chan os.Signal, 1),
		"CONFIG",
		writeConfig(t, "simple-proxy.conf", "UPSTREAM", "localhost:1"),
		"DNS_PROVIDER",
		"not-a-real-provider",
		"CERTIFICATE_STORE",
		filepath.Join(t.TempDir(), "certs.json"),
	)
	assert.ErrorContains(t, err, "not-a-real-provider")
}

func Test_Run_ValidateFlag_ExitsSuccessfullyWithValidConfig(t *testing.T) {
	err := runTest(
		make(chan os.Signal, 1),
		"VALIDATE", "true",
		"CONFIG", writeConfig(t, "simple-proxy.conf", "UPSTREAM", "127.0.0.1:8080"),
	)

	assert.NoError(t, err)
}

func Test_Run_ValidateFlag_ExitsWithErrorForMissingConfig(t *testing.T) {
	err := runTest(
		make(chan os.Signal, 1),
		"VALIDATE", "true",
		"CONFIG", "/does/not/exist.conf",
	)

	assert.ErrorContains(t, err, "failed to open config file")
}

func Test_Run_ValidateFlag_ExitsWithErrorForInvalidConfig(t *testing.T) {
	err := runTest(
		make(chan os.Signal, 1),
		"VALIDATE", "true",
		"CONFIG", testdata.Path("badly-formatted.conf"),
	)

	assert.ErrorContains(t, err, "failed to parse config file")
}

func Test_Run_ValidateFlag_DoesNotStartServer(t *testing.T) {
	signalChan := make(chan os.Signal, 1)
	doneChan := make(chan struct{}, 1)
	configPath := writeConfig(t, "simple-proxy.conf", "UPSTREAM", "127.0.0.1:8080")

	go func() {
		err := runTest(
			signalChan,
			"VALIDATE", "true",
			"CONFIG", configPath,
			"FRONTEND", "tcp",
		)

		assert.NoError(t, err)
		doneChan <- struct{}{}
	}()

	select {
	case <-doneChan:
	case <-time.After(5 * time.Second):
		t.Fatal("Validation timed out - server may have started when it shouldn't have")
	}
}

func Test_Run_ValidateFlag_WorksWithDifferentConfigPaths(t *testing.T) {
	err := runTest(
		make(chan os.Signal, 1),
		"VALIDATE", "true",
		"CONFIG", writeConfig(t, "simple-proxy.conf", "UPSTREAM", "127.0.0.1:8080"),
	)
	assert.NoError(t, err)

	tempFile, err := os.CreateTemp("", "centauri-validate-test-*.conf")
	assert.NoError(t, err)
	defer os.Remove(tempFile.Name())

	_, err = tempFile.WriteString("route test.example.com\n    upstream 127.0.0.1:8080\n")
	assert.NoError(t, err)
	tempFile.Close()

	err = runTest(
		make(chan os.Signal, 1),
		"VALIDATE", "true",
		"CONFIG", tempFile.Name(),
	)
	assert.NoError(t, err)
}

func Test_Run_RedirectsToPrimaryDomain(t *testing.T) {
	upstream := startStaticServer(t)

	c := startCentauri(
		t,
		"CONFIG", writeConfig(t, "domain-redirect.conf", "UPSTREAM", upstream),
		"PROVIDER", "selfsigned",
	)

	time.Sleep(2 * time.Second)

	// Test HTTPS redirect from www.example.com to example.com
	res, err := proxyGet(c.httpsPort, "https://www.example.com/test?param=value")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusPermanentRedirect, res.StatusCode)
	assert.Equal(t, "https://example.com/test?param=value", res.Header.Get("Location"))

	// Test that requests to primary domain (example.com) are not redirected
	res, err = proxyGet(c.httpsPort, "https://example.com/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()
	assert.Contains(t, string(b), "This is the upstream on "+upstream)
}

func Test_Run_UsesSubjectForCertificateNames(t *testing.T) {
	upstream := startStaticServer(t)

	c := startCentauri(
		t,
		"CONFIG", writeConfig(t, "subject.conf", "UPSTREAM", upstream),
		"PROVIDER", "selfsigned",
	)

	time.Sleep(2 * time.Second)

	res, err := proxyGet(c.httpsPort, "https://example.com/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	cert := res.TLS.PeerCertificates[0]
	assert.Equal(t, "example.com", cert.Subject.CommonName)
	assert.Equal(t, []string{"example.com", "*.example.com"}, cert.DNSNames)
	assert.NotContains(t, cert.DNSNames, "www.example.com")

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()
	assert.Contains(t, string(b), "This is the upstream on "+upstream)
}

func Test_Run_RedirectsHttpToHttps(t *testing.T) {
	upstream := startStaticServer(t)

	c := startCentauri(
		t,
		"CONFIG", writeConfig(t, "simple-proxy.conf", "UPSTREAM", upstream),
		"PROVIDER", "selfsigned",
	)

	time.Sleep(2 * time.Second)

	// Test HTTP to HTTPS redirect
	res, err := getClientProxy(c.httpPort).Get("http://example.com/test?param=value")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusPermanentRedirect, res.StatusCode)
	assert.Equal(t, "https://example.com/test?param=value", res.Header.Get("Location"))

	// Test HTTP to HTTPS redirect strips port
	res, err = getClientProxy(c.httpPort).Get("http://example.com:80/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusPermanentRedirect, res.StatusCode)
	assert.Equal(t, "https://example.com/test", res.Header.Get("Location"))

	// Test that invalid host header returns 400
	req, err := http.NewRequest(http.MethodGet, "http://example.com/test", nil)
	assert.NoError(t, err)
	req.Host = "invalid..domain"

	res, err = getClientProxy(c.httpPort).Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
}

func runTest(signalChan <-chan os.Signal, cfg ...string) error {
	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		if !strings.HasPrefix(f.Name, "test") {
			if err := f.Value.Set(f.DefValue); err != nil {
				panic(fmt.Sprintf("Failed to reset flag %s to its default %s: %v", f.Name, f.DefValue, err))
			}
		}
	})

	// Avoid the default ports, which may already be in use. Tests can override these.
	ports := freePorts(2)
	cfg = append([]string{"HTTP_PORT", strconv.Itoa(ports[0]), "HTTPS_PORT", strconv.Itoa(ports[1])}, cfg...)

	for i := 0; i < len(cfg); i += 2 {
		os.Setenv(cfg[i], cfg[i+1])
		defer os.Unsetenv(cfg[i])
	}

	return run([]string{}, signalChan)
}

type centauri struct {
	httpPort  int
	httpsPort int
	signals   chan os.Signal
	stopOnce  sync.Once
	result    chan error
}

// startCentauri runs Centauri with the TCP frontend listening on free ports. It is stopped when the test
// finishes, regardless of whether the test passed, so it never holds on to ports used by later tests.
func startCentauri(t *testing.T, cfg ...string) *centauri {
	ports := freePorts(2)
	c := &centauri{
		httpPort:  ports[0],
		httpsPort: ports[1],
		signals:   make(chan os.Signal, 1),
		result:    make(chan error, 1),
	}

	cfg = append(cfg,
		"FRONTEND", "tcp",
		"HTTP_PORT", strconv.Itoa(c.httpPort),
		"HTTPS_PORT", strconv.Itoa(c.httpsPort),
	)
	go func() {
		c.result <- runTest(c.signals, cfg...)
	}()

	t.Cleanup(func() { c.stop(t) })
	return c
}

func (c *centauri) stop(t *testing.T) {
	c.stopOnce.Do(func() {
		select {
		case c.signals <- os.Interrupt:
		case err := <-c.result:
			// Centauri already exited of its own accord.
			assert.NoError(t, err)
			return
		}

		select {
		case err := <-c.result:
			assert.NoError(t, err)
		case <-time.After(30 * time.Second):
			t.Error("timed out waiting for centauri to stop")
		}
	})
}

// freePorts finds n distinct TCP ports that are currently free.
func freePorts(n int) []int {
	var ports []int
	for range n {
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			panic(fmt.Sprintf("Failed to find a free port: %v", err))
		}
		defer listener.Close()
		ports = append(ports, listener.Addr().(*net.TCPAddr).Port)
	}
	return ports
}

// writeConfig expands ${VARIABLES} in the named testdata file using the given key/value pairs, and
// writes the result to a temporary file.
func writeConfig(t *testing.T, name string, vars ...string) string {
	b, err := os.ReadFile(testdata.Path(name))
	require.NoError(t, err)

	values := make(map[string]string)
	for i := 0; i < len(vars); i += 2 {
		values[vars[i]] = vars[i+1]
	}

	content := os.Expand(string(b), func(key string) string {
		value, ok := values[key]
		if !ok {
			t.Fatalf("no value provided for %s in %s", key, name)
		}
		return value
	})

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), os.FileMode(0600)))
	return path
}

// startStaticServer starts an upstream that echoes its address and the request headers, returning its address.
func startStaticServer(t *testing.T) string {
	listener := listen(t)
	addr := listener.Addr().String()
	serve(t, listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("This is the upstream on %s\n\nYou provided headers:\n", addr)))
		for k := range r.Header {
			w.Write([]byte(fmt.Sprintf("%s: %s\n", k, r.Header.Get(k))))
		}
	}))
	return addr
}

// startServer starts an upstream using the given handler, returning its address.
func startServer(t *testing.T, handler http.Handler) string {
	listener := listen(t)
	serve(t, listener, handler)
	return listener.Addr().String()
}

func listen(t *testing.T) net.Listener {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	return listener
}

func serve(t *testing.T, listener net.Listener, handler http.Handler) {
	errChan := make(chan error, 1)

	go func() {
		panic(<-errChan)
	}()
	srv := frontend.NewServer(handler, errChan)
	go srv.Start(listener)
	t.Cleanup(func() {
		srv.Stop(context.Background())
		_ = listener.Close()
	})
}

func sendNetworkConfig(conn net.Conn, config []byte) error {
	// Send magic bytes
	if _, err := conn.Write([]byte("CENTAURI")); err != nil {
		return err
	}

	// Send version (4 bytes: 0x00 0x00 0x00 0x01)
	version := []byte{0x00, 0x00, 0x00, 0x01}
	if _, err := conn.Write(version); err != nil {
		return err
	}

	// Send payload length (big-endian)
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(config)))
	if _, err := conn.Write(length); err != nil {
		return err
	}

	// Send payload
	if _, err := conn.Write(config); err != nil {
		return err
	}

	return nil
}

func proxyGet(realPort int, fakeUrl string) (*http.Response, error) {
	return getClientProxy(realPort).Get(fakeUrl)
}

func proxyDo(realPort int, req *http.Request) (*http.Response, error) {
	return getClientProxy(realPort).Do(req)
}

func getClientProxy(realPort int) *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				addr = fmt.Sprintf("127.0.0.1:%d", realPort)
				return dialer.Dial(network, addr)
			},
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

type pebble struct {
	directory                  string
	challengeManagementAddress string
}

// startPebble starts pebble and its challenge test server on free ports, stopping them when the test finishes.
func startPebble(t *testing.T, configName string) *pebble {
	ports := freePorts(4)
	dnsAddress := fmt.Sprintf("localhost:%d", ports[0])
	p := &pebble{
		directory:                  fmt.Sprintf("https://localhost:%d/dir", ports[1]),
		challengeManagementAddress: fmt.Sprintf("localhost:%d", ports[2]),
	}

	startPebbleTool(
		t,
		"pebble-challtestsrv",
		nil,
		"-http01", "",
		"-https01", "",
		"-tlsalpn01", "",
		"-doh", "",
		"-dnsserver", dnsAddress,
		"-management", p.challengeManagementAddress,
	)

	startPebbleTool(
		t,
		"pebble",
		[]string{"PEBBLE_VA_NOSLEEP=1", "PEBBLE_WFE_NONCEREJECT=0", "PEBBLE_AUTHZREUSE=0"},
		"-strict",
		"-config", writeConfig(
			t,
			configName,
			"PEBBLE_ADDRESS", fmt.Sprintf("localhost:%d", ports[1]),
			"PEBBLE_MANAGEMENT_ADDRESS", fmt.Sprintf("localhost:%d", ports[3]),
		),
		"-dnsserver", dnsAddress,
	)

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	require.Eventually(t, func() bool {
		resp, err := client.Get(p.directory)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, time.Minute, 500*time.Millisecond, "pebble did not start")

	return p
}

// startPebbleTool runs one of pebble's commands, and waits for it to exit when the test finishes.
func startPebbleTool(t *testing.T, name string, env []string, args ...string) {
	cmd := exec.Command("go", append([]string{"tool", "github.com/letsencrypt/pebble/v2/cmd/" + name}, args...)...)
	cmd.Dir = testdata.Path(".")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), env...)
	require.NoError(t, cmd.Start())

	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
	})
}

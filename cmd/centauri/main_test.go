//go:build integration

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/csmith/centauri/cmd/centauri/testdata"
	"github.com/stretchr/testify/assert"
)

func Test_Run_ErrorsIfFrontendUndefined(t *testing.T) {
	err := runTest(
		make(chan os.Signal, 1),
		"FRONTEND", "not-good",
	)

	assert.ErrorContains(t, err, "unknown frontend: not-good")
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
	upstream := startStaticServer(8701)
	defer upstream.stop(context.Background())

	signalChan := make(chan os.Signal, 1)
	doneChan := make(chan struct{}, 1)

	go func() {
		err := runTest(
			signalChan,
			"CONFIG", testdata.Path("simple-proxy.conf"),
			"PROVIDER", "selfsigned",
			"FRONTEND", "tcp",
			"HTTP_PORT", "8702",
			"HTTPS_PORT", "8703",
		)
		assert.NoError(t, err)
		doneChan <- struct{}{}
	}()

	time.Sleep(2 * time.Second)

	res, err := proxyGet(8703, "https://example.com/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()
	assert.Contains(t, string(b), "This is the upstream on port 8701")

	signalChan <- os.Interrupt
	<-doneChan
}

func Test_Run_SendsXForwardedHeadersToUpstream(t *testing.T) {
	upstream := startStaticServer(8701)
	defer upstream.stop(context.Background())

	signalChan := make(chan os.Signal, 1)
	doneChan := make(chan struct{}, 1)

	go func() {
		err := runTest(
			signalChan,
			"CONFIG", testdata.Path("simple-proxy.conf"),
			"PROVIDER", "selfsigned",
			"FRONTEND", "tcp",
			"HTTP_PORT", "8702",
			"HTTPS_PORT", "8703",
		)
		assert.NoError(t, err)
		doneChan <- struct{}{}
	}()

	time.Sleep(2 * time.Second)

	res, err := proxyGet(8703, "https://example.com/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()
	assert.Contains(t, string(b), "X-Forwarded-For: 127.0.0.1\n")
	assert.Contains(t, string(b), "X-Forwarded-Host: example.com\n")
	assert.Contains(t, string(b), "X-Forwarded-Proto: https\n")

	signalChan <- os.Interrupt
	<-doneChan
}

func Test_Run_TrustedDownstreams_PreservesHeadersFromTrustedSource(t *testing.T) {
	upstream := startStaticServer(8701)
	defer upstream.stop(context.Background())

	signalChan := make(chan os.Signal, 1)
	doneChan := make(chan struct{}, 1)

	go func() {
		err := runTest(
			signalChan,
			"CONFIG", testdata.Path("simple-proxy.conf"),
			"PROVIDER", "selfsigned",
			"FRONTEND", "tcp",
			"HTTP_PORT", "8702",
			"HTTPS_PORT", "8703",
			"TRUSTED_DOWNSTREAMS", "127.0.0.0/8",
		)
		assert.NoError(t, err)
		doneChan <- struct{}{}
	}()

	time.Sleep(2 * time.Second)

	req, err := http.NewRequest(http.MethodGet, "https://example.com/test", nil)
	assert.NoError(t, err)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	req.Header.Set("X-Forwarded-Host", "original.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	res, err := proxyDo(8703, req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()

	assert.Contains(t, string(b), "X-Forwarded-For: 10.0.0.1, 127.0.0.1\n")
	assert.Contains(t, string(b), "X-Forwarded-Host: original.example.com\n")
	assert.Contains(t, string(b), "X-Forwarded-Proto: https\n")

	signalChan <- os.Interrupt
	<-doneChan
}

func Test_Run_TrustedDownstreams_ReplacesHeadersFromUntrustedSource(t *testing.T) {
	upstream := startStaticServer(8701)
	defer upstream.stop(context.Background())

	signalChan := make(chan os.Signal, 1)
	doneChan := make(chan struct{}, 1)

	go func() {
		err := runTest(
			signalChan,
			"CONFIG", testdata.Path("simple-proxy.conf"),
			"PROVIDER", "selfsigned",
			"FRONTEND", "tcp",
			"HTTP_PORT", "8702",
			"HTTPS_PORT", "8703",
			"TRUSTED_DOWNSTREAMS", "192.168.1.0/24",
		)
		assert.NoError(t, err)
		doneChan <- struct{}{}
	}()

	time.Sleep(2 * time.Second)

	req, err := http.NewRequest(http.MethodGet, "https://example.com/test", nil)
	assert.NoError(t, err)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	req.Header.Set("X-Forwarded-Host", "malicious.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")

	res, err := proxyDo(8703, req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()

	assert.Contains(t, string(b), "X-Forwarded-For: 127.0.0.1\n")
	assert.Contains(t, string(b), "X-Forwarded-Host: example.com\n")
	assert.Contains(t, string(b), "X-Forwarded-Proto: https\n")

	assert.NotContains(t, string(b), "10.0.0.1")
	assert.NotContains(t, string(b), "malicious.example.com")

	signalChan <- os.Interrupt
	<-doneChan
}

func Test_Run_IgnoresBadHeadersFromClients(t *testing.T) {
	upstream := startStaticServer(8701)
	defer upstream.stop(context.Background())

	signalChan := make(chan os.Signal, 1)
	doneChan := make(chan struct{}, 1)

	go func() {
		err := runTest(
			signalChan,
			"CONFIG", testdata.Path("simple-proxy.conf"),
			"PROVIDER", "selfsigned",
			"FRONTEND", "tcp",
			"HTTP_PORT", "8702",
			"HTTPS_PORT", "8703",
		)
		assert.NoError(t, err)
		doneChan <- struct{}{}
	}()

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

	res, err := proxyDo(8703, req)
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

	signalChan <- os.Interrupt
	<-doneChan
}

func Test_Run_SendsErrorToClientIfUpstreamUnreachable(t *testing.T) {
	signalChan := make(chan os.Signal, 1)
	doneChan := make(chan struct{}, 1)

	go func() {
		err := runTest(
			signalChan,
			"CONFIG", testdata.Path("simple-proxy-bad-port.conf"),
			"PROVIDER", "selfsigned",
			"FRONTEND", "tcp",
			"HTTP_PORT", "8702",
			"HTTPS_PORT", "8703",
		)
		assert.NoError(t, err)
		doneChan <- struct{}{}
	}()

	time.Sleep(2 * time.Second)

	res, err := proxyGet(8703, "https://example.com/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadGateway, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()
	assert.Contains(t, string(b), "The server was unable to complete your request")

	signalChan <- os.Interrupt
	<-doneChan
}

func Test_Run_ReloadsConfigOnHUP(t *testing.T) {
	upstream := startStaticServer(8701)
	defer upstream.stop(context.Background())

	signalChan := make(chan os.Signal, 1)
	doneChan := make(chan struct{}, 1)

	f, err := os.CreateTemp("", "centauri-integration-test-*.config")
	assert.NoError(t, err)
	f.Close()
	defer os.Remove(f.Name())

	// Start with our bad port
	b, err := os.ReadFile(testdata.Path("simple-proxy-bad-port.conf"))
	assert.NoError(t, err)
	err = os.WriteFile(f.Name(), b, os.FileMode(0600))
	assert.NoError(t, err)

	go func() {
		err := runTest(
			signalChan,
			"CONFIG", f.Name(),
			"PROVIDER", "selfsigned",
			"FRONTEND", "tcp",
			"HTTP_PORT", "8702",
			"HTTPS_PORT", "8703",
		)
		assert.NoError(t, err)
		doneChan <- struct{}{}
	}()

	time.Sleep(2 * time.Second)

	// Make sure we're using the "bad" config
	res, err := proxyGet(8703, "https://example.com/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadGateway, res.StatusCode)

	// Update the config
	b, err = os.ReadFile(testdata.Path("simple-proxy.conf"))
	assert.NoError(t, err)
	err = os.WriteFile(f.Name(), b, os.FileMode(0600))
	assert.NoError(t, err)

	// Send a HUP
	signalChan <- syscall.SIGHUP
	time.Sleep(2 * time.Second)

	// Now the same request should work
	res, err = proxyGet(8703, "https://example.com/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	signalChan <- os.Interrupt
	<-doneChan
}

func Test_Run_ObtainsCertificatesUsingAcme(t *testing.T) {
	upstream := startStaticServer(8701)
	defer upstream.stop(context.Background())

	stopPebble := startPebble()
	defer stopPebble()

	signalChan := make(chan os.Signal, 1)
	doneChan := make(chan struct{}, 1)

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
			"CERTIFICATE_STORE", certsJson.Name(),
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
		assert.True(t, strings.Contains(res.TLS.PeerCertificates[0].Issuer.CommonName, "Pebble Intermediate CA"))

		signalChan <- os.Interrupt
		<-doneChan
		return
	}

	assert.Fail(t, "timeout exceeded")
}

func Test_Run_ValidateFlag_ExitsSuccessfullyWithValidConfig(t *testing.T) {
	err := runTest(
		make(chan os.Signal, 1),
		"VALIDATE", "true",
		"CONFIG", testdata.Path("simple-proxy.conf"),
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

	go func() {
		err := runTest(
			signalChan,
			"VALIDATE", "true",
			"CONFIG", testdata.Path("simple-proxy.conf"),
			"FRONTEND", "tcp",
			"HTTP_PORT", "8702",
			"HTTPS_PORT", "8703",
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
		"CONFIG", testdata.Path("simple-proxy.conf"),
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
	upstream := startStaticServer(8701)
	defer upstream.stop(context.Background())

	signalChan := make(chan os.Signal, 1)
	doneChan := make(chan struct{}, 1)

	go func() {
		err := runTest(
			signalChan,
			"CONFIG", testdata.Path("domain-redirect.conf"),
			"PROVIDER", "selfsigned",
			"FRONTEND", "tcp",
			"HTTP_PORT", "8702",
			"HTTPS_PORT", "8703",
		)
		assert.NoError(t, err)
		doneChan <- struct{}{}
	}()

	time.Sleep(2 * time.Second)

	// Test HTTPS redirect from www.example.com to example.com
	res, err := proxyGet(8703, "https://www.example.com/test?param=value")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusPermanentRedirect, res.StatusCode)
	assert.Equal(t, "https://example.com/test?param=value", res.Header.Get("Location"))

	// Test that requests to primary domain (example.com) are not redirected
	res, err = proxyGet(8703, "https://example.com/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	b, _ := io.ReadAll(res.Body)
	defer res.Body.Close()
	assert.Contains(t, string(b), "This is the upstream on port 8701")

	signalChan <- os.Interrupt
	<-doneChan
}

func Test_Run_RedirectsHttpToHttps(t *testing.T) {
	upstream := startStaticServer(8701)
	defer upstream.stop(context.Background())

	signalChan := make(chan os.Signal, 1)
	doneChan := make(chan struct{}, 1)

	go func() {
		err := runTest(
			signalChan,
			"CONFIG", testdata.Path("simple-proxy.conf"),
			"PROVIDER", "selfsigned",
			"FRONTEND", "tcp",
			"HTTP_PORT", "8702",
			"HTTPS_PORT", "8703",
		)
		assert.NoError(t, err)
		doneChan <- struct{}{}
	}()

	time.Sleep(2 * time.Second)

	// Test HTTP to HTTPS redirect
	res, err := getClientProxy(8702).Get("http://example.com/test?param=value")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusPermanentRedirect, res.StatusCode)
	assert.Equal(t, "https://example.com/test?param=value", res.Header.Get("Location"))

	// Test HTTP to HTTPS redirect strips port
	res, err = getClientProxy(8702).Get("http://example.com:80/test")
	assert.NoError(t, err)
	assert.Equal(t, http.StatusPermanentRedirect, res.StatusCode)
	assert.Equal(t, "https://example.com/test", res.Header.Get("Location"))

	// Test that invalid host header returns 400
	req, err := http.NewRequest(http.MethodGet, "http://example.com/test", nil)
	assert.NoError(t, err)
	req.Host = "invalid..domain"

	res, err = getClientProxy(8702).Do(req)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	signalChan <- os.Interrupt
	<-doneChan
}

func runTest(signalChan <-chan os.Signal, cfg ...string) error {
	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		if !strings.HasPrefix(f.Name, "test") {
			if err := f.Value.Set(f.DefValue); err != nil {
				panic(fmt.Sprintf("Failed to reset flag %s to its default %s: %v", f.Name, f.DefValue, err))
			}
		}
	})

	for i := 0; i < len(cfg); i += 2 {
		os.Setenv(cfg[i], cfg[i+1])
		defer os.Unsetenv(cfg[i])
	}

	return run([]string{}, signalChan)
}

func startStaticServer(port int) *server {
	errChan := make(chan error, 1)

	go func() {
		panic(<-errChan)
	}()
	srv := newServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf("This is the upstream on port %d\n\nYou provided headers:\n", port)))
		for k := range r.Header {
			w.Write([]byte(fmt.Sprintf("%s: %s\n", k, r.Header.Get(k))))
		}
	}), errChan)
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		panic(err)
	}
	go srv.start(listener)
	return srv
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

func startPebble() func() {
	stopChallTestSrv := startChallTestSrv()

	cmd := exec.Command("go", "tool", "github.com/letsencrypt/pebble/v2/cmd/pebble", "-strict", "-config", "pebble-config.json", "-dnsserver", "localhost:8053")
	cmd.Dir = testdata.Path(".")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "PEBBLE_VA_NOSLEEP=1", "PEBBLE_WFE_NONCEREJECT=0", "PEBBLE_AUTHZREUSE=0")

	go func() {
		if err := cmd.Run(); err != nil {
			panic(err)
		}
	}()

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	start := time.Now()
	for time.Since(start) < time.Minute {
		resp, err := client.Get("https://localhost:14000/dir")
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		resp.Body.Close()
		break
	}

	return func() {
		if cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
		}
		stopChallTestSrv()
	}
}

func startChallTestSrv() func() {
	cmd := exec.Command("go", "tool", "github.com/letsencrypt/pebble/v2/cmd/pebble-challtestsrv", "-http01", "\"\"", "-https01", "\"\"", "-tlsalpn01", "\"\"")
	cmd.Dir = testdata.Path(".")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	go func() {
		if err := cmd.Run(); err != nil {
			panic(err)
		}
	}()

	return func() {
		if cmd.Process != nil {
			cmd.Process.Signal(syscall.SIGTERM)
		}
	}
}

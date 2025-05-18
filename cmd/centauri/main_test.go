//go:build integration

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/csmith/centauri/cmd/centauri/testdata"
	"github.com/stretchr/testify/assert"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
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

	res, err := getFromProxy(8703, "https://example.com/test")
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

	res, err := getFromProxy(8703, "https://example.com/test")
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

func getFromProxy(realPort int, fakeUrl string) (*http.Response, error) {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	client := http.Client{
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
	}

	return client.Get(fakeUrl)
}

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"sync"
	"time"

	"github.com/csmith/centauri/proxy"
)

const (
	shutdownTimeout = time.Second * 5
)

type frontend interface {
	Serve(manager *proxy.Manager, rewriter *proxy.Rewriter) error
	Stop(ctx context.Context)
	UsesCertificates() bool
}

var frontends = make(map[string]frontend)

// createProxy creates a new http.Server configured with a reverse proxy backed by the given rewriter.
func createProxy(rewriter *proxy.Rewriter) *http.Server {
	return &http.Server{
		Handler: &httputil.ReverseProxy{
			Director:       rewriter.RewriteRequest,
			ModifyResponse: rewriter.RewriteResponse,
			BufferPool:     newBufferPool(),
			Transport: &http.Transport{
				ForceAttemptHTTP2:   false,
				DisableCompression:  true,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// createRedirector creates a new http.Server configured to redirect all requests to HTTPS.
func createRedirector() *http.Server {
	return &http.Server{Handler: &proxy.Redirector{}}
}

// createTLSConfig creates a new tls.Config following the Mozilla intermediate configuration, and using
// the given manager for obtaining certificates.
func createTLSConfig(manager *proxy.Manager) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		// Generated 2022-02-20, Mozilla Guideline v5.6, Go 1.14.4, intermediate configuration
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
		GetCertificate: manager.CertificateForClient,
		NextProtos:     []string{"h2", "http/1.1"},
	}
}

// startServer starts the given server listening on the given listener.
func startServer(server *http.Server, listener net.Listener) {
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
}

// stopServers gracefully stops the specified http.Server instances with a timeout.
func stopServers(ctx context.Context, servers ...*http.Server) {
	shutdown := func(ctx context.Context, server *http.Server) {
		timeoutContext, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()
		_ = server.Shutdown(timeoutContext)
	}

	for i := range servers {
		if servers[i] != nil {
			shutdown(ctx, servers[i])
		}
	}
}

type bufferPool struct {
	pool sync.Pool
}

func newBufferPool() *bufferPool {
	return &bufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 32*1024)
			},
		},
	}
}

func (b *bufferPool) Get() []byte {
	return b.pool.Get().([]byte)
}

func (b *bufferPool) Put(bytes []byte) {
	b.pool.Put(bytes)
}

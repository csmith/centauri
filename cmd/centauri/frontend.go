package main

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"sync"
	"time"

	"github.com/csmith/centauri/metrics"

	"github.com/csmith/centauri/proxy"
)

const (
	shutdownTimeout   = time.Second * 5
	readHeaderTimeout = time.Second * 5
	readTimeout       = time.Duration(0)
	writeTimeout      = time.Duration(0)
	idleTimeout       = time.Duration(0)
)

type frontend interface {
	Serve(ctx *frontendContext) error
	Stop(ctx context.Context)
	UsesCertificates() bool
}

type frontendContext struct {
	manager  *proxy.Manager
	rewriter *proxy.Rewriter
	recorder *metrics.Recorder
	errChan  chan<- error
}

// createProxy creates a reverse proxy backed by the context's rewriter.
func (fc *frontendContext) createProxy() http.Handler {
	return &httputil.ReverseProxy{
		Rewrite:        fc.rewriter.RewriteRequest,
		ModifyResponse: fc.recorder.TrackResponse(fc.rewriter.RewriteResponse),
		ErrorHandler:   fc.recorder.TrackBadGateway(fc.rewriter.RewriteError(handleError)),
		BufferPool:     newBufferPool(),
		Transport: &http.Transport{
			ForceAttemptHTTP2:   false,
			DisableCompression:  true,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

// createRedirector creates a http.Handler that redirects all requests to HTTPS.
func (fc *frontendContext) createRedirector() http.Handler {
	return &proxy.Redirector{}
}

// createTLSConfig creates a new tls.Config following the Mozilla intermediate configuration, and using
// the context's manager for obtaining certificates.
func (fc *frontendContext) createTLSConfig() *tls.Config {
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
		GetCertificate: fc.recorder.TrackHello(fc.manager.CertificateForClient),
		NextProtos:     []string{"h2", "http/1.1"},
	}
}

// server encapsulates an HTTP server with the ability to gracefully shutdown.
type server struct {
	srv     *http.Server
	errChan chan<- error
}

// newServer creates a new server with the provided handler and error channel.
func newServer(handler http.Handler, errChan chan<- error) *server {
	return &server{
		srv: &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: readHeaderTimeout,
			ReadTimeout:       readTimeout,
			WriteTimeout:      writeTimeout,
			IdleTimeout:       idleTimeout,
		},
		errChan: errChan,
	}
}

// start starts the server listening on the given listener.
func (s *server) start(listener net.Listener) {
	if err := s.srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.errChan <- err
	}
}

// stop gracefully stops the server with a timeout.
func (s *server) stop(ctx context.Context) {
	timeoutContext, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()
	_ = s.srv.Shutdown(timeoutContext)
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

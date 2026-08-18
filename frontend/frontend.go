// Package frontend provides the frontends Centauri can serve requests on, along with the shared
// machinery for building the underlying HTTP handlers, TLS configuration and servers.
package frontend

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
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

// Frontend represents something Centauri can listen for requests on.
type Frontend interface {
	// Serve starts the frontend, reporting any errors that occur after startup on the context's error
	// channel.
	Serve(ctx *Context) error
	// Stop gracefully shuts the frontend down.
	Stop(ctx context.Context)
	// UsesCertificates indicates whether the frontend requires a certificate provider.
	UsesCertificates() bool
}

// Context contains the components shared by all frontends when serving requests.
type Context struct {
	Manager  *proxy.Manager
	Rewriter *proxy.Rewriter
	Recorder *metrics.Recorder
	ErrChan  chan<- error
}

// createProxy creates a reverse proxy backed by the context's rewriter.
func (fc *Context) createProxy() http.Handler {
	return proxy.NewDomainRedirector(
		fc.Manager,
		&httputil.ReverseProxy{
			Rewrite:        fc.Rewriter.RewriteRequest,
			ModifyResponse: fc.Recorder.TrackResponse(fc.Rewriter.RewriteResponse),
			ErrorHandler:   fc.Recorder.TrackBadGateway(fc.Rewriter.RewriteError(handleError)),
			BufferPool:     newBufferPool(),
			Transport: &http.Transport{
				ForceAttemptHTTP2:   false,
				DisableCompression:  true,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		})
}

// createRedirector creates a http.Handler that redirects all requests to HTTPS.
func (fc *Context) createRedirector() http.Handler {
	return &proxy.HttpRedirector{}
}

// createTLSConfig creates a new tls.Config following the Mozilla intermediate configuration, and using
// the context's manager for obtaining certificates.
func (fc *Context) createTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		// Generated 2026-06-18, TLSRef Guideline v6.0, Go 1.23.3, intermediate config, gitrev=9c09b2d
		CurvePreferences: []tls.CurveID{
			tls.X25519MLKEM768,
			tls.X25519,
			tls.CurveP256,
			tls.CurveP384,
		},
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		},
		GetCertificate: fc.Recorder.TrackHello(fc.Manager.CertificateForClient),
		NextProtos:     []string{"h2", "http/1.1"},
	}
}

// Server encapsulates an HTTP server with the ability to gracefully shutdown.
type Server struct {
	srv     *http.Server
	errChan chan<- error
}

// NewServer creates a new server with the provided handler and error channel.
func NewServer(handler http.Handler, errChan chan<- error) *Server {
	return &Server{
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

// Start starts the server listening on the given listener.
func (s *Server) Start(listener net.Listener) {
	if err := s.srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.errChan <- err
	}
}

// Stop gracefully stops the server with a timeout.
func (s *Server) Stop(ctx context.Context) {
	timeoutContext, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()
	_ = s.srv.Shutdown(timeoutContext)
}

const badGatewayError = `<!doctype html>
<html lang="en">
<head>
  <title>502 Bad Gateway</title>
</head>
<body>
  <h1>Bad Gateway</h1>
  <p>The server was unable to complete your request. Please try again later.</p>
</body>
</html>`

// handleError handles the reverse proxy not being able to connect to an upstream
func handleError(writer http.ResponseWriter, request *http.Request, err error) {
	slog.Warn("Failed to connect to upstream", "host", request.Host, "error", err)
	writer.WriteHeader(http.StatusBadGateway)
	_, _ = writer.Write([]byte(badGatewayError))
}

type bufferPool struct {
	pool sync.Pool
}

func newBufferPool() *bufferPool {
	return &bufferPool{
		pool: sync.Pool{
			New: func() any {
				return new(make([]byte, 32*1024))
			},
		},
	}
}

func (b *bufferPool) Get() []byte {
	return *b.pool.Get().(*[]byte)
}

func (b *bufferPool) Put(bytes []byte) {
	b.pool.Put(&bytes)
}

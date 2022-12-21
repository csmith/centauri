//go:build !notcp

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/csmith/centauri/proxy"
)

var (
	httpPort  = flag.Int("http-port", 8080, "Port to listen on for plain HTTP requests for the TCP frontend")
	httpsPort = flag.Int("https-port", 8443, "Port to listen on for HTTPS requests for the TCP frontend")
)

type tcpFrontend struct {
	tlsServer   *http.Server
	plainServer *http.Server
}

func init() {
	frontends["tcp"] = &tcpFrontend{}
}

func (t *tcpFrontend) Serve(manager *proxy.Manager, rewriter *proxy.Rewriter) error {
	log.Printf("Starting TCP server on port %d (https) and %d (http)", *httpsPort, *httpPort)

	tlsListener, err := tls.Listen("tcp", fmt.Sprintf(":%d", *httpsPort), &tls.Config{
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
	})
	if err != nil {
		log.Fatal(err)
	}

	t.tlsServer = &http.Server{
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

	go func() {
		if err := t.tlsServer.Serve(tlsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	t.plainServer = &http.Server{
		Handler: &proxy.Redirector{},
		Addr:    fmt.Sprintf(":%d", *httpPort),
	}

	go func() {
		if err := t.plainServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	return nil
}

func (t *tcpFrontend) Stop(ctx context.Context) {
	shutdown := func(ctx context.Context, server *http.Server) {
		timeoutContext, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()
		_ = server.Shutdown(timeoutContext)
	}

	shutdown(ctx, t.tlsServer)
	shutdown(ctx, t.plainServer)
}

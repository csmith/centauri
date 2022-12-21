//go:build !notailscale

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
	"tailscale.com/tsnet"
)

var (
	tailscaleHostname = flag.String("tailscale-hostname", "centauri", "Hostname to use for the tailscale frontend")
	tailscaleKey      = flag.String("tailscale-key", "", "Auth key to use when connecting to tailscale")
)

type tailscaleFrontend struct {
	tlsServer *http.Server
	server    *http.Server
}

func init() {
	frontends["tailscale"] = &tailscaleFrontend{}
}

func (t *tailscaleFrontend) Serve(manager *proxy.Manager, rewriter *proxy.Rewriter) error {
	if *tailscaleKey == "" {
		return fmt.Errorf("tailscale authentication key not specified")
	}
	log.Printf("Starting TCP server on port %d (https) and %d (http) on %s", 443, 80, *tailscaleHostname)

	srv := &tsnet.Server{
		Hostname: *tailscaleHostname,
		AuthKey:  *tailscaleKey,
		Logf:     func(format string, args ...any) {},
	}

	if err := srv.Start(); err != nil {
		return err
	}

	tsTLSListener, err := srv.Listen("tcp", fmt.Sprintf(":%d", 443))
	if err != nil {
		return err
	}
	tlsListener := tls.NewListener(tsTLSListener, &tls.Config{
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
		return err
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

	tslistener, err := srv.Listen("tcp", fmt.Sprintf(":%d", 80))
	if err != nil {
		return err
	}
	t.server = &http.Server{
		Handler: &proxy.Redirector{},
	}

	go func() {
		if err := t.tlsServer.Serve(tlsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	go func() {
		if err := t.server.Serve(tslistener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	return nil
}

func (t *tailscaleFrontend) Stop(ctx context.Context) {
	shutdown := func(ctx context.Context, server *http.Server) {
		timeoutContext, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()
		_ = server.Shutdown(timeoutContext)
	}

	shutdown(ctx, t.tlsServer)
	shutdown(ctx, t.server)
}

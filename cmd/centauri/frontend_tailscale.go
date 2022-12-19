//go:build !notailscale

package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/csmith/centauri/proxy"
	"tailscale.com/tsnet"
)

var (
	tailscaleHostname = flag.String("tailscale-hostname", "centauri", "Hostname to use for the tailscale frontend")
	tailscaleKey      = flag.String("tailscale-key", "", "API key to use when connecting to tailscale")
)

type tailscaleFrontend struct {
	server *http.Server
}

func init() {
	frontends["tailscale"] = &tailscaleFrontend{}
}

func (t *tailscaleFrontend) Serve(manager *proxy.Manager, rewriter *proxy.Rewriter) error {
	log.Printf("Starting TCP server on http://%s/", *tailscaleHostname)

	srv := &tsnet.Server{
		Hostname: *tailscaleHostname,
		AuthKey:  *tailscaleKey,
		Logf:     log.Printf,
	}

	if err := srv.Start(); err != nil {
		return err
	}

	listener, err := srv.Listen("tcp", ":80")
	if err != nil {
		return err
	}

	t.server = &http.Server{
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
		if err := t.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	return nil
}

func (t *tailscaleFrontend) Stop(ctx context.Context) {
	timeoutContext, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()
	_ = t.server.Shutdown(timeoutContext)
}

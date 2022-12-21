//go:build !notailscale

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/csmith/centauri/proxy"
	"tailscale.com/tsnet"
)

var (
	tailscaleHostname = flag.String("tailscale-hostname", "centauri", "Hostname to use for the tailscale frontend")
	tailscaleKey      = flag.String("tailscale-key", "", "Auth key to use when connecting to tailscale")
	tailscaleMode     = flag.String("tailscale-mode", "http", "Whether to serve plain http on tailscale networks, or https with a redirect from http")
)

type tailscaleFrontend struct {
	tlsServer   *http.Server
	plainServer *http.Server
	tailscale   *tsnet.Server
}

func init() {
	frontends["tailscale"] = &tailscaleFrontend{}
}

func (t *tailscaleFrontend) Serve(manager *proxy.Manager, rewriter *proxy.Rewriter) error {
	if *tailscaleKey == "" {
		return fmt.Errorf("tailscale authentication key not specified")
	}

	t.tailscale = &tsnet.Server{
		Hostname: *tailscaleHostname,
		AuthKey:  *tailscaleKey,
		Logf:     func(format string, args ...any) {},
	}

	if err := t.tailscale.Start(); err != nil {
		return err
	}

	if *tailscaleMode == "http" {
		log.Printf("Starting tailscale server on http://%s/", *tailscaleHostname)

		if err := t.startHttpServer(createProxy(rewriter)); err != nil {
			return err
		}
	} else if *tailscaleMode == "https" {
		log.Printf("Starting tailscale server on https://%s/", *tailscaleHostname)

		if err := t.startHttpServer(createRedirector()); err != nil {
			return err
		}

		if err := t.startHttpsServer(createProxy(rewriter), proxyManager); err != nil {
			return err
		}
	}

	return nil
}

func (t *tailscaleFrontend) startHttpServer(server *http.Server) error {
	listener, err := t.tailscale.Listen("tcp", ":80")
	if err != nil {
		return err
	}

	t.plainServer = server
	startServer(server, listener)
	return nil
}

func (t *tailscaleFrontend) startHttpsServer(server *http.Server, manager *proxy.Manager) error {
	tlsListener, err := t.tailscale.Listen("tcp", ":443")
	if err != nil {
		return err
	}

	t.tlsServer = server
	startServer(t.tlsServer, tls.NewListener(tlsListener, createTLSConfig(manager)))
	return nil
}

func (t *tailscaleFrontend) Stop(ctx context.Context) {
	stopServers(ctx, t.plainServer, t.tlsServer)
	_ = t.tailscale.Close()
}

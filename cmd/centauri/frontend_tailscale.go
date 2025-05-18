//go:build !notailscale

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"tailscale.com/client/local"
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

func (t *tailscaleFrontend) Serve(ctx *frontendContext) error {
	t.tailscale = &tsnet.Server{
		Hostname: *tailscaleHostname,
		AuthKey:  *tailscaleKey,
		Logf:     func(format string, args ...any) {},
	}

	lc, err := t.tailscale.LocalClient()
	if err != nil {
		return err
	}
	ctx.rewriter.AddDecorator(&tailscaleHeaderDecorator{localClient: lc})

	if *tailscaleMode == "http" {
		log.Printf("Starting tailscale server on http://%s/", *tailscaleHostname)

		if err := t.startHttpServer(ctx.createProxy()); err != nil {
			return err
		}
	} else if *tailscaleMode == "https" {
		log.Printf("Starting tailscale server on https://%s/", *tailscaleHostname)

		if err := t.startHttpServer(ctx.createRedirector()); err != nil {
			return err
		}

		if err := t.startHttpsServer(ctx); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("unknown value for tailscale mode: %v (accepted: http, https)", *tailscaleMode)
	}

	return nil
}

func (t *tailscaleFrontend) startHttpServer(server *http.Server) error {
	listener, err := t.tailscale.Listen("tcp", ":80")
	if err != nil {
		return err
	}

	t.plainServer = server
	go startServer(server, listener)
	return nil
}

func (t *tailscaleFrontend) startHttpsServer(ctx *frontendContext) error {
	tlsListener, err := t.tailscale.Listen("tcp", ":443")
	if err != nil {
		return err
	}

	t.tlsServer = ctx.createProxy()
	go startServer(t.tlsServer, tls.NewListener(tlsListener, ctx.createTLSConfig()))
	return nil
}

func (t *tailscaleFrontend) Stop(ctx context.Context) {
	stopServers(ctx, t.plainServer, t.tlsServer)
	_ = t.tailscale.Close()
}

func (t *tailscaleFrontend) UsesCertificates() bool {
	return *tailscaleMode == "https"
}

type tailscaleHeaderDecorator struct {
	localClient *local.Client
}

func (t *tailscaleHeaderDecorator) Decorate(req *http.Request) {
	res, err := t.localClient.WhoIs(req.Context(), req.RemoteAddr)
	if err != nil {
		log.Printf("Unable to get tailscale client info: %v", err)
		return
	}

	req.Header.Set("Tailscale-User-Login", res.UserProfile.LoginName)
	req.Header.Set("Tailscale-User-Name", res.UserProfile.DisplayName)
	req.Header.Set("Tailscale-User-Profile-Pic", res.UserProfile.ProfilePicURL)
}

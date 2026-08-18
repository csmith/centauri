//go:build !notailscale

package frontend

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"

	"tailscale.com/client/local"
	"tailscale.com/tsnet"
)

// NewTailscale creates the tailscale frontend, which joins a tailnet and listens for requests from it.
func NewTailscale(options TailscaleOptions) (Frontend, error) {
	return &tailscaleFrontend{options: options}, nil
}

type tailscaleFrontend struct {
	options     TailscaleOptions
	tlsServer   *Server
	plainServer *Server
	tailscale   *tsnet.Server
}

func (t *tailscaleFrontend) Serve(ctx *Context) error {
	if t.options.Mode != "http" && t.options.Mode != "https" {
		return fmt.Errorf("unknown value for tailscale mode: %v (accepted: http, https)", t.options.Mode)
	}

	t.tailscale = &tsnet.Server{
		Hostname: t.options.Hostname,
		AuthKey:  t.options.AuthKey,
		Logf:     func(format string, args ...any) {},
		UserLogf: func(format string, args ...any) {
			slog.Info(fmt.Sprintf(format, args...), "frontend", "tailscale")
		},
		Dir: t.options.Dir,
	}

	lc, err := t.tailscale.LocalClient()
	if err != nil {
		return err
	}
	ctx.Rewriter.AddDecorator(&tailscaleHeaderDecorator{localClient: lc})

	if t.options.Mode == "http" {
		slog.Info("Starting tailscale server", "hostname", t.options.Hostname, "protocol", "http", "frontend", "tailscale")

		if err := t.startHttpServer(ctx, ctx.createProxy()); err != nil {
			return err
		}
	} else {
		slog.Info("Starting tailscale server", "hostname", t.options.Hostname, "protocol", "https", "frontend", "tailscale")

		if err := t.startHttpServer(ctx, ctx.createRedirector()); err != nil {
			return err
		}

		if err := t.startHttpsServer(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (t *tailscaleFrontend) startHttpServer(ctx *Context, handler http.Handler) error {
	listener, err := t.tailscale.Listen("tcp", ":80")
	if err != nil {
		return err
	}

	t.plainServer = NewServer(handler, ctx.ErrChan)
	go t.plainServer.Start(listener)
	return nil
}

func (t *tailscaleFrontend) startHttpsServer(ctx *Context) error {
	tlsListener, err := t.tailscale.Listen("tcp", ":443")
	if err != nil {
		return err
	}

	t.tlsServer = NewServer(ctx.createProxy(), ctx.ErrChan)
	go t.tlsServer.Start(tls.NewListener(tlsListener, ctx.createTLSConfig()))
	return nil
}

func (t *tailscaleFrontend) Stop(ctx context.Context) {
	if t.plainServer != nil {
		t.plainServer.Stop(ctx)
	}
	if t.tlsServer != nil {
		t.tlsServer.Stop(ctx)
	}
	_ = t.tailscale.Close()
}

func (t *tailscaleFrontend) UsesCertificates() bool {
	return t.options.Mode == "https"
}

type tailscaleHeaderDecorator struct {
	localClient *local.Client
}

func (t *tailscaleHeaderDecorator) Decorate(_, out *http.Request) {
	res, err := t.localClient.WhoIs(out.Context(), out.RemoteAddr)
	if err != nil {
		slog.Warn("Unable to get tailscale client info; not passing headers to upstream", "error", err, "frontend", "tailscale")
		return
	}

	out.Header.Set("Tailscale-User-Login", res.UserProfile.LoginName)
	out.Header.Set("Tailscale-User-Name", res.UserProfile.DisplayName)
	out.Header.Set("Tailscale-User-Profile-Pic", res.UserProfile.ProfilePicURL)
}

//go:build !notcp

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net"
)

var (
	httpPort  = flag.Int("http-port", 8080, "Port to listen on for plain HTTP requests for the TCP frontend")
	httpsPort = flag.Int("https-port", 8443, "Port to listen on for HTTPS requests for the TCP frontend")
)

type tcpFrontend struct {
	tlsServer   *server
	plainServer *server
}

func (t *tcpFrontend) Serve(ctx *frontendContext) error {
	slog.Info("Starting TCP server", "httpsPort", *httpsPort, "httpPort", *httpPort, "frontend", "tcp")

	tlsListener, err := tls.Listen("tcp", fmt.Sprintf(":%d", *httpsPort), ctx.createTLSConfig())
	if err != nil {
		return err
	}
	t.tlsServer = newServer(ctx.createProxy(), ctx.errChan)
	go t.tlsServer.start(tlsListener)

	plainListener, err := net.Listen("tcp", fmt.Sprintf(":%d", *httpPort))
	if err != nil {
		return err
	}

	t.plainServer = newServer(ctx.createRedirector(), ctx.errChan)
	go t.plainServer.start(plainListener)
	return nil
}

func (t *tcpFrontend) Stop(ctx context.Context) {
	t.tlsServer.stop(ctx)
	t.plainServer.stop(ctx)
}

func (t *tcpFrontend) UsesCertificates() bool {
	return true
}

//go:build !notcp

package frontend

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
)

// NewTCP creates the TCP frontend, which listens on plain HTTP and HTTPS ports.
func NewTCP(httpPort, httpsPort int) (Frontend, error) {
	return &tcpFrontend{httpPort: httpPort, httpsPort: httpsPort}, nil
}

type tcpFrontend struct {
	httpPort    int
	httpsPort   int
	tlsServer   *Server
	plainServer *Server
}

func (t *tcpFrontend) Serve(ctx *Context) error {
	slog.Info("Starting TCP server", "httpsPort", t.httpsPort, "httpPort", t.httpPort, "frontend", "tcp")

	tlsListener, err := tls.Listen("tcp", fmt.Sprintf(":%d", t.httpsPort), ctx.createTLSConfig())
	if err != nil {
		return err
	}
	t.tlsServer = NewServer(ctx.createProxy(), ctx.ErrChan)
	go t.tlsServer.Start(tlsListener)

	plainListener, err := net.Listen("tcp", fmt.Sprintf(":%d", t.httpPort))
	if err != nil {
		return err
	}

	t.plainServer = NewServer(ctx.createRedirector(), ctx.ErrChan)
	go t.plainServer.Start(plainListener)
	return nil
}

func (t *tcpFrontend) Stop(ctx context.Context) {
	t.tlsServer.Stop(ctx)
	t.plainServer.Stop(ctx)
}

func (t *tcpFrontend) UsesCertificates() bool {
	return true
}

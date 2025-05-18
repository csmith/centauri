//go:build !notcp

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
)

var (
	httpPort  = flag.Int("http-port", 8080, "Port to listen on for plain HTTP requests for the TCP frontend")
	httpsPort = flag.Int("https-port", 8443, "Port to listen on for HTTPS requests for the TCP frontend")
)

type tcpFrontend struct {
	tlsServer   *http.Server
	plainServer *http.Server
}

func (t *tcpFrontend) Serve(ctx *frontendContext) error {
	log.Printf("Starting TCP server on port %d (https) and %d (http)", *httpsPort, *httpPort)

	tlsListener, err := tls.Listen("tcp", fmt.Sprintf(":%d", *httpsPort), ctx.createTLSConfig())
	if err != nil {
		return err
	}
	t.tlsServer = ctx.createProxy()
	go startServer(t.tlsServer, tlsListener)

	plainListener, err := net.Listen("tcp", fmt.Sprintf(":%d", *httpPort))
	if err != nil {
		return err
	}

	t.plainServer = ctx.createRedirector()
	go startServer(t.plainServer, plainListener)
	return nil
}

func (t *tcpFrontend) Stop(ctx context.Context) {
	stopServers(ctx, t.tlsServer, t.plainServer)
}

func (t *tcpFrontend) UsesCertificates() bool {
	return true
}

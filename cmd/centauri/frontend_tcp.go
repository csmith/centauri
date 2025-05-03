//go:build !notcp

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/csmith/centauri/metrics"
	"log"
	"net"
	"net/http"

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

func (t *tcpFrontend) Serve(manager *proxy.Manager, rewriter *proxy.Rewriter, recorder *metrics.Recorder) error {
	log.Printf("Starting TCP server on port %d (https) and %d (http)", *httpsPort, *httpPort)

	tlsListener, err := tls.Listen("tcp", fmt.Sprintf(":%d", *httpsPort), createTLSConfig(recorder, manager))
	if err != nil {
		return err
	}
	t.tlsServer = createProxy(recorder, rewriter)
	startServer(t.tlsServer, tlsListener)

	plainListener, err := net.Listen("tcp", fmt.Sprintf(":%d", *httpPort))
	if err != nil {
		return err
	}

	t.plainServer = createRedirector()
	startServer(t.plainServer, plainListener)
	return nil
}

func (t *tcpFrontend) Stop(ctx context.Context) {
	stopServers(ctx, t.tlsServer, t.plainServer)
}

func (t *tcpFrontend) UsesCertificates() bool {
	return true
}

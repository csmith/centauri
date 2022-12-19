package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/csmith/centauri/config"
	"github.com/csmith/centauri/proxy"
	"github.com/csmith/envflag"
)

var (
	httpPort  = flag.Int("http-port", 8080, "HTTP port")
	httpsPort = flag.Int("https-port", 8443, "HTTPS port")

	configPath = flag.String("config", "centauri.conf", "Path to config")
)

var proxyManager *proxy.Manager

func main() {
	envflag.Parse()

	providers, err := certProviders()
	if err != nil {
		log.Fatalf("Error creating certificate providers: %v", err)
	}

	proxyManager = proxy.NewManager(providers, "lego")
	rewriter := proxy.NewRewriter(proxyManager)
	updateRoutes()
	listenForHup()
	monitorCerts()

	log.Printf("Starting server on port %d (https) and %d (http)", *httpsPort, *httpPort)

	l, err := tls.Listen("tcp", fmt.Sprintf(":%d", *httpsPort), &tls.Config{
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
		GetCertificate: proxyManager.CertificateForClient,
		NextProtos:     []string{"h2", "http/1.1"},
	})
	if err != nil {
		log.Fatal(err)
	}

	defer l.Close()

	go func() {
		err := http.Serve(l, &httputil.ReverseProxy{
			Director:       rewriter.RewriteRequest,
			ModifyResponse: rewriter.RewriteResponse,
			BufferPool:     newBufferPool(),
			Transport: &http.Transport{
				ForceAttemptHTTP2:   false,
				DisableCompression:  true,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		})
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	go func() {
		err := http.ListenAndServe(fmt.Sprintf(":%d", *httpPort), &proxy.Redirector{})
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, syscall.SIGTERM)

	// Wait for a signal
	log.Printf("Received signal %s, stopping...", <-c)

	// TODO: Stop servers properly
}

func monitorCerts() {
	go func() {
		for {
			time.Sleep(12 * time.Hour)
			log.Printf("Checking for certificate validity...")
			if err := proxyManager.CheckCertificates(); err != nil {
				log.Fatalf("Error performing periodic check of certificates: %v", err)
			}
		}
	}()
}

func listenForHup() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)

	go func() {
		for {
			<-c
			log.Printf("Received SIGHUP, updating routes")
			updateRoutes()
		}
	}()
}

func updateRoutes() {
	log.Printf("Reading config file %s", *configPath)

	configFile, err := os.Open(*configPath)
	if err != nil {
		log.Fatalf("Failed to open config file: %v", err)
	}
	defer configFile.Close()

	routes, err := config.Parse(configFile)
	if err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}

	log.Printf("Installing %d routes", len(routes))
	if err := proxyManager.SetRoutes(routes); err != nil {
		log.Fatalf("Route manager error: %v", err)
	}

	log.Printf("Finished installing %d routes", len(routes))
}

func newBufferPool() *bufferPool {
	return &bufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 32*1024)
			},
		},
	}
}

type bufferPool struct {
	pool sync.Pool
}

func (b *bufferPool) Get() []byte {
	return b.pool.Get().([]byte)
}

func (b *bufferPool) Put(bytes []byte) {
	b.pool.Put(bytes)
}

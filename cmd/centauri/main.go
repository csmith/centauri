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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/csmith/centauri/certificate"
	"github.com/csmith/centauri/config"
	"github.com/csmith/centauri/proxy"
	"github.com/csmith/envflag"
	"github.com/csmith/legotapas"
	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/lego"
)

var (
	httpPort  = flag.Int("http-port", 8080, "HTTP port")
	httpsPort = flag.Int("https-port", 8443, "HTTPS port")

	configPath           = flag.String("config", "centauri.conf", "Path to config")
	userDataPath         = flag.String("user-data", "user.pem", "Path to user data")
	certificateStorePath = flag.String("certificate-store", "certs.json", "Path to certificate store")

	dnsProviderName = flag.String("dns-provider", "", "DNS provider to use")
	acmeEmail       = flag.String("acme-email", "", "Email address for ACME account")
	acmeDirectory   = flag.String("acme-directory", lego.LEDirectoryProduction, "ACME directory to use")
	wildcardDomains = flag.String("wildcard-domains", "", "Space separated list of wildcard domains")
)

var proxyManager *proxy.Manager

func main() {
	envflag.Parse()

	dnsProvider, err := legotapas.CreateProvider(*dnsProviderName)
	if err != nil {
		log.Fatalf("DNS provider error: %v", err)
	}

	legoSupplier, err := certificate.NewLegoSupplier(&certificate.LegoSupplierConfig{
		Path:        *userDataPath,
		Email:       *acmeEmail,
		DirUrl:      *acmeDirectory,
		KeyType:     certcrypto.EC384,
		DnsProvider: dnsProvider,
	})
	if err != nil {
		log.Fatalf("Certificate supplier error: %v", err)
	}

	store, err := certificate.NewStore(*certificateStorePath)
	if err != nil {
		log.Fatalf("Certificate store error: %v", err)
	}

	// Ensure any wildcard domains passed have a "." prefix.
	var wildcards []string
	var wildcardConfig = strings.Split(*wildcardDomains, " ")
	for i := range wildcardConfig {
		if strings.HasPrefix(wildcardConfig[i], ".") {
			wildcards = append(wildcards, wildcardConfig[i])
		} else if len(wildcardConfig[i]) > 0 {
			wildcards = append(wildcards, fmt.Sprintf(".%s", wildcardConfig[i]))
		}
	}

	providers := map[string]proxy.CertificateProvider{
		"lego": certificate.NewManager(store, legoSupplier, time.Hour*24*30, time.Hour*24),
	}
	proxyManager = proxy.NewManager(wildcards, providers, "lego")
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
			BufferPool:     NewBufferPool(),
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

func NewBufferPool() *bufferPool {
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

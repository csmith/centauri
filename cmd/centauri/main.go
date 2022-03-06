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

	dnsProvider     = flag.String("dns-provider", "", "DNS provider to use")
	acmeEmail       = flag.String("acme-email", "", "Email address for ACME account")
	acmeDirectory   = flag.String("acme-directory", lego.LEDirectoryProduction, "ACME directory to use")
	wildcardDomains = flag.String("wildcard-domains", "", "Space separated list of wildcard domains")
)

func main() {
	envflag.Parse()

	configFile, err := os.Open(*configPath)
	if err != nil {
		log.Fatalf("Failed to open config file: %v", err)
	}
	routes, err := config.Parse(configFile)
	if err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}
	_ = configFile.Close()

	dnsProvider, err := legotapas.CreateProvider(*dnsProvider)
	if err != nil {
		log.Fatalf("DNS provider error: %v", err)
	}

	supplier, err := certificate.NewSupplier(&certificate.LegoSupplierConfig{
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

	certManager := certificate.NewManager(store, supplier, supplier, time.Hour*24*30, time.Hour*24)
	routeManager := proxy.NewManager(wildcards, certManager)
	rewriter := proxy.NewRewriter(routeManager)

	if err := routeManager.SetRoutes(routes); err != nil {
		log.Fatalf("Route manager error: %v", err)
	}

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
		GetCertificate: routeManager.CertificateForClient,
	})
	if err != nil {
		log.Fatal(err)
	}

	defer l.Close()

	go func() {
		err := http.Serve(l, &httputil.ReverseProxy{
			Director:       rewriter.RewriteRequest,
			ModifyResponse: rewriter.RewriteResponse,
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
	<-c
}

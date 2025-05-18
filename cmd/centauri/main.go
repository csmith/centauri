package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/csmith/centauri/metrics"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/csmith/centauri/config"
	"github.com/csmith/centauri/proxy"
	"github.com/csmith/envflag"
)

var (
	configPath       = flag.String("config", "centauri.conf", "Path to config")
	selectedFrontend = flag.String("frontend", "tcp", "Frontend to listen on")
	metricsPort      = flag.Int("metrics-port", 0, "Port to expose metrics endpoint on. Disabled by default.")
)

var proxyManager *proxy.Manager

func main() {
	envflag.Parse()

	f, err := createFrontend(*selectedFrontend)
	if err != nil {
		log.Fatalf("Invalid frontend specified: %v", err)
	}

	var provider proxy.CertificateProvider
	if f.UsesCertificates() {
		var err error
		provider, err = certProvider()
		if err != nil {
			log.Fatalf("Error creating certificate providers: %v", err)
		}

		monitorCerts()
	}

	proxyManager = proxy.NewManager(provider)
	rewriter := proxy.NewRewriter(proxyManager)
	if err := updateRoutes(); err != nil {
		log.Fatalf("Failed to load initial configuration: %v", err)
	}
	recorder := metrics.NewRecorder(proxyManager.RouteForDomain)

	if err := f.Serve(&frontendContext{
		manager:  proxyManager,
		rewriter: rewriter,
		recorder: recorder,
	}); err != nil {
		log.Fatalf("Failed to start frontend: %v", err)
	}

	metricsChan := make(chan struct{}, 1)
	if *metricsPort > 0 {
		serveMetrics(recorder, metricsChan)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for sig := range c {
		switch sig {
		case syscall.SIGHUP:
			log.Printf("Received signal %s, updating routes...", sig)
			if err := updateRoutes(); err != nil {
				log.Fatalf("Error updating routes: %v", err)
			}
		case syscall.SIGINT, syscall.SIGTERM:
			log.Printf("Received signal %s, stopping frontend...", sig)
			metricsChan <- struct{}{}
			f.Stop(context.Background())
			log.Printf("Frontend stopped. Goodbye!")
			return
		}
	}
}

func createFrontend(name string) (frontend, error) {
	switch strings.ToLower(name) {
	case "tcp":
		return &tcpFrontend{}, nil
	case "tailscale":
		return &tailscaleFrontend{}, nil
	default:
		return nil, fmt.Errorf("unknown frontend: %s", name)
	}
}

func monitorCerts() {
	go func() {
		for {
			time.Sleep(12 * time.Hour)
			log.Printf("Checking for certificate validity...")
			proxyManager.CheckCertificates()
		}
	}()
}

func updateRoutes() error {
	log.Printf("Reading config file %s", *configPath)

	configFile, err := os.Open(*configPath)
	if err != nil {
		return fmt.Errorf("failed to open config file: %w", err)
	}
	defer configFile.Close()

	routes, fallback, err := config.Parse(configFile)
	if err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	log.Printf("Installing %d routes", len(routes))
	if err := proxyManager.SetRoutes(routes, fallback); err != nil {
		return fmt.Errorf("route manager error: %w", err)
	}

	log.Printf("Finished installing %d routes", len(routes))
	return nil
}

func serveMetrics(recorder *metrics.Recorder, shutdownChan <-chan struct{}) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", recorder.Handler())

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", *metricsPort),
		Handler: mux,
	}

	go func() {
		log.Printf("Starting metrics server on port %d", *metricsPort)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start metrics server: %v", err)
		}
	}()

	go func() {
		<-shutdownChan
		_ = server.Shutdown(context.Background())
	}()
}

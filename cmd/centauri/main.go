package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/csmith/centauri/metrics"
	"log"
	"net"
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
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	envflag.Parse()

	f, err := createFrontend(*selectedFrontend)
	if err != nil {
		return fmt.Errorf("invalid frontend specified: %v", err)
	}

	var provider proxy.CertificateProvider
	if f.UsesCertificates() {
		var err error
		provider, err = certProvider()
		if err != nil {
			return fmt.Errorf("error creating certificate providers: %v", err)
		}

		monitorCerts()
	}

	proxyManager = proxy.NewManager(provider)
	rewriter := proxy.NewRewriter(proxyManager)
	if err := updateRoutes(); err != nil {
		return fmt.Errorf("failed to load initial configuration: %v", err)
	}
	recorder := metrics.NewRecorder(proxyManager.RouteForDomain)

	errChan := make(chan error)
	if err := f.Serve(&frontendContext{
		manager:  proxyManager,
		rewriter: rewriter,
		recorder: recorder,
		errChan:  errChan,
	}); err != nil {
		return fmt.Errorf("failed to start frontend: %v", err)
	}

	metricsChan := make(chan struct{}, 1)
	if *metricsPort > 0 {
		serveMetrics(recorder, metricsChan, errChan)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for {
		select {
		case sig := <-c:
			switch sig {
			case syscall.SIGHUP:
				log.Printf("Received signal %s, updating routes...", sig)
				if err := updateRoutes(); err != nil {
					return fmt.Errorf("error updating routes: %v", err)
				}
			case syscall.SIGINT, syscall.SIGTERM:
				log.Printf("Received signal %s, stopping frontend...", sig)
				metricsChan <- struct{}{}
				f.Stop(context.Background())
				log.Printf("Frontend stopped. Goodbye!")
				return nil
			}
		case err := <-errChan:
			return err
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

func serveMetrics(recorder *metrics.Recorder, shutdownChan <-chan struct{}, errChan chan<- error) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", recorder.Handler())
	s := newServer(mux, errChan)

	go func() {
		log.Printf("Starting metrics server on port %d", *metricsPort)
		if listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *metricsPort)); err != nil {
			errChan <- fmt.Errorf("failed to listen on port %d: %w", *metricsPort, err)
		} else {
			s.start(listener)
		}
	}()

	go func() {
		<-shutdownChan
		s.stop(context.Background())
	}()
}

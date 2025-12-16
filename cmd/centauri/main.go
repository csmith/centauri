package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/pprof"
	"strings"
	"syscall"
	"time"

	"github.com/csmith/centauri/metrics"
	"github.com/csmith/centauri/proxy"
	"github.com/csmith/envflag/v2"
)

var (
	selectedFrontend     = flag.String("frontend", "tcp", "Frontend to listen on")
	selectedConfigSource = flag.String("config-source", "file", "Config source to use")
	trustedDownstreams   = flag.String("trusted-downstreams", "", "Comma-separated list of CIDR ranges to trust X-Forwarded-For headers from")
	metricsPort          = flag.Int("metrics-port", 0, "Port to expose metrics endpoint on. Disabled by default.")
	debugCpuProfile      = flag.String("debug-cpu-profile", "", "File to write cpu profiling information to. Disabled by default.")
	validate             = flag.Bool("validate", false, "Validate config file and exit")
)

var proxyManager *proxy.Manager

func main() {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	if err := run(os.Args[1:], signalChan); err != nil {
		slog.Error("Centauri encountered a fatal error", "error", err)
		os.Exit(1)
	}
}

func run(args []string, signalChan <-chan os.Signal) error {
	envflag.Parse(envflag.WithArguments(args))
	initLogging()

	if *debugCpuProfile != "" {
		slog.Warn("Running with CPU profiling. This will heavily impact performance.", "target", *debugCpuProfile)
		cpuFile, err := os.Create(*debugCpuProfile)
		if err != nil {
			return fmt.Errorf("could not create file for cpu profiling: %w", err)
		}
		defer cpuFile.Close()

		if err := pprof.StartCPUProfile(cpuFile); err != nil {
			return fmt.Errorf("could not start CPU profile: %w", err)
		}
		defer pprof.StopCPUProfile()
	}

	errChan := make(chan error)

	config, err := createConfigSource(*selectedConfigSource)
	if err != nil {
		return fmt.Errorf("invalid config source specified: %v", err)
	}

	if *validate {
		return config.Validate()
	}

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

	downstreams, err := parseDownstreams(*trustedDownstreams)
	if err != nil {
		return fmt.Errorf("could not parse trusted downstreams: %w", err)
	}

	proxyManager = proxy.NewManager(provider)
	rewriter := proxy.NewRewriter(proxyManager, downstreams)

	if err := config.Start(proxyManager.SetRoutes, errChan); err != nil {
		return fmt.Errorf("failed to start config source: %v", err)
	}

	recorder := metrics.NewRecorder(proxyManager.RouteForDomain)

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

	for {
		select {
		case sig := <-signalChan:
			switch sig {
			case syscall.SIGHUP:
				slog.Info("Received signal, reloading config...", "signal", sig)
				config.Reload()
			case syscall.SIGINT, syscall.SIGTERM:
				slog.Info("Received signal, stopping frontend...", "signal", sig)
				metricsChan <- struct{}{}
				config.Stop(context.Background())
				f.Stop(context.Background())
				slog.Info("Frontend stopped. Goodbye!")
				return nil
			}
		case err := <-errChan:
			if f != nil {
				f.Stop(context.Background())
			}
			if config != nil {
				config.Stop(context.Background())
			}
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

func createConfigSource(name string) (configSource, error) {
	switch strings.ToLower(name) {
	case "file":
		return newFileConfigSource(), nil
	case "network":
		return newNetworkConfigSource(), nil
	default:
		return nil, fmt.Errorf("unknown config source: %s", name)
	}
}

func monitorCerts() {
	go func() {
		for {
			time.Sleep(12 * time.Hour)
			slog.Info("Checking for certificate validity...")
			proxyManager.CheckCertificates()
		}
	}()
}

func serveMetrics(recorder *metrics.Recorder, shutdownChan <-chan struct{}, errChan chan<- error) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", recorder.Handler())
	s := newServer(mux, errChan)

	go func() {
		slog.Info("Starting metrics server", "port", *metricsPort)
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

func parseDownstreams(downstreams string) ([]net.IPNet, error) {
	var res []net.IPNet
	parts := strings.Split(downstreams, ",")
	for i := range parts {
		v := strings.TrimSpace(parts[i])
		if v != "" {
			_, ipNet, err := net.ParseCIDR(v)
			if err != nil {
				return nil, fmt.Errorf("failed to parse trusted downstream CIDR '%q': %w", v, err)
			}
			res = append(res, *ipNet)
		}
	}
	return res, nil
}

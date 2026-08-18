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

	"github.com/csmith/centauri/certificate"
	"github.com/csmith/centauri/config"
	"github.com/csmith/centauri/frontend"
	"github.com/csmith/centauri/metrics"
	"github.com/csmith/centauri/proxy"
	"github.com/csmith/envflag/v2"
	"github.com/csmith/legotapas/v2"
	"github.com/csmith/slogflags"
	"github.com/go-acme/lego/v5/certcrypto"
	"github.com/go-acme/lego/v5/lego"
	"github.com/go-acme/lego/v5/log"
)

// certCheckInterval is how often the proxy manager re-checks the certificates for its routes.
const certCheckInterval = 12 * time.Hour

var (
	selectedFrontend     = flag.String("frontend", "tcp", "Frontend to listen on")
	selectedConfigSource = flag.String("config-source", "file", "Config source to use")
	trustedDownstreams   = flag.String("trusted-downstreams", "", "Comma-separated list of CIDR ranges to trust X-Forwarded-For headers from")
	metricsPort          = flag.Int("metrics-port", 0, "Port to expose metrics endpoint on. Disabled by default.")
	debugCpuProfile      = flag.String("debug-cpu-profile", "", "File to write cpu profiling information to. Disabled by default.")
	validate             = flag.Bool("validate", false, "Validate config file and exit")

	configPath           = flag.String("config", "centauri.conf", "Path to config")
	configNetworkAddr    = flag.String("config-network-address", "", "Address to connect to for network config source")
	userDataPath         = flag.String("user-data", "user.pem", "Path to user data")
	certificateStoreType = flag.String("certificate-store-type", "json", "Type of certificate store to use")
	certificateStorePath = flag.String("certificate-store", "certs.json", "Path to certificate store, when using the json certificate store")
	certificateProv      = flag.String("certificate-providers", "lego selfsigned", "Space separated list of certificate providers to use by default in order of preference")
	wildcardDomains      = flag.String("wildcard-domains", "", "Space separated list of wildcard domains")
	useStaples           = flag.Bool("ocsp-stapling", false, "Enable OCSP response stapling")

	httpPort  = flag.Int("http-port", 8080, "Port to listen on for plain HTTP requests for the TCP frontend")
	httpsPort = flag.Int("https-port", 8443, "Port to listen on for HTTPS requests for the TCP frontend")

	tailscaleHostname = flag.String("tailscale-hostname", "centauri", "Hostname to use for the tailscale frontend")
	tailscaleKey      = flag.String("tailscale-key", "", "Auth key to use when connecting to tailscale")
	tailscaleMode     = flag.String("tailscale-mode", "http", "Whether to serve plain http on tailscale networks, or https with a redirect from http")
	tailscaleDir      = flag.String("tailscale-dir", "", "Directory to use to persist tailscale state")

	redisAddress   = flag.String("redis-address", "localhost:6379", "Address of the Redis server, when using the redis certificate store")
	redisUsername  = flag.String("redis-username", "", "Username for the Redis server, when using the redis certificate store")
	redisPassword  = flag.String("redis-password", "", "Password for the Redis server, when using the redis certificate store")
	redisDB        = flag.Int("redis-db", 0, "Redis database number, when using the redis certificate store")
	redisKeyPrefix = flag.String("redis-key-prefix", "centauri", "Prefix for keys, when using the redis certificate store")
	redisUseTLS    = flag.Bool("redis-tls", false, "Use TLS when connecting to the Redis server")

	dnsProviderName         = flag.String("dns-provider", "", "DNS provider to use for ACME DNS-01 challenges")
	acmeEmail               = flag.String("acme-email", "", "Email address for ACME account")
	acmeExternalAccountKid  = flag.String("acme-external-kid", "", "Key ID for ACME external account binding")
	acmeExternalAccountHmac = flag.String("acme-external-hmac", "", "Base64-url-encoded HMAC for ACME external account binding")
	acmeDirectory           = flag.String("acme-directory", lego.DirectoryURLLetsEncrypt, "ACME directory to use")
	acmeProfile             = flag.String("acme-profile", "", "Profile to use when requesting a certificate")
	acmeDisablePropagation  = flag.Bool("acme-disable-propagation-check", false, "Prevents the ACME client from checking that DNS propagation was successful")
	acmePropagationDelay    = flag.Duration("acme-propagation-delay", 10*time.Second, "Length of time to wait for propagation if ACME_DISABLE_PROPAGATION_CHECK is enabled")
	acmeResolvers           = flag.String("acme-resolvers", "", "Comma separated list of nameservers to use for DNS checks. Each should be specified as a host:port pair")
	acmeOverallLimit        = flag.Int("acme-overall-request-limit", 18, "Maximum number of requests to send to the ACME server per second")
	acmeObtainInterval      = flag.Duration("acme-obtain-interval", 0, "Minimum duration between certificate issuance requests. Set to 0 to disable.")
	acmeOverallTimeout      = flag.Duration("acme-overall-timeout", 10*time.Minute, "Maximum time to spend on ACME operations")
)

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

	configSource, err := createConfigSource(*selectedConfigSource)
	if err != nil {
		return fmt.Errorf("invalid config source specified: %v", err)
	}

	if *validate {
		return configSource.Validate()
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
	}

	downstreams, err := proxy.ParseCIDRList(*trustedDownstreams)
	if err != nil {
		return fmt.Errorf("could not parse trusted downstreams: %w", err)
	}

	proxyManager := proxy.NewManager(provider)
	rewriter := proxy.NewRewriter(proxyManager, downstreams)

	if err := configSource.Start(context.Background(), proxyManager.SetRoutes, errChan); err != nil {
		return fmt.Errorf("failed to start config source: %v", err)
	}

	if f.UsesCertificates() {
		go proxyManager.MonitorCertificates(context.Background(), certCheckInterval)
	}

	recorder := metrics.NewRecorder(proxyManager.RouteForDomain)

	if err := f.Serve(&frontend.Context{
		Manager:  proxyManager,
		Rewriter: rewriter,
		Recorder: recorder,
		ErrChan:  errChan,
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
				configSource.Reload()
			case syscall.SIGINT, syscall.SIGTERM:
				slog.Info("Received signal, stopping frontend...", "signal", sig)
				metricsChan <- struct{}{}
				configSource.Stop(context.Background())
				f.Stop(context.Background())
				slog.Info("Frontend stopped. Goodbye!")
				return nil
			}
		case err := <-errChan:
			if f != nil {
				f.Stop(context.Background())
			}
			if configSource != nil {
				configSource.Stop(context.Background())
			}
			return err
		}
	}
}

func initLogging() {
	logger := slogflags.Logger(
		slogflags.WithOldLogLevel(slog.LevelDebug),
		slogflags.WithSetDefault(true),
	)
	log.SetDefault(logger.With("component", "lego"))
}

func createFrontend(name string) (frontend.Frontend, error) {
	switch strings.ToLower(name) {
	case "tcp":
		return frontend.NewTCP(*httpPort, *httpsPort)
	case "tailscale":
		return frontend.NewTailscale(frontend.TailscaleOptions{
			Hostname: *tailscaleHostname,
			AuthKey:  *tailscaleKey,
			Mode:     *tailscaleMode,
			Dir:      *tailscaleDir,
		})
	default:
		return nil, fmt.Errorf("unknown frontend: %s", name)
	}
}

func createConfigSource(name string) (config.Source, error) {
	switch strings.ToLower(name) {
	case "file":
		return config.NewFileSource(*configPath), nil
	case "network":
		return config.NewNetworkSource(*configNetworkAddr), nil
	default:
		return nil, fmt.Errorf("unknown config source: %s", name)
	}
}

func createCertificateStore(name string) (certificate.Store, error) {
	switch strings.ToLower(name) {
	case "json":
		return certificate.NewStore(*certificateStorePath)
	case "redis":
		return certificate.NewRedisStoreFromOptions(certificate.RedisOptions{
			Addr:      *redisAddress,
			Username:  *redisUsername,
			Password:  *redisPassword,
			DB:        *redisDB,
			KeyPrefix: *redisKeyPrefix,
			UseTLS:    *redisUseTLS,
		})
	default:
		return nil, fmt.Errorf("unknown certificate store: %s", name)
	}
}

// certProvider assembles the certificate provider from the configured store and suppliers. If the lego
// supplier cannot be created - for example because no DNS provider is configured - a warning is logged
// and only the selfsigned supplier is used.
func certProvider() (proxy.CertificateProvider, error) {
	store, err := createCertificateStore(*certificateStoreType)
	if err != nil {
		return nil, fmt.Errorf("certificate store error: %v", err)
	}

	var legoConfig *certificate.LegoSupplierConfig
	if *dnsProviderName == "" {
		slog.Warn("Unable to create lego certificate supplier: no DNS provider specified")
	} else if dnsProvider, err := legotapas.CreateProvider(*dnsProviderName); err != nil {
		slog.Warn("Unable to create lego certificate supplier", "error", err)
	} else {
		legoConfig = &certificate.LegoSupplierConfig{
			Path:                    *userDataPath,
			Email:                   *acmeEmail,
			DirUrl:                  *acmeDirectory,
			KeyType:                 certcrypto.EC384,
			DnsProvider:             dnsProvider,
			DisablePropagationCheck: *acmeDisablePropagation,
			PropagationDelay:        *acmePropagationDelay,
			Profile:                 *acmeProfile,
			ExternalAccountKid:      *acmeExternalAccountKid,
			ExternalAccountHmac:     *acmeExternalAccountHmac,
			OverallRequestLimit:     *acmeOverallLimit,
			ObtainInterval:          *acmeObtainInterval,
			Resolvers:               certificate.ParseResolvers(*acmeResolvers),
			Timeout:                 *acmeOverallTimeout,
		}
	}

	return certificate.NewProvider(context.Background(), certificate.ProviderConfig{
		Store:              store,
		Lego:               legoConfig,
		PreferredSuppliers: strings.Split(*certificateProv, " "),
		WildcardDomains:    strings.Split(*wildcardDomains, " "),
		UseStaples:         *useStaples,
	}), nil
}

func serveMetrics(recorder *metrics.Recorder, shutdownChan <-chan struct{}, errChan chan<- error) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", recorder.Handler())
	s := frontend.NewServer(mux, errChan)

	go func() {
		slog.Info("Starting metrics server", "port", *metricsPort)
		if listener, err := net.Listen("tcp", fmt.Sprintf(":%d", *metricsPort)); err != nil {
			errChan <- fmt.Errorf("failed to listen on port %d: %w", *metricsPort, err)
		} else {
			s.Start(listener)
		}
	}()

	go func() {
		<-shutdownChan
		s.Stop(context.Background())
	}()
}

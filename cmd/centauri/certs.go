package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/csmith/centauri/certificate"
	"github.com/csmith/centauri/proxy"
	"github.com/csmith/legotapas/v2"
	"github.com/go-acme/lego/v5/certcrypto"
	"github.com/go-acme/lego/v5/lego"
	"golang.org/x/sys/unix"
)

var (
	userDataPath            = flag.String("user-data", "user.pem", "Path to user data")
	certificateStorePath    = flag.String("certificate-store", "certs.json", "Path to certificate store")
	certificateProviders    = flag.String("certificate-providers", "lego selfsigned", "Space separated list of certificate providers to use by default in order of preference")
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
	acmeOverallTimeout      = flag.Duration("acme-overall-timeout", 10*time.Minute, "Maximum time to spend on ACME operations")
	wildcardDomains         = flag.String("wildcard-domains", "", "Space separated list of wildcard domains")
	useStaples              = flag.Bool("ocsp-stapling", false, "Enable OCSP response stapling")
)

func certProvider() (proxy.CertificateProvider, error) {
	store, err := certificate.NewStore(*certificateStorePath)
	if err != nil {
		return nil, fmt.Errorf("certificate store error: %v", err)
	}

	var wildcardConfig = strings.Split(*wildcardDomains, " ")
	var suppliers = make(map[string]certificate.Supplier)

	if legoSupplier, err := createLegoSupplier(); err != nil {
		slog.Warn("WARNING: Unable to create lego certificate supplier", "error", err)
	} else {
		suppliers["lego"] = legoSupplier
	}

	suppliers["selfsigned"] = certificate.NewSelfSignedSupplier()

	return certificate.NewWildcardResolver(
		certificate.NewManager(store, suppliers, strings.Split(*certificateProviders, " "), *useStaples),
		wildcardConfig,
	), nil
}

func createLegoSupplier() (*certificate.LegoSupplier, error) {
	if *dnsProviderName == "" {
		return nil, fmt.Errorf("no DNS provider specified")
	}

	dnsProvider, err := legotapas.CreateProvider(*dnsProviderName)
	if err != nil {
		return nil, fmt.Errorf("dns provider error: %v", err)
	}

	if err := canWriteToDataPath(); err != nil {
		return nil, fmt.Errorf("unable to write to path %s: %v", *userDataPath, err)
	}

	legoSupplier, err := certificate.NewLegoSupplier(
		context.Background(),
		&certificate.LegoSupplierConfig{
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
			Resolvers:               parseResolvers(*acmeResolvers),
			Timeout:                 *acmeOverallTimeout,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("certificate supplier error: %v", err)
	}
	return legoSupplier, nil
}

func canWriteToDataPath() error {
	if _, err := os.Stat(*userDataPath); errors.Is(err, fs.ErrNotExist) {
		// If the file doesn't exist we need to check write perms on the directory
		return unix.Access(filepath.Dir(*userDataPath), unix.W_OK)
	} else {
		return unix.Access(*userDataPath, unix.W_OK)
	}
}

func parseResolvers(input string) []string {
	var res []string
	parts := strings.Split(input, ",")
	for i := range parts {
		if p := strings.TrimSpace(parts[i]); p != "" {
			res = append(res, p)
		}
	}
	return res
}

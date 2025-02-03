package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/csmith/centauri/certificate"
	"github.com/csmith/centauri/proxy"
	"github.com/csmith/legotapas"
	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/log"
	"golang.org/x/sys/unix"
)

var (
	userDataPath         = flag.String("user-data", "user.pem", "Path to user data")
	certificateStorePath = flag.String("certificate-store", "certs.json", "Path to certificate store")
	certificateProviders = flag.String("certificate-providers", "lego selfsigned", "Space separated list of certificate providers to use by default in order of preference")
	dnsProviderName      = flag.String("dns-provider", "", "DNS provider to use for ACME DNS-01 challenges")
	acmeEmail            = flag.String("acme-email", "", "Email address for ACME account")
	acmeDirectory        = flag.String("acme-directory", lego.LEDirectoryProduction, "ACME directory to use")
	wildcardDomains      = flag.String("wildcard-domains", "", "Space separated list of wildcard domains")
	useStaples           = flag.Bool("ocsp-stapling", false, "Enable OCSP response stapling")
)

func certProvider() (proxy.CertificateProvider, error) {
	store, err := certificate.NewStore(*certificateStorePath)
	if err != nil {
		return nil, fmt.Errorf("certificate store error: %v", err)
	}

	var wildcardConfig = strings.Split(*wildcardDomains, " ")
	var suppliers = make(map[string]certificate.Supplier)

	if legoSupplier, err := createLegoSupplier(); err != nil {
		log.Printf("WARNING: Unable to create lego certificate supplier: %v", err)
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

	legoSupplier, err := certificate.NewLegoSupplier(&certificate.LegoSupplierConfig{
		Path:        *userDataPath,
		Email:       *acmeEmail,
		DirUrl:      *acmeDirectory,
		KeyType:     certcrypto.EC384,
		DnsProvider: dnsProvider,
	})
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

package certificate

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	mathrand "math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v5/acme"
	"github.com/go-acme/lego/v5/acme/api"
	"github.com/go-acme/lego/v5/challenge/dns01"

	"github.com/go-acme/lego/v5/certcrypto"
	legocert "github.com/go-acme/lego/v5/certificate"
	"github.com/go-acme/lego/v5/challenge"
	"github.com/go-acme/lego/v5/lego"
	"github.com/go-acme/lego/v5/registration"
	"golang.org/x/crypto/ocsp"
	"golang.org/x/sys/unix"
	"golang.org/x/time/rate"
)

// registrar is the surface we use to interact with lego's account registration API.
type registrar interface {
	Register(ctx context.Context, options registration.RegisterOptions) (*acme.ExtendedAccount, error)
	RegisterWithExternalAccountBinding(ctx context.Context, options registration.RegisterEABOptions) (*acme.ExtendedAccount, error)
}

// certifier is the surface we use to interact with lego's certificate issuance API.
type certifier interface {
	Obtain(ctx context.Context, request legocert.ObtainRequest) (*legocert.Resource, error)
	GetOCSP(ctx context.Context, bundle []byte) ([]byte, *ocsp.Response, error)
	GetRenewalInfo(ctx context.Context, cert *x509.Certificate) (*legocert.RenewalInfo, error)
}

// LegoSupplier uses a lego client to obtain certificates from an ACME endpoint.
type LegoSupplier struct {
	config        *LegoSupplierConfig
	user          *acmeUser
	profile       string
	keyType       certcrypto.KeyType
	timeout       time.Duration
	obtainLimiter *rate.Limiter

	certifierMutex sync.RWMutex
	certifier      certifier
}

// LegoSupplierConfig contains the configuration used to create a new LegoSupplier.
type LegoSupplierConfig struct {
	// Path is the path to a file on disk where registration data may be cached.
	Path string
	// Email is the contact address to supply to the ACME endpoint.
	Email string
	// DirUrl is the URL of the ACME endpoint.
	DirUrl string
	// Profile is the name of the profile to use when requesting a certificate.
	Profile string
	// KeyType is the type of key to use when generating a certificate.
	KeyType certcrypto.KeyType
	// DnsProvider is the DNS-01 challenge provider that will verify domain ownership.
	DnsProvider challenge.Provider
	// DisablePropagationCheck instructs the lego client to not bother checking for DNS propagation.
	DisablePropagationCheck bool
	// PropagationDelay is the duration to sleep for if the propagation check is disabled.
	PropagationDelay time.Duration
	// ExternalAccountKid is the key ID for an externally bound account.
	ExternalAccountKid string
	// ExternalAccountHmac is the base64-url encoded HMAC key for an externally bound account.
	ExternalAccountHmac string
	// OverallRequestLimit is the maximum number of ACME requests to send per second.
	OverallRequestLimit int
	// ObtainInterval is the minimum duration between certificate issuance requests.
	// A value of zero or less means no limit. Useful for staying under per-account
	// issuance quotas imposed by ACME servers (e.g. Let's Encrypt).
	ObtainInterval time.Duration
	// Resolvers defines the DNS resolvers to use in place of the system resolvers.
	Resolvers []string
	// Timeout is the maximum time to wait for ACME operations to complete.
	Timeout time.Duration
}

// errNotConnected is returned by LegoSupplier methods that are called before Connect has succeeded.
var errNotConnected = errors.New("not yet connected to the ACME server")

const (
	connectInitialDelay = 5 * time.Second
	connectMaxDelay     = 6 * time.Hour
)

// NewLegoSupplier creates a new supplier, loading any cached registration data. The supplier cannot obtain
// certificates until Connect has succeeded.
func NewLegoSupplier(config *LegoSupplierConfig) (*LegoSupplier, error) {
	if err := canWriteTo(config.Path); err != nil {
		return nil, fmt.Errorf("unable to write to path %s: %w", config.Path, err)
	}

	user := &acmeUser{
		email:   config.Email,
		eabKid:  config.ExternalAccountKid,
		eabHmac: config.ExternalAccountHmac,
	}
	if err := user.load(config.Path); err != nil {
		return nil, err
	}

	if len(config.Resolvers) > 0 {
		dns01.SetDefaultClient(
			dns01.NewClient(
				&dns01.Options{
					RecursiveNameservers: config.Resolvers,
				},
			),
		)
	}

	return &LegoSupplier{
		config:        config,
		user:          user,
		profile:       config.Profile,
		keyType:       config.KeyType,
		timeout:       config.Timeout,
		obtainLimiter: rate.NewLimiter(rate.Every(config.ObtainInterval), 1),
	}, nil
}

// Connect creates a client for the ACME server, registering an account with it if necessary.
func (s *LegoSupplier) Connect(ctx context.Context) error {
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	legoConfig := lego.NewConfig(s.user)
	legoConfig.CADirURL = s.config.DirUrl
	legoConfig.Certificate.OverallRequestLimit = s.config.OverallRequestLimit

	client, err := lego.NewClient(legoConfig)
	if err != nil {
		return err
	}

	if err = client.Challenge.SetDNS01Provider(
		s.config.DnsProvider,
		dns01.DisableRecursiveNSsPropagationRequirement(),
		dns01.CondOptions(
			s.config.DisablePropagationCheck,
			dns01.WrapPreCheck(func(ctx context.Context, domain, fqdn, value string, check dns01.PreCheckFunc) (bool, error) {
				slog.Info(
					"Propagation check disabled, not checking DNS at all",
					"domain",
					domain,
					"wait",
					s.config.PropagationDelay,
				)

				select {
				case <-time.After(s.config.PropagationDelay):
				case <-ctx.Done():
					return false, ctx.Err()
				}

				return true, nil
			}),
		),
	); err != nil {
		return err
	}

	if s.user.account == nil {
		if err = s.user.registerAndSave(ctx, client.Registration, s.config.Path); err != nil {
			return err
		}
	}

	s.certifierMutex.Lock()
	defer s.certifierMutex.Unlock()
	s.certifier = client.Certificate
	return nil
}

// GetCertificate obtains a new certificate for the given names, and immediately requests a new OCSP staple.
func (s *LegoSupplier) GetCertificate(ctx context.Context, subject string, altNames []string, shouldStaple bool) (*Details, error) {
	c, err := s.currentCertifier()
	if err != nil {
		return nil, err
	}

	slog.Info("Starting ACME process to obtain certificate", "domain", subject, "altNames", altNames, "timeout", s.timeout, "limited", s.obtainLimiter.Tokens() < 1)

	if err := s.obtainLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	res, err := c.Obtain(
		ctx,
		legocert.ObtainRequest{
			Domains:    append([]string{subject}, altNames...),
			Bundle:     true,
			MustStaple: shouldStaple,
			Profile:    s.profile,
			KeyType:    s.keyType,
		},
	)
	if err != nil {
		return nil, err
	}
	slog.Info("Successfully obtained certificate from ACME provider", "domain", subject, "altNames", altNames)

	pem, err := certcrypto.ParsePEMCertificate(res.Certificate)
	if err != nil {
		return nil, fmt.Errorf("unable to parse returned certificate: %w", err)
	}

	details := &Details{
		Issuer:      string(res.IssuerCertificate),
		PrivateKey:  string(res.PrivateKey),
		Certificate: string(res.Certificate),
		Subject:     subject,
		AltNames:    altNames,
		NotAfter:    pem.NotAfter,
	}

	if shouldStaple {
		slog.Info("Updating OCSP staple for new certificate", "domain", subject, "altNames", altNames)
		if err = s.UpdateStaple(ctx, details); err != nil {
			return nil, fmt.Errorf("unable to get OCSP staple for certificate: %w", err)
		}
		slog.Info("Successfully updated OCSP staple for certificate", "domain", subject, "altNames", altNames)
	}

	return details, nil
}

// UpdateStaple requests a new OCSP staple for the given certificate.
func (s *LegoSupplier) UpdateStaple(ctx context.Context, cert *Details) error {
	c, err := s.currentCertifier()
	if err != nil {
		return err
	}

	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	slog.Info("Updating OCSP staple", "domain", cert.Subject, "altNames", cert.AltNames)
	b, response, err := c.GetOCSP(ctx, []byte(cert.Certificate))
	if err != nil {
		return err
	}

	if response == nil || response.Status != ocsp.Good {
		return fmt.Errorf("OCSP response was not good")
	}

	cert.OcspResponse = b
	cert.NextOcspUpdate = response.NextUpdate
	slog.Info("Successfully updated OCSP staple", "domain", cert.Subject, "altNames", cert.AltNames)
	return nil
}

// UpdateRenewalInfo asks the ACME server when the certificate should be renewed.
func (s *LegoSupplier) UpdateRenewalInfo(ctx context.Context, cert *Details) error {
	c, err := s.currentCertifier()
	if err != nil {
		return err
	}

	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	x509Cert, err := certcrypto.ParsePEMCertificate([]byte(cert.Certificate))
	if err != nil {
		return fmt.Errorf("unable to parse certificate: %w", err)
	}

	response, err := c.GetRenewalInfo(ctx, x509Cert)

	if err != nil {
		if errors.Is(err, api.ErrNoARI) {
			slog.Debug("ACME server does not support ARI", "domain", cert.Subject, "altNames", cert.AltNames)
			cert.AriRenewalTime = time.Time{}
			cert.AriNextUpdate = cert.NotAfter
			cert.AriExplanation = "ARI not supported"
			return nil
		}

		return err
	}

	window := response.SuggestedWindow
	windowDuration := window.End.Sub(window.Start)
	slog.Debug("Got ARI information", "start", window.Start, "end", window.End)
	if windowDuration >= time.Second {
		cert.AriRenewalTime = window.Start.Add(time.Duration(mathrand.Int64N(int64(windowDuration))))
	} else {
		cert.AriRenewalTime = window.Start
	}
	cert.AriNextUpdate = time.Now().Add(response.RetryAfter)
	cert.AriExplanation = response.ExplanationURL

	slog.Info("Updated renewal information", "domain", cert.Subject, "altNames", cert.AltNames, "nextUpdate", cert.AriNextUpdate, "renewalTime", cert.AriRenewalTime, "explanation", response.ExplanationURL)
	return nil
}

func (s *LegoSupplier) MinCertificateValidity() time.Duration {
	return time.Hour * 24 * 30
}

func (s *LegoSupplier) MinStapleValidity() time.Duration {
	return time.Hour * 24
}

// connectWithBackoff calls Connect until it succeeds or the context is cancelled, waiting an exponentially
// increasing amount of time between attempts. The attempted channel is closed once the first attempt has
// finished, whether or not it succeeded.
func (s *LegoSupplier) connectWithBackoff(ctx context.Context, attempted chan<- struct{}) error {
	delay := connectInitialDelay
	for {
		err := s.Connect(ctx)
		if attempted != nil {
			close(attempted)
			attempted = nil
		}

		if err == nil {
			slog.Info("Connected to ACME server", "directory", s.config.DirUrl)
			return nil
		}

		slog.Warn("Unable to connect to ACME server, will retry", "directory", s.config.DirUrl, "error", err, "delay", delay)

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}

		delay = min(delay*2, connectMaxDelay)
	}
}

func (s *LegoSupplier) currentCertifier() (certifier, error) {
	s.certifierMutex.RLock()
	defer s.certifierMutex.RUnlock()

	if s.certifier == nil {
		return nil, errNotConnected
	}
	return s.certifier, nil
}

// acmeUser implements the User interface required by lego for account registration.
type acmeUser struct {
	email   string
	account *acme.ExtendedAccount
	key     crypto.Signer
	eabKid  string
	eabHmac string
}

// savedUser is the on-disk representation of an acmeUser, compatible with the legacy lego v4 format.
type savedUser struct {
	Email        string `json:"email"`
	Registration struct {
		Body acme.Account `json:"body"`
		URI  string       `json:"uri,omitempty"`
	} `json:"registration"`
	Key string `json:"key"`
}

// GetEmail returns the email address of the account.
func (a *acmeUser) GetEmail() string {
	return a.email
}

// GetRegistration returns the registration resource of the account.
func (a *acmeUser) GetRegistration() *acme.ExtendedAccount {
	return a.account
}

// GetPrivateKey returns the private key of the account.
func (a *acmeUser) GetPrivateKey() crypto.Signer {
	return a.key
}

// load attempts to read the cached user details from disk.
// If the user details do not exist, a new private key is created.
func (a *acmeUser) load(path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// No saved data, let's just create a new private key
		slog.Info("No saved user details found, creating a new private key")
		privateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			return fmt.Errorf("unable to generate private key: %w", err)
		}

		a.key = privateKey
		return nil
	} else if err != nil {
		return fmt.Errorf("unable to read saved user data from '%s': %w", path, err)
	}

	var saved savedUser
	if err = json.Unmarshal(b, &saved); err != nil {
		return fmt.Errorf("unable to parse saved user data from '%s': %w", path, err)
	}

	key, err := certcrypto.ParsePEMPrivateKey([]byte(saved.Key))
	if err != nil {
		return fmt.Errorf("unable to decode saved user private key: %w", err)
	}

	a.email = saved.Email
	a.account = &acme.ExtendedAccount{
		Account:  saved.Registration.Body,
		Location: saved.Registration.URI,
	}
	a.key = key
	return nil
}

// registerAndSave registers the user with the given ACME registration service, and on successful registration
// serialises the user information to disk at the specified path.
func (a *acmeUser) registerAndSave(ctx context.Context, registrar registrar, path string) error {
	var err error
	if a.eabHmac != "" && a.eabKid != "" {
		slog.Info("Registering user", "email", a.email, "eab", "enabled", "kid", a.eabKid)
		a.account, err = registrar.RegisterWithExternalAccountBinding(
			ctx,
			registration.RegisterEABOptions{
				TermsOfServiceAgreed: true,
				Kid:                  a.eabKid,
				HmacEncoded:          a.eabHmac,
			})
	} else {
		if a.eabHmac != "" || a.eabKid != "" {
			slog.Warn("Incomplete external account binding configuration. Proceeding without EAB.", "hmac_present", a.eabHmac != "", "kid_present", a.eabKid != "")
		}
		slog.Info("Registering user", "email", a.email, "eab", "disabled")
		a.account, err = registrar.Register(ctx, registration.RegisterOptions{TermsOfServiceAgreed: true})
	}

	if err != nil {
		return fmt.Errorf("unable to register new account: %w", err)
	}

	b, err := json.Marshal(savedUser{
		Email: a.email,
		Registration: struct {
			Body acme.Account "json:\"body\""
			URI  string       "json:\"uri,omitempty\""
		}{
			Body: a.account.Account,
			URI:  a.account.Location,
		},
		Key: string(certcrypto.PEMEncode(a.key)),
	})
	if err != nil {
		return fmt.Errorf("unable to serialize user data: %w", err)
	}

	return os.WriteFile(path, b, 0600)
}

// ParseResolvers parses a comma-separated list of DNS resolver addresses, as accepted by the acme-resolvers
// option. Entries consisting only of whitespace are ignored.
func ParseResolvers(input string) []string {
	var res []string
	parts := strings.Split(input, ",")
	for i := range parts {
		if p := strings.TrimSpace(parts[i]); p != "" {
			res = append(res, p)
		}
	}
	return res
}

// canWriteTo reports whether the current user may write to the given path. If the path does not exist,
// write permission is checked on its parent directory instead, since the file will be created there.
func canWriteTo(path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return unix.Access(filepath.Dir(path), unix.W_OK)
	} else {
		return unix.Access(path, unix.W_OK)
	}
}

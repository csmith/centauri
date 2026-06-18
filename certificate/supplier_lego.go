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
	user      *acmeUser
	certifier certifier
	profile   string
	keyType   certcrypto.KeyType
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
	// Resolvers defines the DNS resolvers to use in place of the system resolvers.
	Resolvers []string
}

// NewLegoSupplier creates a new supplier, registering or retrieving an account with the ACME server as necessary.
func NewLegoSupplier(ctx context.Context, config *LegoSupplierConfig) (*LegoSupplier, error) {
	user := &acmeUser{
		email:   config.Email,
		eabKid:  config.ExternalAccountKid,
		eabHmac: config.ExternalAccountHmac,
	}
	if err := user.load(config.Path); err != nil {
		return nil, err
	}

	legoConfig := lego.NewConfig(user)
	legoConfig.CADirURL = config.DirUrl
	legoConfig.Certificate.OverallRequestLimit = config.OverallRequestLimit

	client, err := lego.NewClient(legoConfig)
	if err != nil {
		return nil, err
	}

	if len(config.Resolvers) > 0 {
		dns01.SetDefaultClient(dns01.NewClient(&dns01.Options{
			RecursiveNameservers: config.Resolvers,
		}))
	}

	if err = client.Challenge.SetDNS01Provider(
		config.DnsProvider,
		dns01.CondOptions(
			config.DisablePropagationCheck,
			dns01.WrapPreCheck(func(ctx context.Context, domain, fqdn, value string, check dns01.PreCheckFunc) (bool, error) {
				slog.Info("Propagation check disabled, not checking DNS at all", "domain", domain, "wait", config.PropagationDelay)

				select {
				case <-time.After(config.PropagationDelay):
				case <-ctx.Done():
					return false, ctx.Err()
				}

				return true, nil
			}),
		),
	); err != nil {
		return nil, err
	}

	if user.account == nil {
		if err = user.registerAndSave(ctx, client.Registration, config.Path); err != nil {
			return nil, err
		}
	}

	s := &LegoSupplier{
		user:      user,
		certifier: client.Certificate,
		profile:   config.Profile,
		keyType:   config.KeyType,
	}

	return s, nil
}

// GetCertificate obtains a new certificate for the given names, and immediately requests a new OCSP staple.
func (s *LegoSupplier) GetCertificate(ctx context.Context, subject string, altNames []string, shouldStaple bool) (*Details, error) {
	slog.Info("Starting ACME process to obtain certificate", "domain", subject, "altNames", altNames)
	res, err := s.certifier.Obtain(ctx, legocert.ObtainRequest{
		Domains:    append([]string{subject}, altNames...),
		Bundle:     true,
		MustStaple: shouldStaple,
		Profile:    s.profile,
		KeyType:    s.keyType,
	})
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
	slog.Info("Updating OCSP staple", "domain", cert.Subject, "altNames", cert.AltNames)
	b, response, err := s.certifier.GetOCSP(ctx, []byte(cert.Certificate))
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
	x509Cert, err := certcrypto.ParsePEMCertificate([]byte(cert.Certificate))
	if err != nil {
		return fmt.Errorf("unable to parse certificate: %w", err)
	}

	response, err := s.certifier.GetRenewalInfo(ctx, x509Cert)

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

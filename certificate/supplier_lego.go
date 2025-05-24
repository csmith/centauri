package certificate

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"log"
	"os"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	legocert "github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
	"golang.org/x/crypto/ocsp"
)

// registrar is the surface we use to interact with lego's account registration API.
type registrar interface {
	Register(options registration.RegisterOptions) (*registration.Resource, error)
}

// certifier is the surface we use to interact with lego's certificate issuance API.
type certifier interface {
	Obtain(request legocert.ObtainRequest) (*legocert.Resource, error)
	GetOCSP(bundle []byte) ([]byte, *ocsp.Response, error)
}

// acmeUser implements the User interface required by lego for account registration.
type acmeUser struct {
	Email        string                 `json:"email"`
	Registration *registration.Resource `json:"registration"`
	Key          string                 `json:"key"`
	key          crypto.PrivateKey
}

// GetEmail returns the email address of the account.
func (a *acmeUser) GetEmail() string {
	return a.Email
}

// GetRegistration returns the registration resource of the account.
func (a *acmeUser) GetRegistration() *registration.Resource {
	return a.Registration
}

// GetPrivateKey returns the private key of the account.
func (a *acmeUser) GetPrivateKey() crypto.PrivateKey {
	return a.key
}

// load attempts to read the cached user details from disk.
// If the user details do not exist, a new private key is created.
func (a *acmeUser) load(path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// No saved data, let's just create a new private key
		log.Printf("No saved user details found, creating a new private key")
		privateKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		if err != nil {
			return fmt.Errorf("unable to generate private key: %w", err)
		}

		a.key = privateKey
		a.Key = string(certcrypto.PEMEncode(privateKey))
		return nil
	} else if err != nil {
		return fmt.Errorf("unable to read saved user data from '%s': %w", path, err)
	}

	if err = json.Unmarshal(b, &a); err != nil {
		return fmt.Errorf("unable to parse saved user data from '%s': %w", path, err)
	}

	key, err := certcrypto.ParsePEMPrivateKey([]byte(a.Key))
	if err != nil {
		return fmt.Errorf("unable to decode saved user private key: %w", err)
	}

	a.key = key
	return nil
}

// registerAndSave registers the user with the given ACME registration service, and on successful registration
// serialises the user information to disk at the specified path.
func (a *acmeUser) registerAndSave(registrar registrar, path string) error {
	log.Printf("Registering user '%s'", a.Email)
	reg, err := registrar.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return fmt.Errorf("unable to register new account: %w", err)
	}
	a.Registration = reg

	b, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("unable to serialize user data: %w", err)
	}

	return os.WriteFile(path, b, 0600)
}

// LegoSupplier uses a lego client to obtain certificates from an ACME endpoint.
type LegoSupplier struct {
	user      *acmeUser
	certifier certifier
}

// LegoSupplierConfig contains the configuration used to create a new LegoSupplier.
type LegoSupplierConfig struct {
	// Path is the path to a file on disk where registration data may be cached.
	Path string
	// Email is the contact address to supply to the ACME endpoint
	Email string
	// DirUrl is the URL of the ACME endpoint.
	DirUrl string
	// KeyType is the type of key to use when generating a certificate.
	KeyType certcrypto.KeyType
	// DnsProvider is the DNS-01 challenge provider that will verify domain ownership.
	DnsProvider challenge.Provider
	// DisablePropagationCheck instructs the lego client to not bother checking for DNS propagation.
	DisablePropagationCheck bool
}

// NewLegoSupplier creates a new supplier, registering or retrieving an account with the ACME server as necessary.
func NewLegoSupplier(config *LegoSupplierConfig) (*LegoSupplier, error) {
	user := &acmeUser{Email: config.Email}
	if err := user.load(config.Path); err != nil {
		return nil, err
	}

	legoConfig := lego.NewConfig(user)
	legoConfig.CADirURL = config.DirUrl
	legoConfig.Certificate.KeyType = config.KeyType

	client, err := lego.NewClient(legoConfig)
	if err != nil {
		return nil, err
	}

	if err = client.Challenge.SetDNS01Provider(
		config.DnsProvider,
		dns01.CondOption(config.DisablePropagationCheck, dns01.DisableAuthoritativeNssPropagationRequirement()),
	); err != nil {
		return nil, err
	}

	if user.Registration == nil {
		if err = user.registerAndSave(client.Registration, config.Path); err != nil {
			return nil, err
		}
	}

	s := &LegoSupplier{
		user:      user,
		certifier: client.Certificate,
	}

	return s, nil
}

// GetCertificate obtains a new certificate for the given names, and immediately requests a new OCSP staple.
func (s *LegoSupplier) GetCertificate(subject string, altNames []string, shouldStaple bool) (*Details, error) {
	log.Printf("Obtaining certificate for '%s' (altNames: %v)", subject, altNames)
	res, err := s.certifier.Obtain(legocert.ObtainRequest{
		Domains:    append([]string{subject}, altNames...),
		Bundle:     true,
		MustStaple: shouldStaple,
	})
	if err != nil {
		return nil, err
	}

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
		if err = s.UpdateStaple(details); err != nil {
			return nil, fmt.Errorf("unable to get OCSP staple for certificate: %w", err)
		}
	}

	return details, nil
}

// UpdateStaple requests a new OCSP staple for the given certificate.
func (s *LegoSupplier) UpdateStaple(cert *Details) error {
	log.Printf("Updating OCSP staple for '%s' (altNames: %v)", cert.Subject, cert.AltNames)
	b, response, err := s.certifier.GetOCSP([]byte(cert.Certificate))
	if err != nil {
		return err
	}

	if response == nil || response.Status != ocsp.Good {
		return fmt.Errorf("OCSP response was not good")
	}

	cert.OcspResponse = b
	cert.NextOcspUpdate = response.NextUpdate
	return nil
}

func (s *LegoSupplier) MinCertificateValidity() time.Duration {
	return time.Hour * 24 * 30
}

func (s *LegoSupplier) MinStapleValidity() time.Duration {
	return time.Hour * 24
}

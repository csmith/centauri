package certificate

import (
	"crypto/tls"
	"fmt"
	"log"
	"time"
)

// Store provides functions to get and store certificates.
type Store interface {
	GetCertificate(subject string, altNames []string) *Details
	SaveCertificate(cert *Details) error
}

// Supplier provides new certificates.
type Supplier interface {
	GetCertificate(subject string, altNames []string) (*Details, error)
}

// Stapler updates the OCSP stape of certificates.
type Stapler interface {
	UpdateStaple(cert *Details) error
}

// Manager is responsible for co-ordinating a certificate store and supplier, providing a means to obtain a valid
// certificate with an OCSP staple.
type Manager struct {
	store    Store
	supplier Supplier
	stapler  Stapler

	minCertValidity   time.Duration
	minStapleValidity time.Duration
}

// NewManager returns a new certificate manager backed by the given store and supplier.
func NewManager(store Store, supplier Supplier, stapler Stapler, minCertValidity time.Duration, minStapleValidity time.Duration) *Manager {
	return &Manager{
		store:             store,
		supplier:          supplier,
		stapler:           stapler,
		minCertValidity:   minCertValidity,
		minStapleValidity: minStapleValidity,
	}
}

// GetCertificate returns a certificate for the given subject and alternate names. This may take some time if a new
// certificate needs to be obtained, or the OCSP staple needs to be updated.
func (m *Manager) GetCertificate(subject string, altNames []string) (*tls.Certificate, error) {
	if cert := m.store.GetCertificate(subject, altNames); cert == nil {
		log.Printf("Obtaining new certificate for '%s'", subject)
		return m.obtain(subject, altNames)
	} else if !cert.ValidFor(m.minCertValidity) {
		log.Printf("Renewing certificate for '%s'", subject)
		return m.obtain(subject, altNames)
	} else if !cert.HasStapleFor(m.minStapleValidity) {
		log.Printf("Obtaining new OCSP staple for '%s'", subject)
		return m.staple(cert)
	} else {
		return cert.keyPair()
	}
}

// obtain gets a new certificate and saves it to the store.
func (m *Manager) obtain(subject string, altNames []string) (*tls.Certificate, error) {
	cert, err := m.supplier.GetCertificate(subject, altNames)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain certificate for %s: %w", subject, err)
	}

	if err := m.store.SaveCertificate(cert); err != nil {
		return nil, fmt.Errorf("failed to save certificate for %s: %s", subject, err)
	}

	return cert.keyPair()
}

// staple updates the OCSP staple for the cert and saves it in the store.
func (m *Manager) staple(cert *Details) (*tls.Certificate, error) {
	if err := m.stapler.UpdateStaple(cert); err != nil {
		return nil, fmt.Errorf("failed to obtain OCSP staple for %s: %w", cert.Subject, err)
	}

	if err := m.store.SaveCertificate(cert); err != nil {
		return nil, fmt.Errorf("failed to save certificate for %s: %s", cert.Subject, err)
	}

	return cert.keyPair()
}

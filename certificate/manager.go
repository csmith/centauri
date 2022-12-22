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

// Supplier provides new certificates and OCSP staples.
type Supplier interface {
	GetCertificate(subject string, altNames []string) (*Details, error)
	UpdateStaple(cert *Details) error
	MinCertificateValidity() time.Duration
	MinStapleValidity() time.Duration
}

// Manager is responsible for co-ordinating a certificate store and supplier, providing a means to obtain a valid
// certificate with an OCSP staple.
type Manager struct {
	store              Store
	suppliers          map[string]Supplier
	supplierPreference []string
}

// NewManager returns a new certificate manager backed by the given store and supplier.
func NewManager(store Store, suppliers map[string]Supplier, supplierPreference []string) *Manager {
	return &Manager{
		store:              store,
		suppliers:          suppliers,
		supplierPreference: supplierPreference,
	}
}

// GetCertificate returns a certificate for the given subject and alternate names. This may take some time if a new
// certificate needs to be obtained, or the OCSP staple needs to be updated.
func (m *Manager) GetCertificate(preferredSupplier string, subject string, altNames []string) (*tls.Certificate, error) {
	supplier, err := m.supplier(preferredSupplier)
	if err != nil {
		return nil, err
	}

	if cert := m.store.GetCertificate(subject, altNames); cert == nil {
		log.Printf("Obtaining new certificate for '%s'", subject)
		return m.obtain(supplier, subject, altNames)
	} else if !cert.ValidFor(supplier.MinCertificateValidity()) {
		log.Printf("Renewing certificate for '%s'", subject)
		return m.obtain(supplier, subject, altNames)
	} else if !cert.HasStapleFor(supplier.MinStapleValidity()) {
		log.Printf("Obtaining new OCSP staple for '%s'", subject)
		return m.staple(supplier, cert)
	} else {
		return cert.keyPair()
	}
}

func (m *Manager) supplier(preferred string) (Supplier, error) {
	if preferred != "" {
		s, ok := m.suppliers[preferred]
		if !ok {
			return nil, fmt.Errorf("requested supplier not found: %v", preferred)
		}
		return s, nil
	}

	for i := range m.supplierPreference {
		s, ok := m.suppliers[m.supplierPreference[i]]
		if ok {
			return s, nil
		}
	}

	return nil, fmt.Errorf("no suppliers found for preference: %v", m.supplierPreference)
}

// obtain gets a new certificate and saves it to the store.
func (m *Manager) obtain(supplier Supplier, subject string, altNames []string) (*tls.Certificate, error) {
	cert, err := supplier.GetCertificate(subject, altNames)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain certificate for %s: %w", subject, err)
	}

	if err := m.store.SaveCertificate(cert); err != nil {
		return nil, fmt.Errorf("failed to save certificate for %s: %s", subject, err)
	}

	return cert.keyPair()
}

// staple updates the OCSP staple for the cert and saves it in the store.
func (m *Manager) staple(supplier Supplier, cert *Details) (*tls.Certificate, error) {
	if err := supplier.UpdateStaple(cert); err != nil {
		return nil, fmt.Errorf("failed to obtain OCSP staple for %s: %w", cert.Subject, err)
	}

	if err := m.store.SaveCertificate(cert); err != nil {
		return nil, fmt.Errorf("failed to save certificate for %s: %s", cert.Subject, err)
	}

	return cert.keyPair()
}

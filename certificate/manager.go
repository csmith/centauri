package certificate

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"time"
)

// Store provides functions to get and store certificates.
type Store interface {
	GetCertificate(subject string, altNames []string) *Details
	SaveCertificate(cert *Details) error
	LockCertificate(subjectName string, altNames []string)
	UnlockCertificate(subjectName string, altNames []string)
}

// Supplier provides new certificates and OCSP staples.
type Supplier interface {
	GetCertificate(subject string, altNames []string, shouldStaple bool) (*Details, error)
	UpdateStaple(cert *Details) error
	MinCertificateValidity() time.Duration
	MinStapleValidity() time.Duration
}

// Manager is responsible for co-ordinating a certificate store and supplier, providing a means to obtain a valid
// certificate with an OCSP staple.
type Manager struct {
	shouldStaple       bool
	store              Store
	suppliers          map[string]Supplier
	supplierPreference []string
}

// NewManager returns a new certificate manager backed by the given store and supplier.
func NewManager(store Store, suppliers map[string]Supplier, supplierPreference []string, shouldStaple bool) *Manager {
	return &Manager{
		shouldStaple:       shouldStaple,
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

	m.store.LockCertificate(subject, altNames)
	defer m.store.UnlockCertificate(subject, altNames)

	if cert := m.store.GetCertificate(subject, altNames); cert == nil {
		slog.Info("Obtaining new certificate", "domain", subject, "altNames", altNames)
		return m.obtain(supplier, subject, altNames)
	} else if !cert.ValidFor(supplier.MinCertificateValidity()) {
		slog.Info("Renewing certificate", "domain", subject, "altNames", altNames)
		return m.obtain(supplier, subject, altNames)
	} else if cert.RequiresStaple() && !cert.HasStapleFor(supplier.MinStapleValidity()) {
		slog.Info("Obtaining new OCSP staple", "domain", subject, "altNames", altNames)
		return m.staple(supplier, cert)
	} else {
		return cert.keyPair()
	}
}

// GetExistingCertificate returns a previously saved certificate with the given subject and alternate names if it is
// still valid. It also indicates whether the certificate is in need of renewal or not. Certificates should be renewed
// by calling GetCertificate, which will block and return the new certificate.
func (m *Manager) GetExistingCertificate(preferredSupplier string, subject string, altNames []string) (*tls.Certificate, bool, error) {
	supplier, err := m.supplier(preferredSupplier)
	if err != nil {
		return nil, false, err
	}

	if cert := m.store.GetCertificate(subject, altNames); cert == nil {
		return nil, true, fmt.Errorf("no stored certificate found")
	} else if !cert.ValidFor(0) || (cert.RequiresStaple() && !cert.HasStapleFor(0)) {
		return nil, true, fmt.Errorf("certificate has expired")
	} else {
		key, err := cert.keyPair()
		needRenewal := !cert.ValidFor(supplier.MinCertificateValidity()) || (cert.RequiresStaple() && !cert.HasStapleFor(supplier.MinStapleValidity()))
		return key, needRenewal, err
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
	cert, err := supplier.GetCertificate(subject, altNames, m.shouldStaple)
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

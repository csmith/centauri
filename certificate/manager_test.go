package certificate

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"
)

type fakeStore struct {
	subject      string
	altNames     []string
	certificate  *Details
	savedCert    *Details
	err          error
	locked       bool
	lockedOnSave bool
	lockedOnGet  bool
}

func (f *fakeStore) GetCertificate(subject string, altNames []string) *Details {
	f.lockedOnGet = f.locked && subject == f.subject && slices.Equal(altNames, f.altNames)
	f.subject = subject
	f.altNames = altNames
	return f.certificate
}

func (f *fakeStore) SaveCertificate(cert *Details) error {
	f.lockedOnSave = f.locked && cert.Subject == f.subject && slices.Equal(cert.AltNames, f.altNames)
	f.savedCert = cert
	return f.err
}

func (f *fakeStore) LockCertificate(subject string, altNames []string) {
	f.subject = subject
	f.altNames = altNames
	f.locked = true
}

func (f *fakeStore) UnlockCertificate(subject string, altNames []string) {
	f.subject = subject
	f.altNames = altNames
	f.locked = false
}

type fakeSupplier struct {
	certificate *Details
	subject     string
	altNames    []string
	err         error
}

func (f *fakeSupplier) GetCertificate(subject string, altNames []string) (*Details, error) {
	f.subject = subject
	f.altNames = altNames
	return f.certificate, f.err
}

func (f *fakeSupplier) UpdateStaple(cert *Details) error {
	f.certificate = cert
	return f.err
}

func (f *fakeSupplier) MinCertificateValidity() time.Duration {
	return time.Hour * 24
}

func (f *fakeSupplier) MinStapleValidity() time.Duration {
	return time.Hour
}

const (
	certPem = `-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIQIRi6zePL6mKjOipn+dNuaTAKBggqhkjOPQQDAjASMRAw
DgYDVQQKEwdBY21lIENvMB4XDTE3MTAyMDE5NDMwNloXDTE4MTAyMDE5NDMwNlow
EjEQMA4GA1UEChMHQWNtZSBDbzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABD0d
7VNhbWvZLWPuj/RtHFjvtJBEwOkhbN/BnnE8rnZR8+sbwnc/KhCk3FhnpHZnQz7B
5aETbbIgmuvewdjvSBSjYzBhMA4GA1UdDwEB/wQEAwICpDATBgNVHSUEDDAKBggr
BgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MCkGA1UdEQQiMCCCDmxvY2FsaG9zdDo1
NDUzgg4xMjcuMC4wLjE6NTQ1MzAKBggqhkjOPQQDAgNIADBFAiEA2zpJEPQyz6/l
Wf86aX6PepsntZv2GYlA5UpabfT2EZICICpJ5h/iI+i341gBmLiAFQOyTDT+/wQc
6MF9+Yw1Yy0t
-----END CERTIFICATE-----
`
	keyPem = `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIIrYSSNQFaA2Hwf1duRSxKtLYX5CB04fSeQ6tF1aY/PuoAoGCCqGSM49
AwEHoUQDQgAEPR3tU2Fta9ktY+6P9G0cWO+0kETA6SFs38GecTyudlHz6xvCdz8q
EKTcWGekdmdDPsHloRNtsiCa697B2O9IFA==
-----END EC PRIVATE KEY-----
`
	ocspResponse = "Yay it worked. This is not really OCSP."
)

func Test_Manager_GetCertificate_retrievesFromStoreIfValid(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 36),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{certificate: cert}
	supplier := &fakeSupplier{}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	c, err := manager.GetCertificate("", "example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.Equal(t, cert.Certificate, string(certcrypto.PEMEncode(certcrypto.DERCertificateBytes(c.Certificate[0]))))
	assert.Equal(t, cert.PrivateKey, string(certcrypto.PEMEncode(c.PrivateKey)))
	assert.Equal(t, cert.OcspResponse, c.OCSPStaple)
	assert.Equal(t, "example.com", store.subject)
	assert.Equal(t, []string{"example.net"}, store.altNames)
}

func Test_Manager_GetCertificate_updatesStapleIfTooOld(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 36),
		NextOcspUpdate: time.Now(),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{certificate: cert}
	supplier := &fakeSupplier{}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	c, err := manager.GetCertificate("", "example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.Equal(t, cert.Certificate, string(certcrypto.PEMEncode(certcrypto.DERCertificateBytes(c.Certificate[0]))))
	assert.Equal(t, cert.PrivateKey, string(certcrypto.PEMEncode(c.PrivateKey)))
	assert.Equal(t, cert.OcspResponse, c.OCSPStaple)
	assert.Equal(t, cert, supplier.certificate, "should pass certificate to supplier")
	assert.Equal(t, cert, store.savedCert, "should save updated cert")
	assert.Equal(t, "example.com", store.subject)
	assert.Equal(t, []string{"example.net"}, store.altNames)
}

func Test_Manager_GetCertificate_returnsErrorIfStaplingFails(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 36),
		NextOcspUpdate: time.Now(),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{certificate: cert}
	supplier := &fakeSupplier{err: fmt.Errorf("oops")}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	_, err := manager.GetCertificate("", "example.com", []string{"example.net"})
	require.Error(t, err)
}

func Test_Manager_GetCertificate_returnsErrorIfSavingAfterStaplingFails(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 36),
		NextOcspUpdate: time.Now(),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{certificate: cert, err: fmt.Errorf("oops")}
	supplier := &fakeSupplier{}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	_, err := manager.GetCertificate("", "example.com", []string{"example.net"})
	require.Error(t, err)
}

func Test_Manager_GetCertificate_obtainsCertificateIfMissing(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 2),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{}
	supplier := &fakeSupplier{certificate: cert}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	c, err := manager.GetCertificate("", "example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.Equal(t, cert.Certificate, string(certcrypto.PEMEncode(certcrypto.DERCertificateBytes(c.Certificate[0]))))
	assert.Equal(t, cert.PrivateKey, string(certcrypto.PEMEncode(c.PrivateKey)))
	assert.Equal(t, cert.OcspResponse, c.OCSPStaple)
	assert.Equal(t, cert, store.savedCert, "should save new cert")
	assert.Equal(t, "example.com", supplier.subject)
	assert.Equal(t, []string{"example.net"}, supplier.altNames)
}

func Test_Manager_GetCertificate_obtainsCertificateIfValidityTooShort(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now(),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{certificate: cert}
	supplier := &fakeSupplier{certificate: cert}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	c, err := manager.GetCertificate("", "example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.Equal(t, cert.Certificate, string(certcrypto.PEMEncode(certcrypto.DERCertificateBytes(c.Certificate[0]))))
	assert.Equal(t, cert.PrivateKey, string(certcrypto.PEMEncode(c.PrivateKey)))
	assert.Equal(t, cert.OcspResponse, c.OCSPStaple)
	assert.Equal(t, cert, store.savedCert, "should save new cert")
	assert.Equal(t, "example.com", supplier.subject)
	assert.Equal(t, []string{"example.net"}, supplier.altNames)
}

func Test_Manager_GetCertificate_returnsErrorIfSupplierFails(t *testing.T) {
	store := &fakeStore{}
	supplier := &fakeSupplier{err: fmt.Errorf("oops")}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	_, err := manager.GetCertificate("", "example.com", []string{"example.net"})
	require.Error(t, err)
}

func Test_Manager_GetCertificate_returnsErrorIfSavingNewCertFails(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 2),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{err: fmt.Errorf("oops")}
	supplier := &fakeSupplier{certificate: cert}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	_, err := manager.GetCertificate("", "example.com", []string{"example.net"})
	require.Error(t, err)
}

func Test_Manager_GetCertificate_usesPreferredSupplierIfSpecified(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 2),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{}
	supplier := &fakeSupplier{certificate: cert}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier, "other": &fakeSupplier{}},
		[]string{"other"},
	)

	c, err := manager.GetCertificate("test", "example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.Equal(t, cert.Certificate, string(certcrypto.PEMEncode(certcrypto.DERCertificateBytes(c.Certificate[0]))))
	assert.Equal(t, cert.PrivateKey, string(certcrypto.PEMEncode(c.PrivateKey)))
	assert.Equal(t, cert.OcspResponse, c.OCSPStaple)
	assert.Equal(t, cert, store.savedCert, "should save new cert")
	assert.Equal(t, "example.com", supplier.subject)
	assert.Equal(t, []string{"example.net"}, supplier.altNames)
}

func Test_Manager_GetCertificate_errorsIfPreferredSupplierNotFound(t *testing.T) {
	manager := NewManager(
		&fakeStore{},
		map[string]Supplier{"test": &fakeSupplier{}, "other": &fakeSupplier{}},
		[]string{"other"},
	)

	_, err := manager.GetCertificate("another", "example.com", []string{"example.net"})
	require.Error(t, err)
}

func Test_Manager_GetCertificate_errorsIfNoSuppliersFound(t *testing.T) {
	manager := NewManager(
		&fakeStore{},
		map[string]Supplier{"test": &fakeSupplier{}, "other": &fakeSupplier{}},
		[]string{"one", "two", "three"},
	)

	_, err := manager.GetCertificate("", "example.com", []string{"example.net"})
	require.Error(t, err)
}

func Test_Manager_GetCertificate_usesSupplierPreferenceIfPreferredSupplierNotSpecified(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 2),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{}
	supplier := &fakeSupplier{certificate: cert}

	manager := &Manager{
		store:              store,
		suppliers:          map[string]Supplier{"test": supplier, "other": &fakeSupplier{}},
		supplierPreference: []string{"missing", "rubbish", "test", "other"},
	}

	c, err := manager.GetCertificate("", "example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.Equal(t, cert.Certificate, string(certcrypto.PEMEncode(certcrypto.DERCertificateBytes(c.Certificate[0]))))
	assert.Equal(t, cert.PrivateKey, string(certcrypto.PEMEncode(c.PrivateKey)))
	assert.Equal(t, cert.OcspResponse, c.OCSPStaple)
	assert.Equal(t, cert, store.savedCert, "should save new cert")
	assert.Equal(t, "example.com", supplier.subject)
	assert.Equal(t, []string{"example.net"}, supplier.altNames)
}

func Test_Manager_GetCertificate_acquiresLockWhenGettingCert(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 36),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{certificate: cert}
	supplier := &fakeSupplier{}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	_, err := manager.GetCertificate("", "example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.True(t, store.lockedOnGet)
}

func Test_Manager_GetCertificate_releasesLockOnCompletion(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 36),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{certificate: cert}
	supplier := &fakeSupplier{}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	_, err := manager.GetCertificate("", "example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.False(t, store.locked)
}

func Test_Manager_GetCertificate_holdsLockWhenSaving(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 2),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
		Subject:        "example.com",
		AltNames:       []string{"example.net"},
	}

	store := &fakeStore{}
	supplier := &fakeSupplier{certificate: cert}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	_, err := manager.GetCertificate("", "example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.True(t, store.lockedOnSave)
}

func Test_Manager_GetExistingCertificate_retrievesFromStoreWhenValid(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 36),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{certificate: cert}
	supplier := &fakeSupplier{}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	c, r, err := manager.GetExistingCertificate("", "example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.Equal(t, cert.Certificate, string(certcrypto.PEMEncode(certcrypto.DERCertificateBytes(c.Certificate[0]))))
	assert.Equal(t, cert.PrivateKey, string(certcrypto.PEMEncode(c.PrivateKey)))
	assert.Equal(t, cert.OcspResponse, c.OCSPStaple)
	assert.False(t, r)
}

func Test_Manager_GetExistingCertificate_retrievesFromStoreWhenExpiringSoon(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 12),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{certificate: cert}
	supplier := &fakeSupplier{}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	c, r, err := manager.GetExistingCertificate("", "example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.Equal(t, cert.Certificate, string(certcrypto.PEMEncode(certcrypto.DERCertificateBytes(c.Certificate[0]))))
	assert.Equal(t, cert.PrivateKey, string(certcrypto.PEMEncode(c.PrivateKey)))
	assert.Equal(t, cert.OcspResponse, c.OCSPStaple)
	assert.True(t, r)
}

func Test_Manager_GetExistingCertificate_retrievesFromStoreWhenNeedsStableSoon(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 36),
		NextOcspUpdate: time.Now().Add(time.Minute * 30),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{certificate: cert}
	supplier := &fakeSupplier{}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	c, r, err := manager.GetExistingCertificate("", "example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.Equal(t, cert.Certificate, string(certcrypto.PEMEncode(certcrypto.DERCertificateBytes(c.Certificate[0]))))
	assert.Equal(t, cert.PrivateKey, string(certcrypto.PEMEncode(c.PrivateKey)))
	assert.Equal(t, cert.OcspResponse, c.OCSPStaple)
	assert.True(t, r)
}

func Test_Manager_GetExistingCertificate_errorsWhenExpired(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * -12),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{certificate: cert}
	supplier := &fakeSupplier{}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	_, r, err := manager.GetExistingCertificate("", "example.com", []string{"example.net"})
	require.Error(t, err)
	assert.True(t, r)
}

func Test_Manager_GetExistingCertificate_errorsWhenNotStapled(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 36),
		NextOcspUpdate: time.Now().Add(time.Hour * -2),
		Certificate:    certPem,
		PrivateKey:     keyPem,
		OcspResponse:   []byte(ocspResponse),
	}

	store := &fakeStore{certificate: cert}
	supplier := &fakeSupplier{}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	_, r, err := manager.GetExistingCertificate("", "example.com", []string{"example.net"})
	require.Error(t, err)
	assert.True(t, r)
}

func Test_Manager_GetExistingCertificate_errorsWhenNoCertificateExists(t *testing.T) {
	store := &fakeStore{certificate: nil}
	supplier := &fakeSupplier{}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	_, r, err := manager.GetExistingCertificate("", "example.com", []string{"example.net"})
	require.Error(t, err)
	assert.True(t, r)
}

func Test_Manager_GetExistingCertificate_errorsWhenSupplierInvalid(t *testing.T) {
	store := &fakeStore{certificate: nil}
	supplier := &fakeSupplier{}

	manager := NewManager(
		store,
		map[string]Supplier{"test": supplier},
		[]string{"test"},
	)

	_, r, err := manager.GetExistingCertificate("blah", "example.com", []string{"example.net"})
	require.Error(t, err)
	assert.False(t, r)
}

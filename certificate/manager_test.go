package certificate

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	subject     string
	altNames    []string
	certificate *Details
	savedCert   *Details
	err         error
}

func (f *fakeStore) GetCertificate(subject string, altNames []string) *Details {
	f.subject = subject
	f.altNames = altNames
	return f.certificate
}

func (f *fakeStore) SaveCertificate(cert *Details) error {
	f.savedCert = cert
	return f.err
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

type fakeStapler struct {
	certificate *Details
	err         error
}

func (f *fakeStapler) UpdateStaple(cert *Details) error {
	f.certificate = cert
	return f.err
}

func Test_Manager_GetCertificate_retrievesFromStoreIfValid(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 2),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
	}

	store := &fakeStore{certificate: cert}

	manager := &Manager{
		store:             store,
		minValidity:       time.Hour,
		minStapleValidity: time.Hour,
	}

	c, err := manager.GetCertificate("example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.Equal(t, cert, c)
	assert.Equal(t, "example.com", store.subject)
	assert.Equal(t, []string{"example.net"}, store.altNames)
}

func Test_Manager_GetCertificate_updatesStapleIfTooOld(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 2),
		NextOcspUpdate: time.Now(),
	}

	store := &fakeStore{certificate: cert}
	stapler := &fakeStapler{}

	manager := &Manager{
		store:             store,
		stapler:           stapler,
		minValidity:       time.Hour,
		minStapleValidity: time.Hour,
	}

	c, err := manager.GetCertificate("example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.Equal(t, cert, c)
	assert.Equal(t, cert, stapler.certificate, "should pass certificate to stapler")
	assert.Equal(t, cert, store.savedCert, "should save updated cert")
	assert.Equal(t, "example.com", store.subject)
	assert.Equal(t, []string{"example.net"}, store.altNames)
}

func Test_Manager_GetCertificate_returnsErrorIfStaplingFails(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 2),
		NextOcspUpdate: time.Now(),
	}

	store := &fakeStore{certificate: cert}
	stapler := &fakeStapler{err: fmt.Errorf("oops")}

	manager := &Manager{
		store:             store,
		stapler:           stapler,
		minValidity:       time.Hour,
		minStapleValidity: time.Hour,
	}

	_, err := manager.GetCertificate("example.com", []string{"example.net"})
	require.Error(t, err)
}

func Test_Manager_GetCertificate_returnsErrorIfSavingAfterStaplingFails(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 2),
		NextOcspUpdate: time.Now(),
	}

	store := &fakeStore{certificate: cert, err: fmt.Errorf("oops")}
	stapler := &fakeStapler{}

	manager := &Manager{
		store:             store,
		stapler:           stapler,
		minValidity:       time.Hour,
		minStapleValidity: time.Hour,
	}

	_, err := manager.GetCertificate("example.com", []string{"example.net"})
	require.Error(t, err)
}

func Test_Manager_GetCertificate_obtainsCertificateIfMissing(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 2),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
	}

	store := &fakeStore{}
	supplier := &fakeSupplier{certificate: cert}

	manager := &Manager{
		store:             store,
		supplier:          supplier,
		minValidity:       time.Hour,
		minStapleValidity: time.Hour,
	}

	c, err := manager.GetCertificate("example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.Equal(t, cert, c)
	assert.Equal(t, cert, store.savedCert, "should save new cert")
	assert.Equal(t, "example.com", supplier.subject)
	assert.Equal(t, []string{"example.net"}, supplier.altNames)
}

func Test_Manager_GetCertificate_obtainsCertificateIfValidityTooShort(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now(),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
	}

	store := &fakeStore{certificate: cert}
	supplier := &fakeSupplier{certificate: cert}

	manager := &Manager{
		store:             store,
		supplier:          supplier,
		minValidity:       time.Hour,
		minStapleValidity: time.Hour,
	}

	c, err := manager.GetCertificate("example.com", []string{"example.net"})
	require.NoError(t, err)
	assert.Equal(t, cert, c)
	assert.Equal(t, cert, store.savedCert, "should save new cert")
	assert.Equal(t, "example.com", supplier.subject)
	assert.Equal(t, []string{"example.net"}, supplier.altNames)
}

func Test_Manager_GetCertificate_returnsErrorIfSupplierFails(t *testing.T) {
	store := &fakeStore{}
	supplier := &fakeSupplier{err: fmt.Errorf("oops")}

	manager := &Manager{
		store:             store,
		supplier:          supplier,
		minValidity:       time.Hour,
		minStapleValidity: time.Hour,
	}

	_, err := manager.GetCertificate("example.com", []string{"example.net"})
	require.Error(t, err)
}

func Test_Manager_GetCertificate_returnsErrorIfSavingNewCertFails(t *testing.T) {
	cert := &Details{
		NotAfter:       time.Now().Add(time.Hour * 2),
		NextOcspUpdate: time.Now().Add(time.Hour * 2),
	}

	store := &fakeStore{err: fmt.Errorf("oops")}
	supplier := &fakeSupplier{certificate: cert}

	manager := &Manager{
		store:             store,
		supplier:          supplier,
		minValidity:       time.Hour,
		minStapleValidity: time.Hour,
	}

	_, err := manager.GetCertificate("example.com", []string{"example.net"})
	require.Error(t, err)
}

package certificate

import (
	"crypto/x509"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_NewProvider_obtainsSelfSignedCertificateIfLegoFails(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "certs.json"))
	require.NoError(t, err)

	// The lego supplier cannot be created because the parent of its user data path does not exist;
	// the provider should fall back to the selfsigned supplier.
	provider := NewProvider(t.Context(), ProviderConfig{
		Store:              store,
		Lego:               &LegoSupplierConfig{Path: filepath.Join(t.TempDir(), "missing", "user.pem")},
		PreferredSuppliers: []string{"lego", "selfsigned"},
	})

	cert, err := provider.GetCertificate(t.Context(), "", "example.com", nil)
	require.NoError(t, err)
	require.NotNil(t, cert)

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, "example.com", leaf.Subject.CommonName)
}

func Test_NewProvider_servesStoredCertificatesFromTheStore(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "certs.json")
	store, err := NewStore(storePath)
	require.NoError(t, err)

	provider := NewProvider(t.Context(), ProviderConfig{
		Store:              store,
		PreferredSuppliers: []string{"selfsigned"},
	})
	_, err = provider.GetCertificate(t.Context(), "", "example.com", nil)
	require.NoError(t, err)

	// A second provider over the same store should find the existing certificate
	reloaded, err := NewStore(storePath)
	require.NoError(t, err)
	provider2 := NewProvider(t.Context(), ProviderConfig{
		Store:              reloaded,
		PreferredSuppliers: []string{"selfsigned"},
	})

	cert, needsRenewal, err := provider2.GetExistingCertificate("", "example.com", nil)
	require.NoError(t, err)
	require.NotNil(t, cert)
	assert.False(t, needsRenewal)
}

func Test_NewProvider_appliesWildcardDomains(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "certs.json"))
	require.NoError(t, err)

	provider := NewProvider(t.Context(), ProviderConfig{
		Store:              store,
		PreferredSuppliers: []string{"selfsigned"},
		WildcardDomains:    []string{"example.com"},
	})

	cert, err := provider.GetCertificate(t.Context(), "", "foo.example.com", nil)
	require.NoError(t, err)
	require.NotNil(t, cert)

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, "*.example.com", leaf.Subject.CommonName)
}

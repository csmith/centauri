package certificate

import (
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_NewProvider_errorsIfLegoSupplierCannotBeCreated(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "certs.json"))
	require.NoError(t, err)

	_, err = NewProvider(
		t.Context(),
		ProviderConfig{
			Store:              store,
			Lego:               &LegoSupplierConfig{Path: filepath.Join(t.TempDir(), "missing", "user.pem")},
			PreferredSuppliers: []string{"lego", "selfsigned"},
		},
	)
	assert.ErrorContains(t, err, "unable to write to path")
}

func Test_NewProvider_doesNotFallBackToSelfSignedWhileLegoIsConnecting(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "certs.json"))
	require.NoError(t, err)

	provider, err := NewProvider(
		t.Context(),
		ProviderConfig{
			Store:              store,
			Lego:               unavailableLegoConfig(t),
			PreferredSuppliers: []string{"lego", "selfsigned"},
		},
	)
	require.NoError(t, err)

	_, err = provider.GetCertificate(t.Context(), "", "example.com", nil)
	assert.ErrorIs(t, err, errNotConnected)
}

func Test_NewProvider_servesStoredLegoCertificatesWhileLegoIsConnecting(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "certs.json"))
	require.NoError(t, err)

	details, err := NewSelfSignedSupplier().GetCertificate(t.Context(), "example.com", nil, false)
	require.NoError(t, err)
	details.Provider = "lego"
	details.NotAfter = time.Now().Add(60 * 24 * time.Hour)
	require.NoError(t, store.SaveCertificate(details))

	provider, err := NewProvider(
		t.Context(),
		ProviderConfig{
			Store:              store,
			Lego:               unavailableLegoConfig(t),
			PreferredSuppliers: []string{"lego", "selfsigned"},
		},
	)
	require.NoError(t, err)

	cert, err := provider.GetCertificate(t.Context(), "", "example.com", nil)
	require.NoError(t, err)
	block, _ := pem.Decode([]byte(details.Certificate))
	require.NotNil(t, block)
	assert.Equal(t, block.Bytes, cert.Certificate[0])
}

func Test_NewProvider_servesStoredCertificatesFromTheStore(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "certs.json")
	store, err := NewStore(storePath)
	require.NoError(t, err)

	provider, err := NewProvider(
		t.Context(),
		ProviderConfig{
			Store:              store,
			PreferredSuppliers: []string{"selfsigned"},
		},
	)
	require.NoError(t, err)
	_, err = provider.GetCertificate(t.Context(), "", "example.com", nil)
	require.NoError(t, err)

	// A second provider over the same store should find the existing certificate
	reloaded, err := NewStore(storePath)
	require.NoError(t, err)
	provider2, err := NewProvider(
		t.Context(),
		ProviderConfig{
			Store:              reloaded,
			PreferredSuppliers: []string{"selfsigned"},
		},
	)
	require.NoError(t, err)

	cert, needsRenewal, err := provider2.GetExistingCertificate("", "example.com", nil)
	require.NoError(t, err)
	require.NotNil(t, cert)
	assert.False(t, needsRenewal)
}

func Test_NewProvider_appliesWildcardDomains(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "certs.json"))
	require.NoError(t, err)

	provider, err := NewProvider(
		t.Context(),
		ProviderConfig{
			Store:              store,
			PreferredSuppliers: []string{"selfsigned"},
			WildcardDomains:    []string{"example.com"},
		},
	)
	require.NoError(t, err)

	cert, err := provider.GetCertificate(t.Context(), "", "foo.example.com", nil)
	require.NoError(t, err)
	require.NotNil(t, cert)

	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, "*.example.com", leaf.Subject.CommonName)
}

// unavailableLegoConfig returns config for a lego supplier whose ACME server never responds successfully.
func unavailableLegoConfig(t *testing.T) *LegoSupplierConfig {
	directory := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			},
		),
	)
	t.Cleanup(directory.Close)

	return &LegoSupplierConfig{
		Path:   filepath.Join(t.TempDir(), "user.pem"),
		DirUrl: directory.URL,
	}
}

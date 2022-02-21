package certificate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_NewStore_returnsErrorIfFileIsUnreadable(t *testing.T) {
	_, err := NewStore(t.TempDir())
	assert.Error(t, err)
}

func Test_NewStore_returnsErrorIfFileCantBeUnmarshalled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	_ = os.WriteFile(path, []byte("{invalid json"), 0600)

	_, err := NewStore(path)
	assert.Error(t, err)
}

func Test_Store_LoadSaveGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "certs.json")
	store, err := NewStore(path)
	require.NoError(t, err, "store should load")

	timestamp := time.Now().Add(time.Hour).UTC()

	cert := &Details{
		Issuer:         "this is the issuer",
		PrivateKey:     "this is the private key",
		Certificate:    "this is the cert",
		Subject:        "subject.example.com",
		AltNames:       []string{"alt1.example.com", "alt2.example.com"},
		NotAfter:       timestamp,
		OcspResponse:   []byte("this is the ocsp response"),
		NextOcspUpdate: timestamp.Add(time.Minute),
	}

	require.NoError(t, store.SaveCertificate(cert), "store should save certificate")

	newStore, err := NewStore(path)
	require.NoError(t, err, "second store should load")

	newCert := newStore.GetCertificate(cert.Subject, cert.AltNames)
	assert.Equal(t, cert, newCert, "certificates should match")
}

func Test_Store_saveCertificate_prunesExpiredCerts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "certs.json")
	store, err := NewStore(path)
	require.NoError(t, err, "store should load")

	certs := []*Details{
		{
			Subject:  "just-expired.example.com",
			NotAfter: time.Now().Add(-time.Hour),
		},
		{
			Subject:  "long-expired.example.com",
			NotAfter: time.Now().Add(-time.Hour * 24 * 365),
		},
		{
			Subject:  "zero-time.example.com",
			NotAfter: time.Time{},
		},
		{
			Subject:  "just-valid.example.com",
			NotAfter: time.Now().Add(time.Hour),
		},
		{
			Subject:  "long-valid.example.com",
			NotAfter: time.Now().Add(time.Hour * 24 * 365),
		},
	}

	for i := range certs {
		require.NoError(t, store.SaveCertificate(certs[i]), "store should save certificate")
	}

	for i := range certs {
		t.Run(certs[i].Subject, func(t *testing.T) {
			hasCert := store.GetCertificate(certs[i].Subject, certs[i].AltNames) != nil
			expectedCert := strings.Contains(certs[i].Subject, "-valid")
			assert.Equal(t, expectedCert, hasCert)
		})
	}
}

func Test_Store_saveCertificate_removesDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "certs.json")
	store, err := NewStore(path)
	require.NoError(t, err, "store should load")

	certs := []*Details{
		{
			Subject:  "example.com",
			NotAfter: time.Now().Add(time.Hour),
		},
		{
			Subject:  "example.com",
			NotAfter: time.Now().Add(time.Hour),
		},
		{
			Subject:  "example.com",
			AltNames: []string{"example.net"},
			NotAfter: time.Now().Add(time.Hour),
		},
		{
			Subject:  "example.com",
			AltNames: []string{"example.net"},
			NotAfter: time.Now().Add(time.Hour),
		},
		{
			Subject:  "example.com",
			AltNames: []string{"example.org"},
			NotAfter: time.Now().Add(time.Hour),
		},
	}

	for i := range certs {
		require.NoError(t, store.SaveCertificate(certs[i]), "store should save certificate")
	}

	assert.Equal(t, 3, len(store.certificates))
}

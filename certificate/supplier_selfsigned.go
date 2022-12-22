package certificate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
)

type SelfSignedSupplier struct {
}

func NewSelfSignedSupplier() *SelfSignedSupplier {
	return &SelfSignedSupplier{}
}

func (s *SelfSignedSupplier) GetCertificate(subject string, altNames []string) (*Details, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Centauri"},
			CommonName:   subject,
		},
		NotBefore: time.Now().Add(-time.Hour),
		NotAfter:  time.Now().Add(time.Hour * 24 * 30),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              append([]string{subject}, altNames...),
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, key.Public(), key)
	if err != nil {
		return nil, err
	}

	return &Details{
		PrivateKey:     string(certcrypto.PEMEncode(key)),
		Certificate:    string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})),
		Subject:        subject,
		AltNames:       altNames,
		NotAfter:       template.NotAfter,
		NextOcspUpdate: time.Now().Add(time.Hour * 24 * 30), // As the cert expires
	}, nil
}

func (s *SelfSignedSupplier) UpdateStaple(_ *Details) error {
	// Shouldn't be called - self-signed certs aren't stapled
	return nil
}

func (s *SelfSignedSupplier) MinCertificateValidity() time.Duration {
	return time.Hour * 24 * 7
}

func (s *SelfSignedSupplier) MinStapleValidity() time.Duration {
	return time.Second
}

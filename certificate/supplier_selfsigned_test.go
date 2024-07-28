package certificate

import (
	"testing"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/stretchr/testify/assert"
)

func Test_SelfSignedSupplier_GetCertificate_returnsCertWithCorrectNames(t *testing.T) {
	supplier := &SelfSignedSupplier{}
	details, err := supplier.GetCertificate("subject.example.com", []string{"alt1.example.com", "alt2.example.com"}, false)

	assert.Nil(t, err)

	cert, err := certcrypto.ParsePEMCertificate([]byte(details.Certificate))

	assert.Nil(t, err)

	assert.Equal(t, "subject.example.com", cert.Subject.CommonName)
	assert.Equal(t, []string{"subject.example.com", "alt1.example.com", "alt2.example.com"}, cert.DNSNames)
}

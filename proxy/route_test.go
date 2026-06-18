package proxy

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Route_CertificateNames_returnsDomainsWhenSubjectNotSet(t *testing.T) {
	route := &Route{
		Domains: []string{"example.com", "www.example.com", "api.example.com"},
	}

	subject, alts := route.CertificateNames()

	assert.Equal(t, "example.com", subject)
	assert.Equal(t, []string{"www.example.com", "api.example.com"}, alts)
}

func Test_Route_CertificateNames_returnsDomainsWhenSubjectEmpty(t *testing.T) {
	route := &Route{
		Domains: []string{"example.com", "www.example.com"},
		Subject: []string{},
	}

	subject, alts := route.CertificateNames()

	assert.Equal(t, "example.com", subject)
	assert.Equal(t, []string{"www.example.com"}, alts)
}

func Test_Route_CertificateNames_returnsSubjectWhenSet(t *testing.T) {
	route := &Route{
		Domains: []string{"example.com", "www.example.com"},
		Subject: []string{"example.com", "*.example.com"},
	}

	subject, alts := route.CertificateNames()

	assert.Equal(t, "example.com", subject)
	assert.Equal(t, []string{"*.example.com"}, alts)
}

func Test_Route_CertificateNames_returnsSingleSubjectWithoutAlts(t *testing.T) {
	route := &Route{
		Domains: []string{"example.com", "www.example.com"},
		Subject: []string{"*.example.com"},
	}

	subject, alts := route.CertificateNames()

	assert.Equal(t, "*.example.com", subject)
	assert.Empty(t, alts)
}

func Test_Route_CertificateNames_returnsSingleDomainWithoutAlts(t *testing.T) {
	route := &Route{
		Domains: []string{"example.com"},
	}

	subject, alts := route.CertificateNames()

	assert.Equal(t, "example.com", subject)
	assert.Empty(t, alts)
}

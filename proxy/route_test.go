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

func Test_Route_ErrorMappingForStatus(t *testing.T) {
	route := &Route{
		ErrorMappings: []ErrorMapping{
			{Status: 404, Upstream: "not-found:8080"},
			{Status: 502, Upstream: "error-pages:8080"},
			{Status: 404, Upstream: "other:8080"},
		},
	}

	assert.Equal(t, &ErrorMapping{Status: 404, Upstream: "not-found:8080"}, route.ErrorMappingForStatus(404))
	assert.Equal(t, &ErrorMapping{Status: 502, Upstream: "error-pages:8080"}, route.ErrorMappingForStatus(502))
	assert.Nil(t, route.ErrorMappingForStatus(500))
	assert.Nil(t, (&Route{}).ErrorMappingForStatus(404))
}

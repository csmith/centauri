package certificate

import (
	"crypto/tls"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeCertManager struct {
	certificate *tls.Certificate
	err         error
	subject     string
	altNames    []string
}

func (f *fakeCertManager) GetCertificate(subject string, altNames []string) (*tls.Certificate, error) {
	f.subject = subject
	f.altNames = altNames
	return f.certificate, f.err
}

var dummyCert = &tls.Certificate{}

func Test_WildcardResolver_passesRequestThroughIfNoDomainsConfigured(t *testing.T) {
	upstream := &fakeCertManager{
		certificate: dummyCert,
		err:         fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, nil)
	cert, err := resolver.GetCertificate("example.com", []string{"foo.example.com", "bar.example.com"})

	assert.Equal(t, upstream.certificate, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "example.com", upstream.subject)
	assert.Equal(t, []string{"foo.example.com", "bar.example.com"}, upstream.altNames)
}

func Test_WildcardResolver_modifiesWildcardDomains(t *testing.T) {
	upstream := &fakeCertManager{
		certificate: dummyCert,
		err:         fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, []string{"example.com", ".example.org"})
	cert, err := resolver.GetCertificate("foo.example.com", []string{"bar.example.org"})

	assert.Equal(t, upstream.certificate, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "*.example.com", upstream.subject)
	assert.Equal(t, []string{"*.example.org"}, upstream.altNames)
}

func Test_WildcardResolver_doesNotModifySubdomains(t *testing.T) {
	upstream := &fakeCertManager{
		certificate: dummyCert,
		err:         fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, []string{"example.com", ".example.org"})
	cert, err := resolver.GetCertificate("foo.bar.example.com", []string{"foo.bar.example.org"})

	assert.Equal(t, upstream.certificate, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "foo.bar.example.com", upstream.subject)
	assert.Equal(t, []string{"foo.bar.example.org"}, upstream.altNames)
}

func Test_WildcardResolver_doesNotModifyRootDomains(t *testing.T) {
	upstream := &fakeCertManager{
		certificate: dummyCert,
		err:         fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, []string{"example.com", ".example.org"})
	cert, err := resolver.GetCertificate("example.com", []string{"example.org"})

	assert.Equal(t, upstream.certificate, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "example.com", upstream.subject)
	assert.Equal(t, []string{"example.org"}, upstream.altNames)
}

func Test_WildcardResolver_doesNotModifyOtherDomains(t *testing.T) {
	upstream := &fakeCertManager{
		certificate: dummyCert,
		err:         fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, []string{"example.com", ".example.org"})
	cert, err := resolver.GetCertificate("example.net", []string{"example.org.example.net"})

	assert.Equal(t, upstream.certificate, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "example.net", upstream.subject)
	assert.Equal(t, []string{"example.org.example.net"}, upstream.altNames)
}

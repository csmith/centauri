package certificate

import (
	"context"
	"crypto/tls"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeCertManager struct {
	certificate  *tls.Certificate
	existingCert *tls.Certificate
	err          error
	supplier     string
	subject      string
	altNames     []string
}

func (f *fakeCertManager) GetCertificate(_ context.Context, preferredSupplier string, subject string, altNames []string) (*tls.Certificate, error) {
	f.supplier = preferredSupplier
	f.subject = subject
	f.altNames = altNames
	return f.certificate, f.err
}

func (f *fakeCertManager) GetExistingCertificate(
	preferredSupplier string,
	subject string,
	altNames []string,
) (*tls.Certificate, error) {
	f.supplier = preferredSupplier
	f.subject = subject
	f.altNames = altNames
	return f.existingCert, f.err
}

var dummyCert = &tls.Certificate{}

func Test_WildcardResolver_GetCertificate_passesRequestThroughIfNoDomainsConfigured(t *testing.T) {
	upstream := &fakeCertManager{
		certificate: dummyCert,
		err:         fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, nil)
	cert, err := resolver.GetCertificate(t.Context(), "supplier", "example.com", []string{"foo.example.com", "bar.example.com"})

	assert.Equal(t, upstream.certificate, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "supplier", upstream.supplier)
	assert.Equal(t, "example.com", upstream.subject)
	assert.Equal(t, []string{"foo.example.com", "bar.example.com"}, upstream.altNames)
}

func Test_WildcardResolver_GetCertificate_modifiesWildcardDomains(t *testing.T) {
	upstream := &fakeCertManager{
		certificate: dummyCert,
		err:         fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, []string{"example.com", ".example.org"})
	cert, err := resolver.GetCertificate(t.Context(), "supplier", "foo.example.com", []string{"bar.example.org"})

	assert.Equal(t, upstream.certificate, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "supplier", upstream.supplier)
	assert.Equal(t, "*.example.com", upstream.subject)
	assert.Equal(t, []string{"*.example.org"}, upstream.altNames)
}

func Test_WildcardResolver_GetCertificate_doesNotModifySubdomains(t *testing.T) {
	upstream := &fakeCertManager{
		certificate: dummyCert,
		err:         fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, []string{"example.com", ".example.org"})
	cert, err := resolver.GetCertificate(t.Context(), "supplier", "foo.bar.example.com", []string{"foo.bar.example.org"})

	assert.Equal(t, upstream.certificate, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "supplier", upstream.supplier)
	assert.Equal(t, "foo.bar.example.com", upstream.subject)
	assert.Equal(t, []string{"foo.bar.example.org"}, upstream.altNames)
}

func Test_WildcardResolver_GetCertificate_doesNotModifyRootDomains(t *testing.T) {
	upstream := &fakeCertManager{
		certificate: dummyCert,
		err:         fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, []string{"example.com", ".example.org"})
	cert, err := resolver.GetCertificate(t.Context(), "supplier", "example.com", []string{"example.org"})

	assert.Equal(t, upstream.certificate, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "supplier", upstream.supplier)
	assert.Equal(t, "example.com", upstream.subject)
	assert.Equal(t, []string{"example.org"}, upstream.altNames)
}

func Test_WildcardResolver_GetCertificate_doesNotModifyOtherDomains(t *testing.T) {
	upstream := &fakeCertManager{
		certificate: dummyCert,
		err:         fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, []string{"example.com", ".example.org"})
	cert, err := resolver.GetCertificate(t.Context(), "supplier", "example.net", []string{"example.org.example.net"})

	assert.Equal(t, upstream.certificate, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "supplier", upstream.supplier)
	assert.Equal(t, "example.net", upstream.subject)
	assert.Equal(t, []string{"example.org.example.net"}, upstream.altNames)
}

func Test_WildcardResolver_GetExistingCertificate_passesRequestThroughIfNoDomainsConfigured(t *testing.T) {
	upstream := &fakeCertManager{
		existingCert: dummyCert,
		err:          fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, nil)
	cert, err := resolver.GetExistingCertificate("supplier", "example.com", []string{"foo.example.com", "bar.example.com"})

	assert.Equal(t, upstream.existingCert, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "supplier", upstream.supplier)
	assert.Equal(t, "example.com", upstream.subject)
	assert.Equal(t, []string{"foo.example.com", "bar.example.com"}, upstream.altNames)
}

func Test_WildcardResolver_GetExistingCertificate_modifiesWildcardDomains(t *testing.T) {
	upstream := &fakeCertManager{
		existingCert: dummyCert,
		err:          fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, []string{"example.com", ".example.org"})
	cert, err := resolver.GetExistingCertificate("supplier", "foo.example.com", []string{"bar.example.org"})

	assert.Equal(t, upstream.existingCert, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "supplier", upstream.supplier)
	assert.Equal(t, "*.example.com", upstream.subject)
	assert.Equal(t, []string{"*.example.org"}, upstream.altNames)
}

func Test_WildcardResolver_GetExistingCertificate_doesNotModifySubdomains(t *testing.T) {
	upstream := &fakeCertManager{
		existingCert: dummyCert,
		err:          fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, []string{"example.com", ".example.org"})
	cert, err := resolver.GetExistingCertificate("supplier", "foo.bar.example.com", []string{"foo.bar.example.org"})

	assert.Equal(t, upstream.existingCert, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "supplier", upstream.supplier)
	assert.Equal(t, "foo.bar.example.com", upstream.subject)
	assert.Equal(t, []string{"foo.bar.example.org"}, upstream.altNames)
}

func Test_WildcardResolver_GetExistingCertificate_doesNotModifyRootDomains(t *testing.T) {
	upstream := &fakeCertManager{
		existingCert: dummyCert,
		err:          fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, []string{"example.com", ".example.org"})
	cert, err := resolver.GetExistingCertificate("supplier", "example.com", []string{"example.org"})

	assert.Equal(t, upstream.existingCert, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "supplier", upstream.supplier)
	assert.Equal(t, "example.com", upstream.subject)
	assert.Equal(t, []string{"example.org"}, upstream.altNames)
}

func Test_WildcardResolver_GetExistingCertificate_doesNotModifyOtherDomains(t *testing.T) {
	upstream := &fakeCertManager{
		existingCert: dummyCert,
		err:          fmt.Errorf("an upstream error, oh my"),
	}
	resolver := NewWildcardResolver(upstream, []string{"example.com", ".example.org"})
	cert, err := resolver.GetExistingCertificate("supplier", "example.net", []string{"example.org.example.net"})

	assert.Equal(t, upstream.existingCert, cert)
	assert.Equal(t, upstream.err, err)
	assert.Equal(t, "supplier", upstream.supplier)
	assert.Equal(t, "example.net", upstream.subject)
	assert.Equal(t, []string{"example.org.example.net"}, upstream.altNames)
}

func Test_WildcardResolver_GetCertificate_removesDuplicateWildcards(t *testing.T) {
	upstream := &fakeCertManager{}
	_, _ = NewWildcardResolver(upstream, []string{"example.com"}).GetCertificate(
		t.Context(),
		"",
		"example.com",
		[]string{"www.example.com", "example.net", "blog.example.com", "shop.example.com"},
	)

	assert.Equal(t, "example.com", upstream.subject)
	assert.Equal(t, []string{"*.example.com", "example.net"}, upstream.altNames)
}

func Test_WildcardResolver_GetCertificate_removesAltNamesMatchingSubject(t *testing.T) {
	upstream := &fakeCertManager{}
	_, _ = NewWildcardResolver(upstream, []string{"example.com"}).GetCertificate(
		t.Context(),
		"",
		"www.example.com",
		[]string{"blog.example.com", "shop.example.com"},
	)

	assert.Equal(t, "*.example.com", upstream.subject)
	assert.Empty(t, upstream.altNames)
}

func Test_WildcardResolver_GetExistingCertificate_removesDuplicateWildcards(t *testing.T) {
	upstream := &fakeCertManager{}
	_, _ = NewWildcardResolver(upstream, []string{"example.com"}).GetExistingCertificate(
		"",
		"www.example.com",
		[]string{"example.com", "blog.example.com", "example.com"},
	)

	assert.Equal(t, "*.example.com", upstream.subject)
	assert.Equal(t, []string{"example.com"}, upstream.altNames)
}

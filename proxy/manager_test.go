package proxy

import (
	"crypto/tls"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func Test_Manager_SetRoutes_returnsErrorIfGetCertificateFails(t *testing.T) {
	certManager := &fakeCertManager{
		err: fmt.Errorf("ruh roh"),
	}

	manager := NewManager(nil, map[string]CertificateProvider{"fake": certManager}, "fake")
	err := manager.SetRoutes([]*Route{{
		Domains: []string{"example.com"},
	}})
	assert.Error(t, err)
	assert.Equal(t, "example.com", certManager.subject)
	assert.Equal(t, []string(nil), certManager.altNames)
}

func Test_Manager_SetRoutes_returnsErrorIfDomainisInvalid(t *testing.T) {
	manager := NewManager(nil, nil, "")
	err := manager.SetRoutes([]*Route{{
		Domains: []string{"example..com"},
	}})
	assert.Error(t, err)
}

func Test_Manager_SetRoutes_requestsWildcardCertificateIfMatching(t *testing.T) {
	certManager := &fakeCertManager{
		err: fmt.Errorf("ruh roh"),
	}

	manager := NewManager([]string{".example.com"}, map[string]CertificateProvider{"fake": certManager}, "fake")
	_ = manager.SetRoutes([]*Route{{
		Domains: []string{"test.example.com"},
	}})
	assert.Equal(t, "*.example.com", certManager.subject)
	assert.EqualValues(t, []string(nil), certManager.altNames)
}

func Test_Manager_SetRoutes_translatesAltNamesToWildcardsIfMatching(t *testing.T) {
	certManager := &fakeCertManager{
		err: fmt.Errorf("ruh roh"),
	}

	manager := NewManager([]string{".example.com"}, map[string]CertificateProvider{"fake": certManager}, "fake")
	_ = manager.SetRoutes([]*Route{{
		Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
	}})
	assert.Equal(t, "test.deep.example.com", certManager.subject)
	assert.EqualValues(t, []string{"*.example.com", "example.com"}, certManager.altNames)
}

func Test_Manager_RouteForDomain_returnsNullIfNoRouteFound(t *testing.T) {
	certManager := &fakeCertManager{
		err: fmt.Errorf("ruh roh"),
	}

	manager := NewManager(nil, map[string]CertificateProvider{"fake": certManager}, "fake")
	res := manager.RouteForDomain("example.com")
	assert.Nil(t, res)
}

func Test_Manager_RouteForDomain_returnsCertificateForDomain(t *testing.T) {
	certManager := &fakeCertManager{
		certificate: dummyCert,
	}

	manager := NewManager(nil, map[string]CertificateProvider{"fake": certManager}, "fake")
	route := &Route{
		Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
	}
	_ = manager.SetRoutes([]*Route{route})

	assert.Equal(t, route, manager.RouteForDomain("example.com"))
	assert.Equal(t, route, manager.RouteForDomain("test.example.com"))
	assert.Equal(t, route, manager.RouteForDomain("test.deep.example.com"))
}

func Test_Manager_CertificateForClient_returnsNullIfNoRouteFound(t *testing.T) {
	certManager := &fakeCertManager{
		err: fmt.Errorf("ruh roh"),
	}

	manager := NewManager(nil, map[string]CertificateProvider{"fake": certManager}, "fake")
	res, err := manager.CertificateForClient(&tls.ClientHelloInfo{ServerName: "example.com"})
	assert.Nil(t, res)
	assert.Nil(t, err)
}

func Test_Manager_CertificateForClient_returnsCertificateForDomain(t *testing.T) {
	certManager := &fakeCertManager{
		certificate: dummyCert,
	}

	manager := NewManager(nil, map[string]CertificateProvider{"fake": certManager}, "fake")
	route := &Route{
		Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
	}
	_ = manager.SetRoutes([]*Route{route})

	res, err := manager.CertificateForClient(&tls.ClientHelloInfo{ServerName: "example.com"})
	assert.Equal(t, dummyCert, res)
	assert.Nil(t, err)

	res, err = manager.CertificateForClient(&tls.ClientHelloInfo{ServerName: "test.example.com"})
	assert.Equal(t, dummyCert, res)
	assert.Nil(t, err)

	res, err = manager.CertificateForClient(&tls.ClientHelloInfo{ServerName: "test.deep.example.com"})
	assert.Equal(t, dummyCert, res)
	assert.Nil(t, err)
}

func Test_Manager_setsCertificateOnRoutes(t *testing.T) {
	certManager := &fakeCertManager{
		certificate: dummyCert,
	}

	manager := NewManager(nil, map[string]CertificateProvider{"fake": certManager}, "fake")
	_ = manager.SetRoutes([]*Route{{
		Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
	}})

	assert.Equal(t, dummyCert, manager.RouteForDomain("example.com").certificate)
	assert.Equal(t, dummyCert, manager.RouteForDomain("test.example.com").certificate)
	assert.Equal(t, dummyCert, manager.RouteForDomain("test.deep.example.com").certificate)
}

func Test_Manager_SetRoutes_removesPreviousRoutes(t *testing.T) {
	certManager := &fakeCertManager{
		certificate: dummyCert,
	}

	manager := NewManager(nil, map[string]CertificateProvider{"fake": certManager}, "fake")
	_ = manager.SetRoutes([]*Route{{
		Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
	}})
	_ = manager.SetRoutes([]*Route{})

	assert.Nil(t, manager.RouteForDomain("example.com"))
	assert.Nil(t, manager.RouteForDomain("test.example.com"))
	assert.Nil(t, manager.RouteForDomain("test.deep.example.com"))
}

func Test_Manager_CheckCertificates_returnsErrorIfGetCertificateFails(t *testing.T) {
	certManager := &fakeCertManager{
		certificate: dummyCert,
	}

	manager := NewManager(nil, map[string]CertificateProvider{"fake": certManager}, "fake")
	_ = manager.SetRoutes([]*Route{{
		Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
	}})

	certManager.err = fmt.Errorf("ruh roh")
	err := manager.CheckCertificates()
	assert.Error(t, err)
}

func Test_Manager_CheckCertificates_usesSupplierSpecifiedByRoute(t *testing.T) {
	certManager1 := &fakeCertManager{}
	certManager2 := &fakeCertManager{}

	manager := NewManager(nil, map[string]CertificateProvider{"f1": certManager1, "f2": certManager2}, "f1")
	_ = manager.SetRoutes([]*Route{{
		Domains:  []string{"test.deep.example.com", "test.example.com", "example.com"},
		Provider: "f2",
	}})

	assert.Equal(t, "", certManager1.subject)
	assert.Equal(t, "test.deep.example.com", certManager2.subject)
}

func Test_Manager_CheckCertificates_fallsBackToDefaultSupplier(t *testing.T) {
	certManager1 := &fakeCertManager{}
	certManager2 := &fakeCertManager{}

	manager := NewManager(nil, map[string]CertificateProvider{"f1": certManager1, "f2": certManager2}, "f2")
	_ = manager.SetRoutes([]*Route{{
		Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
	}})

	assert.Equal(t, "", certManager1.subject)
	assert.Equal(t, "test.deep.example.com", certManager2.subject)
}

func Test_Manager_CheckCertificates_errorsIfProviderNotFound(t *testing.T) {
	certManager1 := &fakeCertManager{}
	certManager2 := &fakeCertManager{}

	manager := NewManager(nil, map[string]CertificateProvider{"f1": certManager1, "f2": certManager2}, "f2")
	err := manager.SetRoutes([]*Route{{
		Domains:  []string{"test.deep.example.com", "test.example.com", "example.com"},
		Provider: "c3p0",
	}})

	assert.Error(t, err)
}

func Test_Manager_CheckCertificates_updatesAllCertificates(t *testing.T) {
	certManager := &fakeCertManager{
		certificate: dummyCert,
	}

	manager := NewManager(nil, map[string]CertificateProvider{"fake": certManager}, "fake")
	_ = manager.SetRoutes([]*Route{{
		Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
	}, {
		Domains: []string{"test.example.net"},
	}})

	newCert := &tls.Certificate{OCSPStaple: []byte("Different!")}
	certManager.certificate = newCert
	err := manager.CheckCertificates()
	require.NoError(t, err)

	assert.Equal(t, newCert, manager.RouteForDomain("example.com").certificate)
	assert.Equal(t, newCert, manager.RouteForDomain("test.example.com").certificate)
	assert.Equal(t, newCert, manager.RouteForDomain("test.deep.example.com").certificate)
	assert.Equal(t, newCert, manager.RouteForDomain("test.example.net").certificate)
}

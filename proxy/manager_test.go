package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
)

type fakeCertManager struct {
	existingCert      *tls.Certificate
	certificate       *tls.Certificate
	existingErr       error
	err               error
	preferredSupplier string
	subject           string
	altNames          []string
}

func (f *fakeCertManager) GetCertificate(_ context.Context, preferredSupplier string, subject string, altNames []string) (*tls.Certificate, error) {
	f.preferredSupplier = preferredSupplier
	f.subject = subject
	f.altNames = altNames
	return f.certificate, f.err
}

func (f *fakeCertManager) GetExistingCertificate(
	preferredSupplier string,
	subject string,
	altNames []string,
) (*tls.Certificate, error) {
	f.preferredSupplier = preferredSupplier
	f.subject = subject
	f.altNames = altNames
	return f.existingCert, f.existingErr
}

var dummyCert = &tls.Certificate{}

func Test_Manager_SetRoutes_doesNotServeRouteIfNoCertificateFound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{
			err:         fmt.Errorf("ruh roh"),
			existingErr: fmt.Errorf("ruh roh"),
		}

		route := &Route{
			Domains: []string{"example.com"},
		}

		manager := NewManager(certManager)
		err := manager.SetRoutes(t.Context(), []*Route{route}, nil)
		synctest.Wait()
		assert.NoError(t, err)
		assert.Equal(t, "example.com", certManager.subject)
		assert.Equal(t, []string{}, certManager.altNames)
		assert.Nil(t, manager.RouteForDomain("example.com"))
	})
}

func Test_Manager_SetRoutes_servesRouteIfOldCertificateFound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{
			err:          fmt.Errorf("ruh roh"),
			existingCert: dummyCert,
		}

		route := &Route{
			Domains: []string{"example.com"},
		}

		manager := NewManager(certManager)
		err := manager.SetRoutes(t.Context(), []*Route{route}, nil)
		synctest.Wait()
		assert.NoError(t, err)
		assert.Equal(t, "example.com", certManager.subject)
		assert.Equal(t, []string{}, certManager.altNames)
		assert.Equal(t, route, manager.RouteForDomain("example.com"))
		assert.Equal(t, dummyCert, route.Certificate())
	})
}

func Test_Manager_SetRoutes_returnsErrorIfDomainIsInvalid(t *testing.T) {
	manager := NewManager(nil)
	err := manager.SetRoutes(
		t.Context(),
		[]*Route{{
			Domains: []string{"example..com"},
		}},
		nil,
	)
	assert.Error(t, err)
}

func Test_Manager_RouteForDomain_returnsNullIfNoRouteFound(t *testing.T) {
	certManager := &fakeCertManager{
		err: fmt.Errorf("ruh roh"),
	}

	manager := NewManager(certManager)
	res := manager.RouteForDomain("example.com")
	assert.Nil(t, res)
}

func Test_Manager_RouteForDomain_returnsFallbackIfSpecified(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{
			certificate: dummyCert,
		}

		manager := NewManager(certManager)
		route := &Route{
			Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
		}
		_ = manager.SetRoutes(t.Context(), []*Route{route}, route)
		synctest.Wait()

		res := manager.RouteForDomain("example.net")
		assert.Equal(t, route, res)
	})
}

func Test_Manager_SetRoutes_obtainsCertificateForFallbackNotInRoutes(t *testing.T) {
	synctest.Test(
		t,
		func(t *testing.T) {
			manager := NewManager(
				&fakeCertManager{
					existingErr: errors.New("ruh roh"),
					certificate: dummyCert,
				},
			)
			fallback := &Route{Domains: []string{"fallback.example.com"}}
			_ = manager.SetRoutes(t.Context(), nil, fallback)
			synctest.Wait()

			assert.Equal(t, fallback, manager.RouteForDomain("example.net"))
		},
	)
}

func Test_Manager_SetRoutes_concurrentWithLookupsOfFallback(t *testing.T) {
	manager := NewManager(nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			_ = manager.SetRoutes(t.Context(), nil, &Route{Domains: []string{"example.com"}})
		}
	}()

	for {
		select {
		case <-done:
			assert.NotNil(t, manager.RouteForDomain("example.net"))
			return
		default:
			manager.RouteForDomain("example.net")
		}
	}
}

func Test_Manager_RouteForDomain_returnsCertificateForDomain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{
			certificate: dummyCert,
		}

		manager := NewManager(certManager)
		route := &Route{
			Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
		}
		_ = manager.SetRoutes(t.Context(), []*Route{route}, nil)
		synctest.Wait()

		assert.Equal(t, route, manager.RouteForDomain("example.com"))
		assert.Equal(t, route, manager.RouteForDomain("test.example.com"))
		assert.Equal(t, route, manager.RouteForDomain("test.deep.example.com"))
	})
}

func Test_Manager_RouteForDomain_matchesCaseInsensitively(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{
			certificate: dummyCert,
		}

		manager := NewManager(certManager)
		route := &Route{
			Domains: []string{"ExAmPlE.com"},
		}
		_ = manager.SetRoutes(t.Context(), []*Route{route}, nil)
		synctest.Wait()

		assert.Equal(t, route, manager.RouteForDomain("example.com"))
		assert.Equal(t, route, manager.RouteForDomain("EXAMPLE.COM"))
	})
}

func Test_Manager_CertificateForClient_returnsNullIfNoRouteFound(t *testing.T) {
	certManager := &fakeCertManager{
		err: fmt.Errorf("ruh roh"),
	}

	manager := NewManager(certManager)
	res, err := manager.CertificateForClient(&tls.ClientHelloInfo{ServerName: "example.com"})
	assert.Nil(t, res)
	assert.NoError(t, err)
}

func Test_Manager_CertificateForClient_returnsCertificateForDomain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{
			certificate: dummyCert,
		}

		manager := NewManager(certManager)
		route := &Route{
			Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
		}
		_ = manager.SetRoutes(t.Context(), []*Route{route}, nil)
		synctest.Wait()
		manager.CheckCertificates(t.Context())

		res, err := manager.CertificateForClient(&tls.ClientHelloInfo{ServerName: "example.com"})
		assert.Equal(t, dummyCert, res)
		assert.NoError(t, err)

		res, err = manager.CertificateForClient(&tls.ClientHelloInfo{ServerName: "test.example.com"})
		assert.Equal(t, dummyCert, res)
		assert.NoError(t, err)

		res, err = manager.CertificateForClient(&tls.ClientHelloInfo{ServerName: "test.deep.example.com"})
		assert.Equal(t, dummyCert, res)
		assert.NoError(t, err)
	})
}

func Test_Manager_CertificateForClient_returnsCertificateForFallbackDomainIfSpecified(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{
			certificate: dummyCert,
		}

		manager := NewManager(certManager)
		route := &Route{
			Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
		}
		_ = manager.SetRoutes(t.Context(), []*Route{route}, route)
		synctest.Wait()
		manager.CheckCertificates(t.Context())

		res, err := manager.CertificateForClient(&tls.ClientHelloInfo{ServerName: "example.net"})
		assert.Equal(t, dummyCert, res)
		assert.NoError(t, err)
	})
}

func Test_Manager_CertificateForClient_returnsErrorIfNoProviderConfigured(t *testing.T) {
	manager := NewManager(nil)
	route := &Route{
		Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
	}
	_ = manager.SetRoutes(t.Context(), []*Route{route}, nil)

	_, err := manager.CertificateForClient(&tls.ClientHelloInfo{ServerName: "example.com"})
	assert.Error(t, err)
}

func Test_Manager_SetRoutes_setsCertificateOnRoutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{
			certificate: dummyCert,
		}

		manager := NewManager(certManager)
		_ = manager.SetRoutes(
			t.Context(),
			[]*Route{{
				Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
			}},
			nil,
		)
		synctest.Wait()
		manager.CheckCertificates(t.Context())

		assert.Equal(t, dummyCert, manager.RouteForDomain("example.com").Certificate())
		assert.Equal(t, dummyCert, manager.RouteForDomain("test.example.com").Certificate())
		assert.Equal(t, dummyCert, manager.RouteForDomain("test.deep.example.com").Certificate())
	})
}

func Test_Manager_SetRoutes_ignoresCertificateIfProviderNotConfigured(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := NewManager(nil)
		_ = manager.SetRoutes(
			t.Context(),
			[]*Route{{
				Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
			}},
			nil,
		)
		synctest.Wait()

		assert.Nil(t, manager.RouteForDomain("example.com").Certificate())
		assert.Nil(t, manager.RouteForDomain("test.example.com").Certificate())
		assert.Nil(t, manager.RouteForDomain("test.deep.example.com").Certificate())
	})
}

func Test_Manager_SetRoutes_removesPreviousRoutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{
			certificate: dummyCert,
		}

		manager := NewManager(certManager)
		_ = manager.SetRoutes(
			t.Context(),
			[]*Route{{
				Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
			}},
			nil,
		)
		synctest.Wait()
		_ = manager.SetRoutes(t.Context(), []*Route{}, nil)
		synctest.Wait()

		assert.Nil(t, manager.RouteForDomain("example.com"))
		assert.Nil(t, manager.RouteForDomain("test.example.com"))
		assert.Nil(t, manager.RouteForDomain("test.deep.example.com"))
	})
}

func Test_Manager_CheckCertificates_doesNotServeUncertifiedRouteIfGetCertificateFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{
			certificate: dummyCert,
		}

		route := &Route{
			Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
		}

		manager := NewManager(certManager)
		_ = manager.SetRoutes(t.Context(), []*Route{route}, nil)
		synctest.Wait()

		route.setCertificate(nil)
		certManager.err = fmt.Errorf("ruh roh")
		certManager.existingErr = fmt.Errorf("ruh roh")
		manager.CheckCertificates(t.Context())
		assert.Nil(t, manager.RouteForDomain("example.com"))
	})
}

func Test_Manager_CheckCertificates_servesRouteIfGetCertificateFails_andPreviousCertificateExists(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{
			certificate:  dummyCert,
			existingCert: dummyCert,
		}

		route := &Route{
			Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
		}

		manager := NewManager(certManager)
		_ = manager.SetRoutes(t.Context(), []*Route{route}, nil)
		synctest.Wait()

		certManager.err = fmt.Errorf("ruh roh")
		manager.CheckCertificates(t.Context())
		assert.Equal(t, route, manager.RouteForDomain("example.com"))
	})
}

func Test_Manager_CheckCertificates_passesSupplierSpecifiedByRoute(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{}

		manager := NewManager(certManager)
		_ = manager.SetRoutes(
			t.Context(),
			[]*Route{{
				Domains:  []string{"test.deep.example.com", "test.example.com", "example.com"},
				Provider: "f2",
			}},
			nil,
		)
		synctest.Wait()

		assert.Equal(t, "f2", certManager.preferredSupplier)
		assert.Equal(t, "test.deep.example.com", certManager.subject)
	})
}

func Test_Manager_SetRoutes_usesSubjectWhenSet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{}

		manager := NewManager(certManager)
		_ = manager.SetRoutes(
			t.Context(),
			[]*Route{{
				Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
				Subject: []string{"example.com", "*.example.com"},
			}},
			nil,
		)
		synctest.Wait()

		assert.Equal(t, "example.com", certManager.subject)
		assert.Equal(t, []string{"*.example.com"}, certManager.altNames)
	})
}

func Test_Manager_SetRoutes_fallsBackToDomainsWhenSubjectNotSet(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{}

		manager := NewManager(certManager)
		_ = manager.SetRoutes(
			t.Context(),
			[]*Route{{
				Domains: []string{"test.deep.example.com", "test.example.com", "example.com"},
			}},
			nil,
		)
		synctest.Wait()

		assert.Equal(t, "test.deep.example.com", certManager.subject)
		assert.Equal(t, []string{"test.example.com", "example.com"}, certManager.altNames)
	})
}

func Test_Manager_CheckCertificates_updatesAllCertificates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{
			certificate: dummyCert,
		}

		manager := NewManager(certManager)
		_ = manager.SetRoutes(
			t.Context(),
			[]*Route{
				{Domains: []string{"test.deep.example.com", "test.example.com", "example.com"}},
				{Domains: []string{"test.example.net"}},
			},
			nil,
		)
		synctest.Wait()

		newCert := &tls.Certificate{OCSPStaple: []byte("Different!")}
		certManager.certificate = newCert
		manager.CheckCertificates(t.Context())

		assert.Equal(t, newCert, manager.RouteForDomain("example.com").Certificate())
		assert.Equal(t, newCert, manager.RouteForDomain("test.example.com").Certificate())
		assert.Equal(t, newCert, manager.RouteForDomain("test.deep.example.com").Certificate())
		assert.Equal(t, newCert, manager.RouteForDomain("test.example.net").Certificate())
	})
}

func Test_Manager_CheckCertificates_servesRoutesIfNoProvider(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := NewManager(nil)
		_ = manager.SetRoutes(
			t.Context(),
			[]*Route{
				{Domains: []string{"test.deep.example.com", "test.example.com", "example.com"}},
				{Domains: []string{"test.example.net"}},
			},
			nil,
		)
		synctest.Wait()

		manager.CheckCertificates(t.Context())
		assert.NotNil(t, manager.RouteForDomain("example.com"))
		assert.NotNil(t, manager.RouteForDomain("test.example.net"))
	})
}

func Test_Manager_MonitorCertificates_checksPeriodicallyUntilCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		certManager := &fakeCertManager{certificate: dummyCert}
		route := &Route{Domains: []string{"example.com"}}

		manager := NewManager(certManager)
		assert.NoError(t, manager.SetRoutes(t.Context(), []*Route{route}, nil))
		synctest.Wait()
		certManager.subject = "" // discard the calls made while the routes were installed

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			manager.MonitorCertificates(ctx, time.Millisecond)
		}()

		time.Sleep(10 * time.Millisecond)
		cancel()
		select {
		case <-done:
		case <-time.After(time.Minute):
			t.Fatal("MonitorCertificates did not return after cancellation")
		}

		assert.Equal(t, "example.com", certManager.subject)
	})
}

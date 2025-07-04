package proxy

import (
	"crypto/tls"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
)

// CertificateProvider defines the interface for providing certificates to a Manager.
type CertificateProvider interface {
	GetCertificate(preferredSupplier string, subject string, altNames []string) (*tls.Certificate, error)
	GetExistingCertificate(preferredSupplier string, subject string, altNames []string) (*tls.Certificate, bool, error)
}

// Manager is responsible for maintaining a set of routes, mapping domains to those routes, and refreshing the
// certificates for those routes.
type Manager struct {
	provider CertificateProvider
	routes   routeMap
	fallback *Route
	lock     *sync.RWMutex
}

// NewManager creates a new route provider. Routes should be set using the SetRoutes method after creation.
// If the provider is nil, then the manager will not obtain certificates and CertificateForClient will always return
// an error.
func NewManager(provider CertificateProvider) *Manager {
	return &Manager{
		provider: provider,
		lock:     &sync.RWMutex{},
	}
}

// SetRoutes replaces all previously registered routes with the given new routes. This func may block while new
// certificates are obtained; during this time the old routes will continue to be served to avoid too much disruption.
//
// If a fallback is specified, then that route will be used for any requests that don't otherwise a route.
func (m *Manager) SetRoutes(newRoutes []*Route, fallback *Route) error {
	for i := range newRoutes {
		m.loadCertificate(newRoutes[i])
	}

	if err := m.routes.Update(newRoutes); err != nil {
		return err
	}

	m.fallback = fallback
	go m.CheckCertificates()
	return nil
}

// loadCertificate attempts to load an existing certificate for use with the given route, to enable it to be served
// immediately without waiting for certificate renewals.
func (m *Manager) loadCertificate(route *Route) {
	if m.provider == nil {
		route.certificateStatus = CertificateNotRequired
		return
	}

	cert, needsRenewal, err := m.provider.GetExistingCertificate(route.Provider, route.Domains[0], route.Domains[1:])
	if err == nil {
		route.certificate = cert
		if needsRenewal {
			slog.Debug("Existing certificate found but it expires soon", "route", route.Domains[0])
			route.certificateStatus = CertificateExpiringSoon
		} else {
			slog.Debug("Existing certificate found", "route", route.Domains[0])
			route.certificateStatus = CertificateGood
		}
	} else {
		slog.Info("No existing certificate found, route will not be served until cert is obtained", "route", route.Domains[0])
		route.certificate = nil
		route.certificateStatus = CertificateMissing
	}
}

// RouteForDomain returns the previously-registered route for the given domain. If no routes match the domain,
// nil is returned.
func (m *Manager) RouteForDomain(domain string) *Route {
	route := m.routeFor(domain)

	if route == nil || route.certificateStatus <= CertificateMissing {
		return nil
	}

	return route
}

// CertificateForClient returns a certificate (if one exists) for the domain specified in the provided
// client hello. If no certificate is available, nil is returned. The error return value is unused, but
// is kept to maintain compatibility with the tls.Config.GetCertificate func signature.
func (m *Manager) CertificateForClient(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if m.provider == nil {
		return nil, fmt.Errorf("this manager does not support obtaining certificates")
	}

	route := m.routeFor(hello.ServerName)
	if route == nil {
		return nil, nil
	}
	return route.certificate, nil
}

// routeFor looks up a route to be used for the given domain. If there is no direct match and a fallback
// route is defined, that will be returned.
func (m *Manager) routeFor(domain string) *Route {
	if match := m.routes.Get(domain); match != nil {
		return match
	}
	return m.fallback
}

// CheckCertificates checks and updates the certificates required for registered routes.
// It should be called periodically to renew certificates and obtain new OCSP staples.
func (m *Manager) CheckCertificates() {
	routes := m.routes.Routes()
	for i := range routes {
		route := routes[i]

		if m.provider == nil {
			route.certificateStatus = CertificateNotRequired
		} else {
			m.updateCert(route)
		}
	}
}

// updateCert updates the certificate for the given route.
func (m *Manager) updateCert(route *Route) {
	cert, err := m.provider.GetCertificate(route.Provider, route.Domains[0], route.Domains[1:])
	if err != nil {
		slog.Error("Failed to update certificate", "route", route.Domains[0], "error", err)
		m.loadCertificate(route)
		return
	}

	route.certificate = cert
	route.certificateStatus = CertificateGood
}

// routeMap maintains a map of domain names to routes, using copy-on-write
// semantics.
type routeMap struct {
	domains atomic.Pointer[map[string]*Route]
	routes  atomic.Pointer[[]*Route]
}

// Update replaces all known routes with the ones provided.
func (r *routeMap) Update(routes []*Route) error {
	newDomains := make(map[string]*Route)
	newRoutes := make([]*Route, len(routes))
	copy(newRoutes, routes)

	for i := range routes {
		route := routes[i]
		for j := range route.Domains {
			if !isDomainName(route.Domains[j]) {
				return fmt.Errorf("invalid domain name: %s", route.Domains[j])
			}

			newDomains[strings.ToLower(route.Domains[j])] = route
		}
	}

	r.domains.Store(&newDomains)
	r.routes.Store(&newRoutes)
	return nil
}

// Get retrieves the route for the given domain, or nil if no such route exists.
func (r *routeMap) Get(domain string) *Route {
	if m := r.domains.Load(); m != nil {
		return (*m)[strings.ToLower(domain)]
	}
	return nil
}

// Routes returns a snapshot of all registered routes.
func (r *routeMap) Routes() []*Route {
	if m := r.routes.Load(); m != nil {
		return *m
	}
	return nil
}

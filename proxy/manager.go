package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"slices"
	"sync/atomic"
	"time"
)

// CertificateProvider defines the interface for providing certificates to a Manager.
type CertificateProvider interface {
	GetCertificate(ctx context.Context, preferredSupplier string, subject string, altNames []string) (*tls.Certificate, error)
	GetExistingCertificate(preferredSupplier string, subject string, altNames []string) (*tls.Certificate, error)
}

// Manager is responsible for maintaining a set of routes, mapping domains to those routes, and refreshing the
// certificates for those routes.
type Manager struct {
	provider CertificateProvider
	routes   routeMap
}

// NewManager creates a new route provider. Routes should be set using the SetRoutes method after creation.
// If the provider is nil, then the manager will not obtain certificates and CertificateForClient will always return
// an error.
func NewManager(provider CertificateProvider) *Manager {
	return &Manager{
		provider: provider,
	}
}

// SetRoutes replaces all previously registered routes with the given new routes. This func may block while new
// certificates are obtained; during this time the old routes will continue to be served to avoid too much disruption.
//
// If a fallback is specified, then that route will be used for any requests that don't otherwise a route.
func (m *Manager) SetRoutes(ctx context.Context, newRoutes []*Route, fallback *Route) error {
	if len(newRoutes) == 0 {
		slog.Warn("No routes configured, Centauri will not proxy anything")
	} else {
		slog.Debug("Configuring proxy manager", "routes", len(newRoutes), "fallback", fallback != nil)
	}

	if fallback != nil && !slices.Contains(newRoutes, fallback) {
		newRoutes = append(slices.Clone(newRoutes), fallback)
	}

	for i := range newRoutes {
		m.loadCertificate(newRoutes[i])
	}

	if err := m.routes.Update(newRoutes, fallback); err != nil {
		return err
	}

	go m.CheckCertificates(ctx)
	return nil
}

// loadCertificate attempts to load an existing certificate for use with the given route, to enable it to be served
// immediately without waiting for certificate renewals.
func (m *Manager) loadCertificate(route *Route) {
	if m.provider == nil {
		return
	}

	primary, alts := route.CertificateNames()
	cert, err := m.provider.GetExistingCertificate(route.Provider, primary, alts)
	if err != nil {
		slog.Info("No existing certificate found, route will not be served until cert is obtained", "route", route.Domains[0])
		cert = nil
	}
	route.setCertificate(cert)
}

// CertificateForClient returns a certificate (if one exists) for the domain specified in the provided
// client hello. If no certificate is available, nil is returned. The error return value is unused, but
// is kept to maintain compatibility with the tls.Config.GetCertificate func signature.
func (m *Manager) CertificateForClient(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if m.provider == nil {
		return nil, fmt.Errorf("this manager does not support obtaining certificates")
	}

	route := m.routes.Get(normaliseHost(hello.ServerName))
	if route == nil {
		return nil, nil
	}
	return route.Certificate(), nil
}

// CheckCertificates checks and updates the certificates required for registered routes.
// It should be called periodically to renew certificates and obtain new OCSP staples.
func (m *Manager) CheckCertificates(ctx context.Context) {
	if m.provider == nil {
		return
	}

	routes := m.routes.Routes()
	for i := range routes {
		m.updateCert(ctx, routes[i])
	}
}

// MonitorCertificates periodically calls CheckCertificates until the context is cancelled.
// It blocks and is intended to be run in its own goroutine.
func (m *Manager) MonitorCertificates(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			slog.Info("Checking for certificate validity...")
			m.CheckCertificates(ctx)
		}
	}
}

// routeForDomain returns the route that can currently serve the given domain, or nil if there is none or it
// doesn't have a certificate yet.
func (m *Manager) routeForDomain(domain string) *Route {
	route := m.routes.Get(domain)

	if route == nil || (m.provider != nil && route.Certificate() == nil) {
		return nil
	}

	return route
}

// updateCert updates the certificate for the given route.
func (m *Manager) updateCert(ctx context.Context, route *Route) {
	primary, alts := route.CertificateNames()
	cert, err := m.provider.GetCertificate(ctx, route.Provider, primary, alts)
	if err != nil {
		slog.Error("Failed to update certificate", "route", route.Domains[0], "error", err)
		m.loadCertificate(route)
		return
	}

	route.setCertificate(cert)
}

// routeMap holds the current set of routes. Each update publishes a new immutable snapshot, so
// readers always see a consistent set of routes, domains and fallback.
type routeMap struct {
	current atomic.Pointer[routeSnapshot]
}

type routeSnapshot struct {
	domains  map[string]*Route
	routes   []*Route
	fallback *Route
}

// Update replaces all known routes and the fallback with the ones provided.
func (r *routeMap) Update(routes []*Route, fallback *Route) error {
	snapshot := &routeSnapshot{
		domains:  make(map[string]*Route),
		routes:   slices.Clone(routes),
		fallback: fallback,
	}

	for i := range routes {
		route := routes[i]
		for j := range route.Domains {
			if !isDomainName(route.Domains[j]) {
				return fmt.Errorf("invalid domain name: %s", route.Domains[j])
			}

			snapshot.domains[normaliseDomain(route.Domains[j])] = route
		}
	}

	r.current.Store(snapshot)
	return nil
}

// Get retrieves the route for the given domain, the fallback if there is no match, or nil if
// there is neither.
func (r *routeMap) Get(domain string) *Route {
	snapshot := r.current.Load()
	if snapshot == nil {
		return nil
	}
	if route := snapshot.domains[normaliseDomain(domain)]; route != nil {
		return route
	}
	return snapshot.fallback
}

func (r *routeMap) Routes() []*Route {
	if snapshot := r.current.Load(); snapshot != nil {
		return snapshot.routes
	}
	return nil
}

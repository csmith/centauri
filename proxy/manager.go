package proxy

import (
	"crypto/tls"
	"strings"
)

// CertificateManager defines the interface for providing certificates to a Manager.
type CertificateManager interface {
	GetCertificate(subject string, altNames []string) (*tls.Certificate, error)
}

// Manager is responsible for maintaining a set of routes, mapping domains to those routes, and refreshing the
// certificates for those routes.
type Manager struct {
	wildcardDomains []string

	certManager CertificateManager

	routes  []*Route
	domains map[string]*Route
}

// NewManager creates a new route manager. Routes should be set using the SetRoutes method after creation.
// Wildcard domains, if provided, MUST each have a leading dot (e.g. ".example.com").
func NewManager(wildcardDomains []string, certManager CertificateManager) *Manager {
	return &Manager{
		wildcardDomains: wildcardDomains,
		certManager:     certManager,
		domains:         make(map[string]*Route),
	}
}

// SetRoutes replaces all previously registered routes with the given new routes. This func may block while new
// certificates are obtained; during this time the old routes will continue to be served to avoid too much disruption.
func (m *Manager) SetRoutes(newRoutes []*Route) error {
	newDomains := make(map[string]*Route)

	for i := range newRoutes {
		route := newRoutes[i]

		if err := m.updateCert(route); err != nil {
			return err
		}

		for i := range route.Domains {
			newDomains[route.Domains[i]] = route
		}
	}

	m.domains = newDomains
	m.routes = newRoutes
	return nil
}

// RouteForDomain returns the previously-registered route for the given domain. If no routes match the domain,
// nil is returned.
func (m *Manager) RouteForDomain(domain string) *Route {
	return m.domains[domain]
}

// CheckCertificates checks and updates the certificates required for registered routes.
// It should be called periodically to renew certificates and obtain new OCSP staples.
func (m *Manager) CheckCertificates() error {
	for i := range m.routes {
		route := m.routes[i]

		if err := m.updateCert(route); err != nil {
			return err
		}
	}

	return nil
}

// updateCert updates the certificate for the given route.
func (m *Manager) updateCert(route *Route) error {
	cert, err := m.certManager.GetCertificate(m.applyWildcard(route.Domains[0]), m.applyWildcards(route.Domains[1:]))
	if err != nil {
		return err
	}

	route.certificate = cert
	return nil
}

// applyWildcards checks each entry in the given slice of domains, replacing it with a wildcard domain if necessary.
func (m *Manager) applyWildcards(domains []string) []string {
	var res []string
	for i := range domains {
		res = append(res, m.applyWildcard(domains[i]))
	}
	return res
}

// applyWildcard tests if any of the configured wildcard domains covers the given domain. If so, it returns the
// matching wildcard domain; otherwise it returns the passed domain unaltered.
func (m *Manager) applyWildcard(domain string) string {
	for i := range m.wildcardDomains {
		prefix := strings.TrimSuffix(domain, m.wildcardDomains[i])
		if prefix != domain && strings.Count(prefix, ".") == 0 {
			return "*" + m.wildcardDomains[i]
		}
	}
	return domain
}

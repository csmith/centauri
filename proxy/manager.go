package proxy

import (
	"crypto/tls"
	"fmt"
	"strings"
)

// CertificateProvider defines the interface for providing certificates to a Manager.
type CertificateProvider interface {
	GetCertificate(subject string, altNames []string) (*tls.Certificate, error)
}

// Manager is responsible for maintaining a set of routes, mapping domains to those routes, and refreshing the
// certificates for those routes.
type Manager struct {
	wildcardDomains []string

	providers       map[string]CertificateProvider
	defaultProvider string

	routes  []*Route
	domains map[string]*Route
}

// NewManager creates a new route provider. Routes should be set using the SetRoutes method after creation.
// Wildcard domains, if provided, MUST each have a leading dot (e.g. ".example.com").
func NewManager(wildcardDomains []string, providers map[string]CertificateProvider, defaultProvider string) *Manager {
	return &Manager{
		wildcardDomains: wildcardDomains,
		providers:       providers,
		defaultProvider: defaultProvider,
		domains:         make(map[string]*Route),
	}
}

// SetRoutes replaces all previously registered routes with the given new routes. This func may block while new
// certificates are obtained; during this time the old routes will continue to be served to avoid too much disruption.
func (m *Manager) SetRoutes(newRoutes []*Route) error {
	newDomains := make(map[string]*Route)

	for i := range newRoutes {
		route := newRoutes[i]

		for j := range route.Domains {
			if !isDomainName(route.Domains[j]) {
				return fmt.Errorf("invalid domain name: %s", route.Domains[j])
			}

			newDomains[route.Domains[j]] = route
		}

		if err := m.updateCert(route); err != nil {
			return err
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

// CertificateForClient returns a certificate (if one exists) for the domain specified in the provided
// client hello. If no certificate is available, nil is returned. The error return value is unused, but
// is kept to maintain compatibility with the tls.Config.GetCertificate func signature.
func (m *Manager) CertificateForClient(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	route := m.domains[hello.ServerName]
	if route == nil {
		return nil, nil
	}
	return route.certificate, nil
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

// selectProvider finds a CertificateProvider appropriate for the given route. If the route specifies its provider,
// that will be used, otherwise the default for this manager will be. If no provider by that name is found, returns
// an error.
func (m *Manager) selectProvider(route *Route) (CertificateProvider, error) {
	provider := route.Provider
	if provider == "" {
		provider = m.defaultProvider
	}

	p, ok := m.providers[provider]
	if !ok {
		return nil, fmt.Errorf("no certificate provider named %s", provider)
	}
	return p, nil
}

// updateCert updates the certificate for the given route.
func (m *Manager) updateCert(route *Route) error {
	provider, err := m.selectProvider(route)
	if err != nil {
		return err
	}

	cert, err := provider.GetCertificate(m.applyWildcard(route.Domains[0]), m.applyWildcards(route.Domains[1:]))
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

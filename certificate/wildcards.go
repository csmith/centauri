package certificate

import (
	"crypto/tls"
	"fmt"
	"strings"
)

// Provider defines the interface for providing certificates to a WildcardResolver.
type Provider interface {
	GetCertificate(preferredSupplier string, subject string, altNames []string) (*tls.Certificate, error)
}

// WildcardResolver wraps around a certificate provider and modifies the domain and altNames
// of any request according to set of wildcard rules.
//
// For example if the domain ".example.com" is treated as a wildcard domain, any certificate
// requests for "foo.example.com", "bar.example.com", etc, will be converted to "*.example.com".
// Requests for "example.com" or "a.b.example.com" will not be modified.
type WildcardResolver struct {
	upstream Provider
	domains  []string
}

// NewWildcardResolver creates a new WildcardResolver that will modify any domain
// in the given list to be wildcards.
func NewWildcardResolver(upstream Provider, domains []string) *WildcardResolver {
	var wildcards []string
	for i := range domains {
		if strings.HasPrefix(domains[i], ".") {
			wildcards = append(wildcards, domains[i])
		} else if len(domains[i]) > 0 {
			wildcards = append(wildcards, fmt.Sprintf(".%s", domains[i]))
		}
	}

	return &WildcardResolver{
		upstream: upstream,
		domains:  wildcards,
	}
}

// GetCertificate returns a certificate from the upstream provider that will cover the
// given subject and altNames, taking into account the configured wildcard domains.
func (w *WildcardResolver) GetCertificate(preferredSupplier string, subject string, altNames []string) (*tls.Certificate, error) {
	return w.upstream.GetCertificate(preferredSupplier, w.applyWildcard(subject), w.applyWildcards(altNames))
}

// applyWildcards checks each entry in the given slice of domains, replacing it with a wildcard domain if necessary.
func (w *WildcardResolver) applyWildcards(domains []string) []string {
	var res []string
	for i := range domains {
		res = append(res, w.applyWildcard(domains[i]))
	}
	return res
}

// applyWildcard tests if any of the configured wildcard domains covers the given domain. If so, it returns the
// matching wildcard domain; otherwise it returns the passed domain unaltered.
func (w *WildcardResolver) applyWildcard(domain string) string {
	for i := range w.domains {
		prefix := strings.TrimSuffix(domain, w.domains[i])
		if prefix != domain && strings.Count(prefix, ".") == 0 {
			return "*" + w.domains[i]
		}
	}
	return domain
}

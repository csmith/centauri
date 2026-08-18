package proxy

import (
	"crypto/tls"
	"sync/atomic"
)

// Route describes one way that a request may be mapped from the original HTTP request to an upstream server.
type Route struct {
	Domains           []string
	Upstreams         []Upstream
	Subject           []string
	Headers           []Header
	ErrorMappings     []ErrorMapping
	Provider          string
	RedirectToPrimary bool

	certificate       atomic.Pointer[tls.Certificate]
	certificateStatus atomic.Int32
}

func (r *Route) Certificate() *tls.Certificate {
	return r.certificate.Load()
}

func (r *Route) setCertificate(cert *tls.Certificate) {
	r.certificate.Store(cert)
}

func (r *Route) CertificateStatus() CertificateStatus {
	return CertificateStatus(r.certificateStatus.Load())
}

func (r *Route) setCertificateStatus(status CertificateStatus) {
	r.certificateStatus.Store(int32(status))
}

func (r *Route) CertificateNames() (string, []string) {
	if len(r.Subject) > 0 {
		return r.Subject[0], r.Subject[1:]
	}
	return r.Domains[0], r.Domains[1:]
}

// ErrorMappingForStatus returns the first error mapping configured for the given status code,
// or nil if there is none.
func (r *Route) ErrorMappingForStatus(status int) *ErrorMapping {
	for i := range r.ErrorMappings {
		if r.ErrorMappings[i].Status == status {
			return &r.ErrorMappings[i]
		}
	}
	return nil
}

// Upstream represents a configured upstream server for a route.
type Upstream struct {
	Host string
}

// CertificateStatus describes the current status of the route's certificate
type CertificateStatus int

const (
	CertificateNotChecked   CertificateStatus = iota // The route has just been initialised, so we don't yet know
	CertificateMissing                               // The certificate is required and no valid one is held
	CertificateExpiringSoon                          // We have a certificate but it needs to be renewed
	CertificateGood                                  // We have a certificate and it is in good order
	CertificateNotRequired                           // We don't have a certificate and are happy about it
)

// HeaderOp determines how a header should be modified.
type HeaderOp int

const (
	HeaderOpDelete  HeaderOp = iota // Deletes all instances of the header
	HeaderOpAdd                     // Adds a new header, regardless of existing ones
	HeaderOpReplace                 // Removes any existing headers of the same name, and adds a new one
	HeaderOpDefault                 // Sets the header if it doesn't already exist, otherwise leaves it alone
)

// Header represents a header that should be modified in the response from upstream.
type Header struct {
	Name      string
	Value     string
	Operation HeaderOp
}

// ErrorMapping describes an upstream that should be used to generate the response for a request
// that results in a particular error status code, either because the upstream returned it or
// because it could not be contacted at all.
type ErrorMapping struct {
	Status   int    // The error status code that triggers the mapping
	Upstream string // The host (and port) of the upstream to fetch the response from
	Path     string // The path to request from the upstream. If empty, the original request path is used.
	RawQuery string // The query string to use when requesting Path, if any
}

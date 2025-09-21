package proxy

import (
	"crypto/tls"
)

// Route describes one way that a request may be mapped from the original HTTP request to an upstream server.
type Route struct {
	Domains           []string
	Upstreams         []Upstream
	Headers           []Header
	Provider          string
	RedirectToPrimary bool

	certificate       *tls.Certificate
	certificateStatus CertificateStatus
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

package proxy

import (
	"crypto/tls"
)

// Route describes one way which a request may be mapped from the original HTTP request to an upstream server.
type Route struct {
	Domains  []string
	Upstream string

	certificate *tls.Certificate
	// TODO: Headers, etc.
}

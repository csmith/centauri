package proxy

import (
	"crypto/tls"
)

// Route describes one way which a request may be mapped from the original HTTP request to an upstream server.
type Route struct {
	Domains  []string
	Upstream string
	Headers  []Header
	Provider string

	certificate *tls.Certificate
}

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

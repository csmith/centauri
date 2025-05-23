package proxy

import (
	"net"
	"net/http"
)

// Decorator modifies a HTTP request in some way before it is proxied.
type Decorator interface {
	Decorate(req *http.Request)
}

type bannedHeaderDecorator struct {
	headers []string
}

// NewBannedHeaderDecorator creates a decorator that removes security related headers supplied by the client.
func NewBannedHeaderDecorator() Decorator {
	return &bannedHeaderDecorator{
		headers: []string{
			// Variety of headers used for passing on the client IP. We don't want to pass on any rubbish clients
			// may send in these headers. Note that we explicitly set (i.e. replace) X-Forwarded-For,
			// X-Forwarded-Proto, and X-Forwarded-Host so they don't need to be included here.
			"X-Real-IP",
			"True-Client-IP",
			"Forwarded",
			"Tailscale-User-Login",
			"Tailscale-User-Name",
			"Tailscale-User-Profile-Pic",
		},
	}
}

func (b *bannedHeaderDecorator) Decorate(req *http.Request) {
	for i := range b.headers {
		req.Header.Del(b.headers[i])
	}
}

type xForwardedForDecorator struct{}

// NewXForwardedForDecorator creates a decorator that sets the X-Forwarded-For and X-Forward-Proto headers
// based on the downstream request.
func NewXForwardedForDecorator() Decorator {
	return &xForwardedForDecorator{}
}

func (x *xForwardedForDecorator) Decorate(req *http.Request) {
	ip, _, _ := net.SplitHostPort(req.RemoteAddr)
	req.Header.Set("X-Forwarded-For", ip)
	req.Header.Set("X-Forwarded-Host", req.Host)
	if req.TLS == nil {
		req.Header.Set("X-Forwarded-Proto", "http")
	} else {
		req.Header.Set("X-Forwarded-Proto", "https")
	}
}

type userAgentDecorator struct{}

// NewUserAgentDecorator creates a decorator that forces a blank user-agent if one wasn't previously set. This
// prevents the Go default user agent being added.
func NewUserAgentDecorator() Decorator {
	return &userAgentDecorator{}
}

func (u *userAgentDecorator) Decorate(req *http.Request) {
	if _, ok := req.Header["User-Agent"]; !ok {
		// explicitly disable User-Agent so it's not set to default value
		req.Header.Set("User-Agent", "")
	}
}

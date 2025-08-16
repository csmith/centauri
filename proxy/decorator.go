package proxy

import (
	"fmt"
	"net"
	"net/http"
)

// Decorator modifies a HTTP request in some way before it is proxied.
// The original, unmodified request is provided in the `in` parameter.
type Decorator interface {
	Decorate(in, out *http.Request)
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

func (b *bannedHeaderDecorator) Decorate(_, out *http.Request) {
	for i := range b.headers {
		out.Header.Del(b.headers[i])
	}
}

type xForwardedForDecorator struct {
	trustedDownstreams []net.IPNet
}

// NewXForwardedForDecorator creates a decorator that sets the X-Forwarded-For and X-Forward-Proto headers
// based on the downstream request.
func NewXForwardedForDecorator(trustedDownstreams []net.IPNet) Decorator {
	return &xForwardedForDecorator{trustedDownstreams: trustedDownstreams}
}

func (x *xForwardedForDecorator) Decorate(in, out *http.Request) {
	ip, _, _ := net.SplitHostPort(out.RemoteAddr)
	trusted := x.trusted(ip)

	if h := in.Header.Get("X-Forwarded-For"); !trusted || h == "" {
		out.Header.Set("X-Forwarded-For", ip)
	} else {
		out.Header.Set("X-Forwarded-For", fmt.Sprintf("%s, %s", h, ip))
	}

	if h := in.Header.Get("X-Forwarded-Host"); !trusted || h == "" {
		out.Header.Set("X-Forwarded-Host", out.Host)
	} else {
		out.Header.Set("X-Forwarded-Host", h)
	}

	if h := in.Header.Get("X-Forwarded-Proto"); !trusted || h == "" {
		if out.TLS == nil {
			out.Header.Set("X-Forwarded-Proto", "http")
		} else {
			out.Header.Set("X-Forwarded-Proto", "https")
		}
	} else {
		out.Header.Set("X-Forwarded-Proto", h)
	}
}

func (x *xForwardedForDecorator) trusted(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for i := range x.trustedDownstreams {
		if x.trustedDownstreams[i].Contains(parsed) {
			return true
		}
	}
	return false
}

type userAgentDecorator struct{}

// NewUserAgentDecorator creates a decorator that forces a blank user-agent if one wasn't previously set. This
// prevents the Go default user agent being added.
func NewUserAgentDecorator() Decorator {
	return &userAgentDecorator{}
}

func (u *userAgentDecorator) Decorate(_, out *http.Request) {
	if _, ok := out.Header["User-Agent"]; !ok {
		// explicitly disable User-Agent so it's not set to default value
		out.Header.Set("User-Agent", "")
	}
}

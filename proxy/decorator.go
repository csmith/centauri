package proxy

import (
	"context"
	"fmt"
	"maps"
	"net"
	"net/http"
	"slices"
	"strings"
)

// Decorator modifies a HTTP request in some way before it is proxied.
// The original, unmodified request is provided in the `in` parameter.
type Decorator interface {
	Decorate(in, out *http.Request)
}

// bannedHeaderPrefixes are lowercase prefixes of client-supplied header names that must never reach
// an upstream. Centauri sets its own values for some of them after sanitising.
var bannedHeaderPrefixes = []string{
	"x-forwarded-",
	"tailscale-",
}

// bannedHeaderNames are lowercase client-supplied header names that must never reach an upstream.
// They are used to pass on the client's address or the original URL, which upstreams may trust.
var bannedHeaderNames = []string{
	"cf-connecting-ip",
	"cf-connecting-ipv6",
	"client-ip",
	"fastly-client-ip",
	"forwarded",
	"true-client-ip",
	"x-client-ip",
	"x-cluster-client-ip",
	"x-original-forwarded-for",
	"x-original-uri",
	"x-original-url",
	"x-originating-ip",
	"x-real-ip",
	"x-remote-addr",
	"x-remote-ip",
	"x-rewrite-url",
}

// bannedHeader reports whether a client-supplied header with the given name must be removed before
// the request is sent to an upstream. Any name containing an underscore is banned, as some upstreams
// treat underscores and hyphens as equivalent.
func bannedHeader(name string) bool {
	if strings.Contains(name, "_") {
		return true
	}
	name = strings.ToLower(name)
	if slices.Contains(bannedHeaderNames, name) {
		return true
	}
	for i := range bannedHeaderPrefixes {
		if strings.HasPrefix(name, bannedHeaderPrefixes[i]) {
			return true
		}
	}
	return false
}

// forwardingHeader reports whether name is an X-Forwarded-* header that may be passed on from a
// trusted downstream.
func forwardingHeader(name string) bool {
	return strings.HasPrefix(strings.ToLower(name), "x-forwarded-") && !strings.Contains(name, "_")
}

// sanitiseHeaders deletes every banned header from h, returning the names it removed.
func sanitiseHeaders(h http.Header) []string {
	var removed []string
	for name := range h {
		if bannedHeader(name) {
			delete(h, name)
			removed = append(removed, name)
		}
	}
	return removed
}

type sanitisedKey struct{}

// withSanitised marks a context as belonging to a request whose headers have been sanitised.
func withSanitised(ctx context.Context) context.Context {
	return context.WithValue(ctx, sanitisedKey{}, true)
}

// isSanitised reports whether the context was marked by withSanitised.
func isSanitised(ctx context.Context) bool {
	v, _ := ctx.Value(sanitisedKey{}).(bool)
	return v
}

type xForwardedForDecorator struct {
	trustedDownstreams []net.IPNet
}

// Decorate sets the X-Forwarded-* headers on out. It must run after the headers have been
// sanitised.
func (x *xForwardedForDecorator) Decorate(in, out *http.Request) {
	maps.Copy(out.Header, x.headers(in, out))
}

// headers returns the X-Forwarded-* headers to send to an upstream. X-Forwarded-For, -Host and
// -Proto are derived from the connection, extending or keeping the values in from a trusted
// downstream. Any other X-Forwarded-* headers are only passed on from trusted downstreams.
func (x *xForwardedForDecorator) headers(in, out *http.Request) http.Header {
	ip, _, _ := net.SplitHostPort(out.RemoteAddr)
	trusted := x.trusted(ip)
	headers := make(http.Header)

	if trusted {
		connection := connectionHeaders(in.Header)
		for name, values := range in.Header {
			if forwardingHeader(name) && !connection[strings.ToLower(name)] {
				headers[http.CanonicalHeaderKey(name)] = slices.Clone(values)
			}
		}
	}

	// Values from a trusted downstream are read from the copies above, so get the same filtering.
	if h := headers.Get("X-Forwarded-For"); !trusted || h == "" {
		headers.Set("X-Forwarded-For", ip)
	} else {
		headers.Set("X-Forwarded-For", fmt.Sprintf("%s, %s", h, ip))
	}

	if h := headers.Get("X-Forwarded-Host"); !trusted || h == "" {
		headers.Set("X-Forwarded-Host", out.Host)
	} else {
		headers.Set("X-Forwarded-Host", h)
	}

	if h := headers.Get("X-Forwarded-Proto"); !trusted || h == "" {
		if out.TLS == nil {
			headers.Set("X-Forwarded-Proto", "http")
		} else {
			headers.Set("X-Forwarded-Proto", "https")
		}
	} else {
		headers.Set("X-Forwarded-Proto", h)
	}

	return headers
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

// connectionHeaders returns the lowercase names listed in h's Connection header. They apply only to
// the connection the request arrived on, so must not be passed on.
func connectionHeaders(h http.Header) map[string]bool {
	names := make(map[string]bool)
	for _, value := range h.Values("Connection") {
		for name := range strings.SplitSeq(value, ",") {
			if name = strings.TrimSpace(name); name != "" {
				names[strings.ToLower(name)] = true
			}
		}
	}
	return names
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

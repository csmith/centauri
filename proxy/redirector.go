package proxy

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
)

// HttpRedirector is a http.Handler that redirects all requests to HTTPS.
type HttpRedirector struct {
}

func (h *HttpRedirector) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	// Remove any port that may be along for the ride
	host, _, err := net.SplitHostPort(request.Host)
	if err != nil {
		host = request.Host
	}

	// Make sure the host isn't garbage
	if !isDomainName(host) {
		slog.Debug("Invalid host header from HTTP client, not redirecting", "host", request.Host)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	targetUrl := url.URL{Scheme: "https", Host: host, Path: request.URL.Path, RawQuery: request.URL.RawQuery}
	http.Redirect(writer, request, targetUrl.String(), http.StatusPermanentRedirect)
}

// RouteProvider is the surface used by DomainRedirector to obtain routes for
// a given domain name.
type RouteProvider interface {
	RouteForDomain(domain string) *Route
}

// DomainRedirector is a http.Handler that redirects requests to a primary
// domain name.
type DomainRedirector struct {
	routeProvider RouteProvider
	next          http.Handler
}

// NewDomainRedirector creates a new DomainRedirector which will obtain routes
// from the given provider. If the request does not need to be redirected, it is
// passed to the `next` handler.
func NewDomainRedirector(provider RouteProvider, next http.Handler) *DomainRedirector {
	return &DomainRedirector{
		routeProvider: provider,
		next:          next,
	}
}

func (d *DomainRedirector) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	host := d.hostForRequest(request)
	route := d.routeProvider.RouteForDomain(host)
	if route != nil && route.RedirectToPrimary && route.Domains[0] != host {
		newAddress := request.URL
		newAddress.Host = route.Domains[0]
		if request.TLS != nil {
			newAddress.Scheme = "https"
		} else {
			newAddress.Scheme = "http"
		}
		http.Redirect(writer, request, newAddress.String(), http.StatusPermanentRedirect)
	} else {
		d.next.ServeHTTP(writer, request)
	}
}

// hostForRequest returns the hostname the given request was for, without any port information.
func (d *DomainRedirector) hostForRequest(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.Host)
	if err != nil {
		return req.Host
	}
	return host
}

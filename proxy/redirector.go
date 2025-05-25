package proxy

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
)

// Redirector is a http.Handler that redirects all requests to HTTPS.
type Redirector struct {
}

func (r *Redirector) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
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

package proxy

import (
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// errorFetchTimeout is how long we wait for an upstream to provide a replacement error response
// before giving up and falling back to the default error handling.
const errorFetchTimeout = 5 * time.Second

// forwardedHeaders are the headers set by our decorators that are copied to error upstreams, so
// that they see the same information about the original request as regular upstreams do.
var forwardedHeaders = []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"}

// newErrorClient creates the client used to fetch responses from error upstreams. It is separate
// from the transport used for proxied requests: error fetches are rare and want a hard timeout
// that regular proxying must not have.
func newErrorClient() *http.Client {
	return &http.Client{
		Timeout: errorFetchTimeout,
		Transport: &http.Transport{
			DisableCompression: true,
		},
	}
}

// replaceWithUpstreamErrorPage replaces the given error response with one fetched from the upstream
// configured for its status code, if there is one. The status code sent to the client is always the
// one from the original response. It returns false - leaving the response untouched - if no error
// mapping is configured for the status code or the mapped upstream could not be contacted.
func (r *Rewriter) replaceWithUpstreamErrorPage(response *http.Response) bool {
	if response.Request == nil || response.Request.URL == nil {
		return false
	}

	replacement := r.fetchErrorPage(response.StatusCode, response.Request)
	if replacement == nil {
		return false
	}

	removeHopByHopHeaders(replacement.Header)
	status, statusCode, request := response.Status, response.StatusCode, response.Request
	if response.Body != nil {
		_ = response.Body.Close()
	}
	*response = *replacement
	response.Status, response.StatusCode, response.Request = status, statusCode, request
	return true
}

// serveUpstreamErrorPage serves a response for the given status code from the upstream configured
// for it on the request's route, if there is one. The client always receives the given status code;
// the upstream provides the headers and body. It returns false if no error mapping is configured or
// the mapped upstream could not be contacted, in which case the caller should fall back to its
// normal error handling.
func (r *Rewriter) serveUpstreamErrorPage(writer http.ResponseWriter, request *http.Request, status int) bool {
	replacement := r.fetchErrorPage(status, request)
	if replacement == nil {
		return false
	}

	defer replacement.Body.Close()
	removeHopByHopHeaders(replacement.Header)
	copyHeaders(writer.Header(), replacement.Header)
	r.rewriteHeaders(writer.Header(), request)
	writer.WriteHeader(status)
	_, _ = io.Copy(writer, replacement.Body)
	return true
}

// fetchErrorPage makes a GET request to the upstream configured for the given status code, using
// the path and query from the original request unless the mapping overrides them. Only the response
// itself is used - its status code is ignored by the caller. It returns nil if there is no mapping,
// no client is configured, or the upstream could not be contacted.
func (r *Rewriter) fetchErrorPage(status int, original *http.Request) *http.Response {
	route := r.provider.RouteForDomain(r.hostForRequest(original))
	if route == nil || r.errorClient == nil {
		return nil
	}

	mapping := route.ErrorMappingForStatus(status)
	if mapping == nil || original.URL == nil {
		return nil
	}

	target := &url.URL{
		Scheme:   "http",
		Host:     mapping.Upstream,
		Path:     original.URL.Path,
		RawPath:  original.URL.RawPath,
		RawQuery: original.URL.RawQuery,
	}
	if mapping.Path != "" {
		target.Path = mapping.Path
		target.RawPath = ""
		target.RawQuery = mapping.RawQuery
	}

	request, err := http.NewRequestWithContext(original.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		slog.Warn("Failed to create error page request", "upstream", mapping.Upstream, "error", err)
		return nil
	}

	for i := range forwardedHeaders {
		if value := original.Header.Get(forwardedHeaders[i]); value != "" {
			request.Header.Set(forwardedHeaders[i], value)
		}
	}

	response, err := r.errorClient.Do(request)
	if err != nil {
		slog.Warn("Failed to fetch error page from upstream", "upstream", mapping.Upstream, "error", err)
		return nil
	}
	return response
}

// copyHeaders copies all headers from src to dst, appending to any existing values.
func copyHeaders(dst, src http.Header) {
	for name, values := range src {
		for i := range values {
			dst.Add(name, values[i])
		}
	}
}

// removeHopByHopHeaders removes headers that relate to the connection between Centauri and the
// error upstream, and must not be passed on to the client. This mirrors what
// httputil.ReverseProxy does for regular upstream responses.
func removeHopByHopHeaders(header http.Header) {
	for _, name := range []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		header.Del(name)
	}
}

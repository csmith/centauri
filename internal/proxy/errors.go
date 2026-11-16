package proxy

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// errorFetchTimeout is how long we wait for an upstream to provide a replacement error response
// before giving up and falling back to the default error handling.
const errorFetchTimeout = 5 * time.Second

// maxErrorRedirects matches the limit http.Client applies by default.
const maxErrorRedirects = 10

// newErrorClient creates the client used to fetch responses from error upstreams. It has a hard
// timeout, and only follows redirects that stay on the same upstream.
func newErrorClient(transport http.RoundTripper) *http.Client {
	return &http.Client{
		Timeout:       errorFetchTimeout,
		CheckRedirect: checkErrorRedirect,
		Transport:     transport,
	}
}

func checkErrorRedirect(request *http.Request, via []*http.Request) error {
	if len(via) >= maxErrorRedirects {
		return fmt.Errorf("stopped after %d redirects", maxErrorRedirects)
	}
	original := via[0].URL
	if request.URL.Scheme != original.Scheme || normaliseAddress(request.URL.Host) != normaliseAddress(original.Host) {
		return fmt.Errorf("refusing to follow redirect from %s to another host: %s", original.Host, request.URL.Redacted())
	}
	return nil
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
	route := DecisionFromContext(original.Context()).Route()
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

	// The request only carries the X-Forwarded-* headers set here, so is marked as sanitised.
	request, err := http.NewRequestWithContext(withSanitised(original.Context()), http.MethodGet, target.String(), nil)
	if err != nil {
		slog.Warn("Failed to create error page request", "upstream", mapping.Upstream, "error", err)
		return nil
	}
	request.Header = r.errorPageForwardedHeaders(original)

	response, err := r.errorClient.Do(request)
	if err != nil {
		slog.Warn("Failed to fetch error page from upstream", "upstream", mapping.Upstream, "error", err)
		return nil
	}
	return response
}

// errorPageForwardedHeaders returns the X-Forwarded-* headers to send to an error upstream, so that
// it sees the same information about the original request as the regular upstream did. If original
// was sent to the upstream, its headers were set by RewriteRequest and are reused. Otherwise it is
// the client's request (for example, if ReverseProxy rejected it before rewriting), and they are
// derived from it in the same way.
func (r *Rewriter) errorPageForwardedHeaders(original *http.Request) http.Header {
	if !isSanitised(original.Context()) {
		return r.forwarding.headers(original, original)
	}

	headers := make(http.Header)
	for name, values := range original.Header {
		if forwardingHeader(name) {
			headers[name] = slices.Clone(values)
		}
	}
	return headers
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
	for name := range connectionHeaders(header) {
		header.Del(name)
	}
	for _, name := range []string{
		"Connection",
		"Proxy-Connection",
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

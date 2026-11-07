package proxy

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Router is a http.Handler that decides how each request should be handled, attaches that
// Decision to the request's context, and then passes the request on accordingly. Everything
// downstream of the Router reads the route from the Decision rather than looking it up again.
type Router struct {
	manager  *Manager
	next     http.Handler
	unrouted http.Handler
}

// NewRouter creates a new Router which will make decisions using the given manager. Requests
// that should be proxied are passed to the `next` handler, requests that don't match a usable
// route are passed to the `unrouted` handler, and requests for a secondary domain of a route
// with RedirectToPrimary set are redirected.
func NewRouter(manager *Manager, next http.Handler, unrouted http.Handler) *Router {
	return &Router{
		manager:  manager,
		next:     next,
		unrouted: unrouted,
	}
}

func (r *Router) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	decision := r.manager.Decide(request)
	request = request.WithContext(withDecision(request.Context(), decision))

	switch decision.outcome {
	case OutcomeProxy:
		r.next.ServeHTTP(writer, request)
	case OutcomeRedirect:
		http.Redirect(writer, request, decision.redirect, http.StatusPermanentRedirect)
	default:
		slog.Debug("No route found for request", "host", request.Host)
		r.unrouted.ServeHTTP(writer, request)
	}
}

// Outcome describes what should be done with a request.
type Outcome int

const (
	// OutcomeUnrouted means the request doesn't match a route that can currently be served.
	OutcomeUnrouted Outcome = iota
	// OutcomeRedirect means the request should be redirected to the route's primary domain.
	OutcomeRedirect
	// OutcomeProxy means the request should be proxied to the decision's upstream.
	OutcomeProxy
)

// Decision is the result of routing a single request. It is made once, as early as possible,
// and stored in the request's context so every later stage acts on the same route and upstream
// even if the configuration is reloaded while the request is in flight.
//
// Decisions can only be made by a Manager. A nil Decision behaves as an unrouted one.
type Decision struct {
	outcome  Outcome
	host     string
	route    *Route
	upstream Upstream
	redirect string
}

// Decide works out how the given request should be handled, using a single consistent view of
// the configured routes. The upstream is chosen here, so it can't change later in the request.
func (m *Manager) Decide(request *http.Request) *Decision {
	host := normaliseHost(request.Host)
	route := m.routeForDomain(host)
	if route == nil || len(route.Upstreams) == 0 {
		return &Decision{outcome: OutcomeUnrouted, host: host}
	}

	if route.RedirectToPrimary && !strings.EqualFold(route.Domains[0], host) {
		target := &url.URL{
			Scheme:   "http",
			Host:     route.Domains[0],
			Path:     request.URL.Path,
			RawPath:  request.URL.RawPath,
			RawQuery: request.URL.RawQuery,
		}
		if request.TLS != nil {
			target.Scheme = "https"
		}
		return &Decision{outcome: OutcomeRedirect, host: host, route: route, redirect: target.String()}
	}

	return &Decision{
		outcome:  OutcomeProxy,
		host:     host,
		route:    route,
		upstream: route.Upstreams[rand.IntN(len(route.Upstreams))],
	}
}

// Outcome returns what should be done with the request.
func (d *Decision) Outcome() Outcome {
	if d == nil {
		return OutcomeUnrouted
	}
	return d.outcome
}

// Host returns the normalised host the request was for: lowercase, with no port or trailing dot.
func (d *Decision) Host() string {
	if d == nil {
		return ""
	}
	return d.host
}

// Route returns the route the request matched, or nil if it was unrouted.
func (d *Decision) Route() *Route {
	if d == nil {
		return nil
	}
	return d.route
}

// Upstream returns the upstream the request should be proxied to. It is only meaningful if the
// outcome is OutcomeProxy.
func (d *Decision) Upstream() Upstream {
	if d == nil {
		return Upstream{}
	}
	return d.upstream
}

// RedirectURL returns the URL the request should be redirected to. It is only meaningful if the
// outcome is OutcomeRedirect.
func (d *Decision) RedirectURL() string {
	if d == nil {
		return ""
	}
	return d.redirect
}

type decisionContextKey struct{}

func withDecision(ctx context.Context, decision *Decision) context.Context {
	return context.WithValue(ctx, decisionContextKey{}, decision)
}

// DecisionFromContext returns the Decision a Router attached to a request's context, or nil if
// there isn't one.
func DecisionFromContext(ctx context.Context) *Decision {
	decision, _ := ctx.Value(decisionContextKey{}).(*Decision)
	return decision
}

// HttpRedirector is a http.Handler that redirects all requests to HTTPS.
type HttpRedirector struct {
}

func (h *HttpRedirector) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	host := normaliseHost(request.Host)

	// Make sure the host isn't garbage
	if !isDomainName(host) {
		slog.Debug("Invalid host header from HTTP client, not redirecting", "host", request.Host)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}

	targetUrl := url.URL{Scheme: "https", Host: host, Path: request.URL.Path, RawQuery: request.URL.RawQuery}
	http.Redirect(writer, request, targetUrl.String(), http.StatusPermanentRedirect)
}

// normaliseHost converts a Host header or SNI server name to the form routes are looked up by:
// lowercase, without any port, brackets or trailing dot.
func normaliseHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	return normaliseDomain(host)
}

func normaliseDomain(domain string) string {
	return strings.TrimSuffix(strings.ToLower(domain), ".")
}

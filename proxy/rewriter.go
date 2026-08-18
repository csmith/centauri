package proxy

import (
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
)

// ParseCIDRList parses a comma-separated list of CIDR ranges, as accepted by the trusted-downstreams
// option. Entries consisting only of whitespace are ignored.
func ParseCIDRList(downstreams string) ([]net.IPNet, error) {
	var res []net.IPNet
	parts := strings.Split(downstreams, ",")
	for i := range parts {
		v := strings.TrimSpace(parts[i])
		if v != "" {
			_, ipNet, err := net.ParseCIDR(v)
			if err != nil {
				return nil, fmt.Errorf("failed to parse CIDR range %q: %w", v, err)
			}
			res = append(res, *ipNet)
		}
	}
	return res, nil
}

// routeProvider is the surface we use to interact with the Manager.
type routeProvider interface {
	RouteForDomain(string) *Route
}

// Rewriter facilitates rewriting HTTP requests and responses according to the routes provided by a Manager.
type Rewriter struct {
	provider    routeProvider
	decorators  []Decorator
	errorClient *http.Client
}

// NewRewriter creates a new Rewriter backed by the given route manager.
func NewRewriter(manager *Manager, trustedDownstreams []net.IPNet) *Rewriter {
	return &Rewriter{
		provider: manager,
		decorators: []Decorator{
			NewXForwardedForDecorator(trustedDownstreams),
			NewBannedHeaderDecorator(),
			NewUserAgentDecorator(),
		},
		errorClient: newErrorClient(),
	}
}

// AddDecorator adds a new Decorator to the chain that is applied to each request.
func (r *Rewriter) AddDecorator(d Decorator) {
	r.decorators = append(r.decorators, d)
}

// RewriteRequest modifies the given request according to the routes provided by the Manager.
// It satisfies the signature of the Rewrite field of httputil.ReverseProxy.
func (r *Rewriter) RewriteRequest(p *httputil.ProxyRequest) {
	route := r.provider.RouteForDomain(r.hostForRequest(p.In))
	if route == nil || len(route.Upstreams) == 0 {
		return
	}

	for i := range r.decorators {
		r.decorators[i].Decorate(p.In, p.Out)
	}

	p.Out.URL.Scheme = "http"
	p.Out.URL.Host = r.selectUpstream(route)
}

// RewriteResponse modifies the given response according to the routes provided by the Manager.
// It satisfies the signature of the ModifyResponse field of httputil.ReverseProxy.
// If the response has an error status code that the route maps to an error upstream, the
// response is replaced with one fetched from that upstream.
func (r *Rewriter) RewriteResponse(response *http.Response) error {
	if response.StatusCode >= 400 {
		r.replaceWithUpstreamErrorPage(response)
	}
	r.rewriteHeaders(response.Header, response.Request)
	return nil
}

// RewriteError handles the reverse proxy being unable to get a usable response from the upstream,
// either because it could not be contacted or because it could not be proxied. It satisfies the
// signature of the ErrorHandler field of httputil.ReverseProxy. If the route maps the status code
// to an error upstream then a response is served from it; otherwise fn is called to serve a
// fallback response.
func (r *Rewriter) RewriteError(fn func(http.ResponseWriter, *http.Request, error)) func(http.ResponseWriter, *http.Request, error) {
	return func(writer http.ResponseWriter, req *http.Request, err error) {
		slog.Warn("Failed to connect to upstream", "host", req.Host, "error", err)

		if r.serveUpstreamErrorPage(writer, req, http.StatusBadGateway) {
			return
		}

		r.rewriteHeaders(writer.Header(), req)
		fn(writer, req, err)
	}
}

// rewriteHeaders adjusts the headers according to the rules in the route.
func (r *Rewriter) rewriteHeaders(headers http.Header, request *http.Request) {
	route := r.provider.RouteForDomain(r.hostForRequest(request))
	if route == nil {
		return
	}

	for i := range route.Headers {
		switch route.Headers[i].Operation {
		case HeaderOpDelete:
			headers.Del(route.Headers[i].Name)
		case HeaderOpAdd:
			headers.Add(route.Headers[i].Name, route.Headers[i].Value)
		case HeaderOpReplace:
			headers.Set(route.Headers[i].Name, route.Headers[i].Value)
		case HeaderOpDefault:
			if headers.Get(route.Headers[i].Name) == "" {
				headers.Set(route.Headers[i].Name, route.Headers[i].Value)
			}
		}
	}
}

// selectUpstream selects an upstream host from the given route. The current implementation simply selects an upstream
// at random.
func (r *Rewriter) selectUpstream(route *Route) string {
	return route.Upstreams[rand.IntN(len(route.Upstreams))].Host
}

// hostForRequest returns the hostname the given request was for, without any port information.
func (r *Rewriter) hostForRequest(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.Host)
	if err != nil {
		return req.Host
	}
	return host
}

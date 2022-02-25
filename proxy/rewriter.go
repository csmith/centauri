package proxy

import (
	"net"
	"net/http"
)

// routeProvider is the surface we use to interact with the Manager.
type routeProvider interface {
	RouteForDomain(string) *Route
}

// Rewriter facilitates rewriting HTTP requests and responses according to the routes provided by a Manager.
type Rewriter struct {
	provider routeProvider
}

// NewRewriter creates a new Rewriter backed by the given route manager.
func NewRewriter(manager *Manager) *Rewriter {
	return &Rewriter{provider: manager}
}

// RewriteRequest modifies the given request according to the routes provided by the Manager.
// It satisfies the signature of the Director field of httputil.ReverseProxy.
func (r *Rewriter) RewriteRequest(req *http.Request) {
	route := r.provider.RouteForDomain(req.TLS.ServerName)
	req.URL.Scheme = "http"
	req.URL.Host = route.Upstream

	ip, _, _ := net.SplitHostPort(req.RemoteAddr)
	req.Header.Set("X-Forwarded-For", ip)
	req.Header.Set("X-Forwarded-Proto", "https")

	if _, ok := req.Header["User-Agent"]; !ok {
		// explicitly disable User-Agent so it's not set to default value
		req.Header.Set("User-Agent", "")
	}
}

// RewriteResponse modifies the given response according to the routes provided by the Manager.
// It satisfies the signature of the ModifyResponse field of httputil.ReverseProxy.
func (r *Rewriter) RewriteResponse(response *http.Response) error {
	return nil
}

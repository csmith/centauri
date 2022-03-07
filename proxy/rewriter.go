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
	provider      routeProvider
	bannedHeaders []string
}

// NewRewriter creates a new Rewriter backed by the given route manager.
func NewRewriter(manager *Manager) *Rewriter {
	return &Rewriter{
		provider: manager,
		bannedHeaders: []string{
			// Variety of headers used for passing on the client IP. We don't want to pass on any rubbish clients
			// may send in these headers. Note that we explicitly set (i.e. replace) X-Forwarded-For and
			// X-Forwarded-Proto, so they don't need to be included here.
			"X-Real-IP",
			"True-Client-IP",
			"X-Forwarded-Host",
			"Forwarded",
		},
	}
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

	for i := range r.bannedHeaders {
		req.Header.Del(r.bannedHeaders[i])
	}

	if _, ok := req.Header["User-Agent"]; !ok {
		// explicitly disable User-Agent so it's not set to default value
		req.Header.Set("User-Agent", "")
	}
}

// RewriteResponse modifies the given response according to the routes provided by the Manager.
// It satisfies the signature of the ModifyResponse field of httputil.ReverseProxy.
func (r *Rewriter) RewriteResponse(response *http.Response) error {
	route := r.provider.RouteForDomain(response.Request.TLS.ServerName)

	for i := range route.Headers {
		switch route.Headers[i].Operation {
		case HeaderOpDelete:
			response.Header.Del(route.Headers[i].Name)
		case HeaderOpAdd:
			response.Header.Add(route.Headers[i].Name, route.Headers[i].Value)
		case HeaderOpReplace:
			response.Header.Set(route.Headers[i].Name, route.Headers[i].Value)
		case HeaderOpDefault:
			if response.Header.Get(route.Headers[i].Name) == "" {
				response.Header.Set(route.Headers[i].Name, route.Headers[i].Value)
			}
		}
	}

	return nil
}

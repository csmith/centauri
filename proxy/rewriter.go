package proxy

import (
	"golang.org/x/exp/rand"
	"net/http"
)

// routeProvider is the surface we use to interact with the Manager.
type routeProvider interface {
	RouteForDomain(string) *Route
}

// Rewriter facilitates rewriting HTTP requests and responses according to the routes provided by a Manager.
type Rewriter struct {
	provider   routeProvider
	decorators []Decorator
}

// NewRewriter creates a new Rewriter backed by the given route manager.
func NewRewriter(manager *Manager) *Rewriter {
	return &Rewriter{
		provider: manager,
		decorators: []Decorator{
			NewXForwardedForDecorator(),
			NewBannedHeaderDecorator(),
			NewUserAgentDecorator(),
		},
	}
}

// RewriteRequest modifies the given request according to the routes provided by the Manager.
// It satisfies the signature of the Director field of httputil.ReverseProxy.
func (r *Rewriter) RewriteRequest(req *http.Request) {
	route := r.provider.RouteForDomain(hostname(req))
	if route == nil || len(route.Upstreams) == 0 {
		return
	}

	for i := range r.decorators {
		r.decorators[i].Decorate(req)
	}

	req.URL.Scheme = "http"
	req.URL.Host = r.selectUpstream(route)
}

// RewriteResponse modifies the given response according to the routes provided by the Manager.
// It satisfies the signature of the ModifyResponse field of httputil.ReverseProxy.
func (r *Rewriter) RewriteResponse(response *http.Response) error {
	route := r.provider.RouteForDomain(hostname(response.Request))
	if route == nil {
		return nil
	}

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

// selectUpstream selects an upstream host from the given route. The current implementation simply selects an upstream
// at random.
func (r *Rewriter) selectUpstream(route *Route) string {
	return route.Upstreams[rand.Intn(len(route.Upstreams))].Host
}

func hostname(req *http.Request) string {
	if req.TLS == nil {
		return req.Host
	} else {
		return req.TLS.ServerName
	}
}

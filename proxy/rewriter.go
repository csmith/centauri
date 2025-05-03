package proxy

import (
	"golang.org/x/exp/rand"
	"net"
	"net/http"
	"net/http/httputil"
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
		r.decorators[i].Decorate(p.Out)
	}

	p.Out.URL.Scheme = "http"
	p.Out.URL.Host = r.selectUpstream(route)
}

// RewriteResponse modifies the given response according to the routes provided by the Manager.
// It satisfies the signature of the ModifyResponse field of httputil.ReverseProxy.
func (r *Rewriter) RewriteResponse(response *http.Response) error {
	r.rewriteHeaders(response.Header, response.Request)
	return nil
}

// RewriteError modifiers the headers in the response according to the rules in the routes.
// It satisfies the signature of the ErrorHandler field of httputil.ReverseProxy
func (r *Rewriter) RewriteError(fn func(http.ResponseWriter, *http.Request, error)) func(http.ResponseWriter, *http.Request, error) {
	return func(writer http.ResponseWriter, req *http.Request, err error) {
		r.rewriteHeaders(req.Header, req)
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
	return route.Upstreams[rand.Intn(len(route.Upstreams))].Host
}

// hostForRequest returns the hostname the given request was for, without any port information.
func (r *Rewriter) hostForRequest(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.Host)
	if err != nil {
		return req.Host
	}
	return host
}

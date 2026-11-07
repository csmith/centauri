package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
)

// ErrDialNotAllowed is returned by AllowlistDialer when asked to connect to an address its policy
// does not allow.
var ErrDialNotAllowed = errors.New("dial to non-upstream address blocked")

// DialContextFunc is the signature of http.Transport.DialContext.
type DialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// AllowlistDialer only opens connections to the upstreams of the route the request was routed
// with, which the Rewriter attaches to the request's context. It is a defence in depth against
// bugs that would otherwise let a client direct Centauri to connect to an arbitrary host: such
// attempts, and any dial without a route in its context, are refused and logged as errors.
//
// The address is checked as given to the dialler, before any DNS resolution. Because the route
// is the one chosen for the request, a config reload while the request is in flight doesn't
// cause its dial to be refused.
type AllowlistDialer struct {
	Dialer net.Dialer
	// OnBlock, if non-nil, is called whenever a connection is refused.
	OnBlock func()
}

// DialContext connects to the given address if it is an upstream of the route in the context.
// It satisfies the signature of http.Transport.DialContext.
func (d *AllowlistDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if route := routeFromContext(ctx); route == nil || !route.allowsDial(addr) {
		slog.Error("Blocked connection to an address that is not a configured upstream", "network", network, "address", addr)
		if d.OnBlock != nil {
			d.OnBlock()
		}
		return nil, fmt.Errorf("%w: %s", ErrDialNotAllowed, addr)
	}
	return d.Dialer.DialContext(ctx, network, addr)
}

type routeContextKey struct{}

func withRoute(ctx context.Context, route *Route) context.Context {
	return context.WithValue(ctx, routeContextKey{}, route)
}

func routeFromContext(ctx context.Context) *Route {
	route, _ := ctx.Value(routeContextKey{}).(*Route)
	return route
}

// normaliseAddress converts an upstream or dial address to a canonical host:port form, so
// that the same address written in different ways compares equal. Hosts are lowercased with
// any trailing dot removed, and a missing port defaults to 80 as it does for http URLs.
func normaliseAddress(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = strings.TrimSuffix(strings.TrimPrefix(addr, "["), "]"), "80"
	}
	return net.JoinHostPort(strings.TrimSuffix(strings.ToLower(host), "."), port)
}

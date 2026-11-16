package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func routedContext(ctx context.Context, route *Route) context.Context {
	return withDecision(ctx, &Decision{outcome: OutcomeProxy, route: route, upstream: route.Upstreams[0]})
}

func Test_AllowlistDialer_connectsToUpstreamOfRouteInContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	blocked := false
	dialer := &AllowlistDialer{OnBlock: func() { blocked = true }}

	conn, err := dialer.DialContext(
		routedContext(t.Context(), &Route{Upstreams: []Upstream{{Host: server.Listener.Addr().String()}}}),
		"tcp",
		server.Listener.Addr().String(),
	)
	require.NoError(t, err)
	_ = conn.Close()
	assert.False(t, blocked)
}

func Test_AllowlistDialer_refusesAddressesNotInRoute(t *testing.T) {
	hit := false
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				hit = true
			},
		),
	)
	defer server.Close()

	blocked := 0
	dialer := &AllowlistDialer{OnBlock: func() { blocked++ }}
	client := &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext}}

	request, err := http.NewRequestWithContext(
		routedContext(t.Context(), &Route{Upstreams: []Upstream{{Host: "upstream:8080"}}}),
		http.MethodGet,
		server.URL,
		nil,
	)
	require.NoError(t, err)
	response, err := client.Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	require.ErrorIs(t, err, ErrDialNotAllowed)
	assert.False(t, hit)
	assert.Equal(t, 1, blocked)
}

func Test_AllowlistDialer_refusesEverythingWithoutRoute(t *testing.T) {
	_, err := (&AllowlistDialer{}).DialContext(t.Context(), "tcp", "upstream:8080")
	require.ErrorIs(t, err, ErrDialNotAllowed)
}

func Test_AllowlistDialer_allowsRoutedRequestAfterUpstreamRemovedByReload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	manager := NewManager(nil)
	require.NoError(
		t,
		manager.SetRoutes(
			t.Context(),
			[]*Route{{Domains: []string{"example.com"}, Upstreams: []Upstream{{Host: server.Listener.Addr().String()}}}},
			nil,
		),
	)

	request := routeRequest(
		t,
		manager,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/", nil),
	)
	proxyRequest := &httputil.ProxyRequest{In: request, Out: request.Clone(request.Context())}
	NewRewriter(nil, nil).RewriteRequest(proxyRequest)

	require.NoError(
		t,
		manager.SetRoutes(
			t.Context(),
			[]*Route{{Domains: []string{"example.com"}, Upstreams: []Upstream{{Host: "other:8080"}}}},
			nil,
		),
	)

	conn, err := (&AllowlistDialer{}).DialContext(proxyRequest.Out.Context(), "tcp", proxyRequest.Out.URL.Host)
	require.NoError(t, err)
	_ = conn.Close()
}

func Test_Decision_allowsDial(t *testing.T) {
	route := &Route{
		Upstreams: []Upstream{{Host: "app:8080"}, {Host: "unchosen:8080"}},
		ErrorMappings: []ErrorMapping{
			{Status: 502, Upstream: "errors.internal:80"},
			{Status: 404, Upstream: "[::1]:9000"},
		},
	}
	decision := &Decision{outcome: OutcomeProxy, route: route, upstream: route.Upstreams[0]}

	tests := []struct {
		addr    string
		allowed bool
	}{
		{"app:8080", true},
		{"APP:8080", true},
		{"app.:8080", true},
		{"app:8081", false},
		{"app:22", false},
		{"app", false},
		{"unchosen:8080", false},
		{"[::1]:9000", true},
		{"[::1]:9001", false},
		{"errors.internal:80", true},
		{"errors.internal", true},
		{"Errors.Internal.:80", true},
		{"errors.internal:443", false},
		{"other:8080", false},
	}
	for _, tt := range tests {
		t.Run(
			tt.addr,
			func(t *testing.T) {
				assert.Equal(t, tt.allowed, decision.allowsDial(tt.addr))
			},
		)
	}
}

func Test_Decision_allowsDial_refusesIfNotProxying(t *testing.T) {
	route := &Route{Upstreams: []Upstream{{Host: "app:8080"}}}
	for _, outcome := range []Outcome{OutcomeUnrouted, OutcomeRedirect} {
		decision := &Decision{outcome: outcome, route: route, upstream: route.Upstreams[0]}
		assert.False(t, decision.allowsDial("app:8080"))
	}
	assert.False(t, (*Decision)(nil).allowsDial("app:8080"))
}

func Test_upstreamTransport_refusesUnsanitisedRequests(t *testing.T) {
	var hit atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit.Store(true) }))
	defer server.Close()

	var blocked []BlockReason
	transport := newUpstreamTransport(func(reason BlockReason) { blocked = append(blocked, reason) })

	route := &Route{Upstreams: []Upstream{{Host: server.Listener.Addr().String()}}}
	request, err := http.NewRequestWithContext(routedContext(t.Context(), route), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	response, err := transport.RoundTrip(request)
	if response != nil {
		_ = response.Body.Close()
	}
	require.ErrorIs(t, err, ErrUnsanitised)
	assert.Nil(t, response)
	assert.False(t, hit.Load())
	assert.Equal(t, []BlockReason{BlockedUnsanitised}, blocked)
}

func Test_upstreamTransport_sendsSanitisedRequestsToUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	transport := newUpstreamTransport(nil)

	route := &Route{Upstreams: []Upstream{{Host: server.Listener.Addr().String()}}}
	request, err := http.NewRequestWithContext(
		withSanitised(routedContext(t.Context(), route)),
		http.MethodGet,
		server.URL,
		nil,
	)
	require.NoError(t, err)

	response, err := transport.RoundTrip(request)
	require.NoError(t, err)
	_ = response.Body.Close()
	assert.Equal(t, http.StatusOK, response.StatusCode)
}

func Test_upstreamTransport_reportsBlockedDials(t *testing.T) {
	var blocked []BlockReason
	transport := newUpstreamTransport(func(reason BlockReason) { blocked = append(blocked, reason) })

	route := &Route{Upstreams: []Upstream{{Host: "upstream:8080"}}}
	request, err := http.NewRequestWithContext(
		withSanitised(routedContext(t.Context(), route)),
		http.MethodGet,
		"http://other:8080/",
		nil,
	)
	require.NoError(t, err)

	response, err := transport.RoundTrip(request)
	if response != nil {
		_ = response.Body.Close()
	}
	require.ErrorIs(t, err, ErrDialNotAllowed)
	assert.Equal(t, []BlockReason{BlockedDial}, blocked)
}

func Test_Rewriter_errorClientUsesUpstreamTransport(t *testing.T) {
	rewriter := NewRewriter(nil, nil)
	assert.Same(t, rewriter.Transport(), rewriter.errorClient.Transport)
}

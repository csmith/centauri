package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_AllowlistDialer_connectsToUpstreamOfRouteInContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	blocked := false
	dialer := &AllowlistDialer{OnBlock: func() { blocked = true }}

	conn, err := dialer.DialContext(
		withRoute(t.Context(), &Route{Upstreams: []Upstream{{Host: server.Listener.Addr().String()}}}),
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
		withRoute(t.Context(), &Route{Upstreams: []Upstream{{Host: "upstream:8080"}}}),
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

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/", nil)
	proxyRequest := &httputil.ProxyRequest{In: request, Out: request.Clone(t.Context())}
	NewRewriter(manager, nil, nil).RewriteRequest(proxyRequest)

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

func Test_Route_allowsDial(t *testing.T) {
	route := &Route{
		Upstreams:     []Upstream{{Host: "app:8080"}, {Host: "[::1]:9000"}},
		ErrorMappings: []ErrorMapping{{Status: 502, Upstream: "errors.internal:80"}},
	}

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
				assert.Equal(t, tt.allowed, route.allowsDial(tt.addr))
			},
		)
	}
}

package proxy

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routedTo returns a copy of the request with a decision to proxy it using the given route.
func routedTo(request *http.Request, route *Route) *http.Request {
	decision := &Decision{outcome: OutcomeProxy, route: route}
	if len(route.Upstreams) > 0 {
		decision.upstream = route.Upstreams[0]
	}
	return request.WithContext(withDecision(request.Context(), decision))
}

func Test_Rewriter_RewriteRequest_SetsHostToUpstream(t *testing.T) {
	route := &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}
	rewriter := &Rewriter{}

	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	request = routedTo(request, route)
	rewriter.RewriteRequest(&httputil.ProxyRequest{In: request, Out: request})

	assert.Equal(t, "http://hostname:8080/foo/bar", request.URL.String())
}

func Test_Rewriter_RewriteRequest_AddsOriginalHostHeader(t *testing.T) {
	route := &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}
	rewriter := &Rewriter{}

	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Host:       "example.com",
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	request = routedTo(request, route)
	rewriter.RewriteRequest(&httputil.ProxyRequest{In: request, Out: request})

	assert.Equal(t, "example.com", request.Header.Get("X-Forwarded-Host"))
}

func Test_Rewriter_RewriteRequest_SetsForwardedForHeader(t *testing.T) {
	route := &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}
	rewriter := &Rewriter{}

	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	request = routedTo(request, route)
	rewriter.RewriteRequest(&httputil.ProxyRequest{In: request, Out: request})

	assert.Equal(t, "127.0.0.1", request.Header.Get("X-Forwarded-For"))
	assert.Equal(t, 1, len(request.Header.Values("X-Forwarded-For")))
}

func Test_Rewriter_RewriteRequest_SetsForwardedProtoHeaderIfHttps(t *testing.T) {
	route := &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}
	rewriter := &Rewriter{}

	u, _ := url.Parse("https://proxy/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	request = routedTo(request, route)
	rewriter.RewriteRequest(&httputil.ProxyRequest{In: request, Out: request})

	assert.Equal(t, "https", request.Header.Get("X-Forwarded-Proto"))
	assert.Equal(t, 1, len(request.Header.Values("X-Forwarded-Proto")))
}

func Test_Rewriter_RewriteRequest_SetsForwardedProtoHeaderIfHttp(t *testing.T) {
	route := &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}
	rewriter := &Rewriter{}

	u, _ := url.Parse("http://proxy/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	request = routedTo(request, route)
	rewriter.RewriteRequest(&httputil.ProxyRequest{In: request, Out: request})

	assert.Equal(t, "http", request.Header.Get("X-Forwarded-Proto"))
	assert.Equal(t, 1, len(request.Header.Values("X-Forwarded-Proto")))
}

func Test_Rewriter_RewriteRequest_ReplacesForwardedForHeader(t *testing.T) {
	route := &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}
	rewriter := &Rewriter{}

	u, _ := url.Parse("https://proxy/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	request = routedTo(request, route)
	request.Header.Set("X-Forwarded-For", "127.0.0.2")
	rewriter.RewriteRequest(&httputil.ProxyRequest{In: request, Out: request})

	assert.Equal(t, "127.0.0.1", request.Header.Get("X-Forwarded-For"))
	assert.Equal(t, 1, len(request.Header.Values("X-Forwarded-For")))
}

func Test_Rewriter_RewriteRequest_ReplacesForwardedProtoHeader(t *testing.T) {
	route := &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}
	rewriter := &Rewriter{}

	u, _ := url.Parse("https://proxy/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	request = routedTo(request, route)
	request.Header.Set("x-forwarded-proto", "ftp")
	rewriter.RewriteRequest(&httputil.ProxyRequest{In: request, Out: request})

	assert.Equal(t, "https", request.Header.Get("X-Forwarded-Proto"))
	assert.Equal(t, 1, len(request.Header.Values("X-Forwarded-Proto")))
}

func Test_Rewriter_RewriteRequest_RemovesBannedHeaders(t *testing.T) {
	route := &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}
	rewriter := NewRewriter(nil, nil)

	u, _ := url.Parse("https://proxy/foo/bar")
	request := &http.Request{
		URL:        u,
		Host:       "example.com",
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	request = routedTo(request, route)
	request.Header["X_Real_IP"] = []string{"6.6.6.6"}
	request.Header["Tailscale_User_Login"] = []string{"acidburn"}
	request.Header.Add("X-Real-IP", "6.6.6.6")
	request.Header.Add("X-Forwarded-Port", "1337")
	request.Header.Add("X-Forwarded-For", "6.6.6.6")
	request.Header.Add("X-Other", "value")
	rewriter.RewriteRequest(&httputil.ProxyRequest{In: request, Out: request})

	assert.Equal(
		t,
		http.Header{
			"X-Forwarded-For":   {"127.0.0.1"},
			"X-Forwarded-Host":  {"example.com"},
			"X-Forwarded-Proto": {"https"},
			"X-Other":           {"value"},
			"User-Agent":        {""},
		},
		request.Header,
	)
}

// tailscaleTestDecorator sets an identity header in the same way the tailscale frontend does.
type tailscaleTestDecorator struct{}

func (tailscaleTestDecorator) Decorate(_, out *http.Request) {
	out.Header.Set("Tailscale-User-Login", "real-user")
}

func Test_Rewriter_RewriteRequest_DecoratorsCanSetBannedHeaders(t *testing.T) {
	route := &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}
	rewriter := NewRewriter(nil, nil)
	rewriter.AddDecorator(tailscaleTestDecorator{})

	in := routedTo(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/", nil), route)
	in.Header.Set("Tailscale-User-Login", "acidburn")
	in.Header["Tailscale_User_Login"] = []string{"acidburn"}
	proxyRequest := &httputil.ProxyRequest{In: in, Out: in.Clone(in.Context())}
	rewriter.RewriteRequest(proxyRequest)

	assert.Equal(t, []string{"real-user"}, proxyRequest.Out.Header.Values("Tailscale-User-Login"))
	assert.NotContains(t, proxyRequest.Out.Header, "Tailscale_User_Login")
}

func Test_Rewriter_RewriteRequest_MarksOnlyOutboundRequestAsSanitised(t *testing.T) {
	route := &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}
	in := routedTo(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/", nil), route)
	proxyRequest := &httputil.ProxyRequest{In: in, Out: in.Clone(in.Context())}

	NewRewriter(nil, nil).RewriteRequest(proxyRequest)

	assert.True(t, isSanitised(proxyRequest.Out.Context()))
	assert.False(t, isSanitised(proxyRequest.In.Context()))
}

func Test_Rewriter_RewriteRequest_DoesNotMarkRequestWithoutDecision(t *testing.T) {
	in := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/", nil)
	proxyRequest := &httputil.ProxyRequest{In: in, Out: in.Clone(in.Context())}

	NewRewriter(nil, nil).RewriteRequest(proxyRequest)

	assert.False(t, isSanitised(proxyRequest.Out.Context()))
}

func Test_Rewriter_RewriteRequest_BlanksUserAgentIfUnset(t *testing.T) {
	route := &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}
	rewriter := &Rewriter{}

	u, _ := url.Parse("https://proxy/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	request = routedTo(request, route)
	rewriter.RewriteRequest(&httputil.ProxyRequest{In: request, Out: request})

	assert.Equal(t, "", request.Header.Get("User-Agent"))
}

func Test_Rewriter_RewriteRequest_LeavesUserAgentIfSet(t *testing.T) {
	route := &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}
	rewriter := &Rewriter{}

	u, _ := url.Parse("https://proxy/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	request = routedTo(request, route)
	request.Header.Set("User-Agent", "foo")
	rewriter.RewriteRequest(&httputil.ProxyRequest{In: request, Out: request})

	assert.Equal(t, "foo", request.Header.Get("User-Agent"))
}

func Test_Rewriter_RewriteResponse_AddsHeaders(t *testing.T) {
	route := &Route{
		Upstreams: []Upstream{{Host: "hostname:8080"}},
		Headers: []Header{
			{Name: "X-Test", Value: "test1", Operation: HeaderOpAdd},
			{Name: "X-Test", Value: "test2", Operation: HeaderOpAdd},
		},
	}
	rewriter := &Rewriter{}

	u, _ := url.Parse("https://example.com/foo/bar")
	request := &http.Request{
		URL: u,
		TLS: &tls.ConnectionState{ServerName: "example.com"},
	}
	request = routedTo(request, route)
	response := &http.Response{
		Request: request,
		Header:  make(http.Header),
	}
	response.Header.Set("X-Test", "test0")

	err := rewriter.RewriteResponse(response)
	require.NoError(t, err)
	assert.Equal(t, []string{"test0", "test1", "test2"}, response.Header.Values("X-Test"))
}

func Test_Rewriter_RewriteRequest_UsesUpstreamFromDecision(t *testing.T) {
	route := &Route{Upstreams: []Upstream{{Host: "first:8080"}, {Host: "second:8080"}}}
	rewriter := &Rewriter{}

	u, _ := url.Parse("/foo/bar")
	request := (&http.Request{URL: u, Header: make(http.Header), RemoteAddr: "127.0.0.1:11003"}).WithContext(
		withDecision(t.Context(), &Decision{outcome: OutcomeProxy, route: route, upstream: route.Upstreams[1]}),
	)
	rewriter.RewriteRequest(&httputil.ProxyRequest{In: request, Out: request})

	assert.Equal(t, "http://second:8080/foo/bar", request.URL.String())
}

func Test_Rewriter_RewriteRequest_ClearsAbsoluteUrlWithoutDecision(t *testing.T) {
	rewriter := &Rewriter{}

	u, _ := url.Parse("http://127.0.0.1:9090/metrics")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		Host:       "127.0.0.1:9090",
		RemoteAddr: "127.0.0.1:11003",
	}
	rewriter.RewriteRequest(&httputil.ProxyRequest{In: request, Out: request})

	assert.Empty(t, request.URL.Scheme)
	assert.Empty(t, request.URL.Host)
}

func Test_Rewriter_RewriteRequest_ClearsAbsoluteUrlIfDecisionIsNotToProxy(t *testing.T) {
	route := &Route{Domains: []string{"example.com"}, Upstreams: []Upstream{{Host: "hostname:8080"}}}
	for _, outcome := range []Outcome{OutcomeUnrouted, OutcomeRedirect} {
		rewriter := &Rewriter{}

		u, _ := url.Parse("http://127.0.0.1:9090/metrics")
		request := (&http.Request{URL: u, Header: make(http.Header), RemoteAddr: "127.0.0.1:11003"}).WithContext(
			withDecision(t.Context(), &Decision{outcome: outcome, route: route, upstream: route.Upstreams[0]}),
		)
		rewriter.RewriteRequest(&httputil.ProxyRequest{In: request, Out: request})

		assert.Empty(t, request.URL.Scheme)
		assert.Empty(t, request.URL.Host)
	}
}

func Test_Rewriter_RewriteResponse_DeletesHeaders(t *testing.T) {
	route := &Route{
		Upstreams: []Upstream{{Host: "hostname:8080"}},
		Headers: []Header{
			{Name: "X-Test", Operation: HeaderOpDelete},
		},
	}
	rewriter := &Rewriter{}

	u, _ := url.Parse("https://example.com:1234/foo/bar")
	request := &http.Request{
		URL: u,
		TLS: &tls.ConnectionState{ServerName: "example.com"},
	}
	request = routedTo(request, route)
	response := &http.Response{
		Request: request,
		Header:  make(http.Header),
	}
	response.Header.Set("X-Test", "test0")

	err := rewriter.RewriteResponse(response)
	require.NoError(t, err)
	assert.Equal(t, []string(nil), response.Header.Values("X-Test"))
}

func Test_Rewriter_RewriteResponse_ReplacesHeaders(t *testing.T) {
	route := &Route{
		Upstreams: []Upstream{{Host: "hostname:8080"}},
		Headers: []Header{
			{Name: "X-Test", Value: "test1", Operation: HeaderOpReplace},
		},
	}
	rewriter := &Rewriter{}

	u, _ := url.Parse("https://example.com:1234/foo/bar")
	request := &http.Request{
		URL: u,
		TLS: &tls.ConnectionState{ServerName: "example.com"},
	}
	request = routedTo(request, route)
	response := &http.Response{
		Request: request,
		Header:  make(http.Header),
	}
	response.Header.Set("X-Test", "test0")

	err := rewriter.RewriteResponse(response)
	require.NoError(t, err)
	assert.Equal(t, []string{"test1"}, response.Header.Values("X-Test"))
}

func Test_Rewriter_RewriteResponse_DefaultsHeader_ifNotPresent(t *testing.T) {
	route := &Route{
		Upstreams: []Upstream{{Host: "hostname:8080"}},
		Headers: []Header{
			{Name: "X-Test", Value: "test1", Operation: HeaderOpDefault},
		},
	}
	rewriter := &Rewriter{}

	u, _ := url.Parse("https://example.com:1234/foo/bar")
	request := &http.Request{
		URL: u,
		TLS: &tls.ConnectionState{ServerName: "example.com"},
	}
	request = routedTo(request, route)
	response := &http.Response{
		Request: request,
		Header:  make(http.Header),
	}

	err := rewriter.RewriteResponse(response)
	require.NoError(t, err)
	assert.Equal(t, []string{"test1"}, response.Header.Values("X-Test"))
}

func Test_Rewriter_RewriteResponse_DefaultsHeader_ifPresent(t *testing.T) {
	route := &Route{
		Upstreams: []Upstream{{Host: "hostname:8080"}},
		Headers: []Header{
			{Name: "X-Test", Value: "test1", Operation: HeaderOpDefault},
		},
	}
	rewriter := &Rewriter{}

	u, _ := url.Parse("https://example.com:1234/foo/bar")
	request := &http.Request{
		URL: u,
		TLS: &tls.ConnectionState{ServerName: "example.com"},
	}
	request = routedTo(request, route)
	response := &http.Response{
		Request: request,
		Header:  make(http.Header),
	}
	response.Header.Set("X-Test", "test0")

	err := rewriter.RewriteResponse(response)
	require.NoError(t, err)
	assert.Equal(t, []string{"test0"}, response.Header.Values("X-Test"))
}

func Test_Rewriter_RewriteResponse_DoesNothingIfRouteIsNil(t *testing.T) {
	rewriter := &Rewriter{}

	u, _ := url.Parse("https://example.com:1234/foo/bar")
	request := &http.Request{
		URL: u,
		TLS: &tls.ConnectionState{ServerName: "example.com"},
	}
	response := &http.Response{
		Request: request,
		Header:  make(http.Header),
	}
	response.Header.Set("X-Test", "test0")

	err := rewriter.RewriteResponse(response)
	require.NoError(t, err)
	assert.Equal(t, []string{"test0"}, response.Header.Values("X-Test"))
}

func Test_ParseCIDRList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty string", "", nil},
		{"whitespace only", " , ,, ", nil},
		{"single CIDR", "10.0.0.0/8", []string{"10.0.0.0/8"}},
		{"multiple CIDRs", "10.0.0.0/8,192.168.0.0/16", []string{"10.0.0.0/8", "192.168.0.0/16"}},
		{"multiple CIDRs with whitespace", " 10.0.0.0/8 , ::1/128 ", []string{"10.0.0.0/8", "::1/128"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nets, err := ParseCIDRList(tt.input)
			require.NoError(t, err)

			var got []string
			for i := range nets {
				got = append(got, nets[i].String())
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_ParseCIDRList_errorsOnInvalidEntry(t *testing.T) {
	_, err := ParseCIDRList("10.0.0.0/8,not-a-cidr")
	assert.ErrorContains(t, err, "not-a-cidr")
}

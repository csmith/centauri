package proxy

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProvider struct {
	route  *Route
	domain string
}

func (f *fakeProvider) RouteForDomain(domain string) *Route {
	f.domain = domain
	return f.route
}

func Test_Rewriter_RewriteRequest_SetsHostToUpstream(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}}
	rewriter := &Rewriter{provider: provider}

	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	rewriter.RewriteRequest(request)

	assert.Equal(t, "http://hostname:8080/foo/bar", request.URL.String())
}

func Test_Rewriter_RewriteRequest_SetsForwardedForHeader(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}}
	rewriter := &Rewriter{provider: provider, decorators: []Decorator{NewXForwardedForDecorator()}}

	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	rewriter.RewriteRequest(request)

	assert.Equal(t, "127.0.0.1", request.Header.Get("X-Forwarded-For"))
	assert.Equal(t, 1, len(request.Header.Values("X-Forwarded-For")))
}

func Test_Rewriter_RewriteRequest_SetsForwardedProtoHeaderIfHttps(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}}
	rewriter := &Rewriter{provider: provider, decorators: []Decorator{NewXForwardedForDecorator()}}

	u, _ := url.Parse("https://proxy/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	rewriter.RewriteRequest(request)

	assert.Equal(t, "https", request.Header.Get("X-Forwarded-Proto"))
	assert.Equal(t, 1, len(request.Header.Values("X-Forwarded-Proto")))
}

func Test_Rewriter_RewriteRequest_SetsForwardedProtoHeaderIfHttp(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}}
	rewriter := &Rewriter{provider: provider, decorators: []Decorator{NewXForwardedForDecorator()}}

	u, _ := url.Parse("http://proxy/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	rewriter.RewriteRequest(request)

	assert.Equal(t, "http", request.Header.Get("X-Forwarded-Proto"))
	assert.Equal(t, 1, len(request.Header.Values("X-Forwarded-Proto")))
}

func Test_Rewriter_RewriteRequest_ReplacesForwardedForHeader(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}}
	rewriter := &Rewriter{provider: provider, decorators: []Decorator{NewXForwardedForDecorator()}}

	u, _ := url.Parse("https://proxy/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	request.Header.Set("X-Forwarded-For", "127.0.0.2")
	rewriter.RewriteRequest(request)

	assert.Equal(t, "127.0.0.1", request.Header.Get("X-Forwarded-For"))
	assert.Equal(t, 1, len(request.Header.Values("X-Forwarded-For")))
}

func Test_Rewriter_RewriteRequest_ReplacesForwardedProtoHeader(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}}
	rewriter := &Rewriter{provider: provider, decorators: []Decorator{NewXForwardedForDecorator()}}

	u, _ := url.Parse("https://proxy/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	request.Header.Set("x-forwarded-proto", "ftp")
	rewriter.RewriteRequest(request)

	assert.Equal(t, "https", request.Header.Get("X-Forwarded-Proto"))
	assert.Equal(t, 1, len(request.Header.Values("X-Forwarded-Proto")))
}

func Test_Rewriter_RewriteRequest_RemovesBannedHeaders(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}}
	rewriter := &Rewriter{provider: provider, decorators: []Decorator{&bannedHeaderDecorator{[]string{"x-test1", "x-test2"}}}}

	u, _ := url.Parse("https://proxy/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	request.Header.Add("x-test1", "value")
	request.Header.Add("x-test1", "value")
	request.Header.Add("X-Test2", "value")
	request.Header.Add("X-Test2-other", "value")
	rewriter.RewriteRequest(request)

	assert.Equal(t, 0, len(request.Header.Values("x-test1")))
	assert.Equal(t, 0, len(request.Header.Values("X-Test2")))
	assert.Equal(t, 1, len(request.Header.Values("X-Test2-other")))
}

func Test_Rewriter_RewriteRequest_BlanksUserAgentIfUnset(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}}
	rewriter := &Rewriter{provider: provider}

	u, _ := url.Parse("https://proxy/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	rewriter.RewriteRequest(request)

	assert.Equal(t, "", request.Header.Get("User-Agent"))
}

func Test_Rewriter_RewriteRequest_LeavesUserAgentIfSet(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}}
	rewriter := &Rewriter{provider: provider}

	u, _ := url.Parse("https://proxy/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	request.Header.Set("User-Agent", "foo")
	rewriter.RewriteRequest(request)

	assert.Equal(t, "foo", request.Header.Get("User-Agent"))
}

func Test_Rewriter_RewriteResponse_AddsHeaders(t *testing.T) {
	provider := &fakeProvider{
		route: &Route{
			Upstreams: []Upstream{{Host: "hostname:8080"}},
			Headers: []Header{
				{Name: "X-Test", Value: "test1", Operation: HeaderOpAdd},
				{Name: "X-Test", Value: "test2", Operation: HeaderOpAdd},
			},
		},
	}
	rewriter := &Rewriter{provider: provider}

	request := &http.Request{
		TLS: &tls.ConnectionState{ServerName: "example.com"},
	}
	response := &http.Response{
		Request: request,
		Header:  make(http.Header),
	}
	response.Header.Set("X-Test", "test0")

	err := rewriter.RewriteResponse(response)
	require.NoError(t, err)
	assert.Equal(t, []string{"test0", "test1", "test2"}, response.Header.Values("X-Test"))
}

func Test_Rewriter_RewriteRequest_UsesHostHeaderIfTlsNotUsed(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstreams: []Upstream{{Host: "hostname:8080"}}}}
	rewriter := &Rewriter{provider: provider}

	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		Host:       "example.com",
		RemoteAddr: "127.0.0.1:11003",
	}
	rewriter.RewriteRequest(request)

	assert.Equal(t, "example.com", provider.domain)
	assert.Equal(t, "http://hostname:8080/foo/bar", request.URL.String())
}

func Test_Rewriter_RewriteRequest_DoesNothingIfRouteIsNill(t *testing.T) {
	provider := &fakeProvider{}
	rewriter := &Rewriter{provider: provider}

	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		Host:       "example.com",
		RemoteAddr: "127.0.0.1:11003",
	}
	rewriter.RewriteRequest(request)

	assert.Equal(t, "example.com", provider.domain)
	assert.Equal(t, "/foo/bar", request.URL.String())
}

func Test_Rewriter_RewriteResponse_DeletesHeaders(t *testing.T) {
	provider := &fakeProvider{
		route: &Route{
			Upstreams: []Upstream{{Host: "hostname:8080"}},
			Headers: []Header{
				{Name: "X-Test", Operation: HeaderOpDelete},
			},
		},
	}
	rewriter := &Rewriter{provider: provider}

	request := &http.Request{
		TLS: &tls.ConnectionState{ServerName: "example.com"},
	}
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
	provider := &fakeProvider{
		route: &Route{
			Upstreams: []Upstream{{Host: "hostname:8080"}},
			Headers: []Header{
				{Name: "X-Test", Value: "test1", Operation: HeaderOpReplace},
			},
		},
	}
	rewriter := &Rewriter{provider: provider}

	request := &http.Request{
		TLS: &tls.ConnectionState{ServerName: "example.com"},
	}
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
	provider := &fakeProvider{
		route: &Route{
			Upstreams: []Upstream{{Host: "hostname:8080"}},
			Headers: []Header{
				{Name: "X-Test", Value: "test1", Operation: HeaderOpDefault},
			},
		},
	}
	rewriter := &Rewriter{provider: provider}

	request := &http.Request{
		TLS: &tls.ConnectionState{ServerName: "example.com"},
	}
	response := &http.Response{
		Request: request,
		Header:  make(http.Header),
	}

	err := rewriter.RewriteResponse(response)
	require.NoError(t, err)
	assert.Equal(t, []string{"test1"}, response.Header.Values("X-Test"))
}

func Test_Rewriter_RewriteResponse_DefaultsHeader_ifPresent(t *testing.T) {
	provider := &fakeProvider{
		route: &Route{
			Upstreams: []Upstream{{Host: "hostname:8080"}},
			Headers: []Header{
				{Name: "X-Test", Value: "test1", Operation: HeaderOpDefault},
			},
		},
	}
	rewriter := &Rewriter{provider: provider}

	request := &http.Request{
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

func Test_Rewriter_RewriteResponse_DoesNothingIfRouteIsNil(t *testing.T) {
	provider := &fakeProvider{}
	rewriter := &Rewriter{provider: provider}

	request := &http.Request{
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

func Test_Rewriter_RewriteResponse_DoesNothingIfRouteHasNoUpstreams(t *testing.T) {
	provider := &fakeProvider{
		route: &Route{
			Upstreams: []Upstream{},
		},
	}
	rewriter := &Rewriter{provider: provider}

	request := &http.Request{
		TLS: &tls.ConnectionState{ServerName: "example.com"},
	}
	response := &http.Response{
		Request: request,
		Header:  make(http.Header),
	}

	err := rewriter.RewriteResponse(response)
	require.NoError(t, err)
}

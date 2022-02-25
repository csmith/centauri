package proxy

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeProvider struct {
	route  *Route
	domain string
}

func (f *fakeProvider) RouteForDomain(domain string) *Route {
	f.domain = domain
	return f.route
}

func Test_Rewriter_SetsHostToUpstream(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstream: "hostname:8080"}}
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

func Test_Rewriter_SetsForwardedForHeader(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstream: "hostname:8080"}}
	rewriter := &Rewriter{provider: provider}

	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	rewriter.RewriteRequest(request)

	assert.Equal(t, "127.0.0.1", request.Header.Get("X-Forwarded-For"))
}

func Test_Rewriter_SetsForwardedProtoHeader(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstream: "hostname:8080"}}
	rewriter := &Rewriter{provider: provider}

	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	rewriter.RewriteRequest(request)

	assert.Equal(t, "https", request.Header.Get("X-Forwarded-Proto"))
}

func Test_Rewriter_BlanksUserAgentIfUnset(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstream: "hostname:8080"}}
	rewriter := &Rewriter{provider: provider}

	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		TLS:        &tls.ConnectionState{ServerName: "example.com"},
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
	}
	rewriter.RewriteRequest(request)

	assert.Equal(t, "", request.Header.Get("User-Agent"))
}

func Test_Rewriter_LeavesUserAgentIfSet(t *testing.T) {
	provider := &fakeProvider{route: &Route{Upstream: "hostname:8080"}}
	rewriter := &Rewriter{provider: provider}

	u, _ := url.Parse("/foo/bar")
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

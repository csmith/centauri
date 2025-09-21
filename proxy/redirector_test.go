package proxy

import (
	"crypto/tls"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeResponseWriter struct {
	header     http.Header
	statusCode int
}

func (f *fakeResponseWriter) Header() http.Header {
	return f.header
}

func (f *fakeResponseWriter) Write(bytes []byte) (int, error) {
	// Don't care
	return 0, nil
}

func (f *fakeResponseWriter) WriteHeader(statusCode int) {
	f.statusCode = statusCode
}

func Test_HttpRedirector_ErrorsIfHostIsEmpty(t *testing.T) {
	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "",
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	redirector := &HttpRedirector{}
	redirector.ServeHTTP(writer, request)

	assert.Equal(t, http.StatusBadRequest, writer.statusCode)
}

func Test_HttpRedirector_ErrorsIfHostIsInvalid(t *testing.T) {
	tests := []string{
		"/invalid/",
		"invalid with spaces",
		"127.0.0.1",
		"127.0.0.1:80",
		"[::1]",
		"[::1]:80",
		"invalid-.example.com",
		"invalid.-example.com",
		"invalid..example.com",
		"invalid.example.com-",
		"invalid-because-this-part-is-just-longer-than-sixty-four-characters.example.com",
		strings.Repeat("invalid-because-the-overall-host-is-too-long.", 6) + ".example.com",
	}

	for _, test := range tests {
		t.Run(test, func(t *testing.T) {
			u, _ := url.Parse("/foo/bar")
			request := &http.Request{
				URL:        u,
				Header:     make(http.Header),
				RemoteAddr: "127.0.0.1:11003",
				Host:       test,
			}

			writer := &fakeResponseWriter{
				header: make(http.Header),
			}

			redirector := &HttpRedirector{}
			redirector.ServeHTTP(writer, request)

			assert.Equal(t, http.StatusBadRequest, writer.statusCode)
		})
	}
}

func Test_HttpRedirector_RedirectsToHttpsUrl(t *testing.T) {
	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "example.com",
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	redirector := &HttpRedirector{}
	redirector.ServeHTTP(writer, request)

	assert.Equal(t, http.StatusPermanentRedirect, writer.statusCode)
	assert.Equal(t, "https://example.com/foo/bar", writer.header.Get("Location"))
}

func Test_HttpRedirector_PreservesQueryString(t *testing.T) {
	u, _ := url.Parse("/foo/bar?baz=quux")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "example.com",
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	redirector := &HttpRedirector{}
	redirector.ServeHTTP(writer, request)

	assert.Equal(t, http.StatusPermanentRedirect, writer.statusCode)
	assert.Equal(t, "https://example.com/foo/bar?baz=quux", writer.header.Get("Location"))
}

func Test_HttpRedirector_StripsPort(t *testing.T) {
	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "example.com:80",
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	redirector := &HttpRedirector{}
	redirector.ServeHTTP(writer, request)

	assert.Equal(t, http.StatusPermanentRedirect, writer.statusCode)
	assert.Equal(t, "https://example.com/foo/bar", writer.header.Get("Location"))
}

type mockRouteProvider struct {
	routes map[string]*Route
}

func (m *mockRouteProvider) RouteForDomain(domain string) *Route {
	return m.routes[domain]
}

type mockNextHandler struct {
	called bool
}

func (m *mockNextHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	m.called = true
}

func Test_DomainRedirector_PassesThroughWhenNoRoute(t *testing.T) {
	provider := &mockRouteProvider{
		routes: make(map[string]*Route),
	}
	nextHandler := &mockNextHandler{}

	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "example.com",
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	redirector := NewDomainRedirector(provider, nextHandler)
	redirector.ServeHTTP(writer, request)

	assert.True(t, nextHandler.called)
	assert.Equal(t, 0, writer.statusCode)
}

func Test_DomainRedirector_PassesThroughWhenRedirectToPrimaryIsFalse(t *testing.T) {
	provider := &mockRouteProvider{
		routes: map[string]*Route{
			"example.com": {
				Domains:           []string{"example.com", "www.example.com"},
				RedirectToPrimary: false,
			},
		},
	}
	nextHandler := &mockNextHandler{}

	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "www.example.com",
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	redirector := NewDomainRedirector(provider, nextHandler)
	redirector.ServeHTTP(writer, request)

	assert.True(t, nextHandler.called)
	assert.Equal(t, 0, writer.statusCode)
}

func Test_DomainRedirector_PassesThroughWhenAlreadyOnPrimaryDomain(t *testing.T) {
	provider := &mockRouteProvider{
		routes: map[string]*Route{
			"example.com": {
				Domains:           []string{"example.com", "www.example.com"},
				RedirectToPrimary: true,
			},
		},
	}
	nextHandler := &mockNextHandler{}

	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "example.com",
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	redirector := NewDomainRedirector(provider, nextHandler)
	redirector.ServeHTTP(writer, request)

	assert.True(t, nextHandler.called)
	assert.Equal(t, 0, writer.statusCode)
}

func Test_DomainRedirector_RedirectsToHttpsWhenTlsPresent(t *testing.T) {
	provider := &mockRouteProvider{
		routes: map[string]*Route{
			"www.example.com": {
				Domains:           []string{"example.com", "www.example.com"},
				RedirectToPrimary: true,
			},
		},
	}
	nextHandler := &mockNextHandler{}

	u, _ := url.Parse("/foo/bar?baz=quux")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "www.example.com",
		TLS:        &tls.ConnectionState{},
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	redirector := NewDomainRedirector(provider, nextHandler)
	redirector.ServeHTTP(writer, request)

	assert.False(t, nextHandler.called)
	assert.Equal(t, http.StatusPermanentRedirect, writer.statusCode)
	assert.Equal(t, "https://example.com/foo/bar?baz=quux", writer.header.Get("Location"))
}

func Test_DomainRedirector_RedirectsToHttpWhenNoTls(t *testing.T) {
	provider := &mockRouteProvider{
		routes: map[string]*Route{
			"www.example.com": {
				Domains:           []string{"example.com", "www.example.com"},
				RedirectToPrimary: true,
			},
		},
	}
	nextHandler := &mockNextHandler{}

	u, _ := url.Parse("/foo/bar?baz=quux")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "www.example.com",
		TLS:        nil,
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	redirector := NewDomainRedirector(provider, nextHandler)
	redirector.ServeHTTP(writer, request)

	assert.False(t, nextHandler.called)
	assert.Equal(t, http.StatusPermanentRedirect, writer.statusCode)
	assert.Equal(t, "http://example.com/foo/bar?baz=quux", writer.header.Get("Location"))
}

func Test_DomainRedirector_StripsPortFromHost(t *testing.T) {
	provider := &mockRouteProvider{
		routes: map[string]*Route{
			"www.example.com": {
				Domains:           []string{"example.com", "www.example.com"},
				RedirectToPrimary: true,
			},
		},
	}
	nextHandler := &mockNextHandler{}

	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "www.example.com:443",
		TLS:        &tls.ConnectionState{},
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	redirector := NewDomainRedirector(provider, nextHandler)
	redirector.ServeHTTP(writer, request)

	assert.False(t, nextHandler.called)
	assert.Equal(t, http.StatusPermanentRedirect, writer.statusCode)
	assert.Equal(t, "https://example.com/foo/bar", writer.header.Get("Location"))
}

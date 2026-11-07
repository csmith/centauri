package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		t.Run(
			test,
			func(t *testing.T) {
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
			},
		)
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

func Test_HttpRedirector_NormalisesHost(t *testing.T) {
	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "Example.COM.:80",
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	(&HttpRedirector{}).ServeHTTP(writer, request)

	assert.Equal(t, http.StatusPermanentRedirect, writer.statusCode)
	assert.Equal(t, "https://example.com/foo/bar", writer.header.Get("Location"))
}

// newTestManager creates a manager without a certificate provider, serving the given routes.
func newTestManager(t *testing.T, fallback *Route, routes ...*Route) *Manager {
	t.Helper()
	manager := NewManager(nil)
	require.NoError(t, manager.SetRoutes(t.Context(), routes, fallback))
	return manager
}

// routeRequest passes the request through a Router, returning the request as it would be given
// to the proxy, or nil if the router didn't proxy it.
func routeRequest(t *testing.T, manager *Manager, request *http.Request) *http.Request {
	t.Helper()
	next := &recordingHandler{}
	NewRouter(manager, next, http.NotFoundHandler()).ServeHTTP(httptest.NewRecorder(), request)
	return next.request
}

type recordingHandler struct {
	request *http.Request
}

func (h *recordingHandler) ServeHTTP(_ http.ResponseWriter, request *http.Request) {
	h.request = request
}

func Test_Manager_Decide(t *testing.T) {
	main := &Route{Domains: []string{"example.com", "www.example.com"}, Upstreams: []Upstream{{Host: "main:8080"}}}
	redirect := &Route{
		Domains:           []string{"Primary.example.net", "alt.example.net"},
		Upstreams:         []Upstream{{Host: "redirect:8080"}},
		RedirectToPrimary: true,
	}
	manager := newTestManager(t, nil, main, redirect, &Route{Domains: []string{"empty.example.com"}})

	tests := []struct {
		host     string
		tls      bool
		outcome  Outcome
		route    *Route
		redirect string
	}{
		{host: "example.com", outcome: OutcomeProxy, route: main},
		{host: "www.example.com", outcome: OutcomeProxy, route: main},
		{host: "EXAMPLE.com", outcome: OutcomeProxy, route: main},
		{host: "example.com:8443", outcome: OutcomeProxy, route: main},
		{host: "example.com.", outcome: OutcomeProxy, route: main},
		{host: "Example.Com.:443", outcome: OutcomeProxy, route: main},
		{host: "example.org", outcome: OutcomeUnrouted},
		{host: "127.0.0.1", outcome: OutcomeUnrouted},
		{host: "", outcome: OutcomeUnrouted},
		{host: "empty.example.com", outcome: OutcomeUnrouted},
		{host: "primary.example.net", outcome: OutcomeProxy, route: redirect},
		{
			host:     "alt.example.net",
			outcome:  OutcomeRedirect,
			route:    redirect,
			redirect: "http://Primary.example.net/foo%2Fbar?baz=quux",
		},
		{
			host:     "ALT.example.net.:443",
			tls:      true,
			outcome:  OutcomeRedirect,
			route:    redirect,
			redirect: "https://Primary.example.net/foo%2Fbar?baz=quux",
		},
	}
	for _, tt := range tests {
		t.Run(
			tt.host,
			func(t *testing.T) {
				request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/foo%2Fbar?baz=quux", nil)
				request.Host = tt.host
				if tt.tls {
					request.TLS = &tls.ConnectionState{}
				}

				decision := manager.Decide(request)
				assert.Equal(t, tt.outcome, decision.Outcome())
				assert.Equal(t, tt.route, decision.Route())
				assert.Equal(t, tt.redirect, decision.RedirectURL())
				if tt.outcome == OutcomeProxy {
					assert.Equal(t, tt.route.Upstreams[0], decision.Upstream())
				} else {
					assert.Empty(t, decision.Upstream())
				}
			},
		)
	}
}

func Test_Manager_Decide_normalisesHost(t *testing.T) {
	manager := newTestManager(t, nil)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	request.Host = "Example.COM.:8443"
	assert.Equal(t, "example.com", manager.Decide(request).Host())
}

func Test_Manager_Decide_usesFallbackForUnknownHosts(t *testing.T) {
	fallback := &Route{Domains: []string{"fallback.example.com"}, Upstreams: []Upstream{{Host: "fallback:8080"}}}
	manager := newTestManager(t, fallback)

	for _, host := range []string{"example.org", "127.0.0.1:8080", "[::1]"} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		request.Host = host

		decision := manager.Decide(request)
		assert.Equal(t, OutcomeProxy, decision.Outcome(), host)
		assert.Equal(t, fallback, decision.Route(), host)
	}
}

func Test_Manager_Decide_picksUpstreamFromRoute(t *testing.T) {
	route := &Route{
		Domains:   []string{"example.com"},
		Upstreams: []Upstream{{Host: "one:8080"}, {Host: "two:8080"}, {Host: "three:8080"}},
	}
	manager := newTestManager(t, nil, route)

	seen := make(map[Upstream]bool)
	for range 100 {
		upstream := manager.Decide(
			httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/", nil),
		).Upstream()
		assert.Contains(t, route.Upstreams, upstream)
		seen[upstream] = true
	}
	assert.Greater(t, len(seen), 1)
}

func Test_Manager_Decide_doesNotRouteWithoutCertificate(t *testing.T) {
	synctest.Test(
		t,
		func(t *testing.T) {
			manager := NewManager(&fakeCertManager{err: errors.New("ruh roh"), existingErr: errors.New("ruh roh")})
			route := &Route{Domains: []string{"example.com"}, Upstreams: []Upstream{{Host: "upstream:8080"}}}
			require.NoError(t, manager.SetRoutes(t.Context(), []*Route{route}, route))
			synctest.Wait()

			for _, host := range []string{"example.com", "example.org"} {
				request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
				request.Host = host
				assert.Equal(t, OutcomeUnrouted, manager.Decide(request).Outcome(), host)
			}
		},
	)
}

func Test_Decision_nilIsUnrouted(t *testing.T) {
	var decision *Decision
	assert.Equal(t, OutcomeUnrouted, decision.Outcome())
	assert.Nil(t, decision.Route())
	assert.Empty(t, decision.Host())
	assert.Empty(t, decision.Upstream())
	assert.Empty(t, decision.RedirectURL())
	assert.Nil(t, DecisionFromContext(t.Context()))
}

func Test_Router_passesProxiedRequestsToNextWithDecision(t *testing.T) {
	route := &Route{Domains: []string{"example.com"}, Upstreams: []Upstream{{Host: "upstream:8080"}}}
	next, unrouted := &recordingHandler{}, &recordingHandler{}

	NewRouter(newTestManager(t, nil, route), next, unrouted).ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/", nil),
	)

	require.NotNil(t, next.request)
	assert.Nil(t, unrouted.request)
	decision := DecisionFromContext(next.request.Context())
	assert.Equal(t, OutcomeProxy, decision.Outcome())
	assert.Equal(t, route, decision.Route())
}

func Test_Router_passesUnroutedRequestsToUnroutedHandler(t *testing.T) {
	routes := []*Route{
		{Domains: []string{"example.com"}, Upstreams: []Upstream{{Host: "upstream:8080"}}},
		{Domains: []string{"empty.example.com"}},
	}
	for _, target := range []string{"http://127.0.0.1:9090/metrics", "http://empty.example.com/"} {
		next, unrouted := &recordingHandler{}, &recordingHandler{}

		NewRouter(newTestManager(t, nil, routes...), next, unrouted).ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil),
		)

		assert.Nil(t, next.request, target)
		require.NotNil(t, unrouted.request, target)
		assert.Equal(t, OutcomeUnrouted, DecisionFromContext(unrouted.request.Context()).Outcome(), target)
	}
}

func Test_Router_redirectsToPrimaryWithoutModifyingRequest(t *testing.T) {
	route := &Route{
		Domains:           []string{"example.com", "www.example.com"},
		Upstreams:         []Upstream{{Host: "upstream:8080"}},
		RedirectToPrimary: true,
	}
	next, unrouted := &recordingHandler{}, &recordingHandler{}

	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		"https://www.example.com:443/foo/bar?baz=quux",
		nil,
	)
	writer := httptest.NewRecorder()
	NewRouter(newTestManager(t, nil, route), next, unrouted).ServeHTTP(writer, request)

	assert.Nil(t, next.request)
	assert.Nil(t, unrouted.request)
	assert.Equal(t, http.StatusPermanentRedirect, writer.Code)
	assert.Equal(t, "https://example.com/foo/bar?baz=quux", writer.Header().Get("Location"))
	assert.Equal(t, "www.example.com:443", request.URL.Host)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func Test_Router_requestKeepsItsRouteWhenConfigReloadedMidRequest(t *testing.T) {
	errorUpstream := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("original error page"))
			},
		),
	)
	defer errorUpstream.Close()

	original := &Route{
		Domains:       []string{"example.com"},
		Upstreams:     []Upstream{{Host: "original:8080"}},
		Headers:       []Header{{Name: "X-Route", Value: "original", Operation: HeaderOpReplace}},
		ErrorMappings: []ErrorMapping{{Status: http.StatusBadGateway, Upstream: errorUpstream.Listener.Addr().String()}},
	}
	replacement := &Route{
		Domains:   []string{"example.com"},
		Upstreams: []Upstream{{Host: "replacement:8080"}},
		Headers:   []Header{{Name: "X-Route", Value: "replacement", Operation: HeaderOpReplace}},
	}

	for _, fail := range []bool{false, true} {
		manager := newTestManager(t, nil, original)
		rewriter := &Rewriter{errorClient: newErrorClient(nil)}

		var dialledHost string
		reverseProxy := &httputil.ReverseProxy{
			Rewrite:        rewriter.RewriteRequest,
			ModifyResponse: rewriter.RewriteResponse,
			ErrorHandler: rewriter.RewriteError(
				func(w http.ResponseWriter, r *http.Request, err error) {
					w.WriteHeader(http.StatusBadGateway)
				},
			),
			Transport: roundTripperFunc(
				func(request *http.Request) (*http.Response, error) {
					dialledHost = request.URL.Host
					require.NoError(t, manager.SetRoutes(t.Context(), []*Route{replacement}, nil))
					if fail {
						return nil, errors.New("upstream unavailable")
					}
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       http.NoBody,
						Request:    request,
					}, nil
				},
			),
		}

		writer := httptest.NewRecorder()
		NewRouter(manager, reverseProxy, http.NotFoundHandler()).ServeHTTP(
			writer,
			httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/", nil),
		)

		assert.Equal(t, "original:8080", dialledHost)
		assert.Equal(t, "original", writer.Header().Get("X-Route"))
		if fail {
			assert.Equal(t, http.StatusBadGateway, writer.Code)
			assert.Equal(t, "original error page", writer.Body.String())
		} else {
			assert.Equal(t, http.StatusOK, writer.Code)
		}
	}
}

func Test_Rewriter_doesNotDialWithoutDecision(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				t.Error("Server should not have been contacted")
			},
		),
	)
	defer server.Close()

	dialled := false
	reverseProxy := &httputil.ReverseProxy{
		Rewrite: (&Rewriter{}).RewriteRequest,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				dialled = true
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		},
	}

	writer := httptest.NewRecorder()
	reverseProxy.ServeHTTP(writer, httptest.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/", nil))

	assert.False(t, dialled)
	assert.Equal(t, http.StatusBadGateway, writer.Code)
}

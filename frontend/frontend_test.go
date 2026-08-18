package frontend

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/csmith/centauri/metrics"
	"github.com/csmith/centauri/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestContext creates a Context backed by a manager without a certificate provider, and installs the
// given routes.
func newTestContext(t *testing.T, routes ...*proxy.Route) *Context {
	t.Helper()

	manager := proxy.NewManager(nil)
	require.NoError(t, manager.SetRoutes(t.Context(), routes, nil))

	return &Context{
		Manager:  manager,
		Rewriter: proxy.NewRewriter(manager, nil),
		Recorder: metrics.NewRecorder(manager.RouteForDomain),
		ErrChan:  make(chan error, 1),
	}
}

func Test_Context_CreateProxy_proxiesToUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello from upstream, you asked for %s", r.URL.Path)
	}))
	defer upstream.Close()

	ctx := newTestContext(t, &proxy.Route{
		Domains:   []string{"example.com"},
		Upstreams: []proxy.Upstream{{Host: upstream.Listener.Addr().String()}},
	})

	proxyServer := httptest.NewServer(ctx.createProxy())
	defer proxyServer.Close()

	request, err := http.NewRequest(http.MethodGet, proxyServer.URL+"/foo/bar", nil)
	require.NoError(t, err)
	request.Host = "example.com"

	response, err := proxyServer.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()

	assert.Equal(t, http.StatusOK, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, "Hello from upstream, you asked for /foo/bar", string(body))
}

func Test_Context_CreateProxy_returnsBadGatewayWhenUpstreamUnreachable(t *testing.T) {
	ctx := newTestContext(t, &proxy.Route{
		Domains:   []string{"example.com"},
		Upstreams: []proxy.Upstream{{Host: "127.0.0.1:1"}},
	})

	proxyServer := httptest.NewServer(ctx.createProxy())
	defer proxyServer.Close()

	request, err := http.NewRequest(http.MethodGet, proxyServer.URL, nil)
	require.NoError(t, err)
	request.Host = "example.com"

	response, err := proxyServer.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()

	assert.Equal(t, http.StatusBadGateway, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "The server was unable to complete your request")
}

func Test_Context_CreateProxy_servesErrorPageFromUpstreamWhenUpstreamUnreachable(t *testing.T) {
	errorUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"unavailable"}`))
	}))
	defer errorUpstream.Close()

	ctx := newTestContext(t, &proxy.Route{
		Domains:   []string{"example.com"},
		Upstreams: []proxy.Upstream{{Host: "127.0.0.1:1"}},
		ErrorMappings: []proxy.ErrorMapping{
			{Status: http.StatusBadGateway, Upstream: errorUpstream.Listener.Addr().String()},
		},
	})

	proxyServer := httptest.NewServer(ctx.createProxy())
	defer proxyServer.Close()

	request, err := http.NewRequest(http.MethodGet, proxyServer.URL+"/widgets/3", nil)
	require.NoError(t, err)
	request.Host = "example.com"

	response, err := proxyServer.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()

	assert.Equal(t, http.StatusBadGateway, response.StatusCode)
	assert.Equal(t, "application/json", response.Header.Get("Content-Type"))
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, `{"error":"unavailable"}`, string(body))
}

func Test_Context_CreateProxy_servesErrorPageFromUpstreamForUpstreamErrorStatuses(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer upstream.Close()

	errorUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/widgets/3", r.URL.Path)
		_, _ = w.Write([]byte("nicer not found page"))
	}))
	defer errorUpstream.Close()

	ctx := newTestContext(t, &proxy.Route{
		Domains:   []string{"example.com"},
		Upstreams: []proxy.Upstream{{Host: upstream.Listener.Addr().String()}},
		ErrorMappings: []proxy.ErrorMapping{
			{Status: http.StatusNotFound, Upstream: errorUpstream.Listener.Addr().String()},
		},
	})

	proxyServer := httptest.NewServer(ctx.createProxy())
	defer proxyServer.Close()

	request, err := http.NewRequest(http.MethodGet, proxyServer.URL+"/widgets/3", nil)
	require.NoError(t, err)
	request.Host = "example.com"

	response, err := proxyServer.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()

	assert.Equal(t, http.StatusNotFound, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, "nicer not found page", string(body))
}

func Test_Context_CreateProxy_fallsBackToDefaultPageWhenErrorUpstreamUnreachable(t *testing.T) {
	errorUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	address := errorUpstream.Listener.Addr().String()
	errorUpstream.Close()

	ctx := newTestContext(t, &proxy.Route{
		Domains:   []string{"example.com"},
		Upstreams: []proxy.Upstream{{Host: "127.0.0.1:1"}},
		ErrorMappings: []proxy.ErrorMapping{
			{Status: http.StatusBadGateway, Upstream: address},
		},
	})

	proxyServer := httptest.NewServer(ctx.createProxy())
	defer proxyServer.Close()

	request, err := http.NewRequest(http.MethodGet, proxyServer.URL, nil)
	require.NoError(t, err)
	request.Host = "example.com"

	response, err := proxyServer.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()

	assert.Equal(t, http.StatusBadGateway, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "The server was unable to complete your request")
}

func Test_Context_CreateProxy_redirectsSecondaryDomainsToPrimary(t *testing.T) {
	ctx := newTestContext(t, &proxy.Route{
		Domains:           []string{"example.com", "www.example.com"},
		Upstreams:         []proxy.Upstream{{Host: "127.0.0.1:1"}},
		RedirectToPrimary: true,
	})

	proxyServer := httptest.NewServer(ctx.createProxy())
	defer proxyServer.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	request, err := http.NewRequest(http.MethodGet, proxyServer.URL+"/foo?a=b", nil)
	require.NoError(t, err)
	request.Host = "www.example.com"

	response, err := client.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()

	assert.Equal(t, http.StatusPermanentRedirect, response.StatusCode)
	assert.Equal(t, "http://example.com/foo?a=b", response.Header.Get("Location"))
}

func Test_Context_CreateRedirector_redirectsToHttps(t *testing.T) {
	ctx := newTestContext(t)

	redirector := httptest.NewServer(ctx.createRedirector())
	defer redirector.Close()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	request, err := http.NewRequest(http.MethodGet, redirector.URL+"/foo?a=b", nil)
	require.NoError(t, err)
	request.Host = "example.com"

	response, err := client.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()

	assert.Equal(t, http.StatusPermanentRedirect, response.StatusCode)
	assert.Equal(t, "https://example.com/foo?a=b", response.Header.Get("Location"))
}

func Test_Context_CreateTLSConfig(t *testing.T) {
	cfg := newTestContext(t).createTLSConfig()

	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	assert.Equal(t, []string{"h2", "http/1.1"}, cfg.NextProtos)
	assert.NotNil(t, cfg.GetCertificate)
	assert.Contains(t, cfg.CurvePreferences, tls.X25519MLKEM768)
	assert.Len(t, cfg.CipherSuites, 6)
}

func Test_Server_servesRequestsAndStopsGracefully(t *testing.T) {
	errChan := make(chan error, 1)
	server := NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hello")
	}), errChan)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go server.Start(listener)

	response, err := http.Get(fmt.Sprintf("http://%s", listener.Addr().String()))
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusOK, response.StatusCode)

	server.Stop(t.Context())

	select {
	case err := <-errChan:
		t.Fatalf("Unexpected error from server: %v", err)
	case <-time.After(100 * time.Millisecond):
		// No error reported; graceful stop is treated as ErrServerClosed
	}
}

func Test_Server_reportsServeErrors(t *testing.T) {
	errChan := make(chan error, 1)
	server := NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), errChan)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	// Closing the listener underneath the server causes Serve to fail
	listener.Close()
	go server.Start(listener)

	select {
	case err := <-errChan:
		assert.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for serve error")
	}
}

func Test_handleError(t *testing.T) {
	recorder := httptest.NewRecorder()
	request, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	handleError(recorder, request, assert.AnError)

	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "<h1>Bad Gateway</h1>")
}

func Test_bufferPool(t *testing.T) {
	pool := newBufferPool()

	buffer := pool.Get()
	assert.Len(t, buffer, 32*1024)

	pool.Put(buffer)
	assert.NotPanics(t, func() {
		pool.Get()
	})
}

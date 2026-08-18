package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRewriter creates a Rewriter that always resolves to the given route, with a working
// error client.
func newTestRewriter(route *Route) *Rewriter {
	return &Rewriter{
		provider:    &fakeProvider{route: route},
		errorClient: newErrorClient(),
	}
}

// newUpstreamResponse creates a response as it might be received from an upstream, for a request
// made to the given path.
func newUpstreamResponse(t *testing.T, method string, statusCode int, body string) (*http.Response, *http.Request) {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), method, "http://upstream:8080/some/path?original=yes", nil)
	require.NoError(t, err)
	request.Host = "example.com"
	request.Header.Set("X-Forwarded-Host", "example.com")

	return &http.Response{
		Status:     "status",
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     make(http.Header),
		Request:    request,
	}, request
}

func Test_Rewriter_RewriteResponse_ReplacesErrorResponsesFromUpstream(t *testing.T) {
	errorUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer errorUpstream.Close()

	route := &Route{
		Upstreams:     []Upstream{{Host: "upstream:8080"}},
		ErrorMappings: []ErrorMapping{{Status: 404, Upstream: errorUpstream.Listener.Addr().String()}},
	}
	rewriter := newTestRewriter(route)
	response, _ := newUpstreamResponse(t, http.MethodGet, 404, "original body")
	response.Header.Set("X-Original", "value")

	require.NoError(t, rewriter.RewriteResponse(response))

	// The status code from the original response is preserved; everything else comes from the
	// error upstream.
	assert.Equal(t, 404, response.StatusCode)
	assert.Equal(t, "application/json", response.Header.Get("Content-Type"))
	assert.Empty(t, response.Header.Get("X-Original"))

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, `{"error":"not found"}`, string(body))
}

func Test_Rewriter_RewriteResponse_StripsHopByHopHeadersFromReplacement(t *testing.T) {
	errorUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Keep-Alive", "timeout=5")
		w.Header().Set("X-Kept", "value")
		_, _ = w.Write([]byte("replacement"))
	}))
	defer errorUpstream.Close()

	route := &Route{
		Upstreams:     []Upstream{{Host: "upstream:8080"}},
		ErrorMappings: []ErrorMapping{{Status: 502, Upstream: errorUpstream.Listener.Addr().String()}},
	}
	rewriter := newTestRewriter(route)
	response, _ := newUpstreamResponse(t, http.MethodGet, 502, "original body")

	require.NoError(t, rewriter.RewriteResponse(response))

	assert.Empty(t, response.Header.Get("Keep-Alive"))
	assert.Equal(t, "value", response.Header.Get("X-Kept"))
}

func Test_Rewriter_RewriteResponse_AppliesHeaderRulesToReplacement(t *testing.T) {
	errorUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("replacement"))
	}))
	defer errorUpstream.Close()

	route := &Route{
		Upstreams:     []Upstream{{Host: "upstream:8080"}},
		ErrorMappings: []ErrorMapping{{Status: 502, Upstream: errorUpstream.Listener.Addr().String()}},
		Headers:       []Header{{Name: "X-Via", Value: "Centauri", Operation: HeaderOpAdd}},
	}
	rewriter := newTestRewriter(route)
	response, _ := newUpstreamResponse(t, http.MethodGet, 502, "original body")

	require.NoError(t, rewriter.RewriteResponse(response))

	assert.Equal(t, []string{"Centauri"}, response.Header.Values("X-Via"))
}

func Test_Rewriter_RewriteResponse_RequestsOriginalPathAndQueryByDefault(t *testing.T) {
	var receivedPath, receivedQuery, receivedMethod, receivedHost string
	errorUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath, receivedQuery, receivedMethod, receivedHost = r.URL.Path, r.URL.RawQuery, r.Method, r.Header.Get("X-Forwarded-Host")
		_, _ = w.Write([]byte("replacement"))
	}))
	defer errorUpstream.Close()

	route := &Route{
		Upstreams:     []Upstream{{Host: "upstream:8080"}},
		ErrorMappings: []ErrorMapping{{Status: 404, Upstream: errorUpstream.Listener.Addr().String()}},
	}
	rewriter := newTestRewriter(route)
	response, _ := newUpstreamResponse(t, http.MethodPost, 404, "original body")

	require.NoError(t, rewriter.RewriteResponse(response))

	assert.Equal(t, "/some/path", receivedPath)
	assert.Equal(t, "original=yes", receivedQuery)
	assert.Equal(t, http.MethodGet, receivedMethod)
	assert.Equal(t, "example.com", receivedHost)
}

func Test_Rewriter_RewriteResponse_RequestsConfiguredPathAndQuery(t *testing.T) {
	var receivedPath, receivedQuery string
	errorUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath, receivedQuery = r.URL.Path, r.URL.RawQuery
		_, _ = w.Write([]byte("replacement"))
	}))
	defer errorUpstream.Close()

	route := &Route{
		Upstreams: []Upstream{{Host: "upstream:8080"}},
		ErrorMappings: []ErrorMapping{{
			Status:   404,
			Upstream: errorUpstream.Listener.Addr().String(),
			Path:     "/error",
			RawQuery: "code=404",
		}},
	}
	rewriter := newTestRewriter(route)
	response, _ := newUpstreamResponse(t, http.MethodGet, 404, "original body")

	require.NoError(t, rewriter.RewriteResponse(response))

	assert.Equal(t, "/error", receivedPath)
	assert.Equal(t, "code=404", receivedQuery)
}

func Test_Rewriter_RewriteResponse_DoesNothingForSuccessResponses(t *testing.T) {
	errorUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Error upstream should not have been contacted")
	}))
	defer errorUpstream.Close()

	route := &Route{
		Upstreams:     []Upstream{{Host: "upstream:8080"}},
		ErrorMappings: []ErrorMapping{{Status: 200, Upstream: errorUpstream.Listener.Addr().String()}},
	}
	rewriter := newTestRewriter(route)
	response, _ := newUpstreamResponse(t, http.MethodGet, 200, "original body")

	require.NoError(t, rewriter.RewriteResponse(response))

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, "original body", string(body))
}

func Test_Rewriter_RewriteResponse_DoesNothingWithoutMapping(t *testing.T) {
	errorUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Error upstream should not have been contacted")
	}))
	defer errorUpstream.Close()

	route := &Route{
		Upstreams:     []Upstream{{Host: "upstream:8080"}},
		ErrorMappings: []ErrorMapping{{Status: 502, Upstream: errorUpstream.Listener.Addr().String()}},
	}
	rewriter := newTestRewriter(route)
	response, _ := newUpstreamResponse(t, http.MethodGet, 404, "original body")

	require.NoError(t, rewriter.RewriteResponse(response))

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, "original body", string(body))
}

func Test_Rewriter_RewriteResponse_FallsBackWhenErrorUpstreamUnreachable(t *testing.T) {
	errorUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	address := errorUpstream.Listener.Addr().String()
	errorUpstream.Close()

	route := &Route{
		Upstreams:     []Upstream{{Host: "upstream:8080"}},
		ErrorMappings: []ErrorMapping{{Status: 404, Upstream: address}},
	}
	rewriter := newTestRewriter(route)
	response, _ := newUpstreamResponse(t, http.MethodGet, 404, "original body")

	require.NoError(t, rewriter.RewriteResponse(response))

	assert.Equal(t, 404, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, "original body", string(body))
}

func Test_Rewriter_RewriteError_ServesFromErrorUpstream(t *testing.T) {
	errorUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"down"}`))
	}))
	defer errorUpstream.Close()

	route := &Route{
		Upstreams:     []Upstream{{Host: "upstream:8080"}},
		ErrorMappings: []ErrorMapping{{Status: 502, Upstream: errorUpstream.Listener.Addr().String()}},
		Headers:       []Header{{Name: "X-Via", Value: "Centauri", Operation: HeaderOpAdd}},
	}
	rewriter := newTestRewriter(route)

	fallbackCalled := false
	handler := rewriter.RewriteError(func(w http.ResponseWriter, r *http.Request, err error) {
		fallbackCalled = true
	})

	recorder := httptest.NewRecorder()
	_, request := newUpstreamResponse(t, http.MethodGet, 502, "")
	handler(recorder, request, assert.AnError)

	assert.False(t, fallbackCalled)
	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "Centauri", recorder.Header().Get("X-Via"))
	assert.Equal(t, `{"error":"down"}`, recorder.Body.String())
}

func Test_Rewriter_RewriteError_FallsBackWithoutMapping(t *testing.T) {
	route := &Route{
		Upstreams: []Upstream{{Host: "upstream:8080"}},
		Headers:   []Header{{Name: "X-Via", Value: "Centauri", Operation: HeaderOpAdd}},
	}
	rewriter := newTestRewriter(route)

	fallbackCalled := false
	handler := rewriter.RewriteError(func(w http.ResponseWriter, r *http.Request, err error) {
		fallbackCalled = true
		w.WriteHeader(http.StatusBadGateway)
	})

	recorder := httptest.NewRecorder()
	_, request := newUpstreamResponse(t, http.MethodGet, 502, "")
	handler(recorder, request, assert.AnError)

	assert.True(t, fallbackCalled)
	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Equal(t, "Centauri", recorder.Header().Get("X-Via"))
}

func Test_Rewriter_RewriteError_FallsBackWhenErrorUpstreamUnreachable(t *testing.T) {
	errorUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	address := errorUpstream.Listener.Addr().String()
	errorUpstream.Close()

	route := &Route{
		Upstreams:     []Upstream{{Host: "upstream:8080"}},
		ErrorMappings: []ErrorMapping{{Status: 502, Upstream: address}},
	}
	rewriter := newTestRewriter(route)

	fallbackCalled := false
	handler := rewriter.RewriteError(func(w http.ResponseWriter, r *http.Request, err error) {
		fallbackCalled = true
	})

	recorder := httptest.NewRecorder()
	_, request := newUpstreamResponse(t, http.MethodGet, 502, "")
	handler(recorder, request, assert.AnError)

	assert.True(t, fallbackCalled)
}

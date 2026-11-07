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

// newTestRewriter creates a Rewriter with a working error client.
func newTestRewriter() *Rewriter {
	return &Rewriter{errorClient: newErrorClient(nil)}
}

// newRoutedRequest creates a request as it might be sent to an upstream, after being routed using
// the given route.
func newRoutedRequest(t *testing.T, route *Route, method string) *http.Request {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), method, "http://upstream:8080/some/path?original=yes", nil)
	require.NoError(t, err)
	request = routedTo(request, route)
	request.Host = "example.com"
	request.Header.Set("X-Forwarded-Host", "example.com")
	return request
}

// newUpstreamResponse creates a response as it might be received from an upstream, for a request
// that was routed using the given route. Its body is "original body".
func newUpstreamResponse(t *testing.T, route *Route, method string, statusCode int) *http.Response {
	t.Helper()

	return &http.Response{
		Status:     "status",
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewBufferString("original body")),
		Header:     make(http.Header),
		Request:    newRoutedRequest(t, route, method),
	}
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
	rewriter := newTestRewriter()
	response := newUpstreamResponse(t, route, http.MethodGet, 404)
	defer response.Body.Close()
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
	rewriter := newTestRewriter()
	response := newUpstreamResponse(t, route, http.MethodGet, 502)
	defer response.Body.Close()

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
	rewriter := newTestRewriter()
	response := newUpstreamResponse(t, route, http.MethodGet, 502)
	defer response.Body.Close()

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
	rewriter := newTestRewriter()
	response := newUpstreamResponse(t, route, http.MethodPost, 404)
	defer response.Body.Close()

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
	rewriter := newTestRewriter()
	response := newUpstreamResponse(t, route, http.MethodGet, 404)
	defer response.Body.Close()

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
	rewriter := newTestRewriter()
	response := newUpstreamResponse(t, route, http.MethodGet, 200)
	defer response.Body.Close()

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
	rewriter := newTestRewriter()
	response := newUpstreamResponse(t, route, http.MethodGet, 404)
	defer response.Body.Close()

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
	rewriter := newTestRewriter()
	response := newUpstreamResponse(t, route, http.MethodGet, 404)
	defer response.Body.Close()

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
	rewriter := newTestRewriter()

	fallbackCalled := false
	handler := rewriter.RewriteError(func(w http.ResponseWriter, r *http.Request, err error) {
		fallbackCalled = true
	})

	recorder := httptest.NewRecorder()
	handler(recorder, newRoutedRequest(t, route, http.MethodGet), assert.AnError)

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
	rewriter := newTestRewriter()

	fallbackCalled := false
	handler := rewriter.RewriteError(func(w http.ResponseWriter, r *http.Request, err error) {
		fallbackCalled = true
		w.WriteHeader(http.StatusBadGateway)
	})

	recorder := httptest.NewRecorder()
	handler(recorder, newRoutedRequest(t, route, http.MethodGet), assert.AnError)

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
	rewriter := newTestRewriter()

	fallbackCalled := false
	handler := rewriter.RewriteError(func(w http.ResponseWriter, r *http.Request, err error) {
		fallbackCalled = true
	})

	recorder := httptest.NewRecorder()
	handler(recorder, newRoutedRequest(t, route, http.MethodGet), assert.AnError)

	assert.True(t, fallbackCalled)
}

func Test_Rewriter_RewriteResponse_FollowsRedirectsOnSameUpstream(t *testing.T) {
	errorUpstream := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/final" {
					_, _ = w.Write([]byte("final page"))
					return
				}
				http.Redirect(w, r, "/final", http.StatusFound)
			},
		),
	)
	defer errorUpstream.Close()

	route := &Route{
		Upstreams:     []Upstream{{Host: "upstream:8080"}},
		ErrorMappings: []ErrorMapping{{Status: 502, Upstream: errorUpstream.Listener.Addr().String()}},
	}
	rewriter := newTestRewriter()
	response := newUpstreamResponse(t, route, http.MethodGet, 502)
	defer response.Body.Close()

	require.NoError(t, rewriter.RewriteResponse(response))

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, "final page", string(body))
}

func Test_Rewriter_RewriteResponse_DoesNotFollowRedirectsToOtherHosts(t *testing.T) {
	otherHit := false
	other := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				otherHit = true
				_, _ = w.Write([]byte("other server"))
			},
		),
	)
	defer other.Close()

	errorUpstream := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, other.URL+"/final", http.StatusFound)
			},
		),
	)
	defer errorUpstream.Close()

	route := &Route{
		Upstreams:     []Upstream{{Host: "upstream:8080"}},
		ErrorMappings: []ErrorMapping{{Status: 502, Upstream: errorUpstream.Listener.Addr().String()}},
	}
	rewriter := newTestRewriter()
	response := newUpstreamResponse(t, route, http.MethodGet, 502)
	defer response.Body.Close()

	require.NoError(t, rewriter.RewriteResponse(response))

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, "original body", string(body))
	assert.False(t, otherHit)
}

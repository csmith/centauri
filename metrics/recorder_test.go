package metrics

import (
	"bytes"
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/csmith/centauri/proxy"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routedRequest passes a request for example.com through a proxy.Router serving the given
// routes, and returns it as the router passed it on, with its routing decision.
func routedRequest(t *testing.T, routes ...*proxy.Route) *http.Request {
	t.Helper()

	manager := proxy.NewManager(nil)
	require.NoError(t, manager.SetRoutes(t.Context(), routes, nil))

	var routed *http.Request
	proxy.NewRouter(
		manager,
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { routed = r }),
		http.NotFoundHandler(),
	).ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/", nil),
	)
	require.NotNil(t, routed)
	return routed
}

var exampleRoute = &proxy.Route{
	Domains:   []string{"example.com"},
	Upstreams: []proxy.Upstream{{Host: "upstream:8080"}},
}

func Test_Recorder_TracksBadGatewayErrors(t *testing.T) {
	rec := NewRecorder()

	rec.TrackBadGateway(func(http.ResponseWriter, *http.Request, error) {})(
		httptest.NewRecorder(),
		routedRequest(t, exampleRoute),
		nil,
	)

	expected := `# HELP centauri_response_total The total number of HTTP responses sent to clients
# TYPE centauri_response_total counter
centauri_response_total{route="example.com",status="502"} 1
`

	assert.NoError(t, testutil.CollectAndCompare(rec.registry, bytes.NewBufferString(expected), "centauri_response_total"))
}

func Test_Recorder_DoesNotTrackBadGatewayWhenClientCancelled(t *testing.T) {
	rec := NewRecorder()

	req := routedRequest(t, exampleRoute)
	ctx, cancel := context.WithCancel(req.Context())
	cancel()

	fnCalled := false
	rec.TrackBadGateway(func(http.ResponseWriter, *http.Request, error) { fnCalled = true })(
		httptest.NewRecorder(),
		req.WithContext(ctx),
		context.Canceled,
	)

	assert.True(t, fnCalled)
	assert.Equal(t, 0, testutil.CollectAndCount(rec.registry, "centauri_response_total"))
}

func Test_Recorder_TracksResponses(t *testing.T) {
	rec := NewRecorder()
	track := rec.TrackResponse(func(response *http.Response) error { return nil })

	_ = track(&http.Response{Request: routedRequest(t, exampleRoute), StatusCode: http.StatusOK})
	_ = track(&http.Response{Request: routedRequest(t, exampleRoute), StatusCode: http.StatusOK})
	_ = track(&http.Response{Request: routedRequest(t, exampleRoute), StatusCode: http.StatusNotFound})

	expected := `# HELP centauri_response_total The total number of HTTP responses sent to clients
# TYPE centauri_response_total counter
centauri_response_total{route="example.com",status="200"} 2
centauri_response_total{route="example.com",status="404"} 1
`

	assert.NoError(t, testutil.CollectAndCompare(rec.registry, bytes.NewBufferString(expected), "centauri_response_total"))
}

func Test_Recorder_IgnoresForwardedHostWhenLabellingResponses(t *testing.T) {
	other := &proxy.Route{
		Domains:   []string{"other.example.com"},
		Upstreams: []proxy.Upstream{{Host: "other:8080"}},
	}
	rec := NewRecorder()

	request := routedRequest(t, exampleRoute, other)
	request.Header.Set("X-Forwarded-Host", "other.example.com")
	_ = rec.TrackResponse(func(response *http.Response) error { return nil })(
		&http.Response{Request: request, StatusCode: http.StatusOK},
	)

	expected := `# HELP centauri_response_total The total number of HTTP responses sent to clients
# TYPE centauri_response_total counter
centauri_response_total{route="example.com",status="200"} 1
`

	assert.NoError(t, testutil.CollectAndCompare(rec.registry, bytes.NewBufferString(expected), "centauri_response_total"))
}

func Test_Recorder_DoesNotTrackResponsesWithoutDecision(t *testing.T) {
	rec := NewRecorder()

	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/", nil)
	request.Header.Set("X-Forwarded-Host", "example.com")
	_ = rec.TrackResponse(func(response *http.Response) error { return nil })(
		&http.Response{Request: request, StatusCode: http.StatusOK},
	)

	assert.Equal(t, 0, testutil.CollectAndCount(rec.registry, "centauri_response_total"))
}

func Test_Recorder_TracksClientHellos(t *testing.T) {
	rec := NewRecorder()

	trackHello := rec.TrackHello(func(info *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if info.ServerName == "example.com" {
			return &tls.Certificate{}, nil
		} else {
			return nil, nil
		}
	})

	_, _ = trackHello(&tls.ClientHelloInfo{ServerName: "example.com"})
	_, _ = trackHello(&tls.ClientHelloInfo{ServerName: "example.net"})
	_, _ = trackHello(&tls.ClientHelloInfo{ServerName: "example.com"})

	expected := `# HELP centauri_tls_hello_total The total number of TLS client hellos processed
# TYPE centauri_tls_hello_total counter
centauri_tls_hello_total{known="false"} 1
centauri_tls_hello_total{known="true"} 2
`

	assert.NoError(t, testutil.CollectAndCompare(rec.registry, bytes.NewBufferString(expected), "centauri_tls_hello_total"))
}

func Test_Recorder_TracksBlockedDials(t *testing.T) {
	rec := NewRecorder()

	rec.TrackBlockedDial()
	rec.TrackBlockedDial()

	assert.InDelta(t, 2.0, testutil.ToFloat64(rec.blockedDials), 0)
}

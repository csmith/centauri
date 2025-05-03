package metrics

import (
	"bytes"
	"crypto/tls"
	"github.com/csmith/centauri/proxy"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"net/http"
	"testing"
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

func Test_Recorder_TracksBadGatewayErrors(t *testing.T) {
	rec := NewRecorder(func(domain string) *proxy.Route {
		return &proxy.Route{
			Domains: []string{"example.com"},
		}
	})

	rec.TrackBadGateway(func(http.ResponseWriter, *http.Request, error) {})(
		&fakeResponseWriter{},
		&http.Request{
			Host: "upstream",
			Header: map[string][]string{
				"X-Forwarded-Host": {"example.com"},
			},
		},
		nil,
	)

	expected := `# HELP centauri_response_total The total number of HTTP responses sent to clients
# TYPE centauri_response_total counter
centauri_response_total{route="example.com",status="502"} 1
`

	assert.NoError(t, testutil.CollectAndCompare(rec.registry, bytes.NewBufferString(expected), "centauri_response_total"))
}

func Test_Recorder_TracksResponses(t *testing.T) {
	rec := NewRecorder(func(domain string) *proxy.Route {
		return &proxy.Route{
			Domains: []string{"example.com"},
		}
	})

	_ = rec.TrackResponse(func(response *http.Response) error { return nil })(
		&http.Response{
			Request: &http.Request{
				Host: "upstream",
				Header: map[string][]string{
					"X-Forwarded-Host": {"example.com"},
				},
			},
			ContentLength: 123,
			StatusCode:    200,
		},
	)

	_ = rec.TrackResponse(func(response *http.Response) error { return nil })(
		&http.Response{
			Request: &http.Request{
				Host: "upstream",
				Header: map[string][]string{
					"X-Forwarded-Host": {"example.com"},
				},
			},
			StatusCode:    200,
			ContentLength: 124,
		},
	)

	_ = rec.TrackResponse(func(response *http.Response) error { return nil })(
		&http.Response{
			Request: &http.Request{
				Host: "upstream",
				Header: map[string][]string{
					"X-Forwarded-Host": {"example.com"},
				},
			},
			StatusCode:    404,
			ContentLength: 888,
		},
	)

	expected := `# HELP centauri_content_length_total The total content-length of responses proxied to clients
# TYPE centauri_content_length_total counter
centauri_content_length_total{route="example.com",status="200"} 247
centauri_content_length_total{route="example.com",status="404"} 888

# HELP centauri_response_total The total number of HTTP responses sent to clients
# TYPE centauri_response_total counter
centauri_response_total{route="example.com",status="200"} 2
centauri_response_total{route="example.com",status="404"} 1
`
	assert.NoError(t, testutil.CollectAndCompare(rec.registry, bytes.NewBufferString(expected), "centauri_response_total", "centauri_content_length_total"))
}

func Test_Recorder_TracksClientHellos(t *testing.T) {
	rec := NewRecorder(func(domain string) *proxy.Route { return nil })

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

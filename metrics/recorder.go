package metrics

import (
	"crypto/tls"
	"fmt"
	"github.com/csmith/centauri/proxy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log/slog"
	"net/http"
)

// Recorder provides methods to track metrics for requests
type Recorder struct {
	routeForDomain  func(domain string) *proxy.Route
	registry        *prometheus.Registry
	helloCounter    *prometheus.CounterVec
	responseCounter *prometheus.CounterVec
}

// NewRecorder creates a new Recorder that will use the given function to map
// request hostnames to routes.
func NewRecorder(routeForDomain func(domain string) *proxy.Route) *Recorder {
	r := &Recorder{
		routeForDomain: routeForDomain,
		registry:       prometheus.NewRegistry(),

		helloCounter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "centauri_tls_hello_total",
			Help: "The total number of TLS client hellos processed",
		}, []string{"known"}),

		responseCounter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "centauri_response_total",
			Help: "The total number of HTTP responses sent to clients",
		}, []string{"route", "status"}),
	}
	r.registerMetrics()
	return r
}

// registerMetrics registers the various metrics we will record with the prometheus registry
func (r *Recorder) registerMetrics() {
	// Centauri-specific metrics
	if err := r.registry.Register(r.helloCounter); err != nil {
		slog.Error("Failed to register hello counter", "error", err)
	}

	if err := r.registry.Register(r.responseCounter); err != nil {
		slog.Error("Failed to register response counter", "error", err)
	}

	// Prometheus-supplied general process metrics
	if err := r.registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
		slog.Error("Failed to register process collector", "error", err)
	}

	if err := r.registry.Register(collectors.NewGoCollector()); err != nil {
		slog.Error("Failed to register go collector", "error", err)
	}
}

// Handler returns a HTTP handler that will provide prometheus metrics.
func (r *Recorder) Handler() http.Handler {
	return promhttp.InstrumentMetricHandler(
		r.registry,
		promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{}),
	)
}

// TrackBadGateway wraps the ErrorHandler field of httputil.ReverseProxy,
// recording a response with an implied 502 status code.
func (r *Recorder) TrackBadGateway(fn func(http.ResponseWriter, *http.Request, error)) func(http.ResponseWriter, *http.Request, error) {
	return func(writer http.ResponseWriter, req *http.Request, err error) {
		if route := r.routeForDomain(req.Header.Get("X-Forwarded-Host")); route != nil {
			r.responseCounter.With(prometheus.Labels{
				"route":  route.Domains[0],
				"status": "502",
			}).Inc()
		}

		fn(writer, req, err)
	}
}

// TrackResponse wraps the ModifyResponse field of httputil.ReverseProxy,
// recording the response and its HTTP status code.
func (r *Recorder) TrackResponse(fn func(*http.Response) error) func(*http.Response) error {
	return func(resp *http.Response) error {
		if route := r.routeForDomain(resp.Request.Header.Get("X-Forwarded-Host")); route != nil {
			r.responseCounter.With(prometheus.Labels{
				"route":  route.Domains[0],
				"status": fmt.Sprintf("%d", resp.StatusCode),
			}).Inc()
		}

		return fn(resp)
	}
}

// TrackHello wraps the GetCertificate field of tls.Config, recording whether
// or not a certificate was returned.
func (r *Recorder) TrackHello(fn func(*tls.ClientHelloInfo) (*tls.Certificate, error)) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		cert, err := fn(hello)

		r.helloCounter.With(prometheus.Labels{
			"known": fmt.Sprintf("%t", cert != nil),
		}).Inc()

		return cert, err
	}
}

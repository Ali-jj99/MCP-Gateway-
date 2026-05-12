// Package metrics provides Prometheus instrumentation for the gateway.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mcpgw",
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests.",
	}, []string{"method", "path", "status"})

	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "mcpgw",
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency in seconds.",
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"method", "path"})

	ErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "mcpgw",
		Name:      "http_errors_total",
		Help:      "Total number of HTTP error responses (4xx and 5xx).",
	}, []string{"method", "path", "status"})

	ActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "mcpgw",
		Name:      "active_connections",
		Help:      "Number of active HTTP connections being processed.",
	})
)

func Handler() http.Handler {
	return promhttp.Handler()
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ActiveConnections.Inc()
		defer ActiveConnections.Dec()

		start := time.Now()
		sw := &statusCapture{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r)

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(sw.status)
		path := r.URL.Path

		RequestsTotal.WithLabelValues(r.Method, path, status).Inc()
		RequestDuration.WithLabelValues(r.Method, path).Observe(duration)

		if sw.status >= 400 {
			ErrorsTotal.WithLabelValues(r.Method, path, status).Inc()
		}
	})
}

type statusCapture struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusCapture) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

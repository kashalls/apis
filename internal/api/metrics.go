package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "apis_http_requests_total",
			Help: "Total HTTP requests processed, labeled by method, matched route, and status code.",
		},
		[]string{"method", "route", "status"},
	)
	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "apis_http_request_duration_seconds",
			Help: "HTTP request duration in seconds, labeled by method and matched route.",
		},
		[]string{"method", "route"},
	)
)

// metricsMiddleware records a request count and duration for every
// request, labeled by the matched chi route pattern rather than the raw
// URL, so path parameters (or unmatched garbage) don't blow up
// cardinality. Must be registered before middleware.Recoverer so a
// recovered panic's status still gets counted.
func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		httpRequestsTotal.WithLabelValues(r.Method, route, strconv.Itoa(ww.Status())).Inc()
		httpRequestDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

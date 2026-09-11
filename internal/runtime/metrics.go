package runtime

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// dependencyPollInterval is how often PollDependencyHealth re-checks
// each dependency. A scrape between polls sees the last-known state, not
// a live check - fine for a gauge whose purpose is "has this dependency
// been up recently," not a synchronous health check (that's /health).
const dependencyPollInterval = 15 * time.Second

// Metrics holds every collector this binary registers. Built once at
// startup and passed to MetricsMiddleware and MetricsHandler - see
// design.md in the phase-8-hardening change for why a metrics endpoint
// exists at all (a standard, scrapable alternative to reading logs or
// querying Postgres directly for operational state).
type Metrics struct {
	registry *prometheus.Registry

	httpRequestsTotal   *prometheus.CounterVec
	httpRequestDuration *prometheus.HistogramVec

	dependencyUp *prometheus.GaugeVec
}

// NewMetrics builds a Metrics with its own registry (not the global
// default one, so tests can build multiple independent instances without
// a "duplicate metrics collector registration" panic).
func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	factory := promauto.With(registry)

	return &Metrics{
		registry: registry,
		httpRequestsTotal: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "console_http_requests_total",
			Help: "Total HTTP requests, by route pattern and status code.",
		}, []string{"route", "status"}),
		httpRequestDuration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "console_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds, by route pattern.",
			Buckets: prometheus.DefBuckets,
		}, []string{"route"}),
		dependencyUp: factory.NewGaugeVec(prometheus.GaugeOpts{
			Name: "console_dependency_up",
			Help: "Whether a health-check dependency is currently reachable (1) or not (0).",
		}, []string{"dependency"}),
	}
}

// Handler serves the Prometheus text-format scrape endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// SetDependencyUp records whether dependency (e.g. "postgres", "nats" -
// the same Checker.Name() values HealthHandler already uses) is
// currently reachable.
func (m *Metrics) SetDependencyUp(dependency string, up bool) {
	value := 0.0
	if up {
		value = 1.0
	}
	m.dependencyUp.WithLabelValues(dependency).Set(value)
}

// GaugeFunc registers a gauge whose value is computed on each scrape by
// calling value - used for transport/orchestrator/revocation counts that
// live in another package's own mutex-guarded state (see
// nodetransport.Registry.Len, orchestrator.Dispatcher.PendingCount,
// rbac.Revoker.Len), so this package never needs to poll or cache them
// itself.
func (m *Metrics) GaugeFunc(name, help string, value func() float64) {
	promauto.With(m.registry).NewGaugeFunc(prometheus.GaugeOpts{
		Name: name,
		Help: help,
	}, value)
}

// WrapMux wraps mux so every request is recorded (count + duration) by
// its matched route pattern (e.g. "GET /api/v1/nodes/{name}", not the
// raw URL path - so metrics stay low-cardinality even with a certname or
// job ID in the URL) and status code. A single wrap around the whole mux
// in cmd/console/main.go, rather than instrumenting every individual
// Register() call site across every package - http.ServeMux.Handler
// already resolves the matched pattern for free.
func WrapMux(mux *http.ServeMux, m *Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := mux.Handler(r)
		route := pattern
		if route == "" {
			route = "unmatched"
		}

		start := time.Now()
		sw := &statusCapturingWriter{ResponseWriter: w, status: http.StatusOK}
		mux.ServeHTTP(sw, r)

		m.httpRequestsTotal.WithLabelValues(route, strconv.Itoa(sw.status)).Inc()
		m.httpRequestDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
	})
}

// PollDependencyHealth periodically runs every checker (the same
// Checker instances HealthHandler already uses - db, bus in
// cmd/console/main.go) and records each one's up/down state as a
// console_dependency_up gauge, until ctx is cancelled. Runs once
// immediately, then every dependencyPollInterval.
func PollDependencyHealth(ctx context.Context, m *Metrics, checkers ...Checker) {
	check := func() {
		for _, c := range checkers {
			m.SetDependencyUp(c.Name(), c.Check(ctx) == nil)
		}
	}
	check()

	ticker := time.NewTicker(dependencyPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

type statusCapturingWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusCapturingWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

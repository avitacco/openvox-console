package runtime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsHandler_ServesPrometheusFormat(t *testing.T) {
	m := NewMetrics()

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want a Prometheus text format", ct)
	}
}

func TestWrapMux_RecordsRequestCountAndRoute(t *testing.T) {
	m := NewMetrics()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/nodes/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := WrapMux(mux, m)

	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/nodes/web01", nil))
	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/nodes/db01", nil))

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()

	if !strings.Contains(body, `route="GET /api/v1/nodes/{name}"`) {
		t.Errorf("expected a low-cardinality route label (the mux pattern, not the raw path with certnames) in:\n%s", body)
	}
	if strings.Contains(body, `route="GET /api/v1/nodes/web01"`) {
		t.Error("route label used the raw URL path instead of the mux pattern - this would be high-cardinality")
	}
	if !strings.Contains(body, `status="200"`) {
		t.Errorf("expected a status label in:\n%s", body)
	}
}

func TestWrapMux_UnmatchedRouteRecordedDistinctly(t *testing.T) {
	m := NewMetrics()
	mux := http.NewServeMux()
	wrapped := WrapMux(mux, m)

	wrapped.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), `route="unmatched"`) {
		t.Errorf("expected an unmatched route to be labeled distinctly, got:\n%s", rec.Body.String())
	}
}

func TestMetrics_SetDependencyUp(t *testing.T) {
	m := NewMetrics()
	m.SetDependencyUp("postgres", true)
	m.SetDependencyUp("nats", false)

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()

	if !strings.Contains(body, `console_dependency_up{dependency="postgres"} 1`) {
		t.Errorf("expected postgres up=1 in:\n%s", body)
	}
	if !strings.Contains(body, `console_dependency_up{dependency="nats"} 0`) {
		t.Errorf("expected nats up=0 in:\n%s", body)
	}
}

func TestPollDependencyHealth_RecordsInitialStateImmediately(t *testing.T) {
	m := NewMetrics()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		PollDependencyHealth(ctx, m, fakeChecker{name: "postgres", err: nil}, fakeChecker{name: "nats", err: errors.New("down")})
		close(done)
	}()

	// PollDependencyHealth checks once synchronously before its first
	// ticker wait, but it's still running in its own goroutine - poll
	// briefly for the gauge to land rather than racing it.
	deadline := time.Now().Add(time.Second)
	var body string
	for time.Now().Before(deadline) {
		rec := httptest.NewRecorder()
		m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		body = rec.Body.String()
		if strings.Contains(body, `console_dependency_up{dependency="postgres"} 1`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(body, `console_dependency_up{dependency="postgres"} 1`) {
		t.Errorf("expected postgres up=1 in:\n%s", body)
	}
	if !strings.Contains(body, `console_dependency_up{dependency="nats"} 0`) {
		t.Errorf("expected nats up=0 in:\n%s", body)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("PollDependencyHealth did not stop after context cancellation")
	}
}

func TestMetrics_GaugeFunc(t *testing.T) {
	m := NewMetrics()
	count := 3
	m.GaugeFunc("test_gauge", "a test gauge", func() float64 { return float64(count) })

	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "test_gauge 3") {
		t.Errorf("expected test_gauge 3 in:\n%s", rec.Body.String())
	}

	count = 7
	rec = httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(rec.Body.String(), "test_gauge 7") {
		t.Errorf("expected test_gauge to be recomputed on scrape (7) in:\n%s", rec.Body.String())
	}
}

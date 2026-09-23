package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeChecker struct {
	name string
	err  error
}

func (f fakeChecker) Name() string                  { return f.name }
func (f fakeChecker) Check(_ context.Context) error { return f.err }

func TestHealthHandler_AllHealthy(t *testing.T) {
	handler := HealthHandler(ModeAll,
		fakeChecker{name: "postgres"},
		fakeChecker{name: "nats"},
	)

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want ok", resp.Status)
	}
	if resp.Checks["postgres"] != "ok" || resp.Checks["nats"] != "ok" {
		t.Errorf("checks = %+v, want both ok", resp.Checks)
	}
}

func TestHealthHandler_DependencyUnhealthy(t *testing.T) {
	handler := HealthHandler(ModeAll,
		fakeChecker{name: "postgres", err: errors.New("connection refused")},
		fakeChecker{name: "nats"},
	)

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "unhealthy" {
		t.Errorf("status = %q, want unhealthy", resp.Status)
	}
	if resp.Checks["postgres"] != "connection refused" {
		t.Errorf("postgres check = %q, want the error message", resp.Checks["postgres"])
	}
	if resp.Checks["nats"] != "ok" {
		t.Errorf("nats check = %q, want ok", resp.Checks["nats"])
	}
}

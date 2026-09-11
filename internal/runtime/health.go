package runtime

import (
	"context"
	"encoding/json"
	"net/http"
)

// Checker is a named dependency the health check endpoint reports on.
type Checker interface {
	Name() string
	Check(ctx context.Context) error
}

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// HealthHandler returns an http.HandlerFunc that reports success only when
// every checker succeeds, and otherwise reports which dependency failed.
func HealthHandler(checkers ...Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		resp := healthResponse{Status: "ok", Checks: map[string]string{}}

		for _, c := range checkers {
			if err := c.Check(ctx); err != nil {
				resp.Status = "unhealthy"
				resp.Checks[c.Name()] = err.Error()
			} else {
				resp.Checks[c.Name()] = "ok"
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if resp.Status != "ok" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

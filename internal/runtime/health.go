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
	Status string `json:"status"`
	// Mode is the run mode this instance is serving, so an operator or
	// load balancer checking an instance learns what it is without
	// having to know how it was configured.
	Mode   string            `json:"mode"`
	Checks map[string]string `json:"checks"`
}

// HealthHandler returns an http.HandlerFunc that reports success only when
// every checker succeeds, and otherwise reports which dependency failed.
//
// The checkers passed are the active mode's own dependencies, not a
// fixed list: an instance must not be reported unhealthy for something
// its mode never contacts. An enc instance has no node transport, so a
// health response naming one would either lie or fail, and a load
// balancer would drain a healthy instance over it.
func HealthHandler(mode Mode, checkers ...Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		resp := healthResponse{Status: "ok", Mode: mode.String(), Checks: map[string]string{}}

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

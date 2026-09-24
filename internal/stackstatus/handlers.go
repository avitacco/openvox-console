package stackstatus

import (
	"encoding/json"
	"net/http"
)

// Handlers serves the stack status API.
type Handlers struct {
	bus      asker
	reporter *Reporter
}

// NewHandlers builds Handlers that ask bus and report reporter's
// instance as the serving one.
func NewHandlers(bus asker, reporter *Reporter) *Handlers {
	return &Handlers{bus: bus, reporter: reporter}
}

// Register wires this package's route onto mux, requiring status:read -
// see internal/rbac.Verifier.Authorize for what authorize does.
//
// The permission is its own rather than reusing rbac:admin because
// "see the shape of the deployment" and "administer users and roles" are
// different jobs, and an operator may legitimately need the first
// without the second.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/status", authorize("status:read", h.stackStatus))
}

func (h *Handlers) stackStatus(w http.ResponseWriter, r *http.Request) {
	self := h.reporter.Report(r.Context())
	stack := Aggregate(r.Context(), h.bus, self)

	// Always 200, even when the picture is incomplete. Incompleteness is
	// part of the answer, not a failure to answer: a non-2xx here would
	// make a page that says "one instance did not reply" indistinguishable
	// from one that could not load at all.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stack)
}

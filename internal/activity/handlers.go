package activity

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

// listStore is the subset of *Store this package's handlers need, so
// they can be tested against a fake without a live Postgres.
type listStore interface {
	ListEvents(ctx context.Context, page, pageSize int, filter EventFilter) ([]Event, int, error)
	DistinctCategories(ctx context.Context) ([]string, error)
	DistinctActions(ctx context.Context) ([]string, error)
}

// Handlers serves the activity log HTTP API.
type Handlers struct {
	store listStore
}

// NewHandlers builds Handlers backed by s.
func NewHandlers(s listStore) *Handlers {
	return &Handlers{store: s}
}

// Register wires this package's route onto mux, requiring activity:read
// - see internal/rbac.Verifier.Authorize for what authorize does.
//
// The route is "audit-log", not "activity": some browser ad/privacy
// blocklists filter fetch/XHR requests whose path contains generic
// tracking-adjacent words like "activity", which broke this exact
// endpoint for real users - see the phase-4-activity-and-audit-log
// change's follow-up fix.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/audit-log", authorize("activity:read", h.listEvents))
	// Populate the category/action filters with real recorded values
	// rather than a hardcoded guess at them - see DistinctCategories'
	// doc comment for why there's no fixed enum to draw from instead.
	mux.HandleFunc("GET /api/v1/audit-log/categories", authorize("activity:read", h.listCategories))
	mux.HandleFunc("GET /api/v1/audit-log/actions", authorize("activity:read", h.listActions))
}

func (h *Handlers) listEvents(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pagination.Params(r)
	filter := EventFilter{
		Category: r.URL.Query().Get("category"),
		Action:   r.URL.Query().Get("action"),
		Actor:    r.URL.Query().Get("actor"),
		SortAsc:  r.URL.Query().Get("sort") == "asc",
	}
	events, total, err := h.store.ListEvents(r.Context(), page, pageSize, filter)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, pagination.New(events, page, pageSize, total))
}

func (h *Handlers) listCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := h.store.DistinctCategories(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, categories)
}

func (h *Handlers) listActions(w http.ResponseWriter, r *http.Request) {
	actions, err := h.store.DistinctActions(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, actions)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

package classcatalog

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/voxpupuli/enterprise-console/internal/taskcatalog"
)

// Handlers serves the class catalogue API. Reading needs
// classifier:read - it is what someone editing a node group browses -
// and asking for a refresh needs classifier:write.
//
// None of these routes carries node data, so none is limited by node
// group scope.
type Handlers struct {
	catalog *Catalog
}

// NewHandlers builds Handlers reading from catalog.
func NewHandlers(catalog *Catalog) *Handlers {
	return &Handlers{catalog: catalog}
}

// Register wires the routes onto mux - see internal/rbac.Verifier.Authorize
// for what authorize does.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/classes/environments", authorize("classifier:read", h.listEnvironments))
	mux.HandleFunc("GET /api/v1/classes", authorize("classifier:read", h.listClasses))
	mux.HandleFunc("POST /api/v1/classes/refresh", authorize("classifier:write", h.refresh))
}

// DefaultEnvironment is the environment Puppet uses when nothing names
// one.
const DefaultEnvironment = "production"

func environmentParam(r *http.Request) string {
	if env := r.URL.Query().Get("environment"); env != "" {
		return env
	}
	return DefaultEnvironment
}

func (h *Handlers) listEnvironments(w http.ResponseWriter, r *http.Request) {
	envs, err := h.catalog.Environments(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if envs == nil {
		envs = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"environments": envs})
}

func (h *Handlers) listClasses(w http.ResponseWriter, r *http.Request) {
	env, err := h.catalog.Classes(r.Context(), environmentParam(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

func (h *Handlers) refresh(w http.ResponseWriter, r *http.Request) {
	env, err := h.catalog.Refresh(r.Context(), environmentParam(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// writeError answers 404 for an environment the server does not have,
// and 503 with a reason for discovery being unavailable - one status for
// all three reasons, because to the caller they are the same condition
// (no list of classes) with different remedies.
func writeError(w http.ResponseWriter, err error) {
	var nf *taskcatalog.NotFoundError
	if errors.As(err, &nf) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	reason := Reason(err)
	if reason == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error(), "reason": reason})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

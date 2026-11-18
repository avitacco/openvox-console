package agentupgrade

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/nodescope"
)

// upgrader is the part of *Coordinator the handlers use.
type upgrader interface {
	UpgradeNow(ctx context.Context, certname string) error
}

// Handlers serves the manual-upgrade endpoint.
type Handlers struct {
	coordinator upgrader
	recordAudit func(r *http.Request, event auditlog.Event)
	logger      *slog.Logger
	scope       nodescope.Func
}

// SetScope limits a manual upgrade to the nodes in the caller's
// nodes:manage node group scope - see internal/nodescope. The upgrades
// the console starts by itself are not a user's action and are not
// affected.
func (h *Handlers) SetScope(scope nodescope.Func) { h.scope = scope }

// NewHandlers builds Handlers. recordAudit records an upgrade request
// that was accepted, like the certificate actions' audit entries.
func NewHandlers(coordinator upgrader, recordAudit func(r *http.Request, event auditlog.Event), logger *slog.Logger) *Handlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handlers{coordinator: coordinator, recordAudit: recordAudit, logger: logger, scope: nodescope.Unscoped}
}

// Register wires the endpoint onto mux. Upgrading a node's client
// installs software on it, so it takes nodes:manage - the permission
// that already covers removing a node - not nodes:read.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /api/v1/nodes/{name}/agent-upgrade", authorize("nodes:manage", h.upgrade))
}

type upgradeAccepted struct {
	Status string `json:"status"`
}

type upgradeRefused struct {
	Error string `json:"error"`
	// Reason is a stable code for the refusal: not-connected, busy,
	// current, in-progress, install-script, declined or no-artifact.
	Reason string `json:"reason"`
}

// upgrade starts an upgrade of one node's client. 202 when the node has
// been told to upgrade (the outcome then shows in its status); 409 with
// the reason when it cannot be.
func (h *Handlers) upgrade(w http.ResponseWriter, r *http.Request) {
	certname := r.PathValue("name")

	// Before anything is asked of the node or looked up about it.
	if !nodescope.Permit(w, r, h.scope, certname) {
		return
	}

	err := h.coordinator.UpgradeNow(r.Context(), certname)
	var cannot *CannotUpgradeError
	switch {
	case err == nil:
		h.recordAudit(r, auditlog.Event{Action: "node.agent.upgrade_requested", ResourceType: "node", ResourceID: certname})
		writeJSON(w, http.StatusAccepted, upgradeAccepted{Status: UpgradeRequested})
	case errors.Is(err, ErrNotConnected):
		writeJSON(w, http.StatusConflict, upgradeRefused{Error: err.Error(), Reason: "not-connected"})
	case errors.Is(err, ErrBusy):
		writeJSON(w, http.StatusConflict, upgradeRefused{Error: err.Error(), Reason: "busy"})
	case errors.Is(err, ErrCurrent):
		writeJSON(w, http.StatusConflict, upgradeRefused{Error: err.Error(), Reason: "current"})
	case errors.Is(err, ErrInProgress):
		writeJSON(w, http.StatusConflict, upgradeRefused{Error: err.Error(), Reason: "in-progress"})
	case errors.As(err, &cannot):
		writeJSON(w, http.StatusConflict, upgradeRefused{Error: err.Error(), Reason: cannot.Blocked})
	default:
		h.logger.Error("client upgrade request failed", "certname", certname, "error", err)
		writeJSON(w, http.StatusInternalServerError, upgradeRefused{Error: "could not start the upgrade: " + err.Error(), Reason: "error"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

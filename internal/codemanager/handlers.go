package codemanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/pagination"
)

const defaultEnvironment = "production"

// Handlers serves the code-manager HTTP API: manual/webhook deploy
// triggers and deploy history.
type Handlers struct {
	deployer       *Deployer
	store          *Store
	bus            eventPublisher
	webhookSecret  string
	logger         *slog.Logger
	recordActivity func(r *http.Request, action, summary string)
	actor          func(r *http.Request) string

	// recordAudit/recordAuditRead are the configurable-audit-logging
	// capability's equivalent of recordActivity, bound to the code
	// category - see design.md in that change.
	recordAudit     func(r *http.Request, event auditlog.Event)
	recordAuditRead func(r *http.Request, event auditlog.Event)
}

// NewHandlers builds Handlers. deployer may be unconfigured (see
// Deployer.Configured) - endpoints then respond 503 rather than the
// console failing to start, matching OIDC's posture for optional
// features. actor extracts the acting user's identity from a request -
// injected, like recordActivity, so this package never imports
// internal/rbac directly (see design.md).
func NewHandlers(
	deployer *Deployer, store *Store, bus eventPublisher, webhookSecret string, logger *slog.Logger,
	recordActivity func(r *http.Request, action, summary string), actor func(r *http.Request) string,
	recordAudit, recordAuditRead func(r *http.Request, event auditlog.Event),
) *Handlers {
	return &Handlers{
		deployer:        deployer,
		store:           store,
		bus:             bus,
		webhookSecret:   webhookSecret,
		logger:          logger,
		recordActivity:  recordActivity,
		actor:           actor,
		recordAudit:     recordAudit,
		recordAuditRead: recordAuditRead,
	}
}

// Register wires this package's routes onto mux. The webhook endpoint
// verifies its own signature rather than using the permission-based
// authorize wrapper - a git host has no console user token to present.
func (h *Handlers) Register(mux *http.ServeMux, authorize func(permission string, next http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /api/v1/code-deploys", authorize("code:deploy", h.triggerDeploy))
	mux.HandleFunc("POST /api/v1/code-deploys/webhook", h.webhookDeploy)
	mux.HandleFunc("GET /api/v1/code-deploys", authorize("code:read", h.listDeploys))
	// Populates the ref filter with real deployed values rather than a
	// hardcoded guess at them - see DistinctRefs' doc comment for why
	// there's no fixed enum to draw from instead (status, unlike ref,
	// already is one - StatusRunning/Succeeded/Failed - so it doesn't
	// need an equivalent route).
	mux.HandleFunc("GET /api/v1/code-deploys/refs", authorize("code:read", h.listRefs))
}

type triggerDeployRequest struct {
	Environment string `json:"environment"`
}

func (h *Handlers) triggerDeploy(w http.ResponseWriter, r *http.Request) {
	var req triggerDeployRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, errors.New("invalid request body"))
			return
		}
	}
	environment := req.Environment
	if environment == "" {
		environment = defaultEnvironment
	}

	deploy, err := h.runDeploy(r.Context(), environment, environment, h.actor(r), r)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, deploy)
}

func (h *Handlers) webhookDeploy(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("could not read request body"))
		return
	}

	if h.webhookSecret == "" || !verifyWebhookSignature(h.webhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		writeError(w, http.StatusUnauthorized, errors.New("invalid webhook signature"))
		return
	}

	payload, err := parseWebhookPayload(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	environment, err := environmentFromRef(payload.Ref)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	ref := payload.After
	if ref == "" {
		ref = environment
	}

	deploy, err := h.runDeploy(r.Context(), environment, ref, "webhook", r)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, deploy)
}

func (h *Handlers) listDeploys(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pagination.Params(r)

	filter := DeployFilter{
		Ref:         r.URL.Query().Get("ref"),
		TriggeredBy: r.URL.Query().Get("triggeredBy"),
		SortAsc:     r.URL.Query().Get("sort") == "asc",
	}
	if status := r.URL.Query().Get("status"); status != "" {
		switch status {
		case StatusRunning, StatusSucceeded, StatusFailed:
			filter.Status = status
		default:
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid status %q: must be %q, %q, or %q", status, StatusRunning, StatusSucceeded, StatusFailed))
			return
		}
	}

	deploys, total, err := h.store.ListDeploys(r.Context(), page, pageSize, filter)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	h.recordAuditRead(r, auditlog.Event{Action: "deploy.list.viewed", ResourceType: "deploy"})
	writeJSON(w, http.StatusOK, pagination.New(deploys, page, pageSize, total))
}

func (h *Handlers) listRefs(w http.ResponseWriter, r *http.Request) {
	refs, err := h.store.DistinctRefs(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, refs)
}

// runDeploy is the full pipeline shared by the manual and webhook
// triggers: record a running attempt, run g10k, activate on success,
// record the final status, publish the deployment-ready event, and
// record an activity entry either way - see design.md and
// specs/code-manager/spec.md for each step's contract.
func (h *Handlers) runDeploy(ctx context.Context, environment, ref, triggeredBy string, r *http.Request) (Deploy, error) {
	if !h.deployer.Configured() {
		return Deploy{}, errors.New("code deployment is not configured")
	}

	id, err := h.store.CreateDeploy(ctx, ref, triggeredBy)
	if err != nil {
		return Deploy{}, fmt.Errorf("record deploy start: %w", err)
	}
	deployID := strconv.FormatInt(id, 10)

	fail := func(deployErr error) (Deploy, error) {
		if err := h.store.CompleteDeploy(ctx, id, StatusFailed, deployErr.Error()); err != nil {
			h.logger.Error("failed to record deploy failure", "error", err, "deployID", id)
		}
		h.recordActivity(r, "deploy.failed", fmt.Sprintf("deploy of %s failed: %s", environment, deployErr.Error()))
		h.recordAudit(r, auditlog.Event{Action: "deploy.failed", ResourceType: "deploy", ResourceID: deployID})
		return Deploy{}, deployErr
	}

	stagingDir, err := h.deployer.Run(ctx, environment)
	if err != nil {
		return fail(err)
	}

	if err := h.deployer.Activate(environment, stagingDir); err != nil {
		return fail(err)
	}

	if err := h.store.CompleteDeploy(ctx, id, StatusSucceeded, ""); err != nil {
		h.logger.Error("failed to record deploy success", "error", err, "deployID", id)
	}
	if err := PublishDeployed(h.bus, environment, ref); err != nil {
		h.logger.Error("failed to publish deployment-ready event", "error", err, "environment", environment)
	}
	h.recordActivity(r, "deploy.succeeded", fmt.Sprintf("deployed %s (%s)", environment, ref))
	h.recordAudit(r, auditlog.Event{Action: "deploy.succeeded", ResourceType: "deploy", ResourceID: deployID})

	completed, err := h.store.GetDeploy(ctx, id)
	if err != nil {
		return Deploy{}, fmt.Errorf("load completed deploy: %w", err)
	}
	return completed, nil
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

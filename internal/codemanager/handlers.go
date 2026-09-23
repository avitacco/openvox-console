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
	deployer     *Deployer
	deployLeases deployLeaser
	store        *Store
	bus          eventPublisher
	// webhookSecret is CONSOLE_CODE_WEBHOOK_SECRET: the secret for the
	// unsuffixed webhook route. A source declaring its own
	// webhook_secret uses that instead - see secretFor.
	webhookSecret  string
	logger         *slog.Logger
	recordActivity func(r *http.Request, action, summary string)
	actor          func(r *http.Request) string

	// recordAudit/recordAuditRead are the configurable-audit-logging
	// capability's equivalent of recordActivity, bound to the code
	// category - see design.md in that change.
	recordAudit     func(r *http.Request, event auditlog.Event)
	recordAuditRead func(r *http.Request, event auditlog.Event)

	// usage supplies the repository overview's node counts. May be nil
	// or unwired - the overview then reports counts as unavailable
	// rather than failing (see listRepositories).
	usage *UsageResolver
}

// SetUsageResolver wires the node-count source for the repository
// overview. Injected after construction, like packageinventory's group
// resolver, because it needs openvoxdb and the classifier - which this
// package deliberately does not import.
func (h *Handlers) SetUsageResolver(u *UsageResolver) {
	h.usage = u
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
	// Per-source webhooks, each verified against that source's own
	// secret, so a secret leaked from one repo cannot deploy another.
	// The unsuffixed route above stays for the default source, which is
	// what every already-configured git host is pointed at.
	mux.HandleFunc("POST /api/v1/code-deploys/webhook/{source}", h.webhookDeploy)
	mux.HandleFunc("GET /api/v1/code-deploys", authorize("code:read", h.listDeploys))
	// Populates the source filter and the manual-deploy picker.
	mux.HandleFunc("GET /api/v1/code-deploys/sources", authorize("code:read", h.listSources))
	// Populates the ref filter with real deployed values rather than a
	// hardcoded guess at them - see DistinctRefs' doc comment for why
	// there's no fixed enum to draw from instead (status, unlike ref,
	// already is one - StatusRunning/Succeeded/Failed - so it doesn't
	// need an equivalent route).
	mux.HandleFunc("GET /api/v1/code-deploys/refs", authorize("code:read", h.listRefs))
	// The repository overview: one row per configured control repo,
	// answering "is this repo doing anything" rather than "what
	// happened recently", which is what code-deploys covers.
	mux.HandleFunc("GET /api/v1/code-repositories", authorize("code:read", h.listRepositories))
}

type triggerDeployRequest struct {
	Source      string `json:"source"`
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
	branch := req.Environment
	if branch == "" {
		branch = defaultEnvironment
	}

	// An unknown source is the caller's mistake, not an upstream
	// failure, so it is a 400 rather than the 502 a failed deploy gets.
	source, ok := h.deployer.Sources().Find(req.Source)
	if !ok {
		if !h.deployer.Configured() {
			writeError(w, http.StatusServiceUnavailable, errors.New("code deployment is not configured"))
			return
		}
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown code source %q", req.Source))
		return
	}

	deploy, err := h.runDeploy(r.Context(), source, branch, branch, h.actor(r), r)
	if err != nil {
		if errors.Is(err, ErrDeployInProgress) {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, deploy)
}

// webhookDeploy serves both the unsuffixed route (the default source)
// and the per-source one. An unknown source and a bad signature are
// deliberately reported identically: telling an unauthenticated caller
// that a source name does not exist would let it enumerate the
// configured ones.
func (h *Handlers) webhookDeploy(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("could not read request body"))
		return
	}

	unauthorized := func() {
		writeError(w, http.StatusUnauthorized, errors.New("invalid webhook signature"))
	}

	source, ok := h.deployer.Sources().Find(r.PathValue("source"))
	if !ok {
		unauthorized()
		return
	}

	secret := h.secretFor(source)
	if secret == "" || !verifyWebhookSignature(secret, body, r.Header.Get("X-Hub-Signature-256")) {
		unauthorized()
		return
	}

	payload, err := parseWebhookPayload(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	branch, err := environmentFromRef(payload.Ref)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	ref := payload.After
	if ref == "" {
		ref = branch
	}

	deploy, err := h.runDeploy(r.Context(), source, branch, ref, "webhook", r)
	if err != nil {
		if errors.Is(err, ErrDeployInProgress) {
			writeError(w, http.StatusConflict, err)
			return
		}
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, deploy)
}

// secretFor returns the webhook secret to verify a push to source
// against: the source's own if it declares one, otherwise the global
// CONSOLE_CODE_WEBHOOK_SECRET. The fallback is what keeps a
// single-repo installation's existing webhook working untouched; it
// applies only to the default source, so one source's pushes can never
// be authenticated by another source's credential.
func (h *Handlers) secretFor(source Source) string {
	if source.WebhookSecret != "" {
		return source.WebhookSecret
	}
	if def, ok := h.deployer.Sources().Default(); ok && def.Name == source.Name {
		return h.webhookSecret
	}
	return ""
}

// listSources reports the sources the UI needs: every configured one
// for the deploy picker, followed by any source that appears in deploy
// history but is no longer configured, so the history filter can still
// narrow to a source whose repo has since been removed. Configured
// says which is which - only a configured source can be deployed to.
//
// Deliberately name and prefix only: a source's private key path and
// webhook secret are configuration, not something any console user
// needs.
func (h *Handlers) listSources(w http.ResponseWriter, r *http.Request) {
	type sourceSummary struct {
		Name       string `json:"name"`
		Prefix     string `json:"prefix"`
		Configured bool   `json:"configured"`
	}

	summaries := make([]sourceSummary, 0)
	configured := make(map[string]bool)
	for _, src := range h.deployer.Sources() {
		configured[src.Name] = true
		summaries = append(summaries, sourceSummary{Name: src.Name, Prefix: src.EffectivePrefix(), Configured: true})
	}

	// A history-only source is a filter convenience, so failing to read
	// them degrades to the configured list rather than failing the
	// request and leaving the page with no source filter at all.
	recorded, err := h.store.DistinctSources(r.Context())
	if err != nil {
		h.logger.Warn("could not list sources from deploy history", "error", err)
	} else {
		for _, name := range recorded {
			if !configured[name] {
				summaries = append(summaries, sourceSummary{Name: name})
			}
		}
	}

	writeJSON(w, http.StatusOK, summaries)
}

func (h *Handlers) listDeploys(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pagination.Params(r)

	filter := DeployFilter{
		Source:      r.URL.Query().Get("source"),
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
func (h *Handlers) runDeploy(ctx context.Context, source Source, branch, ref, triggeredBy string, r *http.Request) (Deploy, error) {
	if !h.deployer.Configured() {
		return Deploy{}, errors.New("code deployment is not configured")
	}

	// Serialized across the cluster, per environment. A deploy writes
	// one environment's directory tree, so two running at once against
	// the same environment would interleave g10k's work and the
	// symlink swap. Two *different* environments are independent, hence
	// a lease per environment rather than one global deploy lease.
	//
	// Nil when no leases were wired in (a test constructing Handlers
	// directly): deploys then behave exactly as they did before, which
	// is correct for a single instance.
	if h.deployLeases == nil {
		return h.runDeployLocked(ctx, source, branch, ref, triggeredBy, r)
	}

	var deploy Deploy
	acquired, err := h.deployLeases.Hold(ctx, DeployLeaseName(source.EnvironmentFor(branch)),
		func(ctx context.Context) error {
			var err error
			deploy, err = h.runDeployLocked(ctx, source, branch, ref, triggeredBy, r)
			return err
		})
	if err != nil {
		return Deploy{}, err
	}
	if !acquired {
		return Deploy{}, ErrDeployInProgress
	}
	return deploy, nil
}

// ErrDeployInProgress reports that another console instance is already
// deploying this environment. Distinguished from a deploy *failure*:
// nothing went wrong, and the caller can retry once the running deploy
// finishes.
var ErrDeployInProgress = errors.New("a deploy of this environment is already running")

// deployLeaser is the subset of *leases.Leases this package needs, so it
// does not depend on that package's construction.
type deployLeaser interface {
	Hold(ctx context.Context, name string, fn func(context.Context) error) (bool, error)
}

// DeployLeaseName is the lease an environment's deploy is coordinated
// under, namespaced so it cannot collide with another singleton's.
func DeployLeaseName(environment string) string { return "code-deploy/" + environment }

// SetDeployLeases wires cluster-wide deploy serialization, the same
// optional-dependency shape as SetUsageResolver. Without it, deploys are
// not serialized - correct only for a single instance.
func (h *Handlers) SetDeployLeases(l deployLeaser) { h.deployLeases = l }

func (h *Handlers) runDeployLocked(ctx context.Context, source Source, branch, ref, triggeredBy string, r *http.Request) (Deploy, error) {
	environment := source.EnvironmentFor(branch)

	id, err := h.store.CreateDeploy(ctx, source.Name, ref, triggeredBy)
	if err != nil {
		return Deploy{}, fmt.Errorf("record deploy start: %w", err)
	}
	deployID := strconv.FormatInt(id, 10)

	fail := func(deployErr error) (Deploy, error) {
		// nil environment/size: a failed attempt produced no tree to
		// describe, and recording zeroes would misreport it as an
		// empty deploy rather than an absent one.
		if err := h.store.CompleteDeploy(ctx, id, StatusFailed, deployErr.Error(), nil, nil); err != nil {
			h.logger.Error("failed to record deploy failure", "error", err, "deployID", id)
		}
		h.recordActivity(r, "deploy.failed", fmt.Sprintf("deploy of %s failed: %s", environment, deployErr.Error()))
		h.recordAudit(r, auditlog.Event{Action: "deploy.failed", ResourceType: "deploy", ResourceID: deployID})
		return Deploy{}, deployErr
	}

	deployment, err := h.deployer.Run(ctx, source.Name, branch)
	if err != nil {
		return fail(err)
	}

	if err := h.deployer.Activate(deployment.Environment, deployment.StagingDir); err != nil {
		return fail(err)
	}

	if err := h.store.CompleteDeploy(ctx, id, StatusSucceeded, "", &deployment.Environment, deployment.SizeBytes); err != nil {
		h.logger.Error("failed to record deploy success", "error", err, "deployID", id)
	}
	if err := PublishDeployed(h.bus, source.Name, environment, ref); err != nil {
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

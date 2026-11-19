package rbac

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// TokenTypeAgent is the type of a token issued to an AI agent. It is
// accepted only by the agent endpoint (see AllowAgentTokens): every other
// endpoint refuses it, so a leaked agent token cannot be replayed against
// the console's REST API.
const TokenTypeAgent = "agent"

// AgentActor is how an agent token is named wherever an actor is recorded -
// the audit log, a job's "triggered by", the activity feed - so that it is
// never mistaken for a person of the same name.
func AgentActor(tokenName string) string { return "agent:" + tokenName }

// Permissions that govern agent requests. They are held by people, never
// by agent tokens.
const (
	PermAgentRequestsRead    = "agent-requests:read"
	PermAgentRequestsApprove = "agent-requests:approve"
)

// AgentPermissions are the permissions an agent token may carry: reading,
// and asking to run. Anything that changes the console's own configuration
// or decides a request is a person's.
var AgentPermissions = []string{
	"nodes:read", "classifier:read", "orchestrator:read", "orchestrator:run", "vulnerabilities:read",
}

// Agent token expiry bounds. A token must expire; the maximum is what
// CONSOLE_AGENT_TOKEN_MAX_DAYS may raise or lower.
const (
	DefaultAgentTokenMaxDays = 90
	MinAgentTokenLife        = time.Hour
)

// ErrAgentTokenInvalid is wrapped by every refusal of an agent token's
// settings.
var ErrAgentTokenInvalid = errors.New("invalid agent token")

// AgentToken is the registry entry of an issued agent token.
type AgentToken struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	JTI          string    `json:"-"`
	Owner        string    `json:"owner"`
	Permissions  []string  `json:"permissions"`
	NodeGroups   []int64   `json:"nodeGroups"`
	TaskPatterns []string  `json:"taskPatterns"`
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

// Expired reports whether the token's life has ended at now.
func (t AgentToken) Expired(now time.Time) bool { return !now.Before(t.ExpiresAt) }

const agentTokenColumns = `id, name, jti, owner, permissions, node_groups, task_patterns, created_at, expires_at`

func scanAgentToken(row pgx.Row) (AgentToken, error) {
	var t AgentToken
	err := row.Scan(&t.ID, &t.Name, &t.JTI, &t.Owner, &t.Permissions, &t.NodeGroups, &t.TaskPatterns, &t.CreatedAt, &t.ExpiresAt)
	return t, err
}

// CreateAgentToken records t, whose JTI is already set. Settings are
// checked by CheckAgentToken before this; the database holds them as given.
func (s *Store) CreateAgentToken(ctx context.Context, t AgentToken) (AgentToken, error) {
	saved, err := scanAgentToken(s.pool.QueryRow(ctx,
		`INSERT INTO agent_tokens (name, jti, owner, permissions, node_groups, task_patterns, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING `+agentTokenColumns,
		t.Name, t.JTI, t.Owner, t.Permissions, nonNilInts(t.NodeGroups), nonNilStrings(t.TaskPatterns), t.ExpiresAt))
	if err != nil {
		return AgentToken{}, translateError(err)
	}
	return saved, nil
}

// GetAgentToken returns the token with id, or ErrNotFound.
func (s *Store) GetAgentToken(ctx context.Context, id int64) (AgentToken, error) {
	t, err := scanAgentToken(s.pool.QueryRow(ctx, `SELECT `+agentTokenColumns+` FROM agent_tokens WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentToken{}, ErrNotFound
	}
	if err != nil {
		return AgentToken{}, fmt.Errorf("get agent token: %w", err)
	}
	return t, nil
}

// GetAgentTokenByJTI returns the token a presented token's jti names, or
// ErrNotFound when it was revoked (revoking deletes the row).
func (s *Store) GetAgentTokenByJTI(ctx context.Context, jti string) (AgentToken, error) {
	t, err := scanAgentToken(s.pool.QueryRow(ctx, `SELECT `+agentTokenColumns+` FROM agent_tokens WHERE jti = $1`, jti))
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentToken{}, ErrNotFound
	}
	if err != nil {
		return AgentToken{}, fmt.Errorf("get agent token: %w", err)
	}
	return t, nil
}

// ListAgentTokens returns every issued agent token, newest first.
func (s *Store) ListAgentTokens(ctx context.Context) ([]AgentToken, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+agentTokenColumns+` FROM agent_tokens ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list agent tokens: %w", err)
	}
	defer rows.Close()
	tokens := make([]AgentToken, 0)
	for rows.Next() {
		t, err := scanAgentToken(rows)
		if err != nil {
			return nil, fmt.Errorf("list agent tokens: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

// DeleteAgentToken removes a token's registry row. Requests it made stay as
// records; those still waiting for a decision are failed, in the same
// transaction, so nobody is left able to approve work from a token that no
// longer exists. Callers revoke its jti first.
func (s *Store) DeleteAgentToken(ctx context.Context, id int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("delete agent token: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx,
		`UPDATE agent_requests SET status = 'failed', failure = 'the agent token was revoked' WHERE token_id = $1 AND status = 'pending'`, id); err != nil {
		return fmt.Errorf("fail pending agent requests: %w", err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM agent_tokens WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete agent token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

func nonNilInts(v []int64) []int64 {
	if v == nil {
		return []int64{}
	}
	return v
}

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// CheckAgentToken refuses settings an agent token may not have: no name, a
// permission outside AgentPermissions, a bad task pattern, or an expiry that
// is missing, too soon or beyond maxLife.
func CheckAgentToken(t AgentToken, now time.Time, maxLife time.Duration) error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrAgentTokenInvalid, fmt.Sprintf(format, args...))
	}
	if strings.TrimSpace(t.Name) == "" {
		return invalid("a name is required")
	}
	if len(t.Permissions) == 0 {
		return invalid("choose at least one permission")
	}
	for _, p := range t.Permissions {
		if !slices.Contains(AgentPermissions, p) {
			return invalid("an agent token cannot carry %q; it may carry %s", p, strings.Join(AgentPermissions, ", "))
		}
	}
	if err := taskscope.ValidatePatterns(taskscope.Unique(t.TaskPatterns)); err != nil {
		return err
	}
	if t.ExpiresAt.IsZero() {
		return invalid("an expiry is required")
	}
	if life := t.ExpiresAt.Sub(now); life < MinAgentTokenLife {
		return invalid("the expiry must be at least %s from now", MinAgentTokenLife)
	} else if life > maxLife {
		return invalid("the expiry may be at most %d days from now", int(maxLife.Hours()/24))
	}
	return nil
}

// IssueAgentToken signs a token for t: its permissions, a node scope of
// exactly t.NodeGroups on each of them (no groups covers no nodes), its
// task allowlist on those groups, and t.ExpiresAt as its expiry. t.JTI is
// filled in on the returned token.
func (i *Issuer) IssueAgentToken(t AgentToken, now time.Time) (string, AgentToken, error) {
	scopes := make(map[string][]int64, len(t.Permissions))
	for _, p := range t.Permissions {
		scopes[p] = nonNilInts(t.NodeGroups)
	}
	var grants []taskscope.Grant
	if patterns := taskscope.Unique(t.TaskPatterns); len(patterns) > 0 {
		grants = []taskscope.Grant{{Tasks: patterns, Groups: nonNilInts(t.NodeGroups)}}
	}
	signed, claims, err := i.IssueUserTokenWithTasks(AgentActor(t.Name), TokenTypeAgent, t.Permissions, scopes, grants, t.ExpiresAt.Sub(now))
	if err != nil {
		return "", AgentToken{}, err
	}
	t.JTI = claims.ID
	return signed, t, nil
}

type allowAgentKey struct{}

// AllowAgentTokens returns ctx marked so that Authenticate accepts an agent
// token on a request carrying it. The agent endpoint sets it on the
// in-process requests it makes for its read tools; a request from outside
// cannot, because a context never travels over the network.
func AllowAgentTokens(ctx context.Context) context.Context {
	return context.WithValue(ctx, allowAgentKey{}, true)
}

func agentTokensAllowed(ctx context.Context) bool {
	allowed, _ := ctx.Value(allowAgentKey{}).(bool)
	return allowed
}

// VerifyAgent verifies a bearer token for the agent endpoint: a valid,
// unexpired, unrevoked token of type agent, and nothing else.
func (v *Verifier) VerifyAgent(token string) (*Claims, error) {
	claims, err := v.Verify(token)
	if err != nil {
		return nil, err
	}
	if claims.Type != TokenTypeAgent {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// --- REST ---

type createAgentTokenRequest struct {
	Name         string   `json:"name"`
	Permissions  []string `json:"permissions"`
	NodeGroups   []int64  `json:"nodeGroups"`
	TaskPatterns []string `json:"taskPatterns"`
	// ExpiresInDays is how long the token lives. Required.
	ExpiresInDays int `json:"expiresInDays"`
}

type agentTokenResponse struct {
	AgentToken
	Token string `json:"token"` // present only on creation - shown once
}

// SetAgentTokenMaxLife sets the longest life an agent token may be issued
// with; zero keeps DefaultAgentTokenMaxDays.
func (h *Handlers) SetAgentTokenMaxLife(d time.Duration) { h.agentMaxLife = d }

func (h *Handlers) agentMaxLifetime() time.Duration {
	if h.agentMaxLife > 0 {
		return h.agentMaxLife
	}
	return DefaultAgentTokenMaxDays * 24 * time.Hour
}

func (h *Handlers) registerAgentTokens(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/agent-tokens", h.verifier.Authorize("rbac:admin", h.listAgentTokens))
	mux.HandleFunc("POST /api/v1/agent-tokens", h.verifier.Authorize("rbac:admin", h.createAgentToken))
	mux.HandleFunc("DELETE /api/v1/agent-tokens/{id}", h.verifier.Authorize("rbac:admin", h.revokeAgentToken))
}

func (h *Handlers) listAgentTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := h.store.ListAgentTokens(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordAuditRead(r, auditlog.Event{Action: "agent_token.list.viewed", ResourceType: "agent_token"})
	writeJSON(w, http.StatusOK, tokens)
}

func (h *Handlers) createAgentToken(w http.ResponseWriter, r *http.Request) {
	var req createAgentTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	claims, _ := ClaimsFromContext(r.Context())
	now := time.Now()
	t := AgentToken{
		Name: strings.TrimSpace(req.Name), Owner: claims.Subject,
		Permissions: slices.Compact(slices.Sorted(slices.Values(req.Permissions))), NodeGroups: req.NodeGroups,
		TaskPatterns: taskscope.Unique(req.TaskPatterns),
		ExpiresAt:    now.Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour),
	}
	if req.ExpiresInDays <= 0 {
		t.ExpiresAt = time.Time{}
	}
	if err := CheckAgentToken(t, now, h.agentMaxLifetime()); err != nil {
		if errors.Is(err, ErrAgentTokenInvalid) {
			writeJSONError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), ErrAgentTokenInvalid.Error()+": "))
			return
		}
		writeStoreError(w, err)
		return
	}
	signed, t, err := h.issuer.IssueAgentToken(t, now)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	saved, err := h.store.CreateAgentToken(r.Context(), t)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordActivity(r, "agent_token.created", "created agent token "+saved.Name)
	h.recordAudit(r, auditlog.Event{Action: "agent_token.created", ResourceType: "agent_token", ResourceID: saved.Name,
		After: map[string]any{"permissions": saved.Permissions, "nodeGroups": saved.NodeGroups, "taskPatterns": saved.TaskPatterns, "expiresAt": saved.ExpiresAt}})
	writeJSON(w, http.StatusCreated, agentTokenResponse{AgentToken: saved, Token: signed})
}

func (h *Handlers) revokeAgentToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	t, err := h.store.GetAgentToken(r.Context(), id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if err := h.revoker.Revoke(r.Context(), t.JTI, t.ExpiresAt); err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := h.store.DeleteAgentToken(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	h.recordActivity(r, "agent_token.revoked", "revoked agent token "+t.Name)
	h.recordAudit(r, auditlog.Event{Action: "agent_token.revoked", ResourceType: "agent_token", ResourceID: t.Name})
	w.WriteHeader(http.StatusNoContent)
}

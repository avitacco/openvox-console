// Package agentrequests holds the changes AI agents ask for until a person
// decides them. An agent's request to run a task, run Puppet, run a plan or
// patch nodes is checked exactly as the console checks a person's, stored
// frozen, and started through the orchestrator only when someone with
// authority approves it.
package agentrequests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Status is where a request is in its life.
type Status string

// A request is pending until it is decided or expires; approving it starts
// a job (approved) unless a check made then fails (failed).
const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusRejected Status = "rejected"
	StatusExpired  Status = "expired"
	StatusFailed   Status = "failed"
)

// ErrNotFound is returned for a request that does not exist.
var ErrNotFound = errors.New("agent request not found")

// ErrNotPending is returned when a decision finds the request already
// decided, or past its time.
var ErrNotPending = errors.New("the request is not pending")

// Request is one change an agent asked for. Body is the request as the
// console's own endpoint takes it, with sensitive values redacted; the
// values themselves are held sealed and never leave the store as part of a
// Request.
type Request struct {
	ID int64 `json:"id"`
	// TokenID is zero once the token has been revoked.
	TokenID   int64           `json:"tokenId,omitempty"`
	TokenName string          `json:"token"`
	Kind      string          `json:"kind"`
	Summary   string          `json:"summary"`
	Body      json.RawMessage `json:"body"`
	// Targets are the nodes the request will act on, resolved when it was
	// made. A plan from code resolves its own, so has none here.
	Targets   []string   `json:"targets"`
	Status    Status     `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	DecidedBy string     `json:"decidedBy,omitempty"`
	DecidedAt *time.Time `json:"decidedAt,omitempty"`
	Note      string     `json:"note,omitempty"`
	JobID     *int64     `json:"jobId,omitempty"`
	Failure   string     `json:"failure,omitempty"`

	sealed []byte
}

// Store is the requests' persistence.
type Store struct{ pool *pgxpool.Pool }

// NewStore builds a Store on pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const columns = `id, token_id, token_name, kind, summary, body, targets, status, created_at, expires_at,
	decided_by, decided_at, note, job_id, failure, sealed`

func scan(row pgx.Row) (Request, error) {
	var r Request
	var decidedBy *string
	var status string
	var tokenID *int64
	err := row.Scan(&r.ID, &tokenID, &r.TokenName, &r.Kind, &r.Summary, &r.Body, &r.Targets, &status, &r.CreatedAt, &r.ExpiresAt,
		&decidedBy, &r.DecidedAt, &r.Note, &r.JobID, &r.Failure, &r.sealed)
	if decidedBy != nil {
		r.DecidedBy = *decidedBy
	}
	if tokenID != nil {
		r.TokenID = *tokenID
	}
	r.Status = Status(status)
	return r, err
}

// SealFunc seals a request's sensitive values once its id is known, since
// the id is what the sealed value is bound to. It returns nil when there is
// nothing to seal.
type SealFunc func(id int64) ([]byte, error)

// Create stores r as pending, expiring after ttl, and returns it with its
// id. maxPending bounds how many pending requests r's token may hold; when
// it holds that many, Create returns ErrTooManyPending and stores nothing.
func (s *Store) Create(ctx context.Context, r Request, ttl time.Duration, maxPending int, seal SealFunc) (Request, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Request{}, fmt.Errorf("create agent request: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Serialized per token, so two simultaneous requests cannot both pass
	// the cap.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('agent_requests'), $1::int)`, int32(r.TokenID)); err != nil {
		return Request{}, fmt.Errorf("lock agent requests: %w", err)
	}
	var pending int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM agent_requests WHERE token_id = $1 AND status = 'pending' AND expires_at > now()`, r.TokenID).Scan(&pending); err != nil {
		return Request{}, fmt.Errorf("count pending agent requests: %w", err)
	}
	if maxPending > 0 && pending >= maxPending {
		return Request{}, ErrTooManyPending
	}
	var id int64
	err = tx.QueryRow(ctx,
		`INSERT INTO agent_requests (token_id, token_name, kind, summary, body, targets, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, now() + $7 * interval '1 second') RETURNING id`,
		r.TokenID, r.TokenName, r.Kind, r.Summary, r.Body, nonNil(r.Targets), int64(ttl.Seconds())).Scan(&id)
	if err != nil {
		return Request{}, fmt.Errorf("store agent request: %w", err)
	}
	if seal != nil {
		sealed, err := seal(id)
		if err != nil {
			return Request{}, err
		}
		if sealed != nil {
			if _, err := tx.Exec(ctx, `UPDATE agent_requests SET sealed = $2 WHERE id = $1`, id, sealed); err != nil {
				return Request{}, fmt.Errorf("store sealed values: %w", err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Request{}, fmt.Errorf("create agent request: %w", err)
	}
	return s.Get(ctx, id)
}

// ErrTooManyPending is returned by Create when a token already holds as
// many pending requests as it may.
var ErrTooManyPending = errors.New("too many pending requests")

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

// Get returns the request with id, or ErrNotFound. A request whose time has
// passed reads as expired whether or not the sweep has marked it yet.
func (s *Store) Get(ctx context.Context, id int64) (Request, error) {
	r, err := scan(s.pool.QueryRow(ctx, `SELECT `+columns+` FROM agent_requests WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	if err != nil {
		return Request{}, fmt.Errorf("get agent request: %w", err)
	}
	return r.settle(time.Now()), nil
}

// settle reads a pending request past its time as expired.
func (r Request) settle(now time.Time) Request {
	if r.Status == StatusPending && !now.Before(r.ExpiresAt) {
		r.Status = StatusExpired
	}
	return r
}

// Filter limits a listing.
type Filter struct {
	// TokenID limits it to one token's requests; zero is every token's.
	TokenID int64
	// Status limits it to one status; empty is all. Pending means pending
	// and not yet past its time.
	Status Status
}

// List returns requests newest first, at most limit of them.
func (s *Store) List(ctx context.Context, f Filter, limit int) ([]Request, error) {
	query := `SELECT ` + columns + ` FROM agent_requests WHERE ($1 = 0 OR token_id = $1)`
	args := []any{f.TokenID}
	switch f.Status {
	case "":
	case StatusPending:
		query += ` AND status = 'pending' AND expires_at > now()`
	case StatusExpired:
		query += ` AND (status = 'expired' OR (status = 'pending' AND expires_at <= now()))`
	default:
		args = append(args, string(f.Status))
		query += fmt.Sprintf(` AND status = $%d`, len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(` ORDER BY id DESC LIMIT $%d`, len(args))
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list agent requests: %w", err)
	}
	defer rows.Close()
	now := time.Now()
	out := make([]Request, 0)
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("list agent requests: %w", err)
		}
		out = append(out, r.settle(now))
	}
	return out, rows.Err()
}

// PendingCount is how many requests await a decision.
func (s *Store) PendingCount(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM agent_requests WHERE status = 'pending' AND expires_at > now()`).Scan(&n)
	return n, err
}

// Claim takes the decision on a pending request: exactly one caller's
// conditional update succeeds, so exactly one decides. It returns
// ErrNotPending to every other, including a request past its time. The
// caller then records the outcome with Approved, Failed or - for a
// rejection, which needs nothing more - Reject.
func (s *Store) Claim(ctx context.Context, id int64, by, note string) (Request, error) {
	r, err := scan(s.pool.QueryRow(ctx,
		`UPDATE agent_requests SET status = 'approved', decided_by = $2, decided_at = now(), note = $3
		 WHERE id = $1 AND status = 'pending' AND expires_at > now() RETURNING `+columns, id, by, note))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := s.Get(ctx, id); errors.Is(getErr, ErrNotFound) {
			return Request{}, ErrNotFound
		}
		return Request{}, ErrNotPending
	}
	if err != nil {
		return Request{}, fmt.Errorf("claim agent request: %w", err)
	}
	return r, nil
}

// Reject decides a pending request against, keeping the approver's note.
func (s *Store) Reject(ctx context.Context, id int64, by, note string) (Request, error) {
	r, err := scan(s.pool.QueryRow(ctx,
		`UPDATE agent_requests SET status = 'rejected', decided_by = $2, decided_at = now(), note = $3
		 WHERE id = $1 AND status = 'pending' AND expires_at > now() RETURNING `+columns, id, by, note))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := s.Get(ctx, id); errors.Is(getErr, ErrNotFound) {
			return Request{}, ErrNotFound
		}
		return Request{}, ErrNotPending
	}
	if err != nil {
		return Request{}, fmt.Errorf("reject agent request: %w", err)
	}
	return r, nil
}

// SetJob records the job an approved request started.
func (s *Store) SetJob(ctx context.Context, id, jobID int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE agent_requests SET job_id = $2 WHERE id = $1`, id, jobID)
	return err
}

// Fail marks a claimed request as one that could not be started, and why.
func (s *Store) Fail(ctx context.Context, id int64, reason string) error {
	_, err := s.pool.Exec(ctx, `UPDATE agent_requests SET status = 'failed', failure = $2 WHERE id = $1`, id, reason)
	return err
}

// FailPending marks a request failed that was decided before anyone could
// claim it for a reason known up front - the token is gone - leaving it
// pending would let it be approved to no purpose.
func (s *Store) FailPending(ctx context.Context, id int64, reason string) error {
	_, err := s.pool.Exec(ctx, `UPDATE agent_requests SET status = 'failed', failure = $2 WHERE id = $1 AND status = 'pending'`, id, reason)
	return err
}

// ExpireDue marks pending requests past their time expired, returning them
// so the sweep can audit each.
func (s *Store) ExpireDue(ctx context.Context) ([]Request, error) {
	rows, err := s.pool.Query(ctx,
		`UPDATE agent_requests SET status = 'expired' WHERE status = 'pending' AND expires_at <= now() RETURNING `+columns)
	if err != nil {
		return nil, fmt.Errorf("expire agent requests: %w", err)
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// openSealed returns a request's sealed bytes. It is the only way they leave.
func (s *Store) sealedOf(ctx context.Context, id int64) ([]byte, error) {
	var sealed []byte
	err := s.pool.QueryRow(ctx, `SELECT sealed FROM agent_requests WHERE id = $1`, id).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sealed, err
}

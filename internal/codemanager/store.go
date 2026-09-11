package codemanager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a deploy record doesn't exist.
var ErrNotFound = fmt.Errorf("deploy not found")

// Deploy is one recorded deploy attempt.
type Deploy struct {
	ID          int64      `json:"id"`
	Ref         string     `json:"ref"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	TriggeredBy string     `json:"triggeredBy"`
	ErrorDetail string     `json:"errorDetail,omitempty"`
}

// Deploy status values.
const (
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
)

// Store persists deploy attempts in Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a Store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// DistinctRefs returns every ref value currently present in deploys,
// sorted alphabetically. Not a fixed enum (unlike Status* above) - a ref
// is whatever environment name a deploy was triggered for, so this is
// for populating a filter's option list with real recorded values
// instead of a hardcoded guess at them.
func (s *Store) DistinctRefs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ref FROM deploys ORDER BY ref`)
	if err != nil {
		return nil, fmt.Errorf("list distinct refs: %w", err)
	}
	defer rows.Close()

	refs := make([]string, 0)
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, fmt.Errorf("list distinct refs: %w", err)
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

// CreateDeploy records a new in-progress deploy attempt and returns its
// assigned ID.
func (s *Store) CreateDeploy(ctx context.Context, ref, triggeredBy string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO deploys (ref, status, started_at, triggered_by) VALUES ($1, $2, now(), $3) RETURNING id`,
		ref, StatusRunning, triggeredBy,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create deploy: %w", err)
	}
	return id, nil
}

// CompleteDeploy marks the deploy identified by id as finished with
// status (StatusSucceeded or StatusFailed) and an optional error detail.
func (s *Store) CompleteDeploy(ctx context.Context, id int64, status, errorDetail string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE deploys SET status = $1, finished_at = now(), error_detail = $2 WHERE id = $3`,
		status, nullIfEmpty(errorDetail), id,
	)
	if err != nil {
		return fmt.Errorf("complete deploy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeployFilter narrows ListDeploys' results. A zero-value field (empty
// string) is not filtered on, and SortAsc false (the default) keeps the
// original most-recent-first order.
type DeployFilter struct {
	Ref         string
	Status      string
	TriggeredBy string
	SortAsc     bool
}

// ListDeploys returns one page of recorded deploy attempts matching
// filter, along with the total number of matching deploys across all
// pages. Sorted by started_at (most recent first unless filter.SortAsc),
// with id as a tiebreaker for deploys sharing a started_at.
func (s *Store) ListDeploys(ctx context.Context, page, pageSize int, filter DeployFilter) ([]Deploy, int, error) {
	var conditions []string
	var args []any
	addFilter := func(column, value string) {
		if value == "" {
			return
		}
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	addFilter("ref", filter.Ref)
	addFilter("status", filter.Status)
	addFilter("triggered_by", filter.TriggeredBy)

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	countQuery := "SELECT count(*) FROM deploys " + where
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count deploys: %w", err)
	}

	// order is one of two hardcoded literals, never interpolated from
	// caller input, so this string-built ORDER BY doesn't reopen the SQL
	// injection risk the $N placeholders above guard against.
	order := "DESC"
	if filter.SortAsc {
		order = "ASC"
	}
	args = append(args, pageSize, (page-1)*pageSize)
	query := fmt.Sprintf(
		`SELECT id, ref, status, started_at, finished_at, triggered_by, COALESCE(error_detail, '')
		 FROM deploys %s ORDER BY started_at %s, id %s LIMIT $%d OFFSET $%d`,
		where, order, order, len(args)-1, len(args),
	)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list deploys: %w", err)
	}
	defer rows.Close()

	deploys := make([]Deploy, 0)
	for rows.Next() {
		var d Deploy
		if err := rows.Scan(&d.ID, &d.Ref, &d.Status, &d.StartedAt, &d.FinishedAt, &d.TriggeredBy, &d.ErrorDetail); err != nil {
			return nil, 0, fmt.Errorf("list deploys: %w", err)
		}
		deploys = append(deploys, d)
	}
	return deploys, total, rows.Err()
}

// GetDeploy returns the deploy identified by id, or ErrNotFound.
func (s *Store) GetDeploy(ctx context.Context, id int64) (Deploy, error) {
	var d Deploy
	err := s.pool.QueryRow(ctx,
		`SELECT id, ref, status, started_at, finished_at, triggered_by, COALESCE(error_detail, '')
		 FROM deploys WHERE id = $1`,
		id,
	).Scan(&d.ID, &d.Ref, &d.Status, &d.StartedAt, &d.FinishedAt, &d.TriggeredBy, &d.ErrorDetail)
	if errors.Is(err, pgx.ErrNoRows) {
		return Deploy{}, ErrNotFound
	}
	if err != nil {
		return Deploy{}, fmt.Errorf("get deploy: %w", err)
	}
	return d, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

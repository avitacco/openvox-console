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
	ID int64 `json:"id"`
	// Source is the control repo this attempt deployed from. Rows
	// written before multi-source support carry the default "control".
	Source string `json:"source"`
	Ref    string `json:"ref"`
	// Environment is the Puppet environment this attempt deployed. Nil
	// for attempts recorded before it was tracked - ref alone cannot
	// supply it retroactively, since a webhook-triggered ref is a
	// commit SHA.
	Environment *string `json:"environment,omitempty"`
	// SizeBytes is the deployed tree's content size. Nil when it was
	// not measured (a failed deploy, or an attempt predating
	// measurement) - which must stay distinct from a real 0.
	SizeBytes   *int64     `json:"sizeBytes,omitempty"`
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

// DistinctSources returns every source value currently present in
// deploys, sorted alphabetically. Drawn from recorded attempts rather
// than from the configured source list so the history filter can still
// offer a source that has since been removed from the configuration -
// its attempts are still in the table.
func (s *Store) DistinctSources(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT source FROM deploys ORDER BY source`)
	if err != nil {
		return nil, fmt.Errorf("list distinct sources: %w", err)
	}
	defer rows.Close()

	sources := make([]string, 0)
	for rows.Next() {
		var source string
		if err := rows.Scan(&source); err != nil {
			return nil, fmt.Errorf("list distinct sources: %w", err)
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

// CreateDeploy records a new in-progress deploy attempt and returns its
// assigned ID.
func (s *Store) CreateDeploy(ctx context.Context, source, ref, triggeredBy string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO deploys (source, ref, status, started_at, triggered_by) VALUES ($1, $2, $3, now(), $4) RETURNING id`,
		source, ref, StatusRunning, triggeredBy,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create deploy: %w", err)
	}
	return id, nil
}

// CompleteDeploy marks the deploy identified by id as finished with
// status (StatusSucceeded or StatusFailed) and an optional error detail.
// environment and sizeBytes are what the deploy actually produced; both
// are nil for an attempt that failed before producing anything, and
// COALESCE leaves any already-recorded value alone rather than nulling
// it.
func (s *Store) CompleteDeploy(ctx context.Context, id int64, status, errorDetail string, environment *string, sizeBytes *int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE deploys
		 SET status = $1, finished_at = now(), error_detail = $2,
		     environment = COALESCE($3, environment),
		     size_bytes  = COALESCE($4, size_bytes)
		 WHERE id = $5`,
		status, nullIfEmpty(errorDetail), environment, sizeBytes, id,
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
	Source      string
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
	addFilter("source", filter.Source)
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
		`SELECT id, source, ref, environment, size_bytes, status, started_at, finished_at, triggered_by, COALESCE(error_detail, '')
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
		if err := rows.Scan(&d.ID, &d.Source, &d.Ref, &d.Environment, &d.SizeBytes, &d.Status, &d.StartedAt, &d.FinishedAt, &d.TriggeredBy, &d.ErrorDetail); err != nil {
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
		`SELECT id, source, ref, environment, size_bytes, status, started_at, finished_at, triggered_by, COALESCE(error_detail, '')
		 FROM deploys WHERE id = $1`,
		id,
	).Scan(&d.ID, &d.Source, &d.Ref, &d.Environment, &d.SizeBytes, &d.Status, &d.StartedAt, &d.FinishedAt, &d.TriggeredBy, &d.ErrorDetail)
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

// SourceDeploySummary is what deploy history knows about one source:
// which environments it has successfully deployed, when it last did so,
// and the size recorded for that most recent success.
type SourceDeploySummary struct {
	Environments []string
	LastDeployAt *time.Time
	SizeBytes    *int64
}

// DeploySummaryBySource aggregates successful deploy history per source.
//
// Successful attempts only: a source whose deploys have all failed has
// deployed no environments and has no size, and counting a failure
// would report code as live that never was. A source absent from the
// result has never deployed successfully - which the caller reports as
// absent rather than as zero.
//
// Sizes come from the most recent successful deploy of any of the
// source's environments, not a sum across them: summing would double
// count modules hardlinked between environments and would grow with
// branch count rather than with the repository.
func (s *Store) DeploySummaryBySource(ctx context.Context) (map[string]SourceDeploySummary, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT ON (source, environment)
		        source, environment, started_at, size_bytes
		 FROM deploys
		 WHERE status = $1 AND environment IS NOT NULL
		 ORDER BY source, environment, started_at DESC`,
		StatusSucceeded,
	)
	if err != nil {
		return nil, fmt.Errorf("summarize deploys by source: %w", err)
	}
	defer rows.Close()

	summaries := make(map[string]SourceDeploySummary)
	for rows.Next() {
		var (
			source      string
			environment string
			startedAt   time.Time
			sizeBytes   *int64
		)
		if err := rows.Scan(&source, &environment, &startedAt, &sizeBytes); err != nil {
			return nil, fmt.Errorf("summarize deploys by source: %w", err)
		}

		summary := summaries[source]
		summary.Environments = append(summary.Environments, environment)
		if summary.LastDeployAt == nil || startedAt.After(*summary.LastDeployAt) {
			at := startedAt
			summary.LastDeployAt = &at
			summary.SizeBytes = sizeBytes
		}
		summaries[source] = summary
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("summarize deploys by source: %w", err)
	}

	// Environment order follows the query's (source, environment) sort,
	// so a repository's environment list does not reshuffle between
	// page loads.
	return summaries, nil
}

package activity

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists activity events in Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a Store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// DistinctCategories returns every category value currently present in
// the activity log, sorted alphabetically. Not a fixed enum this
// package defines - see main.go's activity.NewPublisher calls, one per
// capability (classifier, rbac, codemanager, orchestrator) - just
// whatever's actually been recorded, for populating a filter's option
// list with real values instead of a hardcoded guess at them.
func (s *Store) DistinctCategories(ctx context.Context) ([]string, error) {
	return s.distinctValues(ctx, "category")
}

// DistinctActions returns every action value currently present in the
// activity log, sorted alphabetically - same caveat as
// DistinctCategories.
func (s *Store) DistinctActions(ctx context.Context) ([]string, error) {
	return s.distinctValues(ctx, "action")
}

// distinctValues returns every distinct value of column in the activity
// log. column is always one of the hardcoded literals passed by
// DistinctCategories/DistinctActions above, never derived from a
// request, so building it into the query string isn't a SQL injection
// vector despite not going through a $N placeholder.
func (s *Store) distinctValues(ctx context.Context, column string) ([]string, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT DISTINCT %s FROM activity_log ORDER BY %s`, column, column))
	if err != nil {
		return nil, fmt.Errorf("list distinct %s: %w", column, err)
	}
	defer rows.Close()

	values := make([]string, 0)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("list distinct %s: %w", column, err)
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

// RecordEvent persists e.
func (s *Store) RecordEvent(ctx context.Context, e Event) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO activity_log (occurred_at, category, action, actor, summary) VALUES ($1, $2, $3, $4, $5)`,
		e.OccurredAt, e.Category, e.Action, e.Actor, e.Summary,
	)
	if err != nil {
		return fmt.Errorf("record activity event: %w", err)
	}
	return nil
}

// EventFilter narrows ListEvents' results. A zero-value field (empty
// string) is not filtered on, and SortAsc false (the default) keeps the
// original most-recent-first order.
type EventFilter struct {
	Category string
	Action   string
	Actor    string
	SortAsc  bool
}

// ListEvents returns one page of recorded events matching filter, along
// with the total number of matching events across all pages. Sorted by
// occurred_at (most recent first unless filter.SortAsc), with id as a
// tiebreaker for events sharing an occurred_at.
func (s *Store) ListEvents(ctx context.Context, page, pageSize int, filter EventFilter) ([]Event, int, error) {
	var conditions []string
	var args []any
	addFilter := func(column, value string) {
		if value == "" {
			return
		}
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	addFilter("category", filter.Category)
	addFilter("action", filter.Action)
	addFilter("actor", filter.Actor)

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	countQuery := "SELECT count(*) FROM activity_log " + where
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count activity events: %w", err)
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
		`SELECT occurred_at, category, action, actor, summary FROM activity_log
		 %s ORDER BY occurred_at %s, id %s LIMIT $%d OFFSET $%d`,
		where, order, order, len(args)-1, len(args),
	)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list activity events: %w", err)
	}
	defer rows.Close()

	events := make([]Event, 0)
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.OccurredAt, &e.Category, &e.Action, &e.Actor, &e.Summary); err != nil {
			return nil, 0, fmt.Errorf("list activity events: %w", err)
		}
		events = append(events, e)
	}
	return events, total, rows.Err()
}

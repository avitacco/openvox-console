package rbac

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// CreateServiceToken records a service token's metadata (not the signed
// token string itself - that's shown to the caller once, at issuance,
// and never stored, matching typical API-token UX).
func (s *Store) CreateServiceToken(ctx context.Context, name, jti string, permissions []string) (ServiceToken, error) {
	return s.CreateServiceTokenWithTasks(ctx, name, jti, permissions, nil)
}

// CreateServiceTokenWithTasks is CreateServiceToken limited to the tasks
// taskPatterns allow; none means every task. Patterns taskscope refuses
// are returned as a *taskscope.PatternError.
func (s *Store) CreateServiceTokenWithTasks(ctx context.Context, name, jti string, permissions, taskPatterns []string) (ServiceToken, error) {
	taskPatterns = taskscope.Unique(taskPatterns)
	if err := taskscope.ValidatePatterns(taskPatterns); err != nil {
		return ServiceToken{}, err
	}
	var st ServiceToken
	err := s.pool.QueryRow(ctx,
		`INSERT INTO service_tokens (name, jti, permissions, task_patterns) VALUES ($1, $2, $3, $4)
		 RETURNING id, name, jti, permissions, task_patterns, created_at`,
		name, jti, permissions, taskPatterns,
	).Scan(&st.ID, &st.Name, &st.JTI, &st.Permissions, &st.TaskPatterns, &st.CreatedAt)
	if err != nil {
		return ServiceToken{}, translateError(err)
	}
	return st, nil
}

// GetServiceToken returns the service token identified by id, or
// ErrNotFound.
func (s *Store) GetServiceToken(ctx context.Context, id int64) (ServiceToken, error) {
	var st ServiceToken
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, jti, permissions, task_patterns, created_at FROM service_tokens WHERE id = $1`,
		id,
	).Scan(&st.ID, &st.Name, &st.JTI, &st.Permissions, &st.TaskPatterns, &st.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ServiceToken{}, ErrNotFound
	}
	if err != nil {
		return ServiceToken{}, fmt.Errorf("get service token: %w", err)
	}
	return st, nil
}

// ListServiceTokens returns every issued service token's metadata.
func (s *Store) ListServiceTokens(ctx context.Context) ([]ServiceToken, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, jti, permissions, task_patterns, created_at FROM service_tokens ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list service tokens: %w", err)
	}
	defer rows.Close()

	tokens := make([]ServiceToken, 0)
	for rows.Next() {
		var st ServiceToken
		if err := rows.Scan(&st.ID, &st.Name, &st.JTI, &st.Permissions, &st.TaskPatterns, &st.CreatedAt); err != nil {
			return nil, fmt.Errorf("list service tokens: %w", err)
		}
		tokens = append(tokens, st)
	}
	return tokens, rows.Err()
}

// DeleteServiceToken removes a service token's metadata record. Callers
// are responsible for also revoking its jti (see Revoker) - deleting the
// bookkeeping row alone doesn't invalidate an already-issued token.
func (s *Store) DeleteServiceToken(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM service_tokens WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete service token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

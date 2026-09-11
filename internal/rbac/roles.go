package rbac

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CreateRole creates a role with the given name and permission set.
func (s *Store) CreateRole(ctx context.Context, name string, permissions []string) (Role, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Role{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO roles (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		return Role{}, translateError(err)
	}

	for _, perm := range permissions {
		if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission) VALUES ($1, $2)`, id, perm); err != nil {
			return Role{}, fmt.Errorf("insert permission %q: %w", perm, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Role{}, fmt.Errorf("commit: %w", err)
	}
	return Role{ID: id, Name: name, Permissions: permissions}, nil
}

// GetRole returns the role identified by id, or ErrNotFound.
func (s *Store) GetRole(ctx context.Context, id int64) (Role, error) {
	var r Role
	err := s.pool.QueryRow(ctx, `SELECT id, name FROM roles WHERE id = $1`, id).Scan(&r.ID, &r.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Role{}, ErrNotFound
	}
	if err != nil {
		return Role{}, fmt.Errorf("get role: %w", err)
	}

	perms, err := s.rolePermissions(ctx, id)
	if err != nil {
		return Role{}, err
	}
	r.Permissions = perms
	return r, nil
}

// GetRoleByName returns the role with the given name, or ErrNotFound.
func (s *Store) GetRoleByName(ctx context.Context, name string) (Role, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM roles WHERE name = $1`, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Role{}, ErrNotFound
	}
	if err != nil {
		return Role{}, fmt.Errorf("get role by name: %w", err)
	}
	return s.GetRole(ctx, id)
}

func (s *Store) rolePermissions(ctx context.Context, roleID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT permission FROM role_permissions WHERE role_id = $1 ORDER BY id`, roleID)
	if err != nil {
		return nil, fmt.Errorf("load role permissions: %w", err)
	}
	defer rows.Close()

	perms := make([]string, 0)
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("load role permissions: %w", err)
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

// ListRoles returns every role.
func (s *Store) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM roles ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("list roles: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}

	roles := make([]Role, 0, len(ids))
	for _, id := range ids {
		r, err := s.GetRole(ctx, id)
		if err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	return roles, nil
}

// UpdateRole replaces the stored role matching r.ID with r in full.
func (s *Store) UpdateRole(ctx context.Context, r Role) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `UPDATE roles SET name = $1 WHERE id = $2`, r.Name, r.ID)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, r.ID); err != nil {
		return fmt.Errorf("clear existing permissions: %w", err)
	}
	for _, perm := range r.Permissions {
		if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission) VALUES ($1, $2)`, r.ID, perm); err != nil {
			return fmt.Errorf("insert permission %q: %w", perm, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// DeleteRole removes the role identified by id.
func (s *Store) DeleteRole(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM roles WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AssignRole gives userID the role roleID.
func (s *Store) AssignRole(ctx context.Context, userID, roleID int64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		userID, roleID,
	)
	if err != nil {
		return fmt.Errorf("assign role: %w", err)
	}
	return nil
}

// UnassignRole removes roleID from userID.
func (s *Store) UnassignRole(ctx context.Context, userID, roleID int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1 AND role_id = $2`, userID, roleID)
	if err != nil {
		return fmt.Errorf("unassign role: %w", err)
	}
	return nil
}

// ListUserRoles returns the roles currently assigned to userID.
func (s *Store) ListUserRoles(ctx context.Context, userID int64) ([]Role, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT role_id FROM user_roles WHERE user_id = $1 ORDER BY role_id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list user roles: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("list user roles: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list user roles: %w", err)
	}

	roles := make([]Role, 0, len(ids))
	for _, id := range ids {
		r, err := s.GetRole(ctx, id)
		if err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	return roles, nil
}

// ReconcileOIDCRoles assigns desiredRoleIDs to userID, marking any newly
// added row assigned_via='oidc', and unassigns any role previously
// assigned via this same mapping (assigned_via='oidc') that is no longer
// in desiredRoleIDs. Roles assigned_via='manual' are never added or
// removed by this function, regardless of whether they appear in
// desiredRoleIDs - see design.md in the oidc-authentication change.
// ReconcileOIDCRoles returns the role IDs actually added and removed, so
// callers (see OIDCService.HandleCallback) can publish an activity event
// only when reconciliation genuinely changed something.
func (s *Store) ReconcileOIDCRoles(ctx context.Context, userID int64, desiredRoleIDs []int64) (added, removed []int64, err error) {
	if desiredRoleIDs == nil {
		desiredRoleIDs = []int64{}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, roleID := range desiredRoleIDs {
		var insertedID int64
		err := tx.QueryRow(ctx,
			`INSERT INTO user_roles (user_id, role_id, assigned_via) VALUES ($1, $2, 'oidc')
			 ON CONFLICT (user_id, role_id) DO NOTHING
			 RETURNING role_id`,
			userID, roleID,
		).Scan(&insertedID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, fmt.Errorf("assign oidc role %d: %w", roleID, err)
		}
		if err == nil {
			added = append(added, insertedID)
		}
	}

	rows, err := tx.Query(ctx,
		`DELETE FROM user_roles
		 WHERE user_id = $1 AND assigned_via = 'oidc' AND NOT (role_id = ANY($2))
		 RETURNING role_id`,
		userID, desiredRoleIDs,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unassign stale oidc roles: %w", err)
	}
	for rows.Next() {
		var removedID int64
		if err := rows.Scan(&removedID); err != nil {
			rows.Close()
			return nil, nil, fmt.Errorf("unassign stale oidc roles: %w", err)
		}
		removed = append(removed, removedID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("unassign stale oidc roles: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit: %w", err)
	}
	return added, removed, nil
}

// UserPermissions returns the union of permissions across every role
// currently assigned to userID (deduplicated).
func (s *Store) UserPermissions(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT rp.permission
		FROM user_roles ur
		JOIN role_permissions rp ON rp.role_id = ur.role_id
		WHERE ur.user_id = $1
		ORDER BY rp.permission`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("load user permissions: %w", err)
	}
	defer rows.Close()

	perms := make([]string, 0)
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("load user permissions: %w", err)
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

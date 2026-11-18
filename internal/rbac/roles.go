package rbac

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/voxpupuli/enterprise-console/internal/taskscope"
)

// CreateRole creates a fleet-wide role: one with the given name and
// permission set, granted on the root node group.
func (s *Store) CreateRole(ctx context.Context, name string, permissions []string) (Role, error) {
	return s.CreateScopedRole(ctx, name, permissions, nil)
}

// CreateScopedRole creates a role whose permissions apply to nodeGroups
// and their descendants. A nil nodeGroups means the root group, which is
// what a caller that predates node group scope expects; an empty,
// non-nil list is stored as given and covers no nodes. Naming a group
// that does not exist is ErrUnknownNodeGroup.
func (s *Store) CreateScopedRole(ctx context.Context, name string, permissions []string, nodeGroups []int64) (Role, error) {
	return s.CreateRoleFull(ctx, Role{Name: name, Permissions: permissions, NodeGroups: nodeGroups})
}

// CreateRoleFull creates role as given: CreateScopedRole with a task
// allowlist as well. TaskPatterns that taskscope refuses are returned as
// a *taskscope.PatternError and nothing is created.
func (s *Store) CreateRoleFull(ctx context.Context, role Role) (Role, error) {
	name, permissions, nodeGroups := role.Name, role.Permissions, role.NodeGroups
	patterns := taskscope.Unique(role.TaskPatterns)
	if err := taskscope.ValidatePatterns(patterns); err != nil {
		return Role{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Role{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO roles (name, task_patterns) VALUES ($1, $2) RETURNING id`, name, patterns).Scan(&id); err != nil {
		return Role{}, translateError(err)
	}

	for _, perm := range permissions {
		if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission) VALUES ($1, $2)`, id, perm); err != nil {
			return Role{}, fmt.Errorf("insert permission %q: %w", perm, err)
		}
	}

	if nodeGroups == nil {
		var rootID int64
		if err := tx.QueryRow(ctx, `SELECT id FROM node_groups WHERE is_root`).Scan(&rootID); err != nil {
			return Role{}, fmt.Errorf("find root node group: %w", err)
		}
		nodeGroups = []int64{rootID}
	}
	nodeGroups = uniqueIDs(nodeGroups)
	if err := insertRoleNodeGroups(ctx, tx, id, nodeGroups); err != nil {
		return Role{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Role{}, fmt.Errorf("commit: %w", err)
	}
	return Role{ID: id, Name: name, Permissions: permissions, NodeGroups: nodeGroups, TaskPatterns: patterns}, nil
}

func insertRoleNodeGroups(ctx context.Context, tx pgx.Tx, roleID int64, groupIDs []int64) error {
	for _, groupID := range groupIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO role_node_groups (role_id, group_id) VALUES ($1, $2)`, roleID, groupID); err != nil {
			return translateError(err)
		}
	}
	return nil
}

// uniqueIDs returns ids without repeats, in first-seen order, never nil.
func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (s *Store) roleNodeGroups(ctx context.Context, roleID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT group_id FROM role_node_groups WHERE role_id = $1 ORDER BY group_id`, roleID)
	if err != nil {
		return nil, fmt.Errorf("load role node groups: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("load role node groups: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetRole returns the role identified by id, or ErrNotFound.
func (s *Store) GetRole(ctx context.Context, id int64) (Role, error) {
	var r Role
	err := s.pool.QueryRow(ctx, `SELECT id, name, task_patterns FROM roles WHERE id = $1`, id).Scan(&r.ID, &r.Name, &r.TaskPatterns)
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
	groups, err := s.roleNodeGroups(ctx, id)
	if err != nil {
		return Role{}, err
	}
	r.NodeGroups = groups
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

// UpdateRole replaces the stored role matching r.ID with r. A nil
// r.NodeGroups leaves the role's node groups as they are (a caller that
// predates node group scope sends none); a non-nil list, empty included,
// replaces them. TaskPatterns work the same way: nil leaves the
// allowlist as it is, a non-nil list (empty included) replaces it, and
// a list taskscope refuses is a *taskscope.PatternError.
func (s *Store) UpdateRole(ctx context.Context, r Role) error {
	if r.TaskPatterns != nil {
		r.TaskPatterns = taskscope.Unique(r.TaskPatterns)
		if err := taskscope.ValidatePatterns(r.TaskPatterns); err != nil {
			return err
		}
	}
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

	if r.TaskPatterns != nil {
		if _, err := tx.Exec(ctx, `UPDATE roles SET task_patterns = $1 WHERE id = $2`, r.TaskPatterns, r.ID); err != nil {
			return fmt.Errorf("set task patterns: %w", err)
		}
	}

	if r.NodeGroups != nil {
		if _, err := tx.Exec(ctx, `DELETE FROM role_node_groups WHERE role_id = $1`, r.ID); err != nil {
			return fmt.Errorf("clear existing node groups: %w", err)
		}
		if err := insertRoleNodeGroups(ctx, tx, r.ID, uniqueIDs(r.NodeGroups)); err != nil {
			return err
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

// ViaDirect is the Via value of a role assigned to the user themselves,
// as opposed to one that comes from a group (whose name it is).
const ViaDirect = "direct"

// ListUserRoles returns the roles userID holds: those assigned to them
// and those granted to their groups. Each role's Via says how: "direct",
// and/or the name of each group that grants it, direct first.
func (s *Store) ListUserRoles(ctx context.Context, userID int64) ([]Role, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT role_id, via FROM (
			SELECT role_id, 0 AS rank, 'direct' AS via FROM user_roles WHERE user_id = $1
			UNION ALL
			SELECT gr.role_id, 1, g.name
			FROM user_group_members m
			JOIN user_group_roles gr ON gr.group_id = m.group_id
			JOIN user_groups g ON g.id = m.group_id
			WHERE m.user_id = $1
		) v ORDER BY role_id, rank, lower(via)`, userID)
	if err != nil {
		return nil, fmt.Errorf("list user roles: %w", err)
	}
	var ids []int64
	via := map[int64][]string{}
	for rows.Next() {
		var id int64
		var how string
		if err := rows.Scan(&id, &how); err != nil {
			rows.Close()
			return nil, fmt.Errorf("list user roles: %w", err)
		}
		if _, seen := via[id]; !seen {
			ids = append(ids, id)
		}
		via[id] = append(via[id], how)
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
		r.Via = via[id]
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
		FROM user_effective_roles ur
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

// UserNodeScopes returns, for every permission userID holds, the node
// groups it applies to: the union of the groups of each role that
// carries that permission. A permission whose groups include the root
// is reduced to the root alone, since the root already covers every
// node. A permission held only through roles with no groups maps to an
// empty list. The result is never nil, so a token built from it always
// states a scope - see Claims.NodeScopes for why that matters.
func (s *Store) UserNodeScopes(ctx context.Context, userID int64) (map[string][]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT rp.permission, rng.group_id, COALESCE(g.is_root, false)
		FROM user_effective_roles ur
		JOIN role_permissions rp ON rp.role_id = ur.role_id
		LEFT JOIN role_node_groups rng ON rng.role_id = ur.role_id
		LEFT JOIN node_groups g ON g.id = rng.group_id
		WHERE ur.user_id = $1
		ORDER BY rp.permission, rng.group_id`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("load user node scopes: %w", err)
	}
	defer rows.Close()

	scopes := map[string][]int64{}
	fleetWide := map[string]bool{}
	for rows.Next() {
		var permission string
		var groupID *int64
		var isRoot bool
		if err := rows.Scan(&permission, &groupID, &isRoot); err != nil {
			return nil, fmt.Errorf("load user node scopes: %w", err)
		}
		if _, ok := scopes[permission]; !ok {
			scopes[permission] = []int64{}
		}
		if groupID == nil || fleetWide[permission] {
			continue
		}
		if isRoot {
			fleetWide[permission] = true
			scopes[permission] = []int64{*groupID}
			continue
		}
		scopes[permission] = append(scopes[permission], *groupID)
	}
	return scopes, rows.Err()
}

// NodeGroupRef names one node group, for telling a caller what their
// scope is without requiring classifier:read.
type NodeGroupRef struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	IsRoot bool   `json:"-"`
}

// NodeGroupRefs returns the existing node groups among ids, by name.
func (s *Store) NodeGroupRefs(ctx context.Context, ids []int64) ([]NodeGroupRef, error) {
	refs := make([]NodeGroupRef, 0, len(ids))
	if len(ids) == 0 {
		return refs, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id, name, is_root FROM node_groups WHERE id = ANY($1) ORDER BY name`, ids)
	if err != nil {
		return nil, fmt.Errorf("load node group names: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ref NodeGroupRef
		if err := rows.Scan(&ref.ID, &ref.Name, &ref.IsRoot); err != nil {
			return nil, fmt.Errorf("load node group names: %w", err)
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

// runPermission is the permission a task allowlist narrows.
const runPermission = "orchestrator:run"

// UserTaskGrants returns the task grants of userID: one for each role
// that carries orchestrator:run, pairing the role's allowlist (every
// task, written "*", when it has none) with its node groups.
//
// It returns nil, meaning no limit, when none of those roles has an
// allowlist. That keeps the token as small as it was for every
// installation not using task permissions, and it is why the result is
// never an empty non-nil list: a token cannot tell that from no limit
// (see Claims.TaskGrants). A user with no such role gets nil too; they
// cannot start anything, so there is nothing to limit.
func (s *Store) UserTaskGrants(ctx context.Context, userID int64) ([]taskscope.Grant, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.task_patterns,
		       COALESCE(array_agg(rng.group_id ORDER BY rng.group_id) FILTER (WHERE rng.group_id IS NOT NULL), '{}')
		FROM user_effective_roles ur
		JOIN roles r ON r.id = ur.role_id
		JOIN role_permissions rp ON rp.role_id = r.id AND rp.permission = $2
		LEFT JOIN role_node_groups rng ON rng.role_id = r.id
		WHERE ur.user_id = $1
		GROUP BY r.id, r.task_patterns
		ORDER BY r.id`,
		userID, runPermission,
	)
	if err != nil {
		return nil, fmt.Errorf("load user task grants: %w", err)
	}
	defer rows.Close()

	var grants []taskscope.Grant
	limited := false
	for rows.Next() {
		var patterns []string
		var groups []int64
		if err := rows.Scan(&patterns, &groups); err != nil {
			return nil, fmt.Errorf("load user task grants: %w", err)
		}
		if len(patterns) > 0 {
			limited = true
		} else {
			patterns = []string{taskscope.Everything}
		}
		grants = append(grants, taskscope.Grant{Tasks: patterns, Groups: groups})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load user task grants: %w", err)
	}
	if !limited {
		return nil, nil
	}
	return grants, nil
}

// ServiceTokenGrants is UserTaskGrants for a service token: a single
// fleet-wide grant of its allowlist, or nil (no limit) when it has none.
func ServiceTokenGrants(taskPatterns []string) []taskscope.Grant {
	if len(taskPatterns) == 0 {
		return nil
	}
	return []taskscope.Grant{{Tasks: taskPatterns, Fleet: true}}
}

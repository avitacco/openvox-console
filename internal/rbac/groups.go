package rbac

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Errors specific to user groups.
var (
	// ErrInvalidGroupName is returned for a group name that is empty or
	// longer than MaxGroupNameLength once trimmed.
	ErrInvalidGroupName = errors.New("a group name must be 1 to 64 characters")
	// ErrLastAdmin is returned for a change that would leave no user
	// holding rbac:admin.
	ErrLastAdmin = errors.New("this would leave no user able to administer users and roles")
)

// MaxGroupNameLength is the longest group name, in characters.
const MaxGroupNameLength = 64

// GroupHasMembersError is returned when a group with members is deleted
// without confirmation.
type GroupHasMembersError struct{ Members int }

func (e *GroupHasMembersError) Error() string {
	return fmt.Sprintf("the group has %d members; confirm to delete it", e.Members)
}

// GroupMember names one member of a group.
type GroupMember struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// GroupRole names one role granted to a group.
type GroupRole struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Group is a named set of users that roles are granted to.
type Group struct {
	ID          int64         `json:"id"`
	Name        string        `json:"name"`
	MemberCount int           `json:"memberCount"`
	Roles       []GroupRole   `json:"roles"`
	Members     []GroupMember `json:"members,omitempty"`
}

func cleanGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > MaxGroupNameLength {
		return "", ErrInvalidGroupName
	}
	return name, nil
}

// adminCount is how many users hold rbac:admin through their effective
// roles.
func adminCount(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (int, error) {
	var n int
	err := q.QueryRow(ctx, `
		SELECT count(DISTINCT er.user_id)
		FROM user_effective_roles er
		JOIN role_permissions rp ON rp.role_id = er.role_id AND rp.permission = 'rbac:admin'`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count administrators: %w", err)
	}
	return n, nil
}

// changeKeepingAnAdmin runs change in a transaction and refuses (rolling
// it back) when it takes the number of administrators from some to none.
func (s *Store) changeKeepingAnAdmin(ctx context.Context, change func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	before, err := adminCount(ctx, tx)
	if err != nil {
		return err
	}
	if err := change(tx); err != nil {
		return err
	}
	if before > 0 {
		after, err := adminCount(ctx, tx)
		if err != nil {
			return err
		}
		if after == 0 {
			return ErrLastAdmin
		}
	}
	return tx.Commit(ctx)
}

// missingRow turns a foreign key violation (a group, user or role that
// does not exist) into ErrNotFound.
func missingRow(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return ErrNotFound
	}
	return err
}

// CreateGroup creates a group with no members and no roles.
func (s *Store) CreateGroup(ctx context.Context, name string) (Group, error) {
	name, err := cleanGroupName(name)
	if err != nil {
		return Group{}, err
	}
	var g Group
	err = s.pool.QueryRow(ctx, `INSERT INTO user_groups (name) VALUES ($1) RETURNING id, name`, name).Scan(&g.ID, &g.Name)
	if err != nil {
		return Group{}, translateError(err)
	}
	g.Roles = []GroupRole{}
	return g, nil
}

// RenameGroup changes a group's name.
func (s *Store) RenameGroup(ctx context.Context, id int64, name string) (Group, error) {
	name, err := cleanGroupName(name)
	if err != nil {
		return Group{}, err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE user_groups SET name = $2 WHERE id = $1`, id, name)
	if err != nil {
		return Group{}, translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return Group{}, ErrNotFound
	}
	return s.GetGroup(ctx, id)
}

// ListGroups returns every group by name, with its member count and
// roles (not its members).
func (s *Store) ListGroups(ctx context.Context) ([]Group, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT g.id, g.name, (SELECT count(*) FROM user_group_members m WHERE m.group_id = g.id)
		FROM user_groups g ORDER BY lower(g.name), g.id`)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	groups := []Group{}
	byID := map[int64]int{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.MemberCount); err != nil {
			rows.Close()
			return nil, fmt.Errorf("list groups: %w", err)
		}
		g.Roles = []GroupRole{}
		byID[g.ID] = len(groups)
		groups = append(groups, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	roles, err := s.pool.Query(ctx, `
		SELECT gr.group_id, r.id, r.name
		FROM user_group_roles gr JOIN roles r ON r.id = gr.role_id
		ORDER BY lower(r.name), r.id`)
	if err != nil {
		return nil, fmt.Errorf("list group roles: %w", err)
	}
	defer roles.Close()
	for roles.Next() {
		var gid int64
		var role GroupRole
		if err := roles.Scan(&gid, &role.ID, &role.Name); err != nil {
			return nil, fmt.Errorf("list group roles: %w", err)
		}
		if i, ok := byID[gid]; ok {
			groups[i].Roles = append(groups[i].Roles, role)
		}
	}
	return groups, roles.Err()
}

// GetGroup returns one group with its members and roles.
func (s *Store) GetGroup(ctx context.Context, id int64) (Group, error) {
	var g Group
	err := s.pool.QueryRow(ctx, `SELECT id, name FROM user_groups WHERE id = $1`, id).Scan(&g.ID, &g.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, ErrNotFound
	}
	if err != nil {
		return Group{}, fmt.Errorf("get group: %w", err)
	}
	g.Members = []GroupMember{}
	g.Roles = []GroupRole{}

	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.username FROM user_group_members m JOIN users u ON u.id = m.user_id
		WHERE m.group_id = $1 ORDER BY lower(u.username), u.id`, id)
	if err != nil {
		return Group{}, fmt.Errorf("get group members: %w", err)
	}
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.ID, &m.Username); err != nil {
			rows.Close()
			return Group{}, fmt.Errorf("get group members: %w", err)
		}
		g.Members = append(g.Members, m)
	}
	rows.Close()
	g.MemberCount = len(g.Members)

	rrows, err := s.pool.Query(ctx, `
		SELECT r.id, r.name FROM user_group_roles gr JOIN roles r ON r.id = gr.role_id
		WHERE gr.group_id = $1 ORDER BY lower(r.name), r.id`, id)
	if err != nil {
		return Group{}, fmt.Errorf("get group roles: %w", err)
	}
	defer rrows.Close()
	for rrows.Next() {
		var role GroupRole
		if err := rrows.Scan(&role.ID, &role.Name); err != nil {
			return Group{}, fmt.Errorf("get group roles: %w", err)
		}
		g.Roles = append(g.Roles, role)
	}
	return g, rrows.Err()
}

// DeleteGroup removes a group, its memberships and its role grants. A
// group with members is refused with *GroupHasMembersError unless confirm
// is set. It returns the number of members the group had.
func (s *Store) DeleteGroup(ctx context.Context, id int64, confirm bool) (int, error) {
	members := 0
	err := s.changeKeepingAnAdmin(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT true FROM user_groups WHERE id = $1 FOR UPDATE`, id).Scan(&exists); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("lock group: %w", err)
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM user_group_members WHERE group_id = $1`, id).Scan(&members); err != nil {
			return fmt.Errorf("count members: %w", err)
		}
		if members > 0 && !confirm {
			return &GroupHasMembersError{Members: members}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM user_groups WHERE id = $1`, id); err != nil {
			return fmt.Errorf("delete group: %w", err)
		}
		return nil
	})
	return members, err
}

// AddGroupMember adds userID to the group; adding a member twice is not
// an error.
func (s *Store) AddGroupMember(ctx context.Context, groupID, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		groupID, userID)
	if err != nil {
		return fmt.Errorf("add group member: %w", missingRow(err))
	}
	return nil
}

// RemoveGroupMember removes userID from the group, refusing when that
// leaves no administrator.
func (s *Store) RemoveGroupMember(ctx context.Context, groupID, userID int64) error {
	return s.changeKeepingAnAdmin(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM user_group_members WHERE group_id = $1 AND user_id = $2`, groupID, userID)
		if err != nil {
			return fmt.Errorf("remove group member: %w", err)
		}
		return nil
	})
}

// GrantGroupRole gives every member of the group the role.
func (s *Store) GrantGroupRole(ctx context.Context, groupID, roleID int64) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_group_roles (group_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		groupID, roleID)
	if err != nil {
		return fmt.Errorf("grant group role: %w", missingRow(err))
	}
	return nil
}

// RevokeGroupRole takes the role from the group, refusing when that
// leaves no administrator.
func (s *Store) RevokeGroupRole(ctx context.Context, groupID, roleID int64) error {
	return s.changeKeepingAnAdmin(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM user_group_roles WHERE group_id = $1 AND role_id = $2`, groupID, roleID)
		if err != nil {
			return fmt.Errorf("revoke group role: %w", err)
		}
		return nil
	})
}

// UserGroups returns the groups userID belongs to, by name.
func (s *Store) UserGroups(ctx context.Context, userID int64) ([]GroupRole, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT g.id, g.name FROM user_group_members m JOIN user_groups g ON g.id = m.group_id
		WHERE m.user_id = $1 ORDER BY lower(g.name), g.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list user groups: %w", err)
	}
	defer rows.Close()
	groups := []GroupRole{}
	for rows.Next() {
		var g GroupRole
		if err := rows.Scan(&g.ID, &g.Name); err != nil {
			return nil, fmt.Errorf("list user groups: %w", err)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

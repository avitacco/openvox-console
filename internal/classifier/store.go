package classifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Domain-level errors, distinguished from raw database errors so callers
// (in particular HTTP handlers) can respond appropriately without needing
// to know about Postgres error codes.
var (
	ErrNotFound          = errors.New("group not found")
	ErrDuplicateName     = errors.New("a group with this name already exists")
	ErrDuplicatePriority = errors.New("a group with this priority already exists")
)

// queryer abstracts over *pgxpool.Pool and pgx.Tx, so the same read/write
// helpers work whether or not they're running inside a transaction.
type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Store persists node groups in Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a Store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// CreateGroup inserts g and returns it with its assigned ID.
func (s *Store) CreateGroup(ctx context.Context, g Group) (Group, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Group{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var id int64
	err = tx.QueryRow(ctx,
		`INSERT INTO node_groups (name, environment, priority) VALUES ($1, $2, $3) RETURNING id`,
		g.Name, g.Environment, g.Priority,
	).Scan(&id)
	if err != nil {
		return Group{}, translateError(err)
	}
	g.ID = id

	if err := insertClasses(ctx, tx, id, g.Classes); err != nil {
		return Group{}, err
	}
	if err := insertParameters(ctx, tx, "node_group_parameters", "group_id", id, g.Parameters); err != nil {
		return Group{}, err
	}
	if err := insertRule(ctx, tx, id, g.Rule); err != nil {
		return Group{}, err
	}
	if err := insertPins(ctx, tx, id, g.Pins); err != nil {
		return Group{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Group{}, fmt.Errorf("commit: %w", err)
	}
	return g, nil
}

// GetGroup returns the group identified by id, or ErrNotFound.
func (s *Store) GetGroup(ctx context.Context, id int64) (Group, error) {
	return getGroup(ctx, s.pool, id)
}

// ListGroups returns one page of node groups ordered by priority, along
// with the total number of groups across all pages. For the admin UI.
func (s *Store) ListGroups(ctx context.Context, page, pageSize int) ([]Group, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM node_groups`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count groups: %w", err)
	}

	ids, err := groupIDs(ctx, s.pool, `SELECT id FROM node_groups ORDER BY priority LIMIT $1 OFFSET $2`, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}

	groups, err := hydrateGroups(ctx, s.pool, ids)
	if err != nil {
		return nil, 0, err
	}
	return groups, total, nil
}

// ListAllGroups returns every node group, unpaginated. For the ENC
// classifier (internal/encapi), which must evaluate every group's rule
// against a node - there's no "page" of groups to classify against.
func (s *Store) ListAllGroups(ctx context.Context) ([]Group, error) {
	ids, err := groupIDs(ctx, s.pool, `SELECT id FROM node_groups ORDER BY priority`)
	if err != nil {
		return nil, err
	}
	return hydrateGroups(ctx, s.pool, ids)
}

func groupIDs(ctx context.Context, q queryer, sql string, args ...any) ([]int64, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("list groups: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	return ids, nil
}

func hydrateGroups(ctx context.Context, q queryer, ids []int64) ([]Group, error) {
	groups := make([]Group, 0, len(ids))
	for _, id := range ids {
		g, err := getGroup(ctx, q, id)
		if err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	return groups, nil
}

// UpdateGroup replaces the stored group matching g.ID with g in full.
func (s *Store) UpdateGroup(ctx context.Context, g Group) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE node_groups SET name = $1, environment = $2, priority = $3, updated_at = now() WHERE id = $4`,
		g.Name, g.Environment, g.Priority, g.ID,
	)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	for _, stmt := range []string{
		`DELETE FROM node_group_classes WHERE group_id = $1`, // cascades to class_parameters
		`DELETE FROM node_group_parameters WHERE group_id = $1`,
		`DELETE FROM node_group_rule_conditions WHERE group_id = $1`,
		`DELETE FROM node_group_pins WHERE group_id = $1`,
	} {
		if _, err := tx.Exec(ctx, stmt, g.ID); err != nil {
			return fmt.Errorf("clear existing group data: %w", err)
		}
	}

	if err := insertClasses(ctx, tx, g.ID, g.Classes); err != nil {
		return err
	}
	if err := insertParameters(ctx, tx, "node_group_parameters", "group_id", g.ID, g.Parameters); err != nil {
		return err
	}
	if err := insertRule(ctx, tx, g.ID, g.Rule); err != nil {
		return err
	}
	if err := insertPins(ctx, tx, g.ID, g.Pins); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// DeleteGroup removes the group identified by id.
func (s *Store) DeleteGroup(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM node_groups WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func getGroup(ctx context.Context, q queryer, id int64) (Group, error) {
	var g Group
	err := q.QueryRow(ctx, `SELECT id, name, environment, priority FROM node_groups WHERE id = $1`, id).
		Scan(&g.ID, &g.Name, &g.Environment, &g.Priority)
	if errors.Is(err, pgx.ErrNoRows) {
		return Group{}, ErrNotFound
	}
	if err != nil {
		return Group{}, fmt.Errorf("get group: %w", err)
	}

	classes, err := loadClasses(ctx, q, id)
	if err != nil {
		return Group{}, err
	}
	g.Classes = classes

	params, err := loadParameters(ctx, q, "node_group_parameters", "group_id", id)
	if err != nil {
		return Group{}, err
	}
	g.Parameters = params

	rule, err := loadRule(ctx, q, id)
	if err != nil {
		return Group{}, err
	}
	g.Rule = rule

	pins, err := loadPins(ctx, q, id)
	if err != nil {
		return Group{}, err
	}
	g.Pins = pins

	return g, nil
}

func insertClasses(ctx context.Context, q queryer, groupID int64, classes []Class) error {
	for _, c := range classes {
		var classID int64
		err := q.QueryRow(ctx,
			`INSERT INTO node_group_classes (group_id, class_name) VALUES ($1, $2) RETURNING id`,
			groupID, c.Name,
		).Scan(&classID)
		if err != nil {
			return fmt.Errorf("insert class %q: %w", c.Name, err)
		}
		if err := insertParameters(ctx, q, "node_group_class_parameters", "class_id", classID, c.Parameters); err != nil {
			return err
		}
	}
	return nil
}

func insertParameters(ctx context.Context, q queryer, table, fkColumn string, id int64, params map[string]any) error {
	for name, value := range params {
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode parameter %q: %w", name, err)
		}
		sql := fmt.Sprintf(`INSERT INTO %s (%s, param_name, param_value) VALUES ($1, $2, $3)`, table, fkColumn)
		if _, err := q.Exec(ctx, sql, id, name, encoded); err != nil {
			return fmt.Errorf("insert parameter %q: %w", name, err)
		}
	}
	return nil
}

func insertRule(ctx context.Context, q queryer, groupID int64, rule []Condition) error {
	for _, cond := range rule {
		_, err := q.Exec(ctx,
			`INSERT INTO node_group_rule_conditions (group_id, fact_path, operator, value) VALUES ($1, $2, $3, $4)`,
			groupID, cond.FactPath, cond.Operator, cond.Value,
		)
		if err != nil {
			return fmt.Errorf("insert rule condition: %w", err)
		}
	}
	return nil
}

func insertPins(ctx context.Context, q queryer, groupID int64, pins []string) error {
	for _, certname := range pins {
		if _, err := q.Exec(ctx,
			`INSERT INTO node_group_pins (group_id, certname) VALUES ($1, $2)`,
			groupID, certname,
		); err != nil {
			return fmt.Errorf("insert pin %q: %w", certname, err)
		}
	}
	return nil
}

func loadClasses(ctx context.Context, q queryer, groupID int64) ([]Class, error) {
	rows, err := q.Query(ctx, `SELECT id, class_name FROM node_group_classes WHERE group_id = $1 ORDER BY id`, groupID)
	if err != nil {
		return nil, fmt.Errorf("load classes: %w", err)
	}
	defer rows.Close()

	classes := make([]Class, 0)
	var classIDs []int64
	for rows.Next() {
		var id int64
		var c Class
		if err := rows.Scan(&id, &c.Name); err != nil {
			return nil, fmt.Errorf("load classes: %w", err)
		}
		classes = append(classes, c)
		classIDs = append(classIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load classes: %w", err)
	}

	for i, classID := range classIDs {
		params, err := loadParameters(ctx, q, "node_group_class_parameters", "class_id", classID)
		if err != nil {
			return nil, err
		}
		classes[i].Parameters = params
	}
	return classes, nil
}

func loadParameters(ctx context.Context, q queryer, table, fkColumn string, id int64) (map[string]any, error) {
	sql := fmt.Sprintf(`SELECT param_name, param_value FROM %s WHERE %s = $1 ORDER BY id`, table, fkColumn)
	rows, err := q.Query(ctx, sql, id)
	if err != nil {
		return nil, fmt.Errorf("load parameters: %w", err)
	}
	defer rows.Close()

	params := map[string]any{}
	for rows.Next() {
		var name string
		var raw []byte
		if err := rows.Scan(&name, &raw); err != nil {
			return nil, fmt.Errorf("load parameters: %w", err)
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("decode parameter %q: %w", name, err)
		}
		params[name] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load parameters: %w", err)
	}
	return params, nil
}

func loadRule(ctx context.Context, q queryer, groupID int64) ([]Condition, error) {
	rows, err := q.Query(ctx,
		`SELECT fact_path, operator, value FROM node_group_rule_conditions WHERE group_id = $1 ORDER BY id`,
		groupID,
	)
	if err != nil {
		return nil, fmt.Errorf("load rule: %w", err)
	}
	defer rows.Close()

	rule := make([]Condition, 0)
	for rows.Next() {
		var c Condition
		if err := rows.Scan(&c.FactPath, &c.Operator, &c.Value); err != nil {
			return nil, fmt.Errorf("load rule: %w", err)
		}
		rule = append(rule, c)
	}
	return rule, rows.Err()
}

func loadPins(ctx context.Context, q queryer, groupID int64) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT certname FROM node_group_pins WHERE group_id = $1 ORDER BY id`, groupID)
	if err != nil {
		return nil, fmt.Errorf("load pins: %w", err)
	}
	defer rows.Close()

	pins := make([]string, 0)
	for rows.Next() {
		var certname string
		if err := rows.Scan(&certname); err != nil {
			return nil, fmt.Errorf("load pins: %w", err)
		}
		pins = append(pins, certname)
	}
	return pins, rows.Err()
}

func translateError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "node_groups_name_key":
			return ErrDuplicateName
		case "node_groups_priority_key":
			return ErrDuplicatePriority
		}
	}
	return fmt.Errorf("store error: %w", err)
}

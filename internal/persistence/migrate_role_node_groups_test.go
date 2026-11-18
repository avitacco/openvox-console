package persistence

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// roleGrants returns role name -> the names of the node groups it is
// granted on.
func roleGrants(t *testing.T, dsn string) map[string][]string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
		SELECT r.name, g.name
		  FROM roles r
		  LEFT JOIN role_node_groups rng ON rng.role_id = r.id
		  LEFT JOIN node_groups g ON g.id = rng.group_id
		 ORDER BY r.name, g.name`)
	if err != nil {
		t.Fatalf("query grants: %v", err)
	}
	defer rows.Close()

	grants := map[string][]string{}
	for rows.Next() {
		var role string
		var group *string
		if err := rows.Scan(&role, &group); err != nil {
			t.Fatalf("scan grant: %v", err)
		}
		if group == nil {
			grants[role] = nil
			continue
		}
		grants[role] = append(grants[role], *group)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read grants: %v", err)
	}
	return grants
}

func tableExists(t *testing.T, dsn, table string) bool {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil {
		t.Fatalf("check table %s: %v", table, err)
	}
	return exists
}

// Every role that exists at upgrade was fleet-wide, and must stay so.
func TestMigrate_RoleNodeGroups_ExistingRolesAreGrantedTheRoot(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 29)
	execSQL(t, dsn,
		`INSERT INTO roles (name) VALUES ('admins'), ('readers')`,
		`INSERT INTO node_groups (name, priority, parent_id)
		 SELECT 'web', 10, id FROM node_groups WHERE is_root`,
	)
	migrateTo(t, dsn, 30)

	grants := roleGrants(t, dsn)
	for _, role := range []string{"admins", "readers"} {
		if got := grants[role]; len(got) != 1 || got[0] != "All Nodes" {
			t.Errorf("role %q granted on %v, want only the root group", role, got)
		}
	}
}

func TestMigrate_RoleNodeGroups_NoRoles(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 30)
	if grants := roleGrants(t, dsn); len(grants) != 0 {
		t.Errorf("grants = %v, want none on a console with no roles", grants)
	}
}

func TestMigrate_RoleNodeGroups_UpDownUp(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 29)
	execSQL(t, dsn, `INSERT INTO roles (name) VALUES ('admins')`)

	migrateTo(t, dsn, 30)
	if !tableExists(t, dsn, "role_node_groups") {
		t.Fatal("up: role_node_groups missing")
	}
	migrateTo(t, dsn, 29)
	if tableExists(t, dsn, "role_node_groups") {
		t.Fatal("down: role_node_groups still present")
	}
	migrateTo(t, dsn, 30)
	if got := roleGrants(t, dsn)["admins"]; len(got) != 1 || got[0] != "All Nodes" {
		t.Errorf("after up-down-up, admins granted on %v, want the root group", got)
	}
}

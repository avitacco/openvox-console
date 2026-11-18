package persistence

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestMigrate_UserGroups_UpDownUp(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 34)
	execSQL(t, dsn,
		`INSERT INTO users (username, password_hash) VALUES ('before', 'x')`,
		`INSERT INTO roles (name) VALUES ('before-role')`,
		`INSERT INTO user_roles (user_id, role_id) SELECT u.id, r.id FROM users u, roles r`,
	)

	migrateTo(t, dsn, 35)
	// With no group, the view is exactly user_roles.
	execSQL(t, dsn, `DO $$ BEGIN
	   IF (SELECT count(*) FROM user_effective_roles) <> 1 THEN
	     RAISE EXCEPTION 'the view did not equal user_roles with no groups';
	   END IF;
	 END $$`,
		`INSERT INTO roles (name) VALUES ('via-group')`,
		`INSERT INTO user_groups (name) VALUES ('Team')`,
		`INSERT INTO user_group_members (group_id, user_id) SELECT g.id, u.id FROM user_groups g, users u`,
		`INSERT INTO user_group_roles (group_id, role_id) SELECT g.id, r.id FROM user_groups g, roles r`,
		// The group grants both roles, one of which is also direct: the
		// role held twice counts once.
		`DO $$ BEGIN
		   IF (SELECT count(*) FROM user_effective_roles) <> 2 THEN
		     RAISE EXCEPTION 'effective roles should be 2, a role held twice counting once';
		   END IF;
		 END $$`,
	)
	// Names are unique whatever the case.
	if err := tryExecSQL(dsn, `INSERT INTO user_groups (name) VALUES ('TEAM')`); err == nil {
		t.Fatal("a group name differing only in case was accepted")
	}

	migrateTo(t, dsn, 34)
	execSQL(t, dsn, `DO $$ BEGIN
	   IF (SELECT count(*) FROM user_roles) <> 1 THEN RAISE EXCEPTION 'direct roles were lost'; END IF;
	 END $$`)
	migrateTo(t, dsn, 35)
}

// tryExecSQL runs one statement and returns its error, for a statement
// that is expected to be refused.
func tryExecSQL(dsn, stmt string) error {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, stmt)
	return err
}

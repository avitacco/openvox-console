package persistence

import "testing"

func TestMigrate_TaskPermissions_UpDownUp(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 33)
	// A role and a token from before the upgrade must survive it both
	// ways, and read as allowing every task.
	execSQL(t, dsn,
		`INSERT INTO roles (name) VALUES ('before')`,
		`INSERT INTO service_tokens (name, jti, permissions) VALUES ('before', 'j', ARRAY['orchestrator:run'])`,
	)

	migrateTo(t, dsn, 34)
	execSQL(t, dsn,
		`DO $$ BEGIN
		   IF (SELECT count(*) FROM roles WHERE task_patterns = '{}') <> 1 THEN
		     RAISE EXCEPTION 'an existing role did not start with an empty allowlist';
		   END IF;
		   IF (SELECT count(*) FROM service_tokens WHERE task_patterns = '{}') <> 1 THEN
		     RAISE EXCEPTION 'an existing token did not start with an empty allowlist';
		   END IF;
		 END $$`,
		`UPDATE roles SET task_patterns = ARRAY['patch::*']`,
		`UPDATE service_tokens SET task_patterns = ARRAY['deploy::*']`,
	)

	migrateTo(t, dsn, 33)
	execSQL(t, dsn, `DO $$ BEGIN
	   IF (SELECT count(*) FROM roles WHERE name = 'before') <> 1 THEN RAISE EXCEPTION 'the role was lost'; END IF;
	   IF (SELECT count(*) FROM service_tokens WHERE name = 'before') <> 1 THEN RAISE EXCEPTION 'the token was lost'; END IF;
	 END $$`)
	migrateTo(t, dsn, 34)
	execSQL(t, dsn, `DO $$ BEGIN
	   IF (SELECT count(*) FROM roles WHERE task_patterns = '{}') <> 1 THEN
	     RAISE EXCEPTION 'up again did not give an empty allowlist';
	   END IF;
	 END $$`)
}

package persistence

import "testing"

func TestMigrate_Plans_UpDownUp(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 31)
	// A schedule from before the upgrade must survive it both ways.
	execSQL(t, dsn, `INSERT INTO scheduled_jobs (name, kind, target, start_at, owner, owner_kind)
		 VALUES ('nightly', 'run', '{"group": 1}', now(), 'admin', 'user')`)

	migrateTo(t, dsn, 32)
	if !tableExists(t, dsn, "plans") {
		t.Fatal("up: plans missing")
	}
	execSQL(t, dsn,
		`INSERT INTO plans (name, steps, created_by, updated_by) VALUES ('Patch', '[{"kind":"run"}]', 'admin', 'admin')`,
		`INSERT INTO scheduled_jobs (name, kind, plan_name, steps, target, start_at, owner, owner_kind)
		 VALUES ('weekly patch', 'plan', 'Patch', '[{"kind":"run"}]', '{"group": 1}', now(), 'admin', 'user')`,
		// A plan's name is unique.
		`DO $$ BEGIN
		   BEGIN
		     INSERT INTO plans (name, steps, created_by, updated_by) VALUES ('Patch', '[]', 'a', 'a');
		     RAISE EXCEPTION 'a duplicate plan name was accepted';
		   EXCEPTION WHEN unique_violation THEN NULL;
		   END;
		 END $$`,
	)

	migrateTo(t, dsn, 31)
	if tableExists(t, dsn, "plans") {
		t.Fatal("down: plans still present")
	}
	execSQL(t, dsn, `DO $$ BEGIN
	   IF (SELECT count(*) FROM scheduled_jobs WHERE kind = 'run') <> 1 THEN RAISE EXCEPTION 'the run schedule was lost'; END IF;
	   IF (SELECT count(*) FROM scheduled_jobs WHERE kind = 'plan') <> 0 THEN RAISE EXCEPTION 'a plan schedule survived the down migration'; END IF;
	 END $$`)
	migrateTo(t, dsn, 32)
	if !tableExists(t, dsn, "plans") {
		t.Fatal("up again: plans missing")
	}
}

package persistence

import "testing"

func TestMigrate_PatchRuns_UpDownUp(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 37)

	// Before the migration a patch job is refused.
	if err := tryExecSQL(dsn, `INSERT INTO jobs (kind, status, triggered_by, started_at) VALUES ('patch', 'running', 'a', now())`); err == nil {
		t.Fatal("a patch job was accepted before the migration")
	}
	migrateTo(t, dsn, 38)

	execSQL(t, dsn,
		`INSERT INTO jobs (kind, status, triggered_by, started_at, heartbeat_at) VALUES ('patch', 'running', 'a', now(), now())`,
		`INSERT INTO job_targets (job_id, certname, status, batch, heartbeat_at)
		   SELECT id, 'web01', 'pending', 1, now() FROM jobs WHERE kind = 'patch'`,
	)
	if err := tryExecSQL(dsn, `INSERT INTO jobs (kind, status, triggered_by, started_at) VALUES ('other', 'running', 'a', now())`); err == nil {
		t.Fatal("an unknown job kind was accepted")
	}
	// An existing kind still works and has no heartbeat.
	execSQL(t, dsn, `INSERT INTO jobs (kind, status, triggered_by, started_at) VALUES ('run', 'running', 'a', now())`)

	// Down removes patch jobs with their targets and keeps the rest.
	migrateTo(t, dsn, 37)
	execSQL(t, dsn, `DO $$ BEGIN
	   IF (SELECT count(*) FROM jobs WHERE kind = 'run') <> 1 OR (SELECT count(*) FROM jobs) <> 1
	      OR (SELECT count(*) FROM job_targets) <> 0 THEN RAISE EXCEPTION 'down kept or lost the wrong rows'; END IF;
	 END $$`)
	migrateTo(t, dsn, 38)
}

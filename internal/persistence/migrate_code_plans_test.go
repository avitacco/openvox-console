package persistence

import "testing"

func TestMigrate_CodePlans_UpDownUp(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 39)
	if err := tryExecSQL(dsn, `INSERT INTO jobs (kind, status, triggered_by, started_at) VALUES ('codeplan', 'running', 'a', now())`); err == nil {
		t.Fatal("a code plan job was accepted before the migration")
	}
	migrateTo(t, dsn, 40)
	execSQL(t, dsn,
		`INSERT INTO jobs (kind, plan_name, status, triggered_by, started_at, heartbeat_at, plan_trace, result)
		   VALUES ('codeplan', 'ops::demo', 'running', 'a', now(), now(), '[]', '{"ok":true}')`,
		`INSERT INTO jobs (kind, task_name, status, triggered_by, started_at, parent_job_id, step_order)
		   SELECT 'task', 'x::y', 'running', 'a', now(), id, 1 FROM jobs WHERE kind = 'codeplan'`,
		`INSERT INTO jobs (kind, status, triggered_by, started_at) VALUES ('run', 'running', 'a', now())`,
	)
	// The error column defaults to empty, so no existing job needs a value.
	execSQL(t, dsn, `DO $$ BEGIN
	   IF (SELECT count(*) FROM jobs WHERE error <> '') <> 0 THEN RAISE EXCEPTION 'error should default empty'; END IF;
	 END $$`)
	// Down removes code plans with their step jobs and keeps everything else.
	migrateTo(t, dsn, 39)
	execSQL(t, dsn, `DO $$ BEGIN
	   IF (SELECT count(*) FROM jobs) <> 1 OR (SELECT count(*) FROM jobs WHERE kind = 'run') <> 1 THEN RAISE EXCEPTION 'down kept or lost the wrong rows'; END IF;
	 END $$`)
	migrateTo(t, dsn, 40)
}

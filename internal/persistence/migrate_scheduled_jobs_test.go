package persistence

import "testing"

func TestMigrate_ScheduledJobs_UpDownUp(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 30)
	// A job from before the upgrade must survive it in both directions.
	execSQL(t, dsn, `INSERT INTO jobs (kind, status, triggered_by, started_at) VALUES ('run', 'succeeded', 'admin', now())`)

	migrateTo(t, dsn, 31)
	for _, table := range []string{"scheduled_jobs", "scheduled_job_firings"} {
		if !tableExists(t, dsn, table) {
			t.Fatalf("up: %s missing", table)
		}
	}
	execSQL(t, dsn,
		`INSERT INTO scheduled_jobs (name, kind, target, start_at, owner, owner_kind)
		 VALUES ('nightly', 'run', '{"group": 1}', now(), 'admin', 'user')`,
		`UPDATE jobs SET schedule_id = (SELECT id FROM scheduled_jobs), schedule_name = 'nightly', target_source = '{"kind":"group"}'`,
		// Deleting a schedule leaves its job, still naming it.
		`DELETE FROM scheduled_jobs`,
		`DO $$ BEGIN
		   IF (SELECT count(*) FROM jobs WHERE schedule_id IS NULL AND schedule_name = 'nightly') <> 1 THEN
		     RAISE EXCEPTION 'job lost its schedule name or kept a dangling id';
		   END IF;
		 END $$`,
	)

	migrateTo(t, dsn, 30)
	for _, table := range []string{"scheduled_jobs", "scheduled_job_firings"} {
		if tableExists(t, dsn, table) {
			t.Fatalf("down: %s still present", table)
		}
	}
	execSQL(t, dsn, `DO $$ BEGIN
	   IF (SELECT count(*) FROM jobs) <> 1 THEN RAISE EXCEPTION 'job lost in the down migration'; END IF;
	 END $$`)
	migrateTo(t, dsn, 31)
	if !tableExists(t, dsn, "scheduled_jobs") {
		t.Fatal("up again: scheduled_jobs missing")
	}
}

package persistence

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func columnExists(t *testing.T, dsn, table, column string) bool {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var exists bool
	err = conn.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2)`,
		table, column,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("check column: %v", err)
	}
	return exists
}

func TestMigrate_TaskEnvironmentAndResultUpDownUp(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))

	assertColumns := func(step string, want bool) {
		t.Helper()
		if got := columnExists(t, dsn, "jobs", "environment"); got != want {
			t.Errorf("%s: jobs.environment present = %v, want %v", step, got, want)
		}
		if got := columnExists(t, dsn, "job_targets", "result"); got != want {
			t.Errorf("%s: job_targets.result present = %v, want %v", step, got, want)
		}
	}

	migrateTo(t, dsn, 27)
	assertColumns("up", true)
	migrateTo(t, dsn, 26)
	assertColumns("down", false)
	migrateTo(t, dsn, 27)
	assertColumns("up again", true)
}

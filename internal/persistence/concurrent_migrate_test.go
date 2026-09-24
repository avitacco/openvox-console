package persistence_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/persistence"
)

// Several instances starting at once each call Migrate. golang-migrate's
// Postgres driver takes a pg_advisory_lock around a migration run, so
// they serialize rather than racing: whichever arrives second waits, then
// finds nothing to do.
//
// Worth pinning because the alternative failure is ugly and
// intermittent - concurrent DDL against the same schema_migrations row -
// and it would only ever appear when a deployment scales up, which is
// exactly when nobody wants to debug it.
func TestConcurrentMigrateSerializes(t *testing.T) {
	dsn := os.Getenv("CONSOLE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONSOLE_TEST_POSTGRES_DSN not set; skipping integration test")
	}

	const instances = 4
	var wg sync.WaitGroup
	errs := make([]error, instances)
	for i := 0; i < instances; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = persistence.Migrate(dsn)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("instance %d: concurrent Migrate() failed: %v", i, err)
		}
	}
}

// pgxpool's own connection-string settings must not reach the server.
// Connect understands them; the database/sql driver Migrate uses does
// not, and forwards anything unrecognised as a runtime parameter, which
// Postgres refuses outright.
//
// Worth a test because the failure is quiet and misleading: migration
// failure is non-fatal by design, so sizing the pool through the DSN
// would leave a console running with no activity recorder and no token
// revocation tracking - both skipped when migrations did not apply.
func TestMigrateIgnoresPoolSettingsInTheDSN(t *testing.T) {
	dsn := os.Getenv("CONSOLE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONSOLE_TEST_POSTGRES_DSN not set; skipping integration test")
	}

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}

	for _, param := range []string{
		"pool_max_conns=4",
		"pool_min_conns=1",
		"pool_max_conn_lifetime=1h",
	} {
		if err := persistence.Migrate(dsn + sep + param); err != nil {
			t.Errorf("Migrate() with %s in the DSN: %v", param, err)
		}
	}
}

// A pool setting in the DSN must actually take effect, otherwise there
// is no way to size the pool at all.
func TestConnectHonoursPoolSettingsInTheDSN(t *testing.T) {
	dsn := os.Getenv("CONSOLE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONSOLE_TEST_POSTGRES_DSN not set; skipping integration test")
	}

	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}

	db, err := persistence.Connect(context.Background(), dsn+sep+"pool_max_conns=3")
	if err != nil {
		t.Fatalf("Connect() with a pool setting: %v", err)
	}
	defer db.Close()

	if got := db.Pool.Config().MaxConns; got != 3 {
		t.Errorf("pool MaxConns = %d, want 3; the DSN setting was ignored", got)
	}
}

package persistence_test

import (
	"os"
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

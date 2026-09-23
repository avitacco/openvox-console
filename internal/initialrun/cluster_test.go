package initialrun_test

import (
	"context"
	"sync"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/initialrun"
	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

// Clustering makes a node's connect event visible on every instance:
// $SYS connect events propagate cluster-wide, so OnNodeConnect fires
// once per running instance for what is logically one connection.
//
// That is a designed-for outcome rather than a defect - node-transport's
// spec already requires observers to tolerate duplicate notification -
// and this is what makes it safe: however many instances react, the
// Postgres claim admits exactly one, so a node gets exactly one
// automatic first run.
func TestOnlyOneInstanceClaimsAnInitialRun(t *testing.T) {
	pool := testdb.Pool(t)
	store := initialrun.NewStore(pool)
	ctx := context.Background()

	certname := "cluster-claim-probe.example.com"
	if _, err := pool.Exec(ctx, `DELETE FROM initial_run_claims WHERE certname = $1`, certname); err != nil {
		t.Fatalf("clear previous claim: %v", err)
	}

	// Stands in for N instances all observing the same connect event at
	// once.
	const instances = 8
	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		mu    sync.Mutex
		wins  int
	)
	start.Add(1)
	for i := 0; i < instances; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			won, err := store.Claim(ctx, certname)
			if err != nil {
				t.Errorf("Claim() error: %v", err)
				return
			}
			if won {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	start.Done()
	done.Wait()

	if wins != 1 {
		t.Errorf("%d instances claimed the initial run for one node; want exactly 1", wins)
	}
}

// A reconnect - or a second instance observing the same node later -
// must not produce another automatic run.
func TestARepeatedNotificationDoesNotClaimAgain(t *testing.T) {
	pool := testdb.Pool(t)
	store := initialrun.NewStore(pool)
	ctx := context.Background()

	certname := "cluster-reclaim-probe.example.com"
	if _, err := pool.Exec(ctx, `DELETE FROM initial_run_claims WHERE certname = $1`, certname); err != nil {
		t.Fatalf("clear previous claim: %v", err)
	}

	if won, err := store.Claim(ctx, certname); err != nil || !won {
		t.Fatalf("first Claim() = %v, %v; want true, nil", won, err)
	}
	if won, err := store.Claim(ctx, certname); err != nil || won {
		t.Errorf("second Claim() = %v, %v; want false, nil", won, err)
	}
}

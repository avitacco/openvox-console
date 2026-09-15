package initialrun

import (
	"context"
	"sync"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

func TestClaim_FirstCallerWinsSecondLoses(t *testing.T) {
	pool := testdb.Pool(t)
	store := NewStore(pool)
	ctx := context.Background()

	certname := "claim-sequential.example.com"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM initial_run_claims WHERE certname = $1`, certname)
	})

	won, err := store.Claim(ctx, certname)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !won {
		t.Fatal("first claim should win, got lost")
	}

	won, err = store.Claim(ctx, certname)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if won {
		t.Fatal("second claim should lose, got won")
	}
}

// TestClaim_ConcurrentClaimsYieldExactlyOneWinner is the case the
// ON CONFLICT insert exists for: two console instances observing the
// same connect event, racing to dispatch. Exactly one may win, or a node
// gets two unrequested Puppet runs.
func TestClaim_ConcurrentClaimsYieldExactlyOneWinner(t *testing.T) {
	pool := testdb.Pool(t)
	store := NewStore(pool)
	ctx := context.Background()

	certname := "claim-concurrent.example.com"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM initial_run_claims WHERE certname = $1`, certname)
	})

	const claimers = 16
	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		mu    sync.Mutex
		wins  int
		errs  []error
	)
	start.Add(1)
	done.Add(claimers)

	for range claimers {
		go func() {
			defer done.Done()
			start.Wait()
			won, err := store.Claim(ctx, certname)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			if won {
				wins++
			}
		}()
	}

	start.Done()
	done.Wait()

	for _, err := range errs {
		t.Errorf("concurrent claim returned an error: %v", err)
	}
	if wins != 1 {
		t.Fatalf("expected exactly 1 winner across %d concurrent claims, got %d", claimers, wins)
	}
}

// TestClaim_DistinctCertnamesEachWin guards against an over-broad
// conflict target - every node must be claimable independently.
func TestClaim_DistinctCertnamesEachWin(t *testing.T) {
	pool := testdb.Pool(t)
	store := NewStore(pool)
	ctx := context.Background()

	names := []string{"claim-a.example.com", "claim-b.example.com", "claim-c.example.com"}
	t.Cleanup(func() {
		for _, n := range names {
			_, _ = pool.Exec(ctx, `DELETE FROM initial_run_claims WHERE certname = $1`, n)
		}
	})

	for _, n := range names {
		won, err := store.Claim(ctx, n)
		if err != nil {
			t.Fatalf("claim %q: %v", n, err)
		}
		if !won {
			t.Fatalf("claim %q should win on first attempt", n)
		}
	}
}

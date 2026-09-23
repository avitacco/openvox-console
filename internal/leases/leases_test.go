package leases_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/leases"
	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

// name returns a lease name unique to this test, so tests sharing the
// database never contend for the same row.
func name(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test/%s/%d", t.Name(), time.Now().UnixNano())
}

// The property the whole package exists for: however many instances try
// at once, exactly one gets the lease.
func TestConcurrentAcquireHasExactlyOneWinner(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	n := name(t)

	const holders = 12
	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		mu    sync.Mutex
		wins  int
	)
	start.Add(1)
	for i := 0; i < holders; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			l := leases.New(pool, time.Minute)
			start.Wait()
			ok, err := l.Acquire(ctx, n)
			if err != nil {
				t.Errorf("Acquire() error: %v", err)
				return
			}
			if ok {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	start.Done()
	done.Wait()

	if wins != 1 {
		t.Errorf("winners = %d, want exactly 1", wins)
	}
}

// When the holder stops without releasing - a crashed or killed instance
// - another takes over once the lease expires, with no operator action.
func TestExpiredLeaseIsTakenOver(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	n := name(t)

	// A TTL short enough to expire during the test stands in for an
	// instance that stopped and never released.
	first := leases.New(pool, 50*time.Millisecond)
	ok, err := first.Acquire(ctx, n)
	if err != nil || !ok {
		t.Fatalf("first Acquire() = %v, %v; want true, nil", ok, err)
	}

	second := leases.New(pool, time.Minute)
	if ok, err := second.Acquire(ctx, n); err != nil || ok {
		t.Fatalf("second Acquire() while held = %v, %v; want false, nil", ok, err)
	}

	time.Sleep(150 * time.Millisecond)

	if ok, err := second.Acquire(ctx, n); err != nil || !ok {
		t.Errorf("second Acquire() after expiry = %v, %v; want true, nil", ok, err)
	}
}

func TestHeldReportsWhetherWorkIsInProgress(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	n := name(t)

	l := leases.New(pool, time.Minute)
	if held, err := l.Held(ctx, n); err != nil || held {
		t.Fatalf("Held() before acquire = %v, %v; want false, nil", held, err)
	}

	if _, err := l.Acquire(ctx, n); err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}
	if held, err := l.Held(ctx, n); err != nil || !held {
		t.Errorf("Held() while held = %v, %v; want true, nil", held, err)
	}

	if err := l.Release(ctx, n); err != nil {
		t.Fatalf("Release() error: %v", err)
	}
	if held, err := l.Held(ctx, n); err != nil || held {
		t.Errorf("Held() after release = %v, %v; want false, nil", held, err)
	}
}

// Hold runs the work, then releases - so the next attempt succeeds
// rather than waiting out a TTL.
func TestHoldRunsWorkThenReleases(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	n := name(t)

	l := leases.New(pool, time.Minute)
	var ran bool
	acquired, err := l.Hold(ctx, n, func(context.Context) error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("Hold() error: %v", err)
	}
	if !acquired || !ran {
		t.Fatalf("Hold() acquired = %v, ran = %v; want both true", acquired, ran)
	}

	if held, err := l.Held(ctx, n); err != nil || held {
		t.Errorf("lease still held after Hold returned = %v, %v; want false, nil", held, err)
	}
}

// Hold returns the work's own error, and still releases.
func TestHoldReturnsWorkError(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	n := name(t)

	wantErr := errors.New("sync failed")
	l := leases.New(pool, time.Minute)
	acquired, err := l.Hold(ctx, n, func(context.Context) error { return wantErr })
	if !acquired {
		t.Fatal("Hold() did not acquire a free lease")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("Hold() error = %v, want %v", err, wantErr)
	}
	if held, err := l.Held(ctx, n); err != nil || held {
		t.Errorf("lease still held after failing work = %v, %v; want false, nil", held, err)
	}
}

// A second instance's Hold does not run the work while the first holds
// the lease - the duplicate-work case this exists to prevent.
func TestHoldDoesNotRunWorkWhenHeldElsewhere(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	n := name(t)

	first := leases.New(pool, time.Minute)
	if ok, err := first.Acquire(ctx, n); err != nil || !ok {
		t.Fatalf("first Acquire() = %v, %v; want true, nil", ok, err)
	}

	second := leases.New(pool, time.Minute)
	var ran bool
	acquired, err := second.Hold(ctx, n, func(context.Context) error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("Hold() error: %v", err)
	}
	if acquired {
		t.Error("a second holder acquired a lease already held")
	}
	if ran {
		t.Error("the work ran on a second instance while the first held the lease")
	}
}

// When the lease is lost mid-work, the work's context is cancelled so
// it stops rather than continuing without the exclusivity it assumed.
//
// The row is deleted directly rather than stolen by another holder:
// while the holder keeps renewing, the lease never expires and cannot be
// taken - which is the point of renewal. Deleting it reproduces what a
// holder actually observes when it loses the lease, a renewal that
// affects no rows.
func TestHoldCancelsWorkWhenTheLeaseIsLost(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	n := name(t)

	// Renews every ~20ms, so a deletion is noticed promptly.
	l := leases.New(pool, 60*time.Millisecond)

	cancelled := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		if _, err := pool.Exec(context.Background(),
			`DELETE FROM singleton_leases WHERE name = $1`, n); err != nil {
			t.Errorf("delete lease row: %v", err)
		}
	}()

	acquired, err := l.Hold(ctx, n, func(workCtx context.Context) error {
		select {
		case <-workCtx.Done():
			close(cancelled)
			return workCtx.Err()
		case <-time.After(10 * time.Second):
			return errors.New("work was never cancelled after the lease was lost")
		}
	})
	if !acquired {
		t.Fatal("Hold() did not acquire a free lease")
	}
	if err == nil {
		t.Error("Hold() returned no error after the lease was lost")
	}
	select {
	case <-cancelled:
	default:
		t.Error("the work's context was never cancelled after the lease was lost")
	}
}

package agentupgrade

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

// testStore returns a PgStore and a certname prefix unique to this
// test, whose rows are removed afterwards. Other rows may exist (the
// database is shared with a development console), so tests that depend
// on the cluster-wide count of upgrades in progress clear the table's
// 'requested' rows for their own prefix only and pass limits relative
// to what is already there.
func testStore(t *testing.T) (*PgStore, string) {
	t.Helper()
	pool := testdb.Pool(t)
	prefix := fmt.Sprintf("agentupgrade-%s-%d.", t.Name(), time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM node_agents WHERE certname LIKE $1`, prefix+"%")
	})
	return NewStore(pool), prefix
}

func inProgress(t *testing.T, s *PgStore) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM node_agents WHERE upgrade_state = 'requested'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func oldStatus() *Status {
	built := consoleBuilt.Add(-time.Hour)
	return &Status{Digest: "old", BuildTime: &built, Version: "v1", OS: "linux", Arch: "amd64", InstallMethod: "deb", AcceptsUpgrades: true}
}

func TestPgStore_RecordStatus(t *testing.T) {
	s, prefix := testStore(t)
	ctx := context.Background()
	name := prefix + "n1"

	if r, err := s.Get(ctx, name); err != nil || r != nil {
		t.Fatalf("Get before any report = %+v, %v", r, err)
	}

	if err := s.RecordStatus(ctx, name, oldStatus()); err != nil {
		t.Fatal(err)
	}
	r, err := s.Get(ctx, name)
	if err != nil || r == nil {
		t.Fatalf("Get = %+v, %v", r, err)
	}
	if !r.Capable || r.Digest != "old" || r.Version != "v1" || r.OS != "linux" || r.Arch != "amd64" ||
		r.InstallMethod != "deb" || !r.AcceptsUpgrades || r.BuildTime == nil || !r.BuildTime.Equal(consoleBuilt.Add(-time.Hour)) ||
		r.UpgradeState != UpgradeIdle || r.ReportedAt.IsZero() {
		t.Errorf("record = %+v", r)
	}

	// A client that cannot report replaces what was known.
	if err := s.RecordStatus(ctx, name, nil); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Get(ctx, name)
	if r.Capable || r.Digest != "" || r.BuildTime != nil {
		t.Errorf("after an unsupported reply: %+v", r)
	}

	// An unstamped client has no build time.
	unstamped := oldStatus()
	unstamped.BuildTime = nil
	if err := s.RecordStatus(ctx, name, unstamped); err != nil {
		t.Fatal(err)
	}
	if r, _ = s.Get(ctx, name); !r.Capable || r.BuildTime != nil {
		t.Errorf("unstamped client: %+v", r)
	}
}

func TestPgStore_ClaimFailRetryExhaust(t *testing.T) {
	s, prefix := testStore(t)
	ctx := context.Background()
	name := prefix + "n1"
	now := time.Now().UTC().Truncate(time.Second)
	limit := inProgress(t, s) + 5

	// Nothing to claim for a node that never reported.
	if won, _, err := s.Claim(ctx, name, "new", limit, false, now); err != nil || won {
		t.Fatalf("Claim of an unknown node = %v, %v", won, err)
	}
	if err := s.RecordStatus(ctx, name, oldStatus()); err != nil {
		t.Fatal(err)
	}

	won, attempts, err := s.Claim(ctx, name, "new", limit, false, now)
	if err != nil || !won || attempts != 0 {
		t.Fatalf("first Claim = %v, %d, %v", won, attempts, err)
	}
	if won, _, _ := s.Claim(ctx, name, "new", limit, false, now); won {
		t.Fatal("a node already being upgraded was claimed again")
	}
	if won, _, _ := s.Claim(ctx, name, "new", limit, true, now); won {
		t.Fatal("a forced claim took a node already being upgraded")
	}

	retryAt := now.Add(5 * time.Minute)
	if err := s.Fail(ctx, name, "digest mismatch", false, retryAt); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Get(ctx, name)
	if r.UpgradeState != UpgradeFailed || r.Attempts != 1 || r.LastError != "digest mismatch" ||
		r.RequestedAt != nil || r.NextAttemptAt == nil || !r.NextAttemptAt.Equal(retryAt) {
		t.Fatalf("after Fail: %+v", r)
	}
	// Failing again changes nothing: the row is no longer requested.
	if err := s.Fail(ctx, name, "again", false, retryAt); err != nil {
		t.Fatal(err)
	}
	if r, _ = s.Get(ctx, name); r.Attempts != 1 || r.LastError != "digest mismatch" {
		t.Fatalf("a second Fail was recorded: %+v", r)
	}

	if won, _, _ := s.Claim(ctx, name, "new", limit, false, now.Add(time.Minute)); won {
		t.Fatal("claimed before the next attempt was due")
	}
	won, attempts, _ = s.Claim(ctx, name, "new", limit, false, retryAt)
	if !won || attempts != 1 {
		t.Fatalf("Claim once due = %v with %d attempts, want true with 1", won, attempts)
	}

	if err := s.Fail(ctx, name, "still failing", true, retryAt); err != nil {
		t.Fatal(err)
	}
	if won, _, _ := s.Claim(ctx, name, "new", limit, false, now.Add(24*time.Hour)); won {
		t.Fatal("an exhausted node was claimed for the same build")
	}

	// A different build starts over.
	won, attempts, _ = s.Claim(ctx, name, "newer", limit, false, now)
	if !won || attempts != 0 {
		t.Fatalf("Claim for a different build = %v with %d attempts, want true with 0", won, attempts)
	}
	_ = s.Fail(ctx, name, "x", true, now.Add(time.Hour))

	// And an operator's forced claim overrides exhaustion and backoff.
	won, attempts, _ = s.Claim(ctx, name, "newer", limit, true, now)
	if !won || attempts != 0 {
		t.Fatalf("forced Claim = %v with %d attempts, want true with 0", won, attempts)
	}
}

func TestPgStore_ReleaseDoesNotCountAnAttempt(t *testing.T) {
	s, prefix := testStore(t)
	ctx := context.Background()
	name := prefix + "n1"
	now := time.Now().UTC().Truncate(time.Second)
	limit := inProgress(t, s) + 5

	_ = s.RecordStatus(ctx, name, oldStatus())
	if won, _, _ := s.Claim(ctx, name, "new", limit, false, now); !won {
		t.Fatal("claim failed")
	}
	if err := s.Release(ctx, name, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	r, _ := s.Get(ctx, name)
	if r.UpgradeState != UpgradeIdle || r.Attempts != 0 || r.NextAttemptAt == nil || !r.NextAttemptAt.Equal(now.Add(time.Minute)) {
		t.Errorf("after Release: %+v", r)
	}
	if won, _, _ := s.Claim(ctx, name, "new", limit, false, now); won {
		t.Error("claimed before the released node was due again")
	}
	if won, _, _ := s.Claim(ctx, name, "new", limit, false, now.Add(time.Minute)); !won {
		t.Error("not claimable once due")
	}
}

func TestPgStore_ReportingTheTargetBuildCompletesTheUpgrade(t *testing.T) {
	s, prefix := testStore(t)
	ctx := context.Background()
	name := prefix + "n1"
	now := time.Now().UTC()
	limit := inProgress(t, s) + 5

	_ = s.RecordStatus(ctx, name, oldStatus())
	_, _, _ = s.Claim(ctx, name, "new", limit, false, now)
	_ = s.Fail(ctx, name, "first try failed", false, now)
	_, _, _ = s.Claim(ctx, name, "new", limit, false, now)

	// Still on the old build: nothing is completed.
	_ = s.RecordStatus(ctx, name, oldStatus())
	if r, _ := s.Get(ctx, name); r.UpgradeState != UpgradeRequested || r.LastError == "" {
		t.Fatalf("reporting the old build changed the upgrade: %+v", r)
	}

	upgraded := oldStatus()
	upgraded.Digest = "new"
	_ = s.RecordStatus(ctx, name, upgraded)
	r, _ := s.Get(ctx, name)
	if r.UpgradeState != UpgradeIdle || r.Attempts != 0 || r.LastError != "" || r.RequestedAt != nil || r.NextAttemptAt != nil {
		t.Errorf("after reporting the target build: %+v", r)
	}
}

func TestPgStore_ConcurrencyLimit(t *testing.T) {
	s, prefix := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	limit := inProgress(t, s) + 2

	names := []string{prefix + "a", prefix + "b", prefix + "c"}
	for _, n := range names {
		_ = s.RecordStatus(ctx, n, oldStatus())
	}
	if won, _, _ := s.Claim(ctx, names[0], "new", limit, false, now); !won {
		t.Fatal("first claim refused")
	}
	if won, _, _ := s.Claim(ctx, names[1], "new", limit, false, now); !won {
		t.Fatal("second claim refused")
	}
	if won, _, _ := s.Claim(ctx, names[2], "new", limit, false, now); won {
		t.Fatal("a claim beyond the limit was granted")
	}
	// An operator's request is not held back by the limit.
	if won, _, _ := s.Claim(ctx, names[2], "new", limit, true, now); !won {
		t.Fatal("a forced claim was refused by the concurrency limit")
	}
}

// Two console instances sweeping the same nodes at once: every node is
// claimed by exactly one of them, and together they never exceed the
// limit.
func TestPgStore_ConcurrentClaimersGetDisjointNodes(t *testing.T) {
	s, prefix := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	const nodes, room = 12, 8
	limit := inProgress(t, s) + room

	names := make([]string, nodes)
	for i := range names {
		names[i] = fmt.Sprintf("%sn%02d", prefix, i)
		_ = s.RecordStatus(ctx, names[i], oldStatus())
	}

	var mu sync.Mutex
	wins := map[string]int{}
	var wg sync.WaitGroup
	for claimer := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, n := range names {
				won, _, err := s.Claim(ctx, n, "new", limit, false, now)
				if err != nil {
					t.Errorf("claimer %d: %v", claimer, err)
					return
				}
				if won {
					mu.Lock()
					wins[n]++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()

	total := 0
	for n, count := range wins {
		if count != 1 {
			t.Errorf("%s was claimed %d times", n, count)
		}
		total += count
	}
	if total != room {
		t.Errorf("%d nodes claimed, want exactly the %d the limit allows", total, room)
	}
}

func TestPgStore_List(t *testing.T) {
	s, prefix := testStore(t)
	ctx := context.Background()
	_ = s.RecordStatus(ctx, prefix+"a", oldStatus())
	_ = s.RecordStatus(ctx, prefix+"b", nil)

	all, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]Record{}
	for _, r := range all {
		found[r.Certname] = r
	}
	if !found[prefix+"a"].Capable || found[prefix+"b"].Capable || found[prefix+"b"].Certname == "" {
		t.Errorf("List = %+v / %+v", found[prefix+"a"], found[prefix+"b"])
	}
}

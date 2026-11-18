package patchinventory

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/testdb"
)

var t0 = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

func newStore(t *testing.T) *Store { return NewStore(testdb.ScratchPool(t)) }

func TestStore_SaveAndGet(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	err := s.Save(ctx, "web01", ScanResult{Manager: "apt", RebootRequired: true, Updates: []Update{
		{Name: "curl", Installed: "1", Available: "2"},
		{Name: "libssl3", Installed: "3", Available: "4", Security: true},
		{Name: "curl", Available: "dup"}, // a package once per node
	}}, t0)
	if err != nil {
		t.Fatal(err)
	}
	snap, updates, err := s.Get(ctx, "web01")
	if err != nil || snap == nil {
		t.Fatalf("%v %v", snap, err)
	}
	if snap.UpdateCount != 2 || snap.SecurityCount != 1 || !snap.RebootRequired || snap.Manager != "apt" || snap.ScannedAt == nil || !snap.ScannedAt.Equal(t0) {
		t.Errorf("snapshot = %+v", snap)
	}
	if len(updates) != 2 || updates[0].Name != "libssl3" { // security first
		t.Errorf("updates = %+v", updates)
	}
	if snap, _, _ := s.Get(ctx, "nobody"); snap != nil {
		t.Errorf("a never-scanned node has no snapshot, got %+v", snap)
	}
}

func TestStore_NewerScanReplacesOlder(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	_ = s.Save(ctx, "web01", ScanResult{Manager: "apt", Updates: []Update{{Name: "a", Available: "1"}, {Name: "b", Available: "1"}}}, t0)
	_ = s.Save(ctx, "web01", ScanResult{Manager: "apt", Updates: []Update{{Name: "b", Available: "1"}}}, t0.Add(time.Hour))
	snap, updates, _ := s.Get(ctx, "web01")
	if snap.UpdateCount != 1 || len(updates) != 1 || updates[0].Name != "b" {
		t.Errorf("got %+v %+v", snap, updates)
	}
}

func TestStore_NothingPendingIsNotNeverScanned(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	_ = s.Save(ctx, "web01", ScanResult{Manager: "apt", Updates: nil}, t0)
	snap, updates, _ := s.Get(ctx, "web01")
	if snap == nil || snap.ScannedAt == nil || snap.UpdateCount != 0 || len(updates) != 0 {
		t.Errorf("got %+v %+v", snap, updates)
	}
}

func TestStore_FailureKeepsLastGoodSnapshot(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	_ = s.Save(ctx, "web01", ScanResult{Manager: "apt", Updates: []Update{{Name: "a", Available: "1"}}}, t0)
	if err := s.RecordFailure(ctx, "web01", "mirror unreachable", false, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	snap, updates, _ := s.Get(ctx, "web01")
	if snap.Error != "mirror unreachable" || !snap.AttemptedAt.Equal(t0.Add(time.Hour)) || !snap.ScannedAt.Equal(t0) || len(updates) != 1 {
		t.Errorf("got %+v %+v", snap, updates)
	}
	// A later good scan clears the error.
	_ = s.Save(ctx, "web01", ScanResult{Manager: "apt"}, t0.Add(2*time.Hour))
	if snap, _, _ := s.Get(ctx, "web01"); snap.Error != "" {
		t.Errorf("error should clear, got %q", snap.Error)
	}
}

func TestStore_FailureBeforeAnyScan(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	if err := s.RecordFailure(ctx, "web01", "boom", false, t0); err != nil {
		t.Fatal(err)
	}
	snap, _, _ := s.Get(ctx, "web01")
	if snap == nil || snap.ScannedAt != nil || snap.Error != "boom" {
		t.Errorf("got %+v", snap)
	}
}

func TestStore_UnsupportedClearsOldUpdates(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	_ = s.Save(ctx, "web01", ScanResult{Manager: "apt", Updates: []Update{{Name: "a", Available: "1"}}}, t0)
	_ = s.RecordFailure(ctx, "web01", "unsupported", true, t0.Add(time.Hour))
	snap, updates, _ := s.Get(ctx, "web01")
	if !snap.Unsupported || snap.ScannedAt != nil || snap.UpdateCount != 0 || len(updates) != 0 {
		t.Errorf("got %+v %+v", snap, updates)
	}
}

func TestStore_Search(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	_ = s.Save(ctx, "a", ScanResult{Updates: []Update{{Name: "openssl", Available: "2"}, {Name: "curl", Available: "2"}}}, t0)
	_ = s.Save(ctx, "b", ScanResult{Updates: []Update{{Name: "openssl", Available: "3"}, {Name: "my_pkg", Available: "1"}}}, t0)
	got, _ := s.Search(ctx, "ssl", nil)
	if len(got) != 2 || got[0].Certname != "a" || got[1].Certname != "b" {
		t.Errorf("got %+v", got)
	}
	if got, _ := s.Search(ctx, "ssl", []string{"b"}); len(got) != 1 || got[0].Certname != "b" {
		t.Errorf("scoped got %+v", got)
	}
	if got, _ := s.Search(ctx, "ssl", []string{}); len(got) != 0 {
		t.Errorf("an empty scope matches nothing, got %+v", got)
	}
	// LIKE wildcards in the search are literal.
	if got, _ := s.Search(ctx, "%", nil); len(got) != 0 {
		t.Errorf("%% must not match everything, got %+v", got)
	}
	if got, _ := s.Search(ctx, "my_p", nil); len(got) != 1 {
		t.Errorf("underscore is literal, got %+v", got)
	}
}

func TestStore_Settings(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	st, err := s.Settings(ctx)
	if err != nil || !st.Enabled || st.Interval != DefaultInterval {
		t.Fatalf("defaults = %+v %v", st, err)
	}
	st, err = s.SetSettings(ctx, false, 60)
	if err != nil || st.Enabled || st.Interval != time.Hour {
		t.Fatalf("got %+v %v", st, err)
	}
	for _, bad := range []int{5, 14, 10081, 0, -1} {
		if _, err := s.SetSettings(ctx, true, bad); err != ErrInterval {
			t.Errorf("interval %d: err = %v", bad, err)
		}
	}
	if st, _ := s.Settings(ctx); st.IntervalMinutes != 60 || st.Enabled {
		t.Errorf("a refused change altered the setting: %+v", st)
	}
}

func TestStore_Purge(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	_ = s.Save(ctx, "old", ScanResult{Updates: []Update{{Name: "a", Available: "1"}}}, t0.Add(-100*24*time.Hour))
	_ = s.Save(ctx, "new", ScanResult{}, t0)
	n, err := s.Purge(ctx, t0.Add(-90*24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if snap, _, _ := s.Get(ctx, "old"); snap != nil {
		t.Error("old snapshot survived")
	}
	if snap, _, _ := s.Get(ctx, "new"); snap == nil {
		t.Error("new snapshot was purged")
	}
}

// A reader must never see a summary that disagrees with its list, even
// while scans keep replacing the snapshot.
func TestStore_ReaderNeverSeesHalfASnapshot(t *testing.T) {
	s, ctx := newStore(t), context.Background()
	mk := func(n int) ScanResult {
		r := ScanResult{Manager: "apt"}
		for i := 0; i < n; i++ {
			r.Updates = append(r.Updates, Update{Name: fmt.Sprintf("p%d", i), Available: "1"})
		}
		return r
	}
	_ = s.Save(ctx, "web01", mk(3), t0)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = s.Save(ctx, "web01", mk(2+i%30), t0.Add(time.Duration(i)*time.Second))
		}
	}()
	for i := 0; i < 150; i++ {
		snap, updates, err := s.Get(ctx, "web01")
		if err != nil {
			t.Fatal(err)
		}
		if snap.UpdateCount != len(updates) {
			close(stop)
			wg.Wait()
			t.Fatalf("summary says %d, list has %d", snap.UpdateCount, len(updates))
		}
	}
	close(stop)
	wg.Wait()
}

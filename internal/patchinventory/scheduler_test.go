package patchinventory

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"
)

func TestDue(t *testing.T) {
	now := t0
	attempts := map[string]time.Time{
		"fresh":     now.Add(-time.Hour),
		"exact":     now.Add(-24 * time.Hour),
		"old":       now.Add(-25 * time.Hour),
		"failed":    now.Add(-time.Minute), // a failed attempt counts: no retry every tick
		"offscreen": now.Add(-48 * time.Hour),
	}
	got := Due([]string{"never", "fresh", "exact", "old", "failed", "offscreen"}, attempts, 24*time.Hour, now)
	want := []string{"exact", "never", "offscreen", "old"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func newScheduler(t *testing.T, nodes []string, answers map[string]func() ([]byte, error)) (*Scheduler, *Store, *fakeTransport) {
	st := newStore(t)
	tr := &fakeTransport{answers: answers}
	s := NewScheduler(st, NewScanner(tr, st), nil, fakeSource{nodes: nodes}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.now = func() time.Time { return t0 }
	s.scanner.now = func() time.Time { return s.now() } // attempts are stamped with the same clock
	s.lastPurge = t0
	return s, st, tr
}

func TestRunOnce_ScansDueNodesOnly(t *testing.T) {
	s, st, tr := newScheduler(t, []string{"due", "recent", "gone"}, map[string]func() ([]byte, error){"due": ok(twoUpdates), "recent": ok(twoUpdates)})
	ctx := context.Background()
	_ = st.Save(ctx, "recent", ScanResult{}, t0.Add(-time.Hour))
	_ = st.Save(ctx, "due", ScanResult{}, t0.Add(-30*time.Hour))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if snap, _, _ := st.Get(ctx, "due"); snap.UpdateCount != 2 {
		t.Errorf("due node not scanned: %+v", snap)
	}
	if snap, _, _ := st.Get(ctx, "recent"); snap.UpdateCount != 0 {
		t.Errorf("recent node was rescanned: %+v", snap)
	}
	if tr.calls.Load() != 2 { // due, and the offline one which is attempted and costs nothing
		t.Errorf("dispatched %d times", tr.calls.Load())
	}
	if snap, _, _ := st.Get(ctx, "gone"); snap != nil {
		t.Errorf("an offline node recorded an attempt: %+v", snap)
	}
}

func TestRunOnce_SwitchedOffScansNothing(t *testing.T) {
	s, st, tr := newScheduler(t, []string{"a"}, map[string]func() ([]byte, error){"a": ok(twoUpdates)})
	if _, err := st.SetSettings(context.Background(), false, 60); err != nil {
		t.Fatal(err)
	}
	if err := s.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tr.calls.Load() != 0 {
		t.Errorf("dispatched %d times while off", tr.calls.Load())
	}
}

func TestRunOnce_FailedNodeIsNotRetriedEveryPass(t *testing.T) {
	s, st, tr := newScheduler(t, []string{"a"}, map[string]func() ([]byte, error){"a": ok(`{"error":"boom"}`)})
	ctx := context.Background()
	_ = s.RunOnce(ctx)
	_ = s.RunOnce(ctx)
	if tr.calls.Load() != 1 {
		t.Errorf("a failing node was scanned %d times in two passes", tr.calls.Load())
	}
	if snap, _, _ := st.Get(ctx, "a"); snap.Error != "boom" {
		t.Errorf("got %+v", snap)
	}
	// Once the interval passes it is tried again.
	s.now = func() time.Time { return t0.Add(25 * time.Hour) }
	_ = s.RunOnce(ctx)
	if tr.calls.Load() != 2 {
		t.Errorf("not retried after the interval: %d", tr.calls.Load())
	}
}

func TestRunOnce_PurgesOldSnapshotsDaily(t *testing.T) {
	s, st, _ := newScheduler(t, nil, nil)
	s.lastPurge = t0.Add(-48 * time.Hour)
	ctx := context.Background()
	_ = st.Save(ctx, "stale", ScanResult{}, t0.Add(-100*24*time.Hour))
	if err := s.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if snap, _, _ := st.Get(ctx, "stale"); snap != nil {
		t.Error("old snapshot not purged")
	}
}

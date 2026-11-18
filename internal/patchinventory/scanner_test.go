package patchinventory

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/nodetransport"
)

type fakeTransport struct {
	answers map[string]func() ([]byte, error)
	calls   atomic.Int32
	active  atomic.Int32
	peak    atomic.Int32
	delay   time.Duration
}

func (f *fakeTransport) Dispatch(ctx context.Context, certname string, payload []byte, timeout time.Duration) ([]byte, error) {
	f.calls.Add(1)
	n := f.active.Add(1)
	defer f.active.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	time.Sleep(f.delay)
	if a, ok := f.answers[certname]; ok {
		return a()
	}
	return nil, nodetransport.ErrNodeNotConnected
}

func ok(body string) func() ([]byte, error) {
	return func() ([]byte, error) { return []byte(body), nil }
}

const twoUpdates = `{"result":{"patch_scan":{"manager":"apt","reboot_required":true,"updates":[
 {"name":"curl","installed":"1","available":"2","security":false},
 {"name":"libssl3","installed":"3","available":"4","security":true}]}}}`

func TestScanNode_Success(t *testing.T) {
	st := newStore(t)
	s := NewScanner(&fakeTransport{answers: map[string]func() ([]byte, error){"web01": ok(twoUpdates)}}, st)
	r, err := s.ScanNode(context.Background(), "web01")
	if err != nil || r.Outcome != OutcomeScanned || r.Updates != 2 || r.Security != 1 {
		t.Fatalf("got %+v %v", r, err)
	}
	snap, _, _ := st.Get(context.Background(), "web01")
	if snap == nil || !snap.RebootRequired || snap.Manager != "apt" {
		t.Errorf("stored %+v", snap)
	}
}

func TestScanNode_OfflineChangesNothing(t *testing.T) {
	st := newStore(t)
	_ = st.Save(context.Background(), "web01", ScanResult{Updates: []Update{{Name: "a", Available: "1"}}}, t0)
	s := NewScanner(&fakeTransport{}, st)
	r, err := s.ScanNode(context.Background(), "web01")
	if err != nil || r.Outcome != OutcomeOffline {
		t.Fatalf("got %+v %v", r, err)
	}
	snap, _, _ := st.Get(context.Background(), "web01")
	if !snap.AttemptedAt.Equal(t0) || snap.Error != "" {
		t.Errorf("an offline node's snapshot changed: %+v", snap)
	}
	// And a node never seen leaves no row.
	if _, err := s.ScanNode(context.Background(), "ghost"); err != nil {
		t.Fatal(err)
	}
	if snap, _, _ := st.Get(context.Background(), "ghost"); snap != nil {
		t.Errorf("offline scan created a row: %+v", snap)
	}
}

func TestScanNode_NilTransportIsOffline(t *testing.T) {
	r, err := NewScanner(nil, newStore(t)).ScanNode(context.Background(), "web01")
	if err != nil || r.Outcome != OutcomeOffline {
		t.Errorf("got %+v %v", r, err)
	}
}

func TestScanNode_Failures(t *testing.T) {
	cases := map[string]struct {
		answer  func() ([]byte, error)
		outcome Outcome
		text    string
	}{
		"agent error":  {ok(`{"error":"apt-get update failed (exit 100): Failed to fetch"}`), OutcomeFailed, "Failed to fetch"},
		"old agent":    {ok(`{"error":"unsupported action \"patch_scan\""}`), OutcomeUnsupported, "too old"},
		"no manager":   {ok(`{"result":{"patch_scan":{"unsupported":true,"updates":[]}}}`), OutcomeUnsupported, "no package manager"},
		"garbled":      {ok(`not json`), OutcomeFailed, "could not be read"},
		"empty result": {ok(`{"result":{}}`), OutcomeFailed, "no scan result"},
		"transport":    {func() ([]byte, error) { return nil, errors.New("timed out") }, OutcomeFailed, "timed out"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			st := newStore(t)
			s := NewScanner(&fakeTransport{answers: map[string]func() ([]byte, error){"n": c.answer}}, st)
			r, err := s.ScanNode(context.Background(), "n")
			if err != nil || r.Outcome != c.outcome || !contains(r.Error, c.text) {
				t.Fatalf("got %+v %v", r, err)
			}
			snap, _, _ := st.Get(context.Background(), "n")
			if snap == nil || !contains(snap.Error, c.text) || snap.Unsupported != (c.outcome == OutcomeUnsupported) {
				t.Errorf("stored %+v", snap)
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestScanMany_BoundsConcurrencyAndKeepsOrder(t *testing.T) {
	ft := &fakeTransport{answers: map[string]func() ([]byte, error){}, delay: 20 * time.Millisecond}
	var names []string
	for i := 0; i < 12; i++ {
		n := string(rune('a' + i))
		names = append(names, n)
		if i != 5 { // f is offline
			ft.answers[n] = ok(twoUpdates)
		}
	}
	s := NewScanner(ft, newStore(t))
	s.workers = 3
	out := s.ScanMany(context.Background(), names)
	if len(out) != 12 {
		t.Fatalf("got %d results", len(out))
	}
	for i, r := range out {
		want := OutcomeScanned
		if i == 5 {
			want = OutcomeOffline
		}
		if r.Certname != names[i] || r.Outcome != want {
			t.Errorf("result %d = %+v", i, r)
		}
	}
	if peak := ft.peak.Load(); peak > 3 || peak < 2 {
		t.Errorf("peak concurrency %d, want 2..3", peak)
	}
}

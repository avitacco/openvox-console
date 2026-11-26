package codemanager

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// recordingFlush records each server flushed; a URL in fail fails.
type recordingFlush struct {
	mu   sync.Mutex
	urls []string
	envs []string
	fail map[string]error
}

func (f *recordingFlush) flush(_ context.Context, baseURL, environment string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.urls = append(f.urls, baseURL)
	f.envs = append(f.envs, environment)
	return f.fail[baseURL]
}

func (f *recordingFlush) sorted() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := slices.Clone(f.urls)
	slices.Sort(out)
	return out
}

func flushTestHandlers(recorded *[]recordedActivity) *Handlers {
	return NewHandlers(nil, nil, nil, "", slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(_ *http.Request, action, summary string) {
			*recorded = append(*recorded, recordedActivity{action: action, summary: summary})
		}, noopActor, noopRecordAudit, noopRecordAudit)
}

func servers(certnames ...string) func(context.Context) ([]string, error) {
	return func(context.Context) ([]string, error) { return certnames, nil }
}

func runFlush(h *Handlers) {
	h.flushEnvironmentCache(context.Background(), "production", httptest.NewRequest(http.MethodPost, "/", nil))
}

func TestFlushEnvironmentCache_FlushesConfiguredAndDiscoveredServers(t *testing.T) {
	var recorded []recordedActivity
	h := flushTestHandlers(&recorded)
	rf := &recordingFlush{}
	h.SetEnvironmentCacheFlush(&EnvironmentCacheFlush{
		Primary: "https://openvoxserver:8140/",
		// openvoxserver is the primary under its certname: flushed once.
		Servers:   servers("openvoxserver", "compile1.example.com", "compile2.example.com"),
		Addresses: map[string]string{"compile2.example.com": "https://10.0.0.7:8141"},
		Flush:     rf.flush,
	})

	runFlush(h)

	want := []string{"https://10.0.0.7:8141", "https://compile1.example.com:8140", "https://openvoxserver:8140"}
	if got := rf.sorted(); !slices.Equal(got, want) {
		t.Errorf("flushed %v, want %v", got, want)
	}
	for _, env := range rf.envs {
		if env != "production" {
			t.Errorf("flushed environment %q, want production", env)
		}
	}
	if len(recorded) != 0 {
		t.Errorf("successful flushes recorded activity: %+v", recorded)
	}
}

func TestFlushEnvironmentCache_EachFailedServerIsRecorded(t *testing.T) {
	var recorded []recordedActivity
	h := flushTestHandlers(&recorded)
	rf := &recordingFlush{fail: map[string]error{
		"https://compile1.example.com:8140": errors.New("connection refused"),
	}}
	h.SetEnvironmentCacheFlush(&EnvironmentCacheFlush{
		Primary: "https://openvoxserver:8140",
		Servers: servers("compile1.example.com", "compile2.example.com"),
		Flush:   rf.flush,
	})

	runFlush(h)

	if len(rf.urls) != 3 {
		t.Errorf("flushed %v: one failure must not stop the others", rf.urls)
	}
	if len(recorded) != 1 || recorded[0].action != "deploy.cache_flush_failed" ||
		!strings.Contains(recorded[0].summary, "compile1.example.com") {
		t.Errorf("recorded = %+v, want one deploy.cache_flush_failed naming compile1.example.com", recorded)
	}
}

func TestFlushEnvironmentCache_DiscoveryFailureStillFlushesPrimary(t *testing.T) {
	var recorded []recordedActivity
	h := flushTestHandlers(&recorded)
	rf := &recordingFlush{}
	h.SetEnvironmentCacheFlush(&EnvironmentCacheFlush{
		Primary: "https://openvoxserver:8140",
		Servers: func(context.Context) ([]string, error) { return nil, errors.New("openvoxdb unreachable") },
		Flush:   rf.flush,
	})

	runFlush(h)

	if !slices.Equal(rf.urls, []string{"https://openvoxserver:8140"}) {
		t.Errorf("flushed %v, want the configured server", rf.urls)
	}
	if len(recorded) != 1 || recorded[0].action != "deploy.cache_flush_failed" {
		t.Errorf("recorded = %+v, want the failed lookup recorded", recorded)
	}
}

func TestFlushEnvironmentCache_UnwiredIsNoop(t *testing.T) {
	var recorded []recordedActivity
	h := flushTestHandlers(&recorded)
	runFlush(h)
	if len(recorded) != 0 {
		t.Errorf("recorded = %+v", recorded)
	}
}

func TestHostKey(t *testing.T) {
	for _, tc := range [][2]string{
		{"https://OpenVoxServer:8140/", "openvoxserver:8140"},
		{"https://puppet.example.com", "puppet.example.com:443"},
		{"https://[::1]:8140", "[::1]:8140"},
	} {
		if got := hostKey(tc[0]); got != tc[1] {
			t.Errorf("hostKey(%q) = %q, want %q", tc[0], got, tc[1])
		}
	}
}

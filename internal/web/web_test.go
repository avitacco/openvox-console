package web

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestHandler_ServesWithNoExternalFiles moves the process into an empty
// directory before serving, so a pass proves the assets came from the
// binary (via embed.FS), not from files on disk at runtime.
func TestHandler_ServesWithNoExternalFiles(t *testing.T) {
	empty := t.TempDir()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	if err := os.Chdir(empty); err != nil {
		t.Fatalf("Chdir(%q) error: %v", empty, err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	handler, err := Handler()
	if err != nil {
		t.Fatalf("Handler() error: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "vox-header") {
		t.Errorf("response body does not contain expected shell content: %s", rec.Body.String())
	}
}

func TestHandler_RootUsesVoxblocksComponents(t *testing.T) {
	handler, err := Handler()
	if err != nil {
		t.Fatalf("Handler() error: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	body := rec.Body.String()
	// <vox-footer> was deliberately added despite voxblocks' own
	// app-shell/dashboard reference layouts omitting one on logged-in
	// screens - see layout.html.tmpl: license, OpenVox project link, and
	// build version all need a place to live, and this app's own footer
	// is that place regardless of what the reference layouts do.
	for _, want := range []string{"<vox-header", "<vox-input", "<vox-footer"} {
		if !strings.Contains(body, want) {
			t.Errorf("root page missing %q; body:\n%s", want, body)
		}
	}
	// The shell must not fall back to bespoke markup for elements
	// voxblocks already provides (a hand-rolled <header> or <footer>).
	for _, unwanted := range []string{"<header>", "<footer>"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("root page contains bespoke %q, want the voxblocks component instead", unwanted)
		}
	}
}

func TestHandler_ServesVendoredVoxblocksAssets(t *testing.T) {
	handler, err := Handler()
	if err != nil {
		t.Fatalf("Handler() error: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/vendor/voxblocks.js", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Error("voxblocks.js served with an empty body")
	}
}

package codemanager

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func stagingDirWithMarker(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("write marker file: %v", err)
	}
	return dir
}

func TestActivate_ResolvesToNewContent(t *testing.T) {
	codeDir := t.TempDir()
	d := NewDeployer(Config{CodeDirPath: codeDir})

	staging := stagingDirWithMarker(t, "v1")
	if err := d.Activate("production", staging); err != nil {
		t.Fatalf("Activate() error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(codeDir, "environments", "production", "marker.txt"))
	if err != nil {
		t.Fatalf("read activated marker: %v", err)
	}
	if string(got) != "v1" {
		t.Errorf("marker content = %q, want v1", got)
	}
}

func TestActivate_MigratesAwayFromPreExistingRealDirectory(t *testing.T) {
	codeDir := t.TempDir()
	envDir := filepath.Join(codeDir, "environments", "production")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "old-hand-placed-file.txt"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	d := NewDeployer(Config{CodeDirPath: codeDir})
	staging := stagingDirWithMarker(t, "v1")
	if err := d.Activate("production", staging); err != nil {
		t.Fatalf("Activate() error: %v", err)
	}

	info, err := os.Lstat(envDir)
	if err != nil {
		t.Fatalf("Lstat() error: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("environment path is not a symlink after activation over a pre-existing real directory")
	}
	if _, err := os.Stat(filepath.Join(envDir, "old-hand-placed-file.txt")); err == nil {
		t.Error("stale hand-placed content still present after activation")
	}
}

func TestActivate_SecondActivationIsSymlinkSwap(t *testing.T) {
	codeDir := t.TempDir()
	d := NewDeployer(Config{CodeDirPath: codeDir})

	if err := d.Activate("production", stagingDirWithMarker(t, "v1")); err != nil {
		t.Fatalf("first Activate() error: %v", err)
	}
	if err := d.Activate("production", stagingDirWithMarker(t, "v2")); err != nil {
		t.Fatalf("second Activate() error: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(codeDir, "environments", "production", "marker.txt"))
	if err != nil {
		t.Fatalf("read activated marker: %v", err)
	}
	if string(got) != "v2" {
		t.Errorf("marker content = %q, want v2", got)
	}
}

// TestActivate_ConcurrentReadsNeverObservePartialSwap is the real
// verification for the atomicity requirement in specs/code-manager - not
// just code inspection. A reader goroutine hammers the environment path
// in a tight loop while activations alternate between two distinct
// staged versions; every read must see one complete version's content,
// never a missing file, truncated read, or mixed content.
func TestActivate_ConcurrentReadsNeverObservePartialSwap(t *testing.T) {
	codeDir := t.TempDir()
	d := NewDeployer(Config{CodeDirPath: codeDir})

	stagingA := stagingDirWithMarker(t, "version-A-content")
	stagingB := stagingDirWithMarker(t, "version-B-content")
	markerPath := filepath.Join(codeDir, "environments", "production", "marker.txt")

	if err := d.Activate("production", stagingA); err != nil {
		t.Fatalf("initial Activate() error: %v", err)
	}

	var stop atomic.Bool
	var missingFile atomic.Int64
	var otherErrors atomic.Int64
	var badContent atomic.Int64
	var reads atomic.Int64
	var firstOtherErr atomic.Value

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			content, err := os.ReadFile(markerPath)
			reads.Add(1)
			if err != nil {
				// Only a missing file proves the swap was observable
				// mid-flight: both staging directories exist for the
				// whole test and each always contains marker.txt, so
				// ENOENT can only come from the symlink itself being
				// briefly absent. Any other error (a transient
				// resource limit under load, an interrupted syscall)
				// says nothing about atomicity, so it is counted and
				// reported separately rather than misattributed.
				if errors.Is(err, fs.ErrNotExist) {
					missingFile.Add(1)
				} else {
					otherErrors.Add(1)
					firstOtherErr.CompareAndSwap(nil, err)
				}
				continue
			}
			s := string(content)
			if s != "version-A-content" && s != "version-B-content" {
				badContent.Add(1)
			}
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	staging := []string{stagingB, stagingA}
	i := 0
	for time.Now().Before(deadline) {
		if err := d.Activate("production", staging[i%2]); err != nil {
			t.Errorf("Activate() error during concurrent swap: %v", err)
		}
		i++
	}
	stop.Store(true)
	wg.Wait()

	t.Logf("performed %d activations, %d concurrent reads", i, reads.Load())
	if n := otherErrors.Load(); n > 0 {
		// Not an atomicity failure - surfaced so it is never silently
		// ignored, and so a real one is not mistaken for this.
		t.Logf("%d reads failed for reasons unrelated to atomicity; first was: %v", n, firstOtherErr.Load())
	}
	if missingFile.Load() > 0 {
		t.Errorf("%d reads found no file at all during concurrent activation - swap is not atomic", missingFile.Load())
	}
	if badContent.Load() > 0 {
		t.Errorf("%d reads observed neither version's full content (torn/mixed read) - swap is not atomic", badContent.Load())
	}
	if reads.Load() == 0 {
		t.Fatal("reader goroutine never completed a read - test didn't exercise anything")
	}
}

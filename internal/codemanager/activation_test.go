package codemanager

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
//
// Activations are paced rather than run flat out. Unpaced, this loop
// managed ~71,000 renames in two seconds - one every ~28 microseconds,
// which no deployment remotely approaches - and at that rate CI (though
// never any local environment, across tmpfs, XFS and an overlayfs
// container, and several million reads) intermittently saw a handful of
// reads get ENOENT. That was never explained: the swap is a symlink
// replaced by rename(2), which POSIX defines as atomic, and every
// Activate call returned success. Rather than leave the question
// settled by assertion, it is recorded here as open. What this test
// exists to catch is a non-atomic *implementation* - one that unlinks
// before relinking, or copies into place - and that is caught just as
// well at a sane rate, since the reader never stops.
//
// It DID fail again at this pacing (1 ENOENT in ~206,000 reads on CI),
// so the pacing theory was wrong. Ruled out since, by reproduction
// attempts totalling ~10 million reads without a single failure: tmpfs,
// XFS, overlayfs in a container, and real ext4 on a loop device; CPU
// contention; both churn rates; test parallelism. The remaining
// difference is the kernel - CI runs Ubuntu 24.04 on 6.8.x, the machine
// that could not reproduce it runs 7.2 - which containers cannot
// isolate, since they share the host kernel.
//
// Hence the diagnostic below: when this fails, it should say which link
// in the chain was missing rather than leaving the next person to infer
// it. Note the state is captured just after the failing read, so it may
// already have healed - if everything reads as present, that itself
// says the window is transient rather than a lasting broken state.
func TestActivate_ConcurrentReadsNeverObservePartialSwap(t *testing.T) {
	codeDir := t.TempDir()
	d := NewDeployer(Config{CodeDirPath: codeDir})

	stagingA := stagingDirWithMarker(t, "version-A-content")
	stagingB := stagingDirWithMarker(t, "version-B-content")
	target := filepath.Join(codeDir, "environments", "production")
	markerPath := filepath.Join(target, "marker.txt")

	if err := d.Activate("production", stagingA); err != nil {
		t.Fatalf("initial Activate() error: %v", err)
	}

	var stop atomic.Bool
	var missingFile atomic.Int64
	var otherErrors atomic.Int64
	var badContent atomic.Int64
	var reads atomic.Int64
	var firstOtherErr atomic.Value
	var diagnostic atomic.Value
	var captureDiagnostic sync.Once

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
					// Capture what the filesystem actually looked like
					// at the instant of the first failure. Without this
					// a failure only says "something was missing",
					// which is exactly how far this got chased on
					// guesswork alone - see the doc comment.
					captureDiagnostic.Do(func() {
						diagnostic.Store(describeSwapState(target, err))
					})
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
		// See the doc comment: paced to a rate a real deployment could
		// plausibly reach, while the reader keeps running flat out, so
		// swaps and reads still overlap constantly.
		time.Sleep(time.Millisecond)
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
		if d, ok := diagnostic.Load().(string); ok {
			t.Errorf("state at the first such read:\n%s", d)
		}
	}
	if badContent.Load() > 0 {
		t.Errorf("%d reads observed neither version's full content (torn/mixed read) - swap is not atomic", badContent.Load())
	}
	if reads.Load() == 0 {
		t.Fatal("reader goroutine never completed a read - test didn't exercise anything")
	}
}

// describeSwapState records what the live environment path looked like
// at the moment a read found nothing, so a failure identifies which link
// in the chain was missing - the symlink itself, its destination, or the
// file inside it - rather than leaving that to inference.
func describeSwapState(target string, readErr error) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  read error: %v\n", readErr)

	info, err := os.Lstat(target)
	switch {
	case err != nil:
		fmt.Fprintf(&b, "  lstat(%s): %v  <- the environment symlink itself was absent\n", target, err)
	default:
		fmt.Fprintf(&b, "  lstat(%s): mode=%v symlink=%t\n", target, info.Mode(), info.Mode()&os.ModeSymlink != 0)
	}

	dest, err := os.Readlink(target)
	if err != nil {
		fmt.Fprintf(&b, "  readlink: %v\n", err)
	} else {
		fmt.Fprintf(&b, "  readlink -> %s\n", dest)
		if _, err := os.Stat(dest); err != nil {
			fmt.Fprintf(&b, "  stat(destination): %v  <- symlink pointed somewhere absent\n", err)
		} else {
			fmt.Fprintf(&b, "  stat(destination): present\n")
		}
		if _, err := os.Stat(filepath.Join(dest, "marker.txt")); err != nil {
			fmt.Fprintf(&b, "  stat(destination/marker.txt): %v  <- file inside the destination was absent\n", err)
		} else {
			fmt.Fprintf(&b, "  stat(destination/marker.txt): present\n")
		}
	}

	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		fmt.Fprintf(&b, "  readdir(environments): %v\n", err)
	} else {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		fmt.Fprintf(&b, "  environments/ contains: %v\n", names)
	}
	return b.String()
}

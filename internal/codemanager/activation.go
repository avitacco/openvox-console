package codemanager

import (
	"fmt"
	"os"
	"path/filepath"
)

// Activate makes stagingDir the live content for environment, atomically
// from any concurrent reader's perspective - see design.md: an
// os.Rename of a symlink over an existing path is atomic on the same
// POSIX filesystem, and every reader resolves the environment through
// that symlink, so a concurrent reader observes either the fully old or
// fully new content, never a partial swap.
//
// The very first activation for a given environment is a special case:
// this project's earlier phases hand-placed real (non-symlink) content
// at openvox-code/environments/production for test fixtures. A symlink
// cannot be atomically renamed over a non-empty real directory (POSIX
// forbids it), so if the target exists and is not already a symlink, it
// is removed first - a one-time, non-atomic migration off that hand-
// placed content, which was always scratch fixture data (see design.md's
// Risks). Every activation after that first one is a symlink-to-symlink
// rename and is fully atomic.
func (d *Deployer) Activate(environment, stagingDir string) error {
	liveDir := filepath.Join(d.cfg.CodeDirPath, "environments")
	if err := os.MkdirAll(liveDir, 0o755); err != nil {
		return fmt.Errorf("create environments directory: %w", err)
	}

	target := filepath.Join(liveDir, environment)

	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("remove pre-existing non-symlink environment directory: %w", err)
		}
	}

	absStagingDir, err := filepath.Abs(stagingDir)
	if err != nil {
		return fmt.Errorf("resolve staging directory: %w", err)
	}

	tempLink := target + ".new"
	_ = os.Remove(tempLink) // a previous crashed attempt may have left one behind

	if err := os.Symlink(absStagingDir, tempLink); err != nil {
		return fmt.Errorf("create staged symlink: %w", err)
	}
	if err := os.Rename(tempLink, target); err != nil {
		return fmt.Errorf("activate deploy: %w", err)
	}
	return nil
}

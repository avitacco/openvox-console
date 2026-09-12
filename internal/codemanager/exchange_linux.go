//go:build linux

package codemanager

import (
	"errors"

	"golang.org/x/sys/unix"
)

// exchangePaths atomically swaps what two names refer to, via
// renameat2(RENAME_EXCHANGE). Unlike a plain rename, neither name is
// ever momentarily unoccupied: both exist before the call and both
// exist after, with their targets traded.
//
// Returns errExchangeUnsupported when the running kernel or the
// filesystem underneath these paths has no RENAME_EXCHANGE - it landed
// in Linux 3.15, and some filesystems (NFS in particular) still do not
// implement it - so the caller can fall back rather than fail a deploy.
func exchangePaths(a, b string) error {
	err := unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_EXCHANGE)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.EINVAL), errors.Is(err, unix.EOPNOTSUPP):
		return errExchangeUnsupported
	default:
		return err
	}
}

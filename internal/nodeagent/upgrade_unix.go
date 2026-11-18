//go:build !windows

package nodeagent

import (
	"os"
	"os/exec"
	"syscall"
)

// makePrivateDir creates path afresh, readable and writable by this
// process's user only. Anything left there by an earlier attempt is
// removed first.
func makePrivateDir(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	// MkdirAll is subject to the umask and leaves an existing parent's
	// mode alone; the leaf is what has to be private.
	return os.Chmod(path, 0o700)
}

// startDetached starts name in its own session and does not wait for
// it, so it survives this process being stopped.
func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

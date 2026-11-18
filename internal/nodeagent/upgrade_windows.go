//go:build windows

package nodeagent

import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// privateDirSDDL grants full control to SYSTEM and the Administrators
// group and nothing to anyone else, without inheriting the parent's
// entries. A directory under %ProgramData% otherwise lets ordinary
// users create files in it.
const privateDirSDDL = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"

// makePrivateDir creates path afresh, accessible to administrators
// only. It is created with its final permissions rather than tightened
// afterwards, and an existing directory is removed rather than reused,
// so nothing placed there beforehand survives.
func makePrivateDir(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString(privateDirSDDL)
	if err != nil {
		return err
	}
	sa := &windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.CreateDirectory(p, sa)
}

// startDetached is only used by the Linux package installer.
func startDetached(string, ...string) error {
	return errors.New("not supported on Windows")
}

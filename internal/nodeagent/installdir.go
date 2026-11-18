package nodeagent

import (
	"fmt"
	"github.com/voxpupuli/enterprise-console/internal/taskexec"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func safeRelPath(p string) bool { return taskexec.SafeRelPath(p) }

// installDir is one task run's private directory, laid out as Bolt lays
// it out (design decision 7): the executable at
// <root>/<module>/tasks/<name>, and each supporting file at its
// module-relative path under <root>.
type installDir struct {
	root       string
	executable string
}

// buildInstallDir creates a fresh 0700 directory under base and copies
// the task's files into it from the cache. cached maps each file name
// (as the request lists it) to its verified cache path. module is the
// task's module, impl the chosen implementation's file, and support its
// module-relative supporting files.
func buildInstallDir(base, module, impl string, support []string, cached map[string]string) (*installDir, error) {
	if !safeRelPath(module) || strings.Contains(module, "/") || !safeRelPath(impl) || strings.Contains(impl, "/") {
		return nil, fmt.Errorf("invalid task name or file: %s/%s", module, impl)
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, fmt.Errorf("create task run directory: %w", err)
	}
	root, err := os.MkdirTemp(base, "task-")
	if err != nil {
		return nil, fmt.Errorf("create task run directory: %w", err)
	}
	dir := &installDir{root: root}
	fail := func(err error) (*installDir, error) {
		dir.remove()
		return nil, err
	}

	place := func(name, rel string) error {
		src, ok := cached[name]
		if !ok {
			return fmt.Errorf("file %s was not downloaded", name)
		}
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return err
		}
		return copyFile(src, dst)
	}

	dir.executable = filepath.Join(root, module, "tasks", impl)
	if err := place(impl, module+"/tasks/"+impl); err != nil {
		return fail(fmt.Errorf("install %s: %w", impl, err))
	}
	for _, f := range support {
		if !safeRelPath(f) {
			return fail(fmt.Errorf("invalid supporting file path %q", f))
		}
		if err := place(f, f); err != nil {
			return fail(fmt.Errorf("install %s: %w", f, err))
		}
	}
	return dir, nil
}

// copyFile copies src to a new file dst, executable by its owner only:
// the directory is private to the agent's user, and a supporting file
// may itself be run.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (d *installDir) remove() {
	_ = os.RemoveAll(d.root)
}

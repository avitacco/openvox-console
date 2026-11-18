// Package codeplans discovers and runs plans that live in environment code
// (add-code-plans): YAML plans in a module's plans directory, a subset of
// Bolt's format, interpreted by the console itself.
package codeplans

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Limits on what is read from code.
const (
	MaxPlanFileBytes = 256 << 10
	maxPlanFiles     = 2000
)

// Errors a caller can act on.
var (
	// ErrNotAvailable is returned on an instance with no deployed code: it
	// cannot say which plans exist, which is not the same as none.
	ErrNotAvailable        = errors.New("plans are not available on this console instance: it has no deployed code directory")
	ErrEnvironmentNotFound = errors.New("environment not found")
	ErrPlanNotFound        = errors.New("plan not found")
)

var (
	environmentName = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	moduleName      = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	planFileName    = regexp.MustCompile(`^([a-z][a-z0-9_]*)\.yaml$`)
	// PlanName is what a plan is called: module::plan, or module for init.
	planName = regexp.MustCompile(`^[a-z][a-z0-9_]*(::[a-z][a-z0-9_]*)?$`)
)

// File is one plan file as found in code.
type File struct {
	// Name is the plan's name: module::plan, or module for init.yaml.
	Name   string
	Module string
	// Path is relative to the environment, with slashes.
	Path string
	Data []byte
	// Oversize is set instead of Data when the file is too large to read.
	Oversize bool
}

// Loader reads plan files from the code directory the console deploys.
type Loader struct{ codeDir string }

// NewLoader builds a Loader over codeDir (CONSOLE_CODE_DIR_PATH).
func NewLoader(codeDir string) *Loader { return &Loader{codeDir: codeDir} }

// ValidName reports whether name can be a plan's name.
func ValidName(name string) bool { return planName.MatchString(name) }

// environment resolves an environment's real directory. Environments are
// symlinks to a deploy directory, so every path read afterwards is checked
// against this real root.
func (l *Loader) environment(env string) (string, error) {
	if !environmentName.MatchString(env) {
		return "", ErrEnvironmentNotFound
	}
	if l == nil || l.codeDir == "" {
		return "", ErrNotAvailable
	}
	envs := filepath.Join(l.codeDir, "environments")
	if st, err := os.Stat(envs); err != nil || !st.IsDir() {
		return "", ErrNotAvailable
	}
	real, err := filepath.EvalSymlinks(filepath.Join(envs, env))
	if err != nil {
		return "", ErrEnvironmentNotFound
	}
	if st, err := os.Stat(real); err != nil || !st.IsDir() {
		return "", ErrEnvironmentNotFound
	}
	return real, nil
}

// within reports whether path is root or inside it.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// modulePath reads the environment's modulepath from environment.conf:
// entries relative to the environment, in order. $-variables and absolute
// paths are not followed, so what is read is what the environment ships.
func modulePath(envDir string) []string {
	f, err := os.Open(filepath.Join(envDir, "environment.conf"))
	if err != nil {
		return []string{"modules"}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "modulepath" {
			continue
		}
		var out []string
		for _, e := range strings.Split(value, ":") {
			e = strings.TrimSpace(e)
			if e == "" || strings.HasPrefix(e, "$") || filepath.IsAbs(e) {
				continue
			}
			out = append(out, e)
		}
		return out
	}
	return []string{"modules"}
}

// Files lists the plan files of an environment, one per plan name; where a
// name occurs in more than one module path entry the first wins, as
// Puppet's module lookup does. Sorted by name.
func (l *Loader) Files(env string) ([]File, error) {
	root, err := l.environment(env)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []File
	for _, entry := range modulePath(root) {
		dir, err := filepath.EvalSymlinks(filepath.Join(root, entry))
		if err != nil || !within(root, dir) {
			continue
		}
		mods, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, m := range mods {
			if !moduleName.MatchString(m.Name()) {
				continue
			}
			plansDir, err := filepath.EvalSymlinks(filepath.Join(dir, m.Name(), "plans"))
			if err != nil || !within(root, plansDir) {
				continue
			}
			files, err := os.ReadDir(plansDir)
			if err != nil {
				continue
			}
			for _, pf := range files {
				match := planFileName.FindStringSubmatch(pf.Name())
				if match == nil || !pf.Type().IsRegular() {
					continue
				}
				name := m.Name() + "::" + match[1]
				if match[1] == "init" {
					name = m.Name()
				}
				if seen[name] {
					continue
				}
				seen[name] = true
				if len(out) >= maxPlanFiles {
					return out, fmt.Errorf("more than %d plans: not all are listed", maxPlanFiles)
				}
				file := File{Name: name, Module: m.Name()}
				full := filepath.Join(plansDir, pf.Name())
				if rel, err := filepath.Rel(root, full); err == nil {
					file.Path = filepath.ToSlash(rel)
				}
				data, oversize, err := readBounded(full)
				if err != nil {
					continue
				}
				file.Data, file.Oversize = data, oversize
				out = append(out, file)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// File returns the one plan file named name, or ErrPlanNotFound.
func (l *Loader) File(env, name string) (File, error) {
	if !planName.MatchString(name) {
		return File{}, ErrPlanNotFound
	}
	files, err := l.Files(env)
	if err != nil && len(files) == 0 {
		return File{}, err
	}
	for _, f := range files {
		if f.Name == name {
			return f, nil
		}
	}
	return File{}, ErrPlanNotFound
}

func readBounded(path string) (data []byte, oversize bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, MaxPlanFileBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > MaxPlanFileBytes {
		return nil, true, nil
	}
	return data, false, nil
}

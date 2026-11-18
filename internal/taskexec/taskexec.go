// Package taskexec is the part of running a Puppet task that does not
// depend on where it runs: choosing an implementation, deciding how its
// parameters are passed, reading its result, and bounding its output. The
// node agent runs a task on its own node and the agentless executor runs
// one over SSH; both use this, so a task behaves the same either way.
package taskexec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Output caps. A node's reply travels as one NATS message (1 MB by
// default) and carries stdout twice - raw, and as the result - so each
// stream is bounded well inside that. The agentless executor keeps the
// same bounds so a result is the same size either way.
const (
	MaxStdout = 256 << 10
	MaxStderr = 64 << 10
)

// Implementation is one way to run a task.
type Implementation struct {
	Name         string   `json:"name"`
	Requirements []string `json:"requirements,omitempty"`
	InputMethod  string   `json:"input_method,omitempty"`
	Files        []string `json:"files,omitempty"`
}

// Choose returns the first implementation whose requirements are all among
// features, or an error saying what the node provides and what the task
// offers.
func Choose(impls []Implementation, features []string) (Implementation, error) {
	for _, impl := range impls {
		ok := true
		for _, req := range impl.Requirements {
			if !slices.Contains(features, req) {
				ok = false
				break
			}
		}
		if ok {
			return impl, nil
		}
	}
	var offered []string
	for _, impl := range impls {
		offered = append(offered, fmt.Sprintf("%s (requires %s)", impl.Name, strings.Join(impl.Requirements, ", ")))
	}
	return Implementation{}, fmt.Errorf("no implementation of this task suits this node (it provides %s; the task offers %s)",
		strings.Join(features, ", "), strings.Join(offered, "; "))
}

// InputMethodFor is the input method an implementation runs with: its
// own, else the task's (taskMethod), else the default - powershell for a
// PowerShell script, as Bolt does, and both for anything else.
func InputMethodFor(taskMethod string, impl Implementation) string {
	switch {
	case impl.InputMethod != "":
		return impl.InputMethod
	case taskMethod != "":
		return taskMethod
	case strings.EqualFold(filepath.Ext(impl.Name), ".ps1"):
		return "powershell"
	default:
		return "both"
	}
}

// Input builds what a task is given: its parameters (plus _task, and
// _installdir when it has supporting files) on stdin as JSON and/or as
// PT_ environment variables, per method. The powershell method sends
// JSON on stdin too - the wrapper turns it into named parameters.
func Input(method string, params map[string]any, taskName, installDir string) (stdin []byte, env []string, err error) {
	all := make(map[string]any, len(params)+2)
	for k, v := range params {
		all[k] = v
	}
	all["_task"] = taskName
	if installDir != "" {
		all["_installdir"] = installDir
	}

	if method == "stdin" || method == "both" || method == "powershell" {
		if stdin, err = json.Marshal(all); err != nil {
			return nil, nil, fmt.Errorf("encode task input: %w", err)
		}
	}
	if method == "environment" || method == "both" {
		names := make([]string, 0, len(all))
		for k := range all {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			value, ok := all[k].(string)
			if !ok {
				b, err := json.Marshal(all[k])
				if err != nil {
					return nil, nil, fmt.Errorf("encode %s: %w", k, err)
				}
				value = string(b)
			}
			env = append(env, "PT_"+k+"="+value)
		}
	}
	return stdin, env, nil
}

// ParseOutput is a task's structured result: its stdout when that is a
// JSON object, and otherwise the text under _output, as Bolt and PE
// report it.
func ParseOutput(stdout string) json.RawMessage {
	trimmed := bytes.TrimSpace([]byte(stdout))
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var obj map[string]json.RawMessage
		if json.Unmarshal(trimmed, &obj) == nil {
			return json.RawMessage(trimmed)
		}
	}
	out, _ := json.Marshal(map[string]string{"_output": stdout})
	return out
}

// Capped keeps the first Limit bytes written to it and counts the rest,
// so a task printing without end cannot exhaust memory.
type Capped struct {
	Limit int
	buf   bytes.Buffer
	total int
}

// NewCapped returns a Capped keeping limit bytes.
func NewCapped(limit int) *Capped { return &Capped{Limit: limit} }

// Write implements io.Writer.
func (b *Capped) Write(p []byte) (int, error) {
	b.total += len(p)
	if room := b.Limit - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

// String is what was kept, saying so at the end when that is not all.
func (b *Capped) String() string {
	if b.total <= b.Limit {
		return b.buf.String()
	}
	return b.buf.String() + fmt.Sprintf("\n[output truncated: %d of %d bytes shown]", b.Limit, b.total)
}

// SafeRelPath reports whether p is a relative, slash-separated path that
// stays inside whatever directory it is joined to: no absolute paths, no
// "..", no empty or "." components, no backslashes (a separator on
// Windows). File names arrive from the console, so this is what keeps a
// task from being written outside its install directory.
func SafeRelPath(p string) bool {
	if p == "" || strings.Contains(p, "\\") || strings.Contains(p, ":") || path.IsAbs(p) {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

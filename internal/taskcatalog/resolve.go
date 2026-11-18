package taskcatalog

import (
	"fmt"
	"strings"
)

// Resolved is everything a node needs to run a task: the payload of a
// run_task dispatch (design decision 3). Params and TimeoutSeconds are
// filled in by the orchestrator; Resolve sets the rest.
//
// The node chooses the implementation, downloads Files from its own
// Puppet server, and runs nothing unless each file's SHA256 matches.
type Resolved struct {
	Task            string                   `json:"task"`
	Environment     string                   `json:"environment"`
	InputMethod     string                   `json:"input_method,omitempty"`
	Implementations []ResolvedImplementation `json:"implementations"`
	Files           []ResolvedFile           `json:"files"`
	Params          map[string]any           `json:"params"`
	TimeoutSeconds  int                      `json:"timeout_seconds"`
}

// ResolvedImplementation is one way to run the task. Name is a file in
// the task's own directory; Files are the module-relative supporting
// files it needs, with any directory entry already expanded into the
// files under it.
type ResolvedImplementation struct {
	Name         string   `json:"name"`
	Requirements []string `json:"requirements,omitempty"`
	InputMethod  string   `json:"input_method,omitempty"`
	Files        []string `json:"files,omitempty"`
}

// ResolvedFile is one downloadable file. Path and Params are the
// openvoxserver request that serves it, relative to whichever Puppet
// server the node uses.
type ResolvedFile struct {
	Filename string            `json:"filename"`
	Path     string            `json:"path"`
	Params   map[string]string `json:"params,omitempty"`
	SHA256   string            `json:"sha256"`
	Size     int64             `json:"size"`
}

// Resolve turns a task fetched by Client.Task into a dispatch payload,
// failing when its metadata names a file openvoxserver did not list -
// broken metadata that would otherwise surface on every node instead of
// once, here.
func Resolve(task Task) (Resolved, error) {
	r := Resolved{
		Task:        task.Name,
		Environment: task.Environment,
		InputMethod: task.Metadata.InputMethod,
	}
	byName := map[string]bool{}
	for _, f := range task.Files {
		byName[f.Filename] = true
		r.Files = append(r.Files, ResolvedFile{
			Filename: f.Filename,
			Path:     f.URI.Path,
			Params:   f.URI.Params,
			SHA256:   f.SHA256,
			Size:     f.SizeBytes,
		})
	}

	impls := task.Metadata.Implementations
	if len(impls) == 0 {
		// No implementations list: the task is its one file in the
		// task directory.
		var own []string
		for _, f := range task.Files {
			if !strings.Contains(f.Filename, "/") {
				own = append(own, f.Filename)
			}
		}
		if len(own) != 1 {
			return Resolved{}, fmt.Errorf("task %s has %d executables and no implementations list; want exactly one", task.Name, len(own))
		}
		impls = []Implementation{{Name: own[0]}}
	}

	for _, impl := range impls {
		if !byName[impl.Name] {
			return Resolved{}, fmt.Errorf("task %s: implementation %s is not among the task's files", task.Name, impl.Name)
		}
		var files []string
		for _, entry := range append(append([]string{}, task.Metadata.Files...), impl.Files...) {
			expanded, err := expandSupportingFile(task, entry)
			if err != nil {
				return Resolved{}, err
			}
			files = append(files, expanded...)
		}
		r.Implementations = append(r.Implementations, ResolvedImplementation{
			Name:         impl.Name,
			Requirements: impl.Requirements,
			InputMethod:  impl.InputMethod,
			Files:        dedupe(files),
		})
	}
	return r, nil
}

// expandSupportingFile maps one metadata "files" entry to the listed
// files it covers: itself, or everything under it when it names a
// directory (an entry ending in "/").
func expandSupportingFile(task Task, entry string) ([]string, error) {
	var out []string
	for _, f := range task.Files {
		if f.Filename == entry || strings.HasSuffix(entry, "/") && strings.HasPrefix(f.Filename, entry) {
			out = append(out, f.Filename)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("task %s: supporting file %s is not among the task's files", task.Name, entry)
	}
	return out, nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

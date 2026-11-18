package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Patch scopes: which pending updates a patch run applies.
const (
	PatchScopeSecurity = "security"
	PatchScopeAll      = "all"
	PatchScopePackages = "packages"
)

// Limits and defaults of a patch run's options.
const (
	DefaultPatchBatchSize = 10
	MaxPatchBatchSize     = 100
	DefaultStopAfter      = 3
	MaxStopAfter          = 1000
	MaxPatchPackages      = 200
)

// PatchOptions are a patch run's options as stored: every field is
// explicit, so what a run did is never a matter of defaults that have
// since changed.
type PatchOptions struct {
	// Scope is which updates to apply: security, all, or packages.
	Scope string `json:"scope"`
	// Packages are the packages to upgrade when Scope is packages.
	Packages []string `json:"packages,omitempty"`
	// Reboot restarts a node that needs it once it has been patched.
	Reboot bool `json:"reboot,omitempty"`
	// DryRun reports what would be upgraded and installs nothing.
	DryRun bool `json:"dryRun,omitempty"`
	// BatchSize is how many nodes are patched at once.
	BatchSize int `json:"batchSize"`
	// StopAfterFailures is how many failed nodes end the run, so that no
	// further batch starts. Zero never stops.
	StopAfterFailures int `json:"stopAfterFailures"`
}

// InvalidPatchOptionsError is a patch request refused for its options.
// Field names the option, as the request spells it.
type InvalidPatchOptionsError struct{ Field, Reason string }

func (e *InvalidPatchOptionsError) Error() string { return e.Reason }

// packageName is what a package manager accepts as a package name; it
// is deliberately narrower than every name that exists, so that a value
// can never be read as an option or a shell word.
var packageName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+:_~-]*$`)

// decodePatchOptions reads a request's options, refusing any it does not
// know, and fills in the defaults.
func decodePatchOptions(raw json.RawMessage) (PatchOptions, error) {
	var in struct {
		Scope             string   `json:"scope"`
		Packages          []string `json:"packages"`
		Reboot            bool     `json:"reboot"`
		DryRun            bool     `json:"dryRun"`
		BatchSize         *int     `json:"batchSize"`
		StopAfterFailures *int     `json:"stopAfterFailures"`
	}
	if len(raw) > 0 && string(raw) != "null" {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			return PatchOptions{}, &InvalidPatchOptionsError{Reason: "invalid patch options: " + err.Error()}
		}
	}
	o := PatchOptions{Scope: in.Scope, Reboot: in.Reboot, DryRun: in.DryRun, BatchSize: DefaultPatchBatchSize, StopAfterFailures: DefaultStopAfter}
	if o.Scope == "" {
		o.Scope = PatchScopeSecurity
	}
	switch o.Scope {
	case PatchScopeSecurity, PatchScopeAll, PatchScopePackages:
	default:
		return PatchOptions{}, &InvalidPatchOptionsError{Field: "scope", Reason: fmt.Sprintf("scope must be %q, %q or %q", PatchScopeSecurity, PatchScopeAll, PatchScopePackages)}
	}
	seen := map[string]bool{}
	for _, p := range in.Packages {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !packageName.MatchString(p) {
			return PatchOptions{}, &InvalidPatchOptionsError{Field: "packages", Reason: fmt.Sprintf("%q is not a valid package name", p)}
		}
		if !seen[p] {
			seen[p] = true
			o.Packages = append(o.Packages, p)
		}
	}
	sort.Strings(o.Packages)
	switch {
	case o.Scope == PatchScopePackages && len(o.Packages) == 0:
		return PatchOptions{}, &InvalidPatchOptionsError{Field: "packages", Reason: "name at least one package, or choose another scope"}
	case o.Scope != PatchScopePackages && len(o.Packages) > 0:
		return PatchOptions{}, &InvalidPatchOptionsError{Field: "packages", Reason: "packages are only used when the scope is packages"}
	case len(o.Packages) > MaxPatchPackages:
		return PatchOptions{}, &InvalidPatchOptionsError{Field: "packages", Reason: fmt.Sprintf("at most %d packages may be named", MaxPatchPackages)}
	}
	if in.BatchSize != nil {
		if *in.BatchSize < 1 || *in.BatchSize > MaxPatchBatchSize {
			return PatchOptions{}, &InvalidPatchOptionsError{Field: "batchSize", Reason: fmt.Sprintf("the batch size must be between 1 and %d", MaxPatchBatchSize)}
		}
		o.BatchSize = *in.BatchSize
	}
	if in.StopAfterFailures != nil {
		if *in.StopAfterFailures < 0 || *in.StopAfterFailures > MaxStopAfter {
			return PatchOptions{}, &InvalidPatchOptionsError{Field: "stopAfterFailures", Reason: fmt.Sprintf("the stop threshold must be between 0 and %d", MaxStopAfter)}
		}
		o.StopAfterFailures = *in.StopAfterFailures
	}
	if o.DryRun && o.Reboot {
		return PatchOptions{}, &InvalidPatchOptionsError{Field: "reboot", Reason: "a dry run never reboots"}
	}
	return o, nil
}

// describe names the options for the activity log.
func (o PatchOptions) describe() string {
	parts := []string{o.Scope + " updates"}
	if o.Scope == PatchScopePackages {
		parts = []string{"packages " + strings.Join(o.Packages, ", ")}
	}
	if o.DryRun {
		parts = append(parts, "dry run")
	}
	if o.Reboot {
		parts = append(parts, "reboot if required")
	}
	parts = append(parts, fmt.Sprintf("batches of %d", o.BatchSize))
	if o.StopAfterFailures > 0 {
		parts = append(parts, fmt.Sprintf("stop after %d failed", o.StopAfterFailures))
	}
	return strings.Join(parts, ", ")
}

// setPatchOptions derives PatchOptions from a patch job's stored params.
func (j *Job) setPatchOptions() {
	if j.Kind != JobKindPatch || len(j.Params) == 0 {
		return
	}
	var o PatchOptions
	if err := json.Unmarshal(j.Params, &o); err == nil {
		j.PatchOptions = &o
	}
}

// NodesBeingPatchedError is a patch request refused because some of its
// nodes are already being patched.
type NodesBeingPatchedError struct{ Nodes []string }

func (e *NodesBeingPatchedError) Error() string {
	return "already being patched by another run: " + strings.Join(e.Nodes, ", ")
}

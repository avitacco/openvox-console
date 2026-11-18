package codeplans

// TargetResult is one node's outcome of a task step.
type TargetResult struct {
	Target string
	OK     bool
	// Value is the task's structured result (what it printed as JSON, or
	// its text under _output).
	Value any
	Error string
}

// StepResult is the value a step's name refers to in later steps:
//
//	ok             every target succeeded
//	results        each target's {target, ok, value, error}, in target order
//	targets        every target the step ran on
//	ok_targets     those that succeeded
//	error_targets  those that failed
//	value          the one target's value, for a step on a single target
func StepResult(results []TargetResult) map[string]any {
	ok := true
	rs := make([]any, 0, len(results))
	targets, okTargets, errTargets := []any{}, []any{}, []any{}
	for _, r := range results {
		entry := map[string]any{"target": r.Target, "ok": r.OK, "value": r.Value}
		if r.Error != "" {
			entry["error"] = r.Error
		}
		rs = append(rs, entry)
		targets = append(targets, r.Target)
		if r.OK {
			okTargets = append(okTargets, r.Target)
		} else {
			errTargets = append(errTargets, r.Target)
			ok = false
		}
	}
	out := map[string]any{"ok": ok, "results": rs, "targets": targets, "ok_targets": okTargets, "error_targets": errTargets}
	if len(results) == 1 {
		out["value"] = results[0].Value
	}
	return out
}

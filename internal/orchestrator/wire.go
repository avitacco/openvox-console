package orchestrator

import "encoding/json"

// Module/action identifiers - see internal/nodeagent's handler for the
// node-side counterpart; both sides independently encode/decode this
// shared wire shape (see design.md - this project controls both ends,
// so this is our own consistent scheme). The module name groups related
// actions; it is a purely internal label the node side decodes but
// never matches on, so it carries no compatibility constraint.
const (
	moduleNamePuppet = "puppet"
	actionRun        = "run"
	actionRunTask    = "run_task"
	// actionPackageInventoryStatus and actionPackageInventorySet are new
	// as of add-package-inventory-toggle - still the same module, since
	// a state change still triggers a real Puppet run, recognizably the
	// same kind of operation as actionRun/actionRunTask rather than a
	// new category the wire scheme needs to model separately (see
	// design.md there).
	actionPackageInventoryStatus = "package_inventory_status"
	actionPackageInventorySet    = "package_inventory_set"
)

// requestData is a dispatch request's payload, sent over a node's
// dispatch subject (see internal/nodetransport/subjects.go) - mirrors
// internal/nodeagent's requestData (the two packages don't share the Go
// type since the wire contract, not the type, is what's shared). Unlike
// Unlike a protocol-level envelope, there's no transaction/request ID
// field - NATS's own reply-subject already correlates a response to its
// request, so nothing else needs to.
type requestData struct {
	Module string          `json:"module"`
	Action string          `json:"action"`
	Params json.RawMessage `json:"params,omitempty"`
}

// taskParams is requestData.Params' shape for an actionRunTask request.
type taskParams struct {
	Task   string          `json:"task"`
	Params json.RawMessage `json:"params,omitempty"`
}

// packageInventorySetParams is requestData.Params' shape for an
// actionPackageInventorySet request. Not needed for
// actionPackageInventoryStatus, which takes no params.
type packageInventorySetParams struct {
	Enabled bool `json:"enabled"`
}

// wireResponse is what a node-agent replies with on a dispatch's reply
// subject - Result on success, Error on an execution failure or a busy
// rejection (see specs/node-agent's "Single in-flight request per
// client" requirement), never both.
type wireResponse struct {
	Result *responseData `json:"result,omitempty"`
	Error  string        `json:"error,omitempty"`
}

// responseData is a completed run/task's payload. PackageInventory is
// only set on a package-inventory status/set response - Output is its
// zero value (and PackageInventory.Ran false) on a status request (no
// run occurs) and on a set request that changed nothing (already in the
// requested state), since neither triggers a Puppet run. Ran is a
// separate explicit field rather than inferring "no run happened" from
// Output being the zero value, since a real run can legitimately
// produce empty stdout/stderr and exit 0 (an unchanged, successful
// run) - indistinguishable from "never ran" without it.
type responseData struct {
	Output           outputData                `json:"output"`
	PackageInventory *packageInventoryResponse `json:"package_inventory,omitempty"`
}

// packageInventoryResponse reports the node's package-inventory
// reporting state after a status or set request, and whether that
// request actually triggered a Puppet run (see responseData's doc
// comment for why this can't be inferred from Output alone).
type packageInventoryResponse struct {
	Enabled bool `json:"enabled"`
	Ran     bool `json:"ran"`
}

type outputData struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitcode"`
}

// puppetRunFailed reports whether a Puppet run's exit code indicates
// failure. Under --detailed-exitcodes, the code is a bitmask: bit 0
// (value 1) means the run itself errored (e.g. failed to compile or
// apply the catalog at all), bit 1 (value 2) means changes were
// applied, bit 2 (value 4) means resource failures occurred during the
// run. Only 0 (no changes) and 2 (changes applied, no errors) are
// success; every other value - including a bare 1, which the previous
// version of this check missed - has the failure bit set somewhere.
func puppetRunFailed(exitCode int) bool {
	return exitCode != 0 && exitCode != 2
}

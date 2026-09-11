package packageinventory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// dispatchTimeout bounds how long a status/set request waits for a
// node-agent's response - short, since neither action is expected to
// take long (a status check is instant; a set action runs one Puppet
// run, which this project's other dispatched actions already treat as
// fast relative to orchestrator's own much longer dispatchTimeout for
// arbitrary runs - see internal/orchestrator/dispatcher.go).
const dispatchTimeout = 2 * time.Minute

// transport is the subset of *nodetransport.Server this package needs -
// structurally identical to internal/orchestrator.Transport (the same
// *nodetransport.Server value satisfies both), so handlers can be
// tested against a fake without a live NATS transport.
type transport interface {
	Dispatch(ctx context.Context, certname string, payload []byte, timeout time.Duration) ([]byte, error)
}

// Wire types mirror internal/orchestrator's and internal/nodeagent's own
// copies - the JSON contract is shared, not the Go type (see design.md
// in add-package-inventory-toggle).
const (
	moduleNamePuppet             = "puppet"
	actionPackageInventoryStatus = "package_inventory_status"
	actionPackageInventorySet    = "package_inventory_set"
)

type dispatchRequestData struct {
	Module string          `json:"module"`
	Action string          `json:"action"`
	Params json.RawMessage `json:"params,omitempty"`
}

type packageInventorySetParams struct {
	Enabled bool `json:"enabled"`
}

type dispatchWireResponse struct {
	Result *dispatchResult `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type dispatchResult struct {
	Output           dispatchOutput          `json:"output"`
	PackageInventory *packageInventoryResult `json:"package_inventory,omitempty"`
}

type dispatchOutput struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitcode"`
}

type packageInventoryResult struct {
	Enabled bool `json:"enabled"`
	Ran     bool `json:"ran"`
}

// dispatchPackageInventory builds the requestData for action/params,
// dispatches it to certname over transport, and decodes the response -
// the one place both endpoints share, since the request/response
// envelope handling is otherwise identical between status and set.
func dispatchPackageInventory(ctx context.Context, t transport, certname, action string, params any) (*dispatchResult, error) {
	var paramsRaw json.RawMessage
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("encode dispatch params: %w", err)
		}
		paramsRaw = raw
	}

	payload, err := json.Marshal(dispatchRequestData{Module: moduleNamePuppet, Action: action, Params: paramsRaw})
	if err != nil {
		return nil, fmt.Errorf("encode dispatch request: %w", err)
	}

	respBytes, err := t.Dispatch(ctx, certname, payload, dispatchTimeout)
	if err != nil {
		return nil, err
	}

	var resp dispatchWireResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("decode dispatch response: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	if resp.Result == nil || resp.Result.PackageInventory == nil {
		return nil, fmt.Errorf("node-agent response missing package_inventory result")
	}
	return resp.Result, nil
}

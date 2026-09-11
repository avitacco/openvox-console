## Why

Package-inventory reporting currently ships as static content baked into
every node-agent-client `.deb`/`.rpm` (`add-native-agent-packaging`) -
every node that installs node-agent-client gets it, unconditionally,
with no way to turn it off for a specific node, and no way to turn it
on for a node after the fact without reinstalling the package. An
operator should be able to control this per node, from the console,
and see the effect immediately rather than waiting for that node's next
scheduled Puppet run.

## What Changes

- Add an operator-facing control (node detail page) to enable or
  disable package-inventory reporting for a specific node.
- Enabling or disabling immediately dispatches a command to that node
  over the existing NATS-based node transport (the same mechanism
  orchestrator runs already use) that writes or removes the
  `/opt/puppetlabs/facter/facts.d/package_inventory.sh` external fact
  and then runs `puppet agent -t` right away, so the change (or its
  removal) is reflected in openvoxdb immediately, not on the next
  scheduled run.
- The control is disabled in the UI whenever the node is not currently
  connected - toggling requires a live dispatch, and a disconnected
  node can't receive one (no queued/deferred toggle is introduced).
- **BREAKING** (of `add-native-agent-packaging`'s current behavior):
  `cmd/build-agent-packages` stops bundling the package-inventory fact
  script as static `.deb`/`.rpm` content. node-agent-client itself now
  owns writing and removing that file, embedding the same two
  per-package-manager-family fact script variants
  (`dpkg-query`/`rpm -qa`) that used to live in
  `internal/agentdist/pkgassets/`.
- Toggling is audit-logged under the existing `nodes/inventory` audit
  category (`internal/auditlog`) as a mutating action - no change to
  `audit-log-emission`'s own spec, since that capability's audit-level
  framework is already generic across categories and doesn't enumerate
  individual actions.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `node-agent`: node-agent-client gains a new dispatched request type -
  a package-inventory status check and a set (enable/disable) action -
  alongside its existing run and task request handling, subject to the
  same single-in-flight-request constraint.
- `package-inventory`: adds the operator-facing toggle itself (read the
  current per-node state, enable/disable it) as new endpoints, on top
  of the existing node package list and fleet-wide search.

## Impact

- `cmd/node-agent-client/`, `internal/nodeagent/` - new dispatch action
  handling (write/remove the fact file, run `puppet agent -t`, report
  status), and the fact script assets move here from
  `internal/agentdist/pkgassets/`.
- `internal/agentdist/` (`cmd/build-agent-packages`, `pkgassets/`) - the
  package-inventory fact script stops being packaged content.
- `internal/packageinventory/` - new endpoints for reading and setting
  the per-node toggle, dispatching through `internal/nodetransport`
  (directly, not through `internal/orchestrator`'s job-tracking
  dispatcher - this is a synchronous status/set action, not a trackable
  job with history).
- `internal/nodeconnectivity` - its existing live connectivity signal
  gates the UI control's enabled/disabled state; no new connectivity
  mechanism needed.
- `internal/auditlog` - a new mutating-action call site under the
  existing `nodes/inventory` category.
- `frontend/` (node detail page) - the new toggle control.
- No change to `internal/orchestrator` (only reused as a precedent for
  how `internal/nodetransport.Dispatch` is already used) or to
  `openvoxdb`/`_puppet_inventory_1` fact-consumption mechanics (already
  solved, unaffected by how the fact file gets onto disk).

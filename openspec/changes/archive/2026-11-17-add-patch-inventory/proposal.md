# Proposal

## Why

The console shows which packages a node has installed, but not which updates
it is missing - the first question anyone asks before patching. Week 11's patch
runs need a trustworthy picture of pending updates to act on, so visibility
comes first and on its own: it is useful by itself and de-risks the runs.

## What Changes

- The node agent gains a read-only `patch_scan` action: it asks the node's
  package manager (apt, or dnf/yum) which updates are pending, which of them
  are security updates where the manager says so, and whether a reboot is
  already required.
- The console stores the latest scan per node (replaced whole on each scan)
  and serves it: a fleet view, a per-node detail, and a package search across
  the fleet's pending updates.
- A user with `orchestrator:run` can scan one node or a node group now. The
  scan goes through the same node-scope checks as any run.
- The console scans every connected node on an interval (default 24 hours,
  changeable or switchable off by a `nodes:manage` user), under a lease so
  one instance scans at a time.
- Scans are audited. Reads need `nodes:read` and are filtered to the caller's
  node scope. No new permission is introduced.
- Console pages, translations in all 15 locales, and site guide updates.
- **Not included:** applying patches, patch windows or blackouts, reboots,
  Windows and macOS, zypper, and CVE correlation (the vulnerability engine
  stays separate).

## Capabilities

### New Capabilities
- `patch-inventory`: what updates each node is missing - collection by the node
  agent, per-node snapshot, fleet and per-node views, on-demand and periodic
  scans, scope and audit rules.

### Modified Capabilities

None. The node-agent dispatch action is described in `patch-inventory`.

## Impact

- `internal/nodeagent` (new action), new `internal/patchinventory` package
  (store, handlers, scheduler), `internal/app` wiring, migration 000037.
- Frontend: a fleet Patching page, a patch panel on the node page, a scan
  control, and the interval setting.
- Nodes need an agent built from this version to be scanned; older agents
  answer "unsupported action" and are shown as not yet able to report.
- `marketing/guides/` and `operations.md` updated.

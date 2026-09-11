## Why

Nodes that no longer exist (decommissioned test instances, retired
VMs) stay on the dashboard forever - openvoxdb never forgets a node on
its own, and this console has no way to tell it to. An operator needs
a way to remove a node from the console, with the underlying openvoxdb
data actually going away rather than just being hidden by console-side
filtering.

## What Changes

- Add a "Delete" action on the node detail/list view that, after a
  confirmation modal explaining the consequence, deactivates the node
  in openvoxdb (`POST /pdb/cmd/v1` `deactivate node`, confirmed working
  live this session with a current, non-stale `producer_timestamp` -
  an old one is silently ignored by openvoxdb as a stale command).
- A deactivated node stops appearing in the node list automatically -
  confirmed live this session that openvoxdb's `nodes` query
  unconditionally excludes deactivated/expired nodes itself, with no
  documented override (an explicit `deactivated is not null` filter
  clause still returns nothing), so no console-side filtering change
  is needed for this.
- No "show inactive" option: confirmed live that openvoxdb provides no
  way to list deactivated nodes back out through its query API at all
  (only a direct single-node lookup by exact certname still shows one).
  Building a list view for them would mean the console inventing and
  maintaining its own separate record of "nodes I've deleted," which
  was deliberately ruled out - a deleted node is simply gone from the
  console until an operator happens to know its certname, or forever,
  whichever comes first.
- Deleting a node also cleans its CA certificate record, not just its
  openvoxdb inventory record - a real gap found live during
  implementation: the node list is a union of openvoxdb's inventory and
  the separate connectivity/CA registry
  (`frontend/src/nodes.js`'s `allCertnames()`), so deactivating a node
  in openvoxdb alone left it still visible via its lingering cert entry.
  Confirmed with the user: one "Delete" action does both, rather than
  requiring a separate "Clean certificate" action to actually get a
  node off the page.
- Actual removal of the node's historical data (facts, catalogs,
  reports) is left to openvoxdb's own background garbage collection
  (its `node-purge-ttl`-driven sweep of already-deactivated nodes) -
  this console does not force an immediate purge. Confirmed with the
  user: deactivate-and-hide-now, erase-on-openvoxdb's-own-schedule is
  the intended behavior, not a fallback taken because immediate purge
  couldn't be verified.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `inventory`: adds the delete (deactivate) action. The node list's
  exclusion of deactivated nodes is existing openvoxdb behavior, not a
  new requirement this capability's spec needs to state.
- `openvoxdb-client`: gains command submission (not just PQL queries)
  as a second responsibility - specifically, submitting a `deactivate
  node` command - and a single-node lookup alongside the existing
  collection query, since the two behave differently with respect to
  deactivated nodes (see design.md). The capability's Purpose statement
  ("the console's only path for *querying* openvoxdb") will need a
  small wording update when this archives to reflect that it also
  submits commands now, not just queries.

## Impact

- `internal/openvoxdb` - new command-submission method alongside the
  existing query-only `Client`, and a new single-node lookup method
  (`NodeByCertname`) distinct from the existing `Nodes()` collection
  query.
- `internal/inventory` - new `DELETE /api/v1/nodes/{name}` endpoint,
  using `NodeByCertname` to distinguish an unknown certname from an
  already-deactivated one (`Nodes()` can't tell them apart - see
  design.md), and also cleaning the node's CA certificate record
  (reusing the same `certstatus.Client.Clean` operation
  `internal/nodeconnectivity`'s existing "Clean certificate" action
  already uses) so it actually disappears from the page.
- `frontend/` (node list and detail pages) - the delete button and its
  confirmation modal, via the existing `confirmDialog` helper already
  used for the cert revoke/clean actions.
- RBAC: a new `nodes:manage` permission gates the delete endpoint -
  not the existing `nodes:certs:manage`, since that's specifically
  scoped to certificate lifecycle (sign/revoke/clean) as an operator-
  invoked action in its own right, a different concern from node
  deletion even though deletion now also cleans a cert as a
  side effect.
- No change to the actual `_puppet_inventory_1`/package-inventory
  mechanics, orchestrator, or node-transport - this is purely about
  the node's openvoxdb inventory record and its visibility.

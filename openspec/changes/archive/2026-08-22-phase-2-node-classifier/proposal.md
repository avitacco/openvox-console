## Why

Nothing yet tells openvox-server what to apply to a node. Phase 2 adds node
group management and classification, exposed through the same External
Node Classifier (ENC) mechanism any open-source Puppet Server install
uses (`node_terminus = exec`), so openvox-server gets its classification
from this console without needing PE-specific tooling. Internally, this is
also the chance to redesign Puppet Enterprise's inheritance-plus-rules-
plus-pinning group model - a known source of user confusion - into
something clearer, while still producing correct output against the same
external contract.

## What Changes

- Add node group CRUD: each group has classes (optionally parameterized),
  top-scope parameters, an environment, a match rule (fact-based) and/or
  explicit pinned nodes, and an explicit numeric priority.
- Add classification resolution: given a node, determine every matching
  group and merge their classes/parameters/environment into one result,
  with conflicts resolved by explicit group priority (flat, not PE's
  implicit parent/child tree depth - see design.md).
- Add the ENC HTTP endpoint: `GET /api/v1/enc/{certname}`, returning JSON
  classification data.
- Add a small bridge script (`external_nodes` target) that openvox-server's
  `node_terminus = exec` invokes: it calls the ENC endpoint and writes the
  YAML document Puppet's exec terminus contract requires (verified against
  Puppet's own ENC documentation - classes/parameters/environment keys,
  hash-form classes for parameterized ones, exit 0 on success and non-zero
  on "node not found").
- Add console UI for group management (list/create/edit/delete groups).

## Capabilities

### New Capabilities
- `classifier`: node group CRUD, classification merge/precedence
  resolution, and the console UI for managing groups.
- `enc-api`: the external-facing ENC HTTP contract that openvox-server's
  classifier terminus (via the bridge script) depends on - kept separate
  from `classifier` because it's the piece that must stay wire-compatible
  regardless of how the internal group/merge model evolves.

### Modified Capabilities
(none - this phase doesn't change persistence, messaging, service-runtime,
web-shell, openvoxdb-client, inventory, or reporting requirements; it adds
new tables to the console's shared schema, which is an implementation
detail of `persistence`, not a change to its documented requirements)

## Impact

- New Go packages for `classifier` and `enc-api`.
- New Postgres tables (node groups and their classes/parameters/rules) in
  the console's existing shared schema - a new migration, not a change to
  the `persistence` capability's contract.
- New console UI pages for group management, on the existing embedded
  web-shell/voxblocks foundation.
- New external artifact: a small bridge script plus `puppet.conf`
  (`node_terminus`, `external_nodes`) configuration for openvox-server,
  documented for the `docker-compose.yml` `openvox` profile.
- External contract: `GET /api/v1/enc/{certname}` is now a real external
  compatibility surface (indirectly, via the bridge script) - changes to
  it after this phase need the same care as any other external contract.

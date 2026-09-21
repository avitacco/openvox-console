## Why

Several console features shipped without a spec delta. The behavior is
in the code, in the UI, and in front of users, but the specs still
describe a thinner console than the one that exists - `inventory`'s node
detail view is still "displays a single node's full fact set", which was
true several iterations ago.

That gap is the problem this change fixes. It adds no behavior; it
records what was built so the specs can be trusted as a description of
the system rather than of its early phases.

## What Changes

Documentation of already-shipped behavior. No code changes.

- **Node detail is tabbed**, with facts, packages, runs and
  vulnerabilities as separate panels rather than one page of everything.

- **Facts render two ways.** A structured view summarises the facts an
  operator actually reads - storage and memory capacity with usage
  shown proportionally, filesystems, network interfaces with every
  address and which interface carries it - and a raw view shows the
  complete fact set as JSON, because the structured view is deliberately
  a subset.

- **The node list supports selecting nodes and running Puppet on them**
  in one action, rather than visiting each node in turn.

- **Packages are browsable as a catalogue**: a paginated list filtered
  by name, version, provider and node group, instead of only the
  single-package search that the spec describes today.

- **Group lists show how many nodes match each group**, so a group's
  reach is visible without opening it.

- **Activity entries identify who acted**, not just what happened.

## Capabilities

### New Capabilities

None. Every behavior here belongs to a capability that already exists.

### Modified Capabilities

- `inventory`: node detail view gains its tabbed structure and the two
  fact presentations; node list view gains multi-select and running
  Puppet against the selection.
- `package-inventory`: adds catalogue browsing alongside the existing
  single-package search.
- `classifier`: adds per-group node counts in group listings.
- `activity`: history identifies the actor behind each entry.

## Impact

No implementation impact - this change describes code that is already
merged and running:

- `frontend/src/node.js`, `frontend/templates/pages/node.tmpl`
- `frontend/src/nodes.js`, `frontend/templates/pages/nodes.tmpl`
- `frontend/src/packages.js`, `internal/packageinventory/handlers.go`
- `internal/groupnodes/handlers.go`
- `frontend/src/index.js`, `frontend/src/activity.js`

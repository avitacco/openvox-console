## Context

openvoxdb's PQL schema (confirmed live against this project's dev instance,
v8.15.0) exposes two package-related query entities:

- `package_inventory { certname, package_name, provider, version }` - one
  row per package per node.
- `packages { package_name, provider, version }` - the deduplicated catalog
  of packages across the whole fleet, with no certname.

Both currently return `[]` in this dev environment (no real data has ever
been observed) - see proposal.md's documented-risk note.

`internal/openvoxdb` already follows a consistent pattern for this kind of
addition: a typed `Client` method per query shape (`Nodes`, `Facts`,
`FactCertnames`, `Reports`, `Events`), each building a PQL string via
`pqlString()` and decoding into a typed slice. See `queries.go`.

This project maps most capability specs onto their own `internal/<name>`
Go package (`nodeconnectivity`, `infracert`, `groupnodes`, `certstatus`,
`auditlog`, ...) even where the underlying data is node-centric like
`internal/inventory`'s. `package-inventory` follows that pattern rather
than growing `internal/inventory` further, since it's a distinct query
surface (its own openvoxdb entity, its own permission-gated endpoints)
that only happens to also be keyed by certname.

## Goals / Non-Goals

**Goals:**
- Query methods for both node-level package lists and fleet-wide search
  by package name (optionally + version).
- Endpoints and UI that behave correctly whether openvoxdb returns real
  data or an honestly-empty result, without guessing at data that isn't
  there (matches this project's established convention - see
  `add-dashboard-fleet-status-stats`'s `corrective_change` handling).

**Non-Goals:**
- Populating package-inventory data on managed nodes (agent-side
  configuration, e.g. `package_inventory_enabled`, or a control-repo
  module) - out of scope; this change only reads whatever openvoxdb has.
- Using the `packages` (fleet-wide distinct catalog, no certname) entity -
  not needed for either requirement in scope: the node package list
  filters `package_inventory` by `certname`, and the fleet-wide search
  filters the same entity by `package_name`/`version`, which already
  returns the matching certnames directly. `packages` would only add value
  as a package-name autocomplete source, deferred to a future change if
  wanted.
- Package version comparison/ordering (e.g. "nodes below version X") -
  the spec's fleet-wide search is an exact-version match only.

## Decisions

**New `internal/packageinventory` package**, mirroring `internal/inventory`'s
and `internal/nodeconnectivity`'s existing shape: a narrow interface over
the openvoxdb client (`NodePackages(ctx, certname) ([]Package, error)`,
`SearchPackages(ctx, name string, version *string) ([]PackageResult, error)`),
`Handlers` wrapping it, registered in `cmd/console/main.go` the same way
the other inventory-adjacent capabilities are. Alternative considered:
folding this into `internal/inventory` directly - rejected for the reason
in Context above (own query surface, own spec, own endpoints - consistent
with how `node-connectivity`/`node-certificate-management` already stayed
separate from `inventory` despite also being node-centric).

**Both new `internal/openvoxdb` query methods use the single
`package_inventory` entity**, per the Non-Goals note above:
```go
func (c *Client) NodePackages(ctx context.Context, certname string) ([]Package, error)
// PQL: package_inventory { certname = <certname> }

func (c *Client) SearchPackages(ctx context.Context, name string, version *string) ([]Package, error)
// PQL: package_inventory { package_name = <name> [and version = <version>] }
```
`Package` carries `Certname`, `PackageName`, `Provider`, `Version` -
`NodePackages` and `SearchPackages` share the same result type since both
query the same entity, just filtered differently; each endpoint's handler
picks the fields relevant to its response shape.

**Two endpoints**: `GET /api/v1/nodes/{certname}/packages` (node package
list) and `GET /api/v1/packages?name=<name>&version=<version>` (fleet-wide
search, `version` optional) - both under `nodes:read`, matching the spec.

**Frontend**: a "Packages" section added to the existing node detail page
(`node.html`/`node.js`), rendered the same way the existing Facts section
is (a simple table, no new component patterns needed); a new top-level
"Packages" page (`packages.html`/`packages.js`) with a name/version search
form and results table, plus a nav link alongside Nodes/Groups/Jobs.

## Risks / Trade-offs

- **Real package-inventory data may never reach openvoxdb in this dev
  environment**, for the same class of reason `corrective_change` doesn't:
  an agent-side setting or PDB command-version gap this project doesn't
  control. → Mitigation: build and ship the feature against the documented
  query entities regardless (correct today for any environment where the
  data *is* populated); verify live during implementation's tasks whether
  this specific dev environment can produce it end-to-end, and document
  whatever is found (working or blocked) rather than silently shipping an
  always-empty feature with no explanation - same pattern already
  established for `corrective_change`.
- **`package_inventory` rows have no timestamp** (unlike `facts`/`reports`),
  so the UI cannot show "as of" staleness for package data the way it can
  for facts. → Accepted: not a requirement in the spec; note it in the UI
  copy only if it turns out to read as confusing during live verification.

## Open Questions

- Whether this dev environment's `openvoxserver`/agent stack can be made to
  produce real `package_inventory` rows at all (e.g. via an agent
  `puppet.conf` setting), and if so what the setting is - deferred to
  implementation's live-verification tasks, matching how `corrective_change`
  was investigated in `add-dashboard-fleet-status-stats`. This does not
  change the specs, the chosen approach, or the task breakdown: the same
  tasks execute either way, and the finding gets documented as part of
  live verification regardless of the outcome.

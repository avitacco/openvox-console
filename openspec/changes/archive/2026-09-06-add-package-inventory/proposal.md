## Why

Console users have no way to see what software packages are installed on a
managed node, or answer "which nodes have package X installed (and at what
version)" - a routine operational and security need (e.g. auditing fleet-wide
exposure to a CVE in a specific library version) that today requires logging
into each node by hand. openvoxdb (confirmed live against this project's dev
instance, v8.15.0) already exposes this data natively via its
`package_inventory` (per-node: certname, package_name, provider, version) and
`packages` (fleet-wide distinct package/provider/version catalog) PQL query
entities, so this is a read-only extension of the console's existing
query/reporting layer, not new agent or orchestrator work.

## What Changes

- Add package-inventory query methods to `internal/openvoxdb`'s client,
  querying the `package_inventory` and `packages` PQL entities the same way
  existing methods query `nodes`/`facts`/`reports`/`events`.
- New `package-inventory` capability: HTTP endpoints for (a) a single node's
  installed packages and (b) a fleet-wide search for nodes with a given
  package installed (optionally at a given version) - both gated behind the
  same `nodes:read` permission as the existing inventory endpoints.
- Console UI: a "Packages" section on the existing node detail page
  (mirrors the existing Facts section), and a new fleet-wide Packages page
  for searching which nodes have a given package installed.
- **Documented risk, not a behavior change**: this project's `operations.md`
  already found that `openvoxserver` submits reports via a PDB command
  version that silently drops `corrective_change` even though openvoxdb's
  schema supports it. Whether real package-inventory data reaches openvoxdb
  from this same `openvoxserver`/agent stack is unconfirmed - the query
  entities exist and return correctly-shaped empty results today, but no
  data has been observed yet. The client/API/UI are specified to handle an
  honestly-empty result either way; live verification during implementation
  will determine whether this dev environment can produce real end-to-end
  data, and any limitation found will be documented (per this project's
  established pattern for this exact kind of gap), not worked around.

## Capabilities

### New Capabilities
- `package-inventory`: node-level and fleet-wide installed-package
  reporting, sourced from openvoxdb's `package_inventory`/`packages` query
  entities.

### Modified Capabilities
(none - `openvoxdb-client`'s existing "PQL query execution" requirement
already covers any query entity, package inventory included, with no
requirement-level change needed there)

## Impact

- **Code**: `internal/openvoxdb` (new query methods + types), a new internal
  package for the package-inventory HTTP endpoints (or an extension of
  `internal/inventory` - decided in design.md), `frontend/src` (new node
  detail "Packages" section, new fleet-wide packages page + template),
  `cmd/console/main.go` (wiring the new handlers).
- **No new external dependency, no schema/migration change**: openvoxdb
  already stores and serves this data; the console only needs to query it.

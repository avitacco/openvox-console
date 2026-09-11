## Why

The console currently has no way to show real data. Phase 0 proved the
binary/Postgres/NATS/frontend pipeline works, but there's nothing yet
connecting it to openvoxdb - the data warehouse that already holds node
facts, catalog run history, and events for every OpenVox-managed node.
Wiring up inventory and reporting is the lowest-risk, highest-visible-value
next step: it validates the whole console shell against a real openvoxdb
instance before anything else (classification, RBAC, orchestration) is
built on top of it.

## What Changes

- Add a PQL query layer that talks to a real openvoxdb instance over its
  HTTP API (the same openvoxdb wired into `docker-compose.yml`'s `openvox`
  profile).
- Add a node inventory view: list nodes with their facts, status, and last
  check-in time, backed by PQL queries.
- Add a reports and event inspector: browse catalog run history for a node
  and drill into resource-level events within a run.
- Add basic search/filtering over the node inventory and report/event
  lists.

## Capabilities

### New Capabilities
- `openvoxdb-client`: PQL query layer - authenticated HTTP client for
  openvoxdb's query API, translating internal query requests into PQL and
  parsing responses into typed results other capabilities consume.
- `inventory`: node inventory view - list/detail of nodes with their facts,
  status, and last check-in time, with search/filtering, backed by
  `openvoxdb-client`.
- `reporting`: reports and event inspector - catalog run history per node
  and resource-level event drill-down within a run, with search/filtering,
  backed by `openvoxdb-client`.

### Modified Capabilities
(none - openvoxdb is a separate external system; this phase does not change
the console's own persistence, messaging, service-runtime, or web-shell
requirements)

## Impact

- New Go packages consuming `openvoxdb-client`, `inventory`, `reporting`.
- New dependency: an HTTP client to openvoxdb's PQL query API (TLS against
  the OpenVox CA, matching the `openvoxserver`/`openvoxdb` stack already in
  `docker-compose.yml`).
- New console UI pages (node inventory, report/event views), built on the
  existing embedded web-shell and voxblocks component library - no change
  to how the frontend pipeline itself works.
- New configuration: openvoxdb connection settings (URL, TLS material).
- No impact on external contracts - the console is a read-only PQL client
  here, not a producer of any API other systems depend on yet.

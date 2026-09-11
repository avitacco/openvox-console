## Why

Operators enrolling a new node (as just happened live, debugging a real
enrollment) have no way to see, from the console UI, whether a given
node's `node-agent-client` actually has a live connection to the node
transport - that state exists today (`internal/nodetransport.Registry`)
but is only ever surfaced as an aggregate count on `/metrics`, never
per-node, and never through any HTTP API or page. The existing node
inventory ("Dashboard", at `/`) answers "does this node have facts/a
recent report in openvoxdb" - a different question from "can the console
currently dispatch an on-demand run to this node," which is exactly what
someone debugging enrollment needs to see at a glance.

## What Changes

- Add a new HTTP endpoint reporting which nodes currently hold a live
  node transport connection (backed by `internal/nodetransport.Registry`).
- Add a new, separate "Nodes" page (distinct from the existing Dashboard/
  inventory page) listing every node known to openvoxdb alongside its
  live connection status - reusing the existing node-listing API for the
  node universe/search/pagination and joining in connection status
  client-side, rather than duplicating that logic.
- Add a "Nodes" entry to the primary navigation, alongside the existing
  Dashboard and Groups sections.

## Capabilities

### New Capabilities
- `node-connectivity`: reports which nodes currently hold a live node
  transport connection, and provides a dedicated page for browsing every
  known node's identity alongside that status.

### Modified Capabilities
- `web-shell`: "Primary navigation across pages" - the set of top-level
  sections a user can navigate between gains "Nodes" alongside the
  existing "node inventory, node groups".

## Impact

- **Code**: a new small package (or a new file within an existing
  HTTP-facing package - see design.md) exposing
  `internal/nodetransport.Registry`'s connection state via a new,
  authenticated (`nodes:read`, matching `inventory`'s existing gate)
  endpoint; `cmd/console/main.go` wires it to the already-constructed
  `nodeTransport`. A new page template + JS
  (`frontend/templates/pages/nodes.tmpl` / `frontend/src/nodes.js`,
  following `groups.tmpl`/`groups.js`'s existing pattern) and a new
  sidenav entry in `frontend/templates/layout.html.tmpl`.
- **No database migration** - this reads existing in-memory state
  (`nodetransport.Registry`) and the existing openvoxdb-backed node list
  (`internal/inventory`'s existing `GET /api/v1/nodes`), nothing new is
  persisted.
- **No breaking change** - purely additive: a new endpoint, a new page,
  one new nav entry. The existing Dashboard/inventory page and its API
  are untouched.

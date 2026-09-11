## Context

Confirmed by direct investigation, not assumption:

- `internal/inventory/handlers.go` already exposes `GET /api/v1/nodes`
  (paginated, filterable by name/fact, `nodeSummary{certname, status,
  reportTimestamp}`), `GET /api/v1/nodes/summary`, and `GET
  /api/v1/nodes/{name}` - all gated on `nodes:read`, all sourced purely
  from openvoxdb (`queryClient` interface). Nothing here knows about
  `internal/nodetransport` at all.
- `internal/nodetransport.Server.Registry()` returns a `*Registry` with
  `Lookup(certname) bool` and `Len() int`. `cmd/console/main.go`'s only
  use of it today is feeding the aggregate `console_node_agent_connected`
  Prometheus gauge (main.go:282-291) - no per-node HTTP exposure exists
  anywhere.
- `frontend/gen/main.go` is the page manifest: each entry (`Name`,
  `Title`, `Script`, `ShowHeader`, `ActiveNav`) drives which template +
  JS bundle gets built into which `.html` file, and which sidenav item
  lights up. `index.html` (the Dashboard) and `node.html`/`report.html`
  already use `ActiveNav: "nodes"` - that key is taken, so the new page
  needs its own.
- `frontend/templates/layout.html.tmpl`'s sidenav items follow two
  patterns: always-visible (Dashboard, Groups - `{{if eq .ActiveNav
  "x"}} current{{end}}`) and conditionally-hidden-by-default (Jobs,
  Deploys, Activity, Admin - `id="nav-x-link"` + inline `style="display:
  none"` unless active, toggled by JS at runtime for features that may
  not be configured). Node connectivity is always meaningful (it's not
  behind an optional feature flag), so it follows the always-visible
  pattern.
- `groups.tmpl`/`groups.js` is the simplest existing "list page" to
  mirror: a bare `<div id="results">` container the JS fills in, no
  filter UI.

## Goals / Non-Goals

**Goals:**
- Answer "is the console currently able to dispatch to this node" as a
  standalone, at-a-glance page - the question the live enrollment
  debugging session this proposal grew out of needed answered and
  couldn't get from the UI.

**Non-Goals:**
- Real-time/live-updating status (a WebSocket or polling connection) -
  the spec explicitly only requires accuracy on refresh. A future
  enhancement, not blocking this one.
- Replacing or modifying the existing Dashboard/inventory page or its
  API - this is additive only (see proposal.md's Impact).
- Historical connection state (when a node last connected/disconnected,
  connection duration, ...) - `nodetransport.Registry` doesn't track
  this today and adding it is out of scope; this change only surfaces
  the boolean state that already exists.

## Decisions

**New endpoint `GET /api/v1/node-connectivity`, returning connected
certnames, not folded into `internal/inventory`.** Alternative
considered: add a `connected` field directly to `inventory`'s
`nodeSummary`/`nodeDetail` - rejected because it would make `inventory`
(currently pure-openvoxdb) depend on `internal/nodetransport`, crossing
a capability boundary (`inventory` vs the new `node-connectivity`) for a
concern that's genuinely separate: openvoxdb-sourced facts/reports vs.
in-memory transport connection state, which can already disagree with
each other on plenty of legitimate axes (a node can have rich openvoxdb
history and no live connection, or vice versa for a brand-new
not-yet-reporting node). Response shape: `{"connected":
["certname1", "certname2", ...]}` - a set, not a per-node status object,
since `Registry` has no per-node metadata beyond membership itself
(see Non-Goals).

**The frontend joins node identity (from the existing `/api/v1/nodes`)
and connection status (from the new endpoint) client-side, not
server-side.** Two small, independent fetches composed in
`frontend/src/nodes.js`, mirroring how `index.js` already composes
`/api/v1/nodes` and `/api/v1/nodes/summary` as two separate calls
rendered together. Avoids a server-side join that would otherwise force
either `inventory` to depend on `nodetransport` (rejected above) or a
new package to depend on `openvoxdb` merely to re-fetch what `inventory`
already fetches.

**New page (`nodes.html`), new `ActiveNav` value (`"node-connectivity"`),
new always-visible sidenav item - not reusing the Dashboard's `"nodes"`
key.** The proposal's own scoping decision (a separate page, not an
addition to the Dashboard) requires a distinct nav key; reusing
`"nodes"` would make both pages claim the same sidenav item as current
simultaneously; `"node-connectivity"` avoids collision. Placed as an
always-visible item (see Context) since node connectivity, unlike Jobs/
Deploys/Activity, has no "is this feature even configured" gate to hide
behind - the node transport being unconfigured just means every node
shows not-connected, which is itself useful information, not a reason
to hide the page.

## Risks / Trade-offs

- **[Risk]** Client-side join means two round trips instead of one,
  and a moment where the page has fetched one but not the other.
  **Mitigation:** both endpoints are small/fast (in-memory set membership
  and an existing paginated query); render nodes first with a
  "checking..." connectivity state, then fill in once the second fetch
  resolves - standard progressive-rendering pattern already used
  elsewhere in this frontend (e.g. `index.js`'s summary cards vs. table).
- **[Trade-off]** No live updates means the page can show stale
  "connected" state for a node that disconnected moments ago - accepted
  per Non-Goals; a manual refresh is the documented way to get current
  state, matching every other page in this frontend today (none of them
  live-update).

## Migration Plan

- No database migration - `nodetransport.Registry` is existing in-memory
  state; nothing new is persisted.
- Purely additive: new endpoint, new page, new nav item. No existing
  route, page, or config changes. Safe to deploy with zero operator
  action; nothing to roll back beyond reverting the code if needed.

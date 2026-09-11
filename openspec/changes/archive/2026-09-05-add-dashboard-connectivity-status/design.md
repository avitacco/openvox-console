## Context

`GET /api/v1/node-connectivity` (`internal/nodeconnectivity/handlers.go`)
already returns, for every certname the node transport registry or the
CA knows about, `{certname, connected, lastConnected, lastDisconnected,
certStatus, isInfrastructure, infrastructureReason}`. `frontend/src/
nodes.js` already fetches this and renders per-node connection badges
and a connection filter on `/nodes.html`. The Dashboard
(`frontend/src/index.js`) currently only calls `GET
/api/v1/nodes/summary` (openvoxdb-report-derived) for its "Fleet
status" row, in `#status-summary`, rendered as `vox-stat` cards inside
a `vox-grid` (see the just-completed `add-dashboard-fleet-status-stats`
change for the existing four-card pattern).

## Goals / Non-Goals

**Goals:**
- Surface connected/disconnected counts on the Dashboard with zero
  backend changes, reusing `GET /api/v1/node-connectivity` exactly as
  `nodes.js` already consumes it.
- Match the existing `vox-stat`/`vox-grid` presentation the four
  report-status cards already use, so the row reads as one consistent
  set of six cards rather than two visually distinct groups.

**Non-Goals:**
- Merging node-connectivity data into `/api/v1/nodes/summary` or into
  `internal/inventory` - the two data sources stay separate, per
  `internal/nodeconnectivity`'s existing package-doc rationale
  (openvoxdb inventory and live node-transport connectivity can
  legitimately disagree and describe different things).
- Per-node detail on the Dashboard (which nodes are disconnected) -
  that's what `/nodes.html` is for; the Dashboard gets only the two
  aggregate counts.
- Live-updating the count without a refresh beyond the Dashboard's
  existing 30s polling interval (`setInterval` in `index.js`), which
  already covers this the same way it covers the other four cards.

## Decisions

**Count client-side from the existing per-node array, not a new
backend aggregate endpoint.** `GET /api/v1/node-connectivity` already
returns every known node's `connected` boolean; the response is small
(one row per known certname, same data `nodes.js` already renders a
full table from), so summing `connected`/`!connected` in
`frontend/src/index.js` after the existing fetch is simpler than adding
a second backend summary endpoint whose only consumer would duplicate
logic `nodeconnectivity` already computes per-node. This mirrors how
`nodes.js` already does its own client-side filtering/sorting over the
same response.

**"Disconnected" means "known to `/api/v1/node-connectivity` and not
currently connected"**, not "every node openvoxdb has ever reported
on." A node that has never connected but has cert history (e.g. an
unsigned cert request) is still "known" to this endpoint (see its
existing certname-union behavior in handlers.go) and correctly counts
as disconnected; a node openvoxdb knows about but that has no cert
request and has never attempted a node-transport connection simply
never appears in `/api/v1/node-connectivity`'s response at all, and so
is not counted in either bucket - consistent with the endpoint's
existing documented behavior of only describing certnames it actually
has some record of, and with node-connectivity's deliberate separation
from openvoxdb inventory (see Non-Goals above).

**Two new `vox-stat` cards appended to the existing `STATUS_CARDS`-
driven row**, fetched via a second, independent `fetchJSON` call
alongside the existing summary fetch (not merged into one function) -
`loadStatusSummary` already has its own try/catch and empty-state
handling tied to `/api/v1/nodes/summary`'s shape; keeping the
connectivity fetch separate means a connectivity fetch failure doesn't
blank out the four report-status cards, and vice versa, matching how
`loadRecentActivity`/`loadRecentJobs`/`load` are already independent,
separately-erroring sections on this page.

## Risks / Trade-offs

- [Two independent fetches on page load/refresh instead of one] →
  Acceptable: the endpoint is already proven cheap enough for
  `nodes.js`'s full table+filtering use on every load of that page,
  and the Dashboard already makes four independent fetches per load
  (summary, connectivity, activity, jobs) - a fifth follows the
  established pattern rather than introducing a new one.
- [`isInfrastructure` nodes counted alongside managed nodes in
  Disconnected/Connected] → Acceptable and consistent with the
  endpoint's own model: `nodeconnectivity` reports infrastructure
  nodes' connectivity like any other node (`nodes.js` only uses
  `isInfrastructure` to control default visibility in its own table,
  not to exclude them from the underlying data) - the Dashboard summary
  counts everyone the same way, with no separate infrastructure
  carve-out.

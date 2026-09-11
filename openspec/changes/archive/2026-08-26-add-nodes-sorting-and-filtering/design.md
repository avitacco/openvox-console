## Context

The Nodes page (`frontend/src/nodes.js`) currently merges two fetches:
`GET /api/v1/nodes?page=N` (openvoxdb inventory, server-side paginated -
25/page default, 100 max, via `internal/pagination`) for the row set,
and `GET /api/v1/node-connectivity` (unpaginated, every known certname's
connection/cert-status in one response) overlaid onto whichever rows the
current page contains.

`internal/inventory`'s own handler already fetches the *entire* node
list from openvoxdb on every request regardless of `pageSize` - its
comment states plainly: "pagination here only trims the HTTP response,
it doesn't reduce the openvoxdb query cost." So the backend pays the
full-fleet cost today no matter what the frontend requests.

See proposal.md for why sorting/filtering needs to operate on the whole
fleet rather than one page at a time.

## Goals / Non-Goals

**Goals:**
- Sort by any column, filter by name/connection/cert-status, both
  applied across every known node.
- No backend changes - use the existing two endpoints as they are.

**Non-Goals:**
- Server-side sort/filter query params on `/api/v1/nodes` - rejected,
  see Decisions.
- Changing the node inventory page's own pagination (unaffected -
  different page, different use of the same endpoint).
- Persisting sort/filter state across page loads (URL params, local
  storage) - out of scope for v1; state resets on navigation/refresh,
  matching this page's existing no-persistence behavior for everything
  else.
- Live-updating sort/filter as connectivity changes in the background -
  matches the existing "Dedicated nodes page" requirement's explicit
  non-goal (refresh to see updates).

## Decisions

**Fetch the entire node list client-side instead of adding server-side
sort/filter.** Alternative considered: extend `/api/v1/nodes` with
`sort`/`filter` query params, matching its existing `name`/`fact`/
`value` filter pattern. Rejected because connection/last-connected/cert-
status - three of the four sortable/filterable columns - live in a
different package (`internal/nodeconnectivity`) queried from a
different data source (the node transport registry and the CA), not
openvoxdb. Sorting by those server-side would mean either duplicating
that data into `internal/inventory` or having it call into
`nodeconnectivity`, coupling two packages this project has deliberately
kept separate because "these can legitimately disagree" (see design.md
in `add-nodes-page`). Fetching client-side avoids that coupling
entirely, costs nothing extra on the backend (per Context above), and
this project has no documented fleet-size concern that would argue
against shipping the full list to the browser.

**Fetch-all implemented as a pagination loop, not a raised page-size
cap.** `internal/pagination.MaxPageSize` is 100; nodes.js will request
`pageSize=100` and keep requesting subsequent pages until it has
collected `total` items, rather than asking the backend to raise its
cap for one caller. Keeps the existing pagination contract intact for
every other consumer of `/api/v1/nodes` (the node inventory page).

**Sorting and filtering are pure client-side state over the fetched
data**, re-rendering the full row set on every sort/filter change - no
new fetch triggered by sorting or filtering, only by the initial load
and by a cert action's connectivity refresh (existing behavior,
unchanged).

**Filter/sort UI**: a text input (name filter) and two `<select>`
dropdowns (connection status, cert status) above the table, plus
clickable `<th>` column headers for sort (ascending by default, click
again to reverse) with a simple indicator (▲/▼) on the active sort
column - no new design-system component needed, matches this project's
existing plain-HTML-controls approach (e.g. `groups.js`'s filter
inputs).

**CA-only certnames (the existing union-with-connectivity rows) remain
included** in sort/filter, since they're already part of the fetched
data - the fetch-all change doesn't affect that logic, only replaces
`page.items` for a single page with the concatenation of every page's
items.

## Risks / Trade-offs

- **A very large fleet means a slower initial Nodes page load** (many
  sequential paginated requests to assemble the full list, then a
  larger single render). Acceptable given this project's documented
  scale (no fleet-size concern found anywhere in the codebase or
  architecture docs) and that the backend already does equivalent work
  internally per-request today. If this becomes a real problem at some
  future scale, the mitigation is exactly the rejected alternative
  above (server-side sort/filter) - revisit then, not preemptively.
- **Removing pagination controls from the Nodes page** is a visible UX
  change beyond "add sorting and filtering" - flagged here rather than
  silently bundled, since a fetch-all page has no meaningful pagination
  concept left to control (see proposal.md's Impact section).

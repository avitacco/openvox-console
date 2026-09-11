## Why

The Nodes page lists every node in the order openvoxdb happens to
return them, with no way to find a specific node or group of nodes
without reading the whole table. As the connectivity and certificate
status columns have grown (this session's earlier changes), the page
has become useful for exactly the kind of triage - "which nodes are
disconnected," "which nodes have a pending certificate" - that's
tedious without sorting or filtering.

## What Changes

- Add client-side sorting to every column (Node, Connection, Last
  connected, Cert status) via clickable column headers, toggling
  ascending/descending.
- Add client-side filtering: a text filter on node name (substring
  match), plus dropdown filters for Connection status (All/Connected/
  Not connected) and Cert status (All/Signed/Requested/Revoked/Unknown).
- **Design decision**: sorting and filtering apply across the *entire*
  fleet, not just the currently-loaded page - see design.md for why this
  requires changing how the Nodes page fetches its node list, since
  `/api/v1/nodes` is currently server-side paginated (25/page by
  default). This was chosen over extending `/api/v1/nodes` with
  sort/filter query params, and over the previous discussion is
  recorded in design.md rather than left as an open question, since
  investigation showed the backend already fetches the entire node
  list from openvoxdb internally on every request regardless of page
  size (`internal/inventory`'s own comment: "pagination here only trims
  the HTTP response, it doesn't reduce the openvoxdb query cost") - so
  fetching everything client-side adds no new backend cost, and this
  project has no documented fleet-size-at-scale concern that would
  argue for keeping pagination.

## Capabilities

### Modified Capabilities
- `node-connectivity`: the "Dedicated nodes page" requirement gains
  sorting and filtering behavior.

## Impact

- **Code**: `frontend/src/nodes.js` (fetch-all instead of paginated
  fetch, sort/filter state and rendering), `frontend/templates/pages/
  nodes.tmpl` (filter controls markup). No backend/API changes -
  `/api/v1/nodes` and `/api/v1/node-connectivity` are used as they
  already exist.
- **Removed**: the Nodes page's pagination controls (`paginationHTML`/
  `bindPagination`), since the full fleet is now loaded and sorted/
  filtered client-side rather than paged. The node inventory page
  (`/api/v1/nodes`'s other consumer) keeps its own pagination
  unchanged - this only affects the Nodes page's own use of it.

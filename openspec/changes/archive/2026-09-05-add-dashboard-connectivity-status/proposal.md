## Why

`internal/nodeconnectivity` already reports, per node, whether the
console currently holds a live node transport connection to it (`GET
/api/v1/node-connectivity`), but that data is only surfaced on the
dedicated nodes page (`/nodes.html`, `frontend/src/nodes.js`). The
Dashboard's "Fleet status" row (`frontend/src/index.js`) shows only
openvoxdb-report-derived stats (Failed/Corrected/Intentional changes/
Unchanged) - an operator glancing at the Dashboard has no way to tell
how many managed nodes are actually reachable for on-demand
orchestration right now without navigating to the nodes page. The user
wants connected/disconnected status visible on the Dashboard.

## What Changes

- Add two more `vox-stat` cards to the Dashboard's existing "Fleet
  status" row - Connected, Disconnected - alongside the four existing
  report-status cards, populated from the existing `GET
  /api/v1/node-connectivity` endpoint (the same one `nodes.js` already
  calls), fetched and counted client-side in `frontend/src/index.js`.
  No backend change: this reuses the endpoint as-is.
- "Disconnected" counts every certname `/api/v1/node-connectivity`
  knows about (i.e. has ever held a connection, or has cert-status
  history) with `connected: false` - not every node in openvoxdb's
  inventory, since node transport connectivity and openvoxdb inventory
  are deliberately separate universes (see
  `internal/nodeconnectivity`'s package doc) and this change does not
  merge them.
- No new stat-row when the node transport is disabled (no
  `CONSOLE_NODE_TRANSPORT_ADDR`): the Connected/Disconnected cards
  still render (both 0), consistent with the endpoint's existing
  documented behavior of reporting every node as not connected rather
  than erroring when unconfigured.

## Capabilities

### Modified Capabilities
- `node-connectivity`: adds a "Dashboard connectivity summary"
  requirement - this is new ground (the existing spec covers the
  connectivity endpoint and the dedicated nodes page, but nothing about
  the Dashboard), so it's written as an ADDED requirement under this
  existing capability.

## Impact

- **Code**: `frontend/src/index.js` (fetch `/api/v1/node-connectivity`
  alongside the existing summary fetch, count connected/disconnected,
  render two more `vox-stat` cards), `frontend/templates/pages/
  index.tmpl` (no structural change expected - `#status-summary` stays
  the mount point for all six cards).
- **APIs**: none - reuses `GET /api/v1/node-connectivity` exactly as
  it exists today; no backend changes.
- **Infrastructure**: none.

## 1. Fetch the full fleet

- [x] 1.1 Replace `frontend/src/nodes.js`'s single paginated `/api/v1/nodes?page=N` fetch with a loop that requests `pageSize=100` and keeps fetching subsequent pages until every item (`total`) is collected into one array - verify `./frontend/build.sh` succeeds
- [x] 1.2 Remove the Nodes page's pagination controls (`paginationHTML`/`bindPagination` calls) now that the full fleet is loaded at once, confirming the node inventory page's own pagination (a different page, `frontend/src/index.js`) is untouched - verify by reading `index.js` still imports and calls `paginationHTML`/`bindPagination` unchanged

## 2. Sorting

- [x] 2.1 Add clickable column headers (Node, Connection, Last connected, Cert status) that set a sort column + direction, re-rendering the full row set in that order, with a ▲/▼ indicator on the active column - verify by loading the real Nodes page and confirming each column sorts ascending then descending on repeated clicks
- [x] 2.2 Define each column's sort comparator: Node by name (locale-aware string compare), Connection by connected-before-not-connected, Last connected by timestamp (nodes with no timestamp sort last regardless of direction), Cert status by the signed/requested/revoked/unknown grouping - verify by inspecting real mixed-state data (some nodes connected/some not, some with timestamps/some without) sorts sensibly, not just alphabetically by badge text

## 3. Filtering

- [x] 3.1 Add a node-name text filter (substring match, case-insensitive) above the table - verify live: typing a substring narrows the table to matching certnames only
- [x] 3.2 Add Connection-status and Cert-status dropdown filters (All/Connected/Not connected; All/Signed/Requested/Revoked/Unknown) - verify live: each filter option narrows the table to only matching rows
- [x] 3.3 Confirm filters and sort compose correctly (filtering then sorting the filtered set, not the full set) - verify live: apply a filter, then sort, and confirm rows outside the filter never reappear

## 4. Verification and documentation

- [x] 4.1 Confirm the CA-only-certname rows (nodes known to `/api/v1/node-connectivity` but not `/api/v1/nodes`, e.g. a pending certificate request never seen by openvoxdb) still appear and remain sortable/filterable after the fetch-all change - verify live using a scratch pending certificate the same way `add-node-certificate-management`'s live verification did
- [x] 4.2 Run the full test suite (`make test`), confirm it passes, `gofmt -l .` and `go vet ./...` clean (no Go changes are expected in this frontend-only change, but this confirms nothing else broke)

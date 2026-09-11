## 1. Backend: node connection status endpoint

- [x] 1.1 Create a new small package exposing `GET /api/v1/node-connectivity` (gated on `nodes:read`, matching `inventory`'s existing pattern), returning `{"connected": [...certnames]}` sourced from `internal/nodetransport.Registry` - verify with a unit test using a fake registry, covering an empty set and a populated one
- [x] 1.2 Wire the new handler into `cmd/console/main.go`, using the already-constructed `nodeTransport` (nil-safe: if `nodeTransport` is nil, i.e. `CONSOLE_NODE_TRANSPORT_ADDR` unset, the endpoint returns an empty connected set rather than erroring) - verify `go build ./...` succeeds and a real request against a running console returns a well-formed JSON response

## 2. Frontend: nodes page

- [x] 2.1 Add `nodes.html` to `frontend/gen/main.go`'s page manifest (`ActiveNav: "node-connectivity"`) and create `frontend/templates/pages/nodes.tmpl` (mirroring `groups.tmpl`'s minimal `<div id="results">` structure) - verify `./frontend/build.sh` succeeds and the page is embedded
- [x] 2.2 Add a new always-visible "Nodes" sidenav item to `frontend/templates/layout.html.tmpl` (matching the Dashboard/Groups items' pattern, `ActiveNav` value `"node-connectivity"`) - verify live in a browser that the item appears, links to `/nodes.html`, and is marked current only on that page
- [x] 2.3 Create `frontend/src/nodes.js`: fetch `/api/v1/nodes` (reusing pagination as `index.js` does) and `/api/v1/node-connectivity`, render a table of every node's name and connection status (progressive: show nodes immediately, fill in connectivity once that fetch resolves) - verify live in a browser against a real console with at least one connected and one not-connected node, confirming both states render correctly

## 3. End-to-end verification

- [x] 3.1 Run the full test suite (`make test`), confirm it passes, `gofmt -l .` and `go vet ./...` clean
- [x] 3.2 Live: open `/nodes.html` in a browser against a real running console with a real connected `node-agent-client`, confirm that node shows as connected, then stop the agent process and confirm a page refresh shows it as not connected

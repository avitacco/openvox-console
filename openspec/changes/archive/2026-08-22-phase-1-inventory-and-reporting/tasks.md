## 1. openvoxdb-client: connection and query execution

- [x] 1.1 Add openvoxdb connection config (URL, client cert/key/CA file
      paths) to `internal/runtime.Config` and verify startup fails with a
      clear error when a required value is missing, matching the existing
      config-loading pattern
- [x] 1.2 Implement an HTTPS client presenting the configured client
      certificate and verify it connects successfully to a running
      openvoxdb instance (`make openvox-up`)
- [x] 1.3 Implement `Query(ctx, pql string) (Result, error)` against
      `/pdb/query/v4` and verify it returns parsed results for a valid
      query against real openvoxdb data
- [x] 1.4 Verify a syntactically invalid PQL query returns an error without
      being sent to openvoxdb
- [x] 1.5 Verify a query issued while openvoxdb is unreachable returns a
      clear "unreachable" error and does not crash the console process

## 2. openvoxdb-client: typed queries for nodes, facts, reports, events

- [x] 2.1 Add a typed query + result struct for node listing (name,
      status, last check-in) and verify it returns real data from
      openvoxdb
- [x] 2.2 Add a typed query + result struct for a node's factset and
      verify it returns real fact data for a known node
- [x] 2.3 Add a typed query + result struct for a node's report history
      and verify it returns real report data ordered by time
- [x] 2.4 Add a typed query + result struct for a report's resource events
      and verify it returns real event data for a known report

## 3. Inventory: API and UI

- [x] 3.1 Add `GET /api/v1/nodes` returning the node list and verify it
      responds with real node data from openvoxdb
- [x] 3.2 Verify `GET /api/v1/nodes` returns an empty list (not an error)
      when openvoxdb reports no nodes
- [x] 3.3 Add `GET /api/v1/nodes/{name}` returning a node's facts and
      verify it responds with real fact data
- [x] 3.4 Add node name and fact-value filtering to `GET /api/v1/nodes`
      (query parameters) and verify the returned list narrows correctly
      for both filter types
- [x] 3.5 Add the node inventory list page (voxblocks components,
      `fetch()` against the API) and verify it renders real nodes with
      status and last check-in time when loaded in a browser
- [x] 3.6 Add the node detail page showing facts and verify it renders a
      selected node's real fact data
- [x] 3.7 Wire the search/filter UI to the API's filter parameters and
      verify filtering in the browser narrows the displayed list

## 4. Reporting: API and UI

- [x] 4.1 Add `GET /api/v1/nodes/{name}/reports` returning report history
      and verify it responds with real report data ordered by most recent
      first
- [x] 4.2 Verify `GET /api/v1/nodes/{name}/reports` returns an empty list
      (not an error) for a node with no recorded runs
- [x] 4.3 Add `GET /api/v1/reports/{id}/events` returning resource-level
      events and verify it responds with real event data for a known
      report
- [x] 4.4 Add status filtering (query parameters) to both endpoints and
      verify the returned lists narrow correctly
- [x] 4.5 Add the report history page and verify it renders a node's real
      run history with timestamp and status
- [x] 4.6 Add the event drill-down view and verify it renders a selected
      report's real resource events
- [x] 4.7 Wire the status-filter UI to the API's filter parameters and
      verify filtering in the browser narrows both report and event lists

## 5. Local dev setup and integration verification

- [x] 5.1 Document the console client cert generation step
      (`puppetserver ca generate --certname console` against the running
      `openvoxserver` container) and verify a cert generated this way
      authenticates successfully
- [x] 5.2 Run the console end to end against `make openvox-up` and verify:
      the node inventory view shows real nodes, the report history view
      shows real runs, and the event drill-down shows real events for at
      least one node exercised via a real agent run (this phase's exit
      criteria)

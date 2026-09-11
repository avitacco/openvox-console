## 1. Connection timestamps

- [x] 1.1 Add `LastConnected`/`LastDisconnected` tracking to `internal/nodetransport.Registry` (captured in `markConnected`/`markDisconnected`), with accessors reporting both as `*time.Time` (nil when never observed) - verify with a unit test: connect, check `LastConnected` is set and `LastDisconnected` is nil; disconnect, check `LastDisconnected` is now set too
- [x] 1.2 Extend `internal/nodeconnectivity`'s response to include both timestamps per node (not just the flat `connected` list - see design.md, this changes the response shape) - verify with a unit test using a fake registry
- [x] 1.3 Verify live: connect a real node-agent-client to a real console, confirm `LastConnected` appears in the API response; disconnect it, confirm `LastDisconnected` also appears

## 2. Certificate status

- [x] 2.1 Create `internal/certstatus`: a client calling openvoxserver's `GET /puppet-ca/v1/certificate_statuses/<ignored>` (bulk form) with the configured CA-client cert/key, returning a `map[certname]string` of states - verify with a unit test against a fake HTTP server returning a realistic response body
- [x] 2.2 Add `CONSOLE_CA_CLIENT_CERT_FILE`/`_KEY_FILE`/`_URL` to `internal/runtime.Config`, all optional, unset by default - verify with unit tests covering unset (feature disabled) and configured cases
- [x] 2.3 Wire `internal/certstatus` into `cmd/console/main.go` and `internal/nodeconnectivity`'s handler (nil-safe: unconfigured reports every node's cert status as `"unknown"`) - verify `go build ./...` succeeds
- [x] 2.4 Live: mint a real CA-client certificate against the real openvoxserver (`puppetserver ca generate --ca-client`, stopping/restarting the container as required), configure a scratch console with it, and confirm the node-connectivity endpoint reports real signed/unknown cert statuses for real nodes - use a scratch/throwaway credential and node for this, not anything shared, given the credential's privilege (see design.md's Risks)

## 3. Frontend

- [x] 3.1 Add "Last connected" and "Cert status" columns to `frontend/src/nodes.js`'s table, formatting timestamps as relative/local time and absent timestamps as "never" - verify `./frontend/build.sh` succeeds
- [x] 3.2 Live: view the real Nodes page in a browser against a real console with real data for both new columns (a node with a real last-connected timestamp, and - if task 2.4's scratch CA-client credential is still available - a node with a real cert status) - screenshot as evidence, matching `add-nodes-page`'s verification style

## 4. Documentation and end-to-end verification

- [x] 4.1 Document the CA-client credential runbook in `operations.md`: the exact privilege it grants (full CA admin, not read-only), the generation steps (stop openvoxserver, `puppetserver ca generate --ca-client`, restart), and the config wiring - verify by re-reading it as if setting this up cold, confirming every step is concrete and no privilege is glossed over
- [x] 4.2 Run the full test suite (`make test`), confirm it passes, `gofmt -l .` and `go vet ./...` clean

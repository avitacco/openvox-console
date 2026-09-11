## 1. Statelessness audit and documentation

- [x] 1.1 Write the operator-facing statelessness boundary document: enumerate every in-memory-only state in the running console (`nodetransport.Registry` sessions, `orchestrator.Dispatcher.pending`, `rbac.Revoker`'s cache) and state for each whether a request touching it can be safely routed to any instance or only the one that created it - verify by having someone unfamiliar with the codebase read it and correctly predict the outcome of three hypothetical failover scenarios
- [x] 1.2 Cross-reference the new orchestrator-dispatcher-affinity finding in architecture-summary.md's existing Phase 7 deferral note, so it isn't documented in only one place - verify the two documents agree

## 2. Metrics endpoint

- [x] 2.1 Add `github.com/prometheus/client_golang` and expose `GET /metrics` (unauthenticated, matching `/health`) in `internal/runtime` - verify `go build ./...` succeeds and a real request against a running console returns Prometheus text-format output
- [x] 2.2 Instrument HTTP request counts and latencies (by route and status) via middleware - verify by making several real requests and confirming their counts/latencies appear in a subsequent scrape
- [x] 2.3 Add gauges for the existing health-check dependencies (Postgres reachable, NATS reachable) alongside the existing `/health` endpoint, not replacing it - verify both endpoints agree when a dependency is deliberately made unreachable

## 3. Broker/orchestrator/revocation visibility

- [x] 3.1 Add a `Len()`-style accessor to `nodetransport.Registry` reporting its current session count, guarded by the same mutex as the sessions map - verify with a unit test connecting and disconnecting sessions and asserting the count tracks correctly
- [x] 3.2 Add the equivalent accessor to `orchestrator.Dispatcher` for its `pending` map - verify with a unit test
- [x] 3.3 Add the equivalent accessor to `rbac.Revoker` for its in-memory revocation cache size - verify with a unit test
- [x] 3.4 Wire all three as metrics gauges scraped at `/metrics` - verify against a real running console with a real connected `node-agent-client` that the connected-agent gauge reflects reality

## 4. Postgres reconnection resilience

- [x] 4.1 Add a new opt-in `docker-compose.yml` profile bringing up a real Postgres primary + streaming-replication standby, with a `make postgres-replication-up` target - verify the standby actually replicates a write made on the primary
- [x] 4.2 Document the failover runbook (promote the standby, repoint the console's `CONSOLE_POSTGRES_DSN`) - verify by actually performing a failover against the fixture from 4.1 and confirming the standby serves reads/writes correctly post-promotion. Added a `postgres-proxy` fixture service (`postgres-replication-init/proxy-entrypoint.sh`, a `socat` forwarder) so a client's DSN can hold a stable address across promotion, plus a `make postgres-replication-failover` target - documented in operations.md's new "Postgres failover runbook" section
- [x] 4.3 Run the real console against the fixture, kill the primary mid-run, promote the standby, and confirm the console's `/health` and a real write-path endpoint recover without a process restart - verify live; if this reveals a real gap in `internal/persistence`'s pool handling, fix it here (see design.md's "Postgres reconnection resilience is verified behavior, not new retry machinery" - upgrade to new code only if the test proves it's needed) and note the finding in this task's own line before checking it off. **Finding: no gap.** Ran a real console instance against the fixture (via the proxy), created a real user through `POST /api/v1/users`, killed the primary container, ran `make postgres-replication-failover`, and confirmed `/health` went `unhealthy` -> `ok` and a second `POST /api/v1/users` succeeded - all against the same console process (verified via PID), with zero new code. `pgxpool`'s existing reconnection behavior was sufficient.

## 5. JWT signing key rotation

- [x] 5.1 Add `kid` to `Issuer`'s signing config and to every issued token's JWT header - verify with a unit test asserting an issued token's header carries the configured `kid`
- [x] 5.2 Change `Verifier` to hold a `map[string]*ecdsa.PublicKey` (kid -> key) instead of one key, selecting the verification key by the token's header `kid`, falling back to the configured primary key when a token has no `kid` (pre-rotation tokens) - verify with unit tests covering a matching kid, an unknown kid, and a missing kid
- [x] 5.3 Add `CONSOLE_RBAC_SIGNING_KEY_ID` and `CONSOLE_RBAC_VERIFICATION_KEYS_DIR` to `internal/runtime.Config`, both optional with backward-compatible defaults - verify with unit tests covering unset (today's single-key behavior unchanged) and configured cases
- [x] 5.4 Wire the new config into `cmd/console/main.go`'s `Issuer`/`Verifier` construction - verify `go build ./...` succeeds and the console starts correctly with no rotation config set
- [x] 5.5 Add a rotation-friendly variant of the `make rbac-keys` Makefile target that generates a new named key without touching the currently-active one - verify by running it and confirming the existing active key file is untouched. Verified live: `md5sum certs/rbac-signing-key.pem` before/after `make rbac-rotate-key KID=test-rotation-key` was identical, and the new key landed in `certs/rbac-verification-keys/test-rotation-key.pem`
- [x] 5.6 Document the key rotation procedure (generate, add to verification set, promote to active signer, retire after the old key's longest-lived token would have expired) - verify by actually performing a live rotation end to end: issue a token under the old key, rotate, confirm the old token still verifies, issue a new token, confirm it carries the new `kid`. Verified live against a real running console instance (see operations.md's "JWT signing key rotation" section), including retiring the old key afterward and confirming its pre-rotation token was then rejected

## 6. End-to-end verification

- [x] 6.1 Run the console live, exercise a representative mix of endpoints, and confirm `/metrics` reports plausible, changing values (not just present but static/wrong). Verified against the real running dev instance (already carrying genuine browsing traffic): per-route request/latency histograms were populated, `console_http_requests_total{route="/health"}` incremented 1 -> 4 across 3 real requests, and `console_node_transport_connected_agents` correctly showed `1` for a real connected agent
- [x] 6.2 Run `make test` and confirm the full suite passes, and `gofmt -l .` / `go vet ./...` are clean. All three verified clean/passing

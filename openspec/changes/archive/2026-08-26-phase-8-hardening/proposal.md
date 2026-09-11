## Why

Every phase built so far has shipped against a working single-instance development setup, with no metrics surface, no documented Postgres replication story, and a JWT signing key that cannot be rotated without an instant mass logout of every active session. Phase 8 in phased-build-plan.md is exactly this: a production-readiness pass across everything already built, before this console is asked to run as a real, monitored, horizontally-scaled service rather than a dev fixture.

## What Changes

- **Statelessness audit, with the one known exception documented rather than silently ignored.** `internal/rbac.Revoker` is already a correct multi-instance pattern (in-memory cache, Postgres source of truth, NATS propagation) - used as the reference. The node transport's connection registry (`internal/nodetransport`) is single-instance by design, already called out in architecture-summary.md as deferred to Phase 7 federation; this change additionally documents a related, previously-unstated consequence: `internal/orchestrator.Dispatcher`'s in-flight dispatch request tracking is also implicitly pinned to whichever instance dispatched a job, for the same underlying reason. Neither is fixed here - Phase 7 owns that - but both are made explicit in an operator-facing statement of exactly what does and does not survive a failover today.
- **Postgres reconnection resilience.** `internal/persistence`'s connection pool SHALL recover automatically from a connection interruption (e.g. a replica promotion during failover) rather than requiring the console to be restarted, extending the project's existing "never fatal at startup" posture to "never permanently fatal at runtime" for transient Postgres unavailability. A documented, tested replication/failover runbook accompanies this (operational documentation, not new application behavior, so it lives in design.md/tasks.md rather than the spec).
- **A Prometheus-compatible metrics endpoint**, including operational gauges the console has never surfaced before - connected-agent count, in-flight orchestrator dispatch count, and revocation-list size - so an operator has more to look at than log lines and direct database queries.
- **JWT signing key rotation.** The system moves from exactly one signing/verification key to one active signing key plus a bounded set of still-valid verification keys, selected via a `kid` claim - so rotating a key no longer instantly invalidates every outstanding token, and the process for doing it safely is documented.

## Capabilities

### New Capabilities
(none)

### Modified Capabilities
- `service-runtime`: adds a metrics endpoint, broker/orchestrator operational introspection, Postgres reconnection resilience, and a documented statelessness boundary (which in-memory state does and does not survive failover).
- `rbac`: adds JWT signing key rotation (multiple valid verification keys, `kid`-selected, one active signing key).

## Impact

- New dependency: a Prometheus metrics client library (`github.com/prometheus/client_golang`).
- `internal/runtime`: metrics registration and a new/extended introspection endpoint; Postgres pool reconnection behavior.
- `internal/nodetransport`, `internal/orchestrator`: expose the counts the introspection endpoint reports.
- `internal/rbac/keys.go`, `tokens.go`, `middleware.go`: multi-key verification, `kid` claim on issued tokens, key-set loading/config.
- `Makefile`/`docker-compose.yml`: a replication-capable Postgres fixture for testing failover; a key-rotation helper target alongside the existing `rbac-keys`.
- A new operational runbook document (Postgres replication/failover, JWT key rotation procedure).
- `cmd/console/main.go` wiring for all of the above.
- Note (out of scope for this change, surfaced for awareness): `openspec/specs/` currently has no `node-transport`/`orchestrator`/`node-agent-client`/`agent-distribution` main spec - Phase 6's delta specs were archived without ever being synced. This change works around that gap by keeping broker/orchestrator introspection requirements framed from `service-runtime`'s side rather than editing a main spec that doesn't exist; backfilling that gap properly is a separate, future change.

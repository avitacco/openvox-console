## Context

See proposal.md for motivation. `internal/messaging.Bus` already provides
generic `Publish(subject, data []byte)`/`Subscribe(subject, handler)` over
an embedded, unclustered NATS server (`internal/messaging/nats.go`) -
`internal/rbac`'s `Revoker` (`revoke.go`) is the one existing consumer,
publishing/subscribing on a single subject (`rbac.revoked`) with a small
JSON envelope, started at boot (`Revoker.Start`) before the HTTP server
begins accepting requests, so no event published by a live request can be
missed. This capability follows the same shape for a second subject.

No package outside `internal/rbac` currently imports it - `classifier`,
`inventory`, `reporting`, and `encapi` only receive a plain
`authorize func(permission string, next http.HandlerFunc) http.HandlerFunc`
from `cmd/console/main.go`, keeping capabilities decoupled from each
other. `classifier`'s mutating handlers are `createGroup`/`updateGroup`/
`deleteGroup` in `internal/classifier/handlers.go`; `rbac`'s are in
`internal/rbac/handlers.go` (`createUser`, `deleteUser`, `createRole`,
`updateRole`, `deleteRole`, `assignRole`, `unassignRole`,
`createServiceToken`, `revokeServiceToken`) plus `OIDCService.HandleCallback`
in `internal/rbac/oidc.go` for provisioning/reconciliation.

## Goals / Non-Goals

**Goals:**
- One activity log, fed by every capability, matching
  architecture-summary.md's explicit design ("a single subscriber
  persisting to one table, rather than each service independently writing
  its own audit entries").
- Publishing an event never blocks or fails the action being recorded -
  NATS publish is fire-and-forget and in-process; a publish error is
  logged, not returned as the request's error.
- Preserve the existing loose coupling between capabilities: `classifier`
  and `rbac` publish through an injected function, the same shape as the
  already-established `authorize` injection, not by importing
  `internal/activity` or each other directly.

**Non-Goals:**
- Durable delivery guarantees (JetStream) - architecture-summary.md
  already rules this out ("core fire-and-forget pub/sub is sufficient");
  the subscriber is started before HTTP serving begins, matching
  `Revoker.Start`'s existing ordering, so single-instance delivery is
  reliable in practice without needing broker-level durability.
- Retention/archival policy, export, or a query language beyond
  most-recent-first with a category filter - a v1 audit trail, not a SIEM.
- Instrumenting inventory/reporting (read-only capabilities - nothing to
  audit) or code manager/orchestrator (later phases, not built yet).

## Decisions

**A single NATS subject (`activity.events`) and one JSON envelope**,
not one subject per action. `Revoker` uses a single subject for a single
event shape; `activity` has many producers and action kinds, but they all
reduce to the same shape (category, action, actor, summary, timestamp),
and the log is explicitly meant to be one unified feed - a single
subject keeps the subscriber (and any future consumer) trivial: subscribe
once, persist every message.

```go
type Event struct {
    Category   string    `json:"category"`   // "classifier" | "rbac"
    Action     string    `json:"action"`     // "group.created", "role.assigned", ...
    Actor      string    `json:"actor"`      // username, or "system" for OIDC-driven changes
    Summary    string    `json:"summary"`    // human-readable, e.g. "created group web-servers"
    OccurredAt time.Time `json:"occurredAt"`
}
```

**Actor and publisher are injected functions, mirroring `authorize`.**
`cmd/console/main.go` already passes
`authorize func(permission string, next http.HandlerFunc) http.HandlerFunc`
into every package's `Register`. This change adds one more:
`recordActivity func(r *http.Request, action, summary string)` - a single
closure built in `main.go` that knows how to (a) read the actor from the
request's `rbac.Claims` (via context) and (b) publish an
`activity.Event` with a fixed `category` baked in per package
(`classifier.NewHandlers(store, recordActivity)` closes over
`"classifier"`; `rbac` does the same with `"rbac"`). Neither package
imports `internal/activity` or `internal/rbac` - only `cmd/console/main.go`
does, exactly where the existing `authorize` wiring already lives.

**Actor extraction lives in `cmd/console/main.go`, not `internal/activity`.**
Reading `rbac.Claims` from the request context is `internal/rbac`'s
concern; `internal/activity` only defines the `Event` shape and the
publish/persist mechanics, so it doesn't need to import `internal/rbac`
either, keeping the dependency graph a strict one-way fan-in from
`main.go` rather than a new cross-capability edge.

**Publish failures are logged, never surfaced to the caller.** A group or
user mutation has already succeeded in Postgres by the time the activity
event would publish; failing the HTTP response over an audit-logging
hiccup would be worse than a missed audit entry. `recordActivity` takes
no error return - it logs via `slog` internally and returns.

**Activity handler summaries are generated server-side, not
client-supplied.** Every producer already knows exactly what changed
(the group/user/role name, the field being assigned); building a fixed,
readable summary string at the point of the action is simpler and more
trustworthy than storing structured detail and rendering it generically
later. A `details` JSONB column is deliberately not added in this phase -
`summary` covers the stated Phase 4 exit criteria on its own, and a
structured-detail UI is easy to layer on later without a breaking schema
change (an additive column, not a rewrite).

## Risks / Trade-offs

- [A burst of `activity.events` messages while the subscriber briefly lags
  could, in principle, reorder persistence relative to `occurred_at`] →
  Accepted: `occurred_at` is set at publish time and the UI sorts by it,
  not by NATS delivery/insert order, so a brief lag doesn't misorder what
  the user sees.
- [Adding a new required argument (`recordActivity`) to two packages'
  `NewHandlers`/`Register` touches call sites in `cmd/console/main.go`] →
  Small, mechanical change; matches the same pattern already used for
  `authorize`.
- [Free-text `summary` strings aren't machine-filterable beyond
  `category`] → Accepted per Non-Goals; a v1 audit trail favors
  readability over query power.

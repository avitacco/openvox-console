## Context

See proposal.md for motivation. Two existing pieces of infrastructure this design builds directly on:

- `internal/runtime.NewLogger(w io.Writer) *slog.Logger` already wraps `slog.NewJSONHandler(w, nil)` - the console's operational logs are already structured JSON. This design reuses that exact mechanism for audit events rather than introducing a second logging library or format.
- `internal/activity`'s existing pattern - an injected closure (`recordActivity func(r *http.Request, action, summary string)`) passed into each capability's `Handlers`, so packages never import `internal/activity` directly - is the established convention for "every capability reports something to a cross-cutting concern" in this codebase. See specs/audit-log-emission/spec.md for the behavior contract this design implements.

## Goals / Non-Goals

**Goals:**
- One consistent, per-category-configurable emitter usable from every existing package, following the codebase's established injected-closure convention.
- Emission decoupled from Postgres/NATS health - a synchronous local write, not a fire-and-forget publish, so the audit trail's existence doesn't depend on the same infrastructure the in-app activity feed does.
- Zero new required infrastructure or external dependency.

**Non-Goals** (see proposal.md - these are deliberate, confirmed with the user):
- Any storage, retention, export, or tamper-evidence for the emitted stream beyond the local synchronous write itself.
- Delivery guarantees beyond that local write - if the process crashes mid-write, that event is lost, the same inherent limit as any local-only log emission.
- Per-action configuration granularity - categories match existing package boundaries (six knobs), not dozens of individually-toggleable actions.

## Decisions

**New `internal/auditlog` package, following `internal/activity`'s injected-closure pattern rather than direct import.** `cmd/console/main.go` builds one `*auditlog.Emitter` from the per-category `Level` config and passes narrow closures into each consuming package's `NewHandlers`, mirroring `recordActivity` exactly. Alternative considered: let packages import `internal/auditlog` directly, since (unlike `internal/activity`) it has no NATS/Postgres dependency to hide and would be trivial to construct in tests either way - rejected in favor of consistency with the codebase's one existing convention for "report to a cross-cutting concern," so a future reader doesn't have to learn two different patterns for what looks like the same kind of problem.

**Per-category `Level` (`off`/`writes`/`full`) resolved once at startup, not re-read per request.** `internal/runtime.Config` gains one field per category (`CONSOLE_AUDIT_NODES`, `CONSOLE_AUDIT_CLASSIFIER`, `CONSOLE_AUDIT_RBAC`, `CONSOLE_AUDIT_AUTH`, `CONSOLE_AUDIT_CODE`, `CONSOLE_AUDIT_ORCHESTRATOR`, each `off`/`writes`/`full`, defaulting to `writes`), read once into an `auditlog.Config` at startup. Same "no per-request lookup" reasoning already used for RBAC permissions embedded in issued JWTs rather than looked up live.

**Emission is a synchronous `slog` write, not a NATS publish.** `Emitter.Write(...)`/`Emitter.Read(...)` marshal directly through a `*slog.Logger` built via the existing `runtime.NewLogger`, in-process, on the calling goroutine. This directly closes the durability gap identified in the original gap analysis: an audit event's existence no longer depends on NATS or Postgres being healthy at that instant, unlike `internal/activity`'s fire-and-forget publish. Trade-off: adds a synchronous JSON marshal + write to the request path (microseconds) instead of a near-zero-cost async publish - accepted, since reliability of the audit trail is the entire point of this capability.

**Fixed event schema, not ad hoc per-call-site fields.** Every emitted event carries the same key set: `action` (stable identifier, e.g. `"user.login.success"`, `"node.viewed"`), `actor`, `category`, `resourceType`, `resourceId`, and optional `before`/`after` (structured values, only for changes where the store makes them available) - plus `slog`'s own built-in `time`/`level`/`msg`. One predictable shape a downstream log pipeline can rely on, rather than reverse-engineering per-action formats.

**Read-level instrumentation is added explicitly per read handler, not generic HTTP middleware.** A middleware wrapping every route could only capture method/path/status - not the structured resource identifiers the spec requires (e.g. "viewed node `web01`'s facts", not `GET /api/v1/nodes/web01`), and can't distinguish a meaningful data read from routine/health-check traffic without handler-level knowledge anyway. Costs more call sites (every read handler across inventory, reporting, classifier, rbac, code-manager, orchestrator) but keeps every event meaningful - and matches the same per-handler shape write instrumentation already uses today.

**No new persistent state.** The emitter never writes to Postgres and has no schema of its own - directly enforces the "the app's job ends at emission" boundary; nothing to migrate, nothing to retain, nothing to clean up.

## Risks / Trade-offs

- **[Risk]** Read-level instrumentation touches a large number of call sites across most packages - explicitly excluded from the original phase-4 activity log for this reason. **Mitigation:** mechanical, one line per handler, same shape as existing write instrumentation; tasks.md sequences it package-by-package so it can land incrementally rather than as one large change.
- **[Risk]** A synchronous write on every audited request adds latency to that request, unlike today's fire-and-forget publish. **Mitigation:** a local JSON marshal + write is on the order of microseconds - negligible next to the openvoxdb/Postgres round-trips already on those same request paths.
- **[Risk]** Recording an attempted username on a failed login could itself be treated as sensitive in some environments. **Mitigation:** standard, expected audit-trail practice - distinct from the existing HTTP-response-level protection against account-enumeration/timing attacks (`internal/rbac.AuthService.Login`'s dummy-hash comparison), which is about what's returned to the *requester*, not what's recorded internally for the *operator*.
- **[Risk]** Two now-coexisting logging systems (`internal/activity`'s Postgres+NATS feed, this new emitter) could appear to disagree about "the same event" if an operator compares them directly. **Mitigation:** they're allowed to diverge by design - `internal/activity` remains a UI convenience feed, not full coverage; this document and the proposal make that boundary explicit rather than implying parity.
- **[Risk]** A misconfigured separate output path (bad path, unwritable directory) could crash the console at startup if treated as fatal. **Mitigation:** follow this project's established "never fatal at startup" posture (same as the node transport, OIDC discovery) - log a warning and fall back to stdout rather than refusing to start.

## Migration Plan

- New environment variables only, each optional with a documented default (`writes` per category, stdout for output) - purely additive, no existing configuration changes shape or meaning.
- No database migration - this path never touches Postgres.
- Rollback is reverting the emitter wiring in `cmd/console/main.go`; since nothing is persisted by this capability, there is no data to clean up.

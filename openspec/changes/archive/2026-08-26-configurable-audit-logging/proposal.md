## Why

The console's existing activity log (`internal/activity`, from the phase-4-activity-and-audit-log change) is a lightweight, in-app "recent activity" feed for the admin UI - useful, but not designed for compliance use. It has real gaps for that purpose: authentication events (login success/failure, logout) are never recorded at all; several mutating endpoints added since (self-service profile/password changes, admin edits to an existing user) emit nothing; and it has no concept of read/view access. It is also, by original design, explicitly *not* a system of record - no retention policy, no export, no tamper-evidence, unbounded Postgres growth.

An organization evaluating this console for a real compliance-relevant environment needs the application itself to reliably produce a complete, structured stream of audit-relevant events - what happens to that stream afterward (storage, retention, immutability, export, SIEM ingestion) is squarely the operator's responsibility, using infrastructure they already run for this. The console's job is to be a trustworthy source, not to become a SIEM itself.

## What Changes

- New, independently configurable audit level - `off` / `writes` / `full` - per capability category (nodes/inventory, classifier/groups, rbac/users, auth, code deploys, orchestrator jobs), not one global switch. `writes` covers mutating actions and authentication events for that category; `full` adds read/view access to that category's data. An operator with no compliance need can leave everything `off`; one under a strict regime can turn any category up to `full` independently of the others.
- New structured JSON audit event emission, one event per line, via the console's existing `log/slog`-based logger - to stdout by default, or to a separate configured file path for operators who want audit events physically separated from ordinary operational logs at the source.
- A richer per-event schema than the existing activity log's five plain fields: a stable event/action identifier, actor, category, timestamp, structured resource identifiers, and before/after values where applicable (e.g. a role's permission diff) - not just a free-text summary.
- Closes real coverage gaps that exist today regardless of the new configurability: login success/failure and logout are never recorded; the self-service `GET/PUT /api/v1/me` endpoints record nothing; an admin editing an existing user via `PUT /api/v1/users/{id}` records nothing (only create/delete do today); permission/role changes record only a prose summary with no before/after diff.
- New read-access instrumentation across existing read handlers (inventory, reporting, classifier, rbac, code-manager, orchestrator), active only for a category whose configured level is `full`.
- The existing `internal/activity` Postgres-backed table and admin UI "recent activity" feed are **unchanged** - this is a new, separate, parallel emission path serving a different purpose (compliance-facing event stream vs. in-app operator convenience), not a replacement.

## Capabilities

### New Capabilities
- `audit-log-emission`: per-category-configurable, structured audit event emission (auth events, mutating actions, and optionally read access) to a log sink the operator controls - separate from and in addition to the existing in-app activity feed.

### Modified Capabilities
(none - existing capabilities' own behavior is unchanged; this change observes their existing actions rather than altering what they do. See design.md for how emission is wired into existing handlers without changing their specs.)

## Impact

- New package (e.g. `internal/auditlog`) providing the level-aware, structured emitter, analogous in shape to `internal/activity`'s existing injected-closure pattern (`recordActivity func(...)`) so consuming packages don't import it directly.
- New `internal/runtime.Config` fields and `CONSOLE_AUDIT_*`-prefixed environment variables: one level knob per category, plus an optional output-file-path override (default stdout).
- Call-site changes across `internal/rbac` (login/refresh/logout, `getMe`/`updateMe`, `updateUser`), `internal/classifier`, `internal/inventory`, `internal/reporting`, `internal/codemanager`, `internal/orchestrator` handlers - both wiring the new emitter alongside existing `recordActivity` calls, and adding it to previously-uninstrumented write and read call sites.
- `cmd/console/main.go` wiring for the new emitter and per-category level configuration.
- No database schema changes - this path never persists to Postgres by design.

## Why

Right now, nothing in the console records who did what, or when. An admin
granting themselves `rbac:admin`, a group change that suddenly reclassifies
every Linux node, an OIDC login silently picking up a new role from a group
claim - none of it is visible after the fact. `architecture-summary.md`
already commits to this as a core design piece ("Internal audit/activity
event bus... the activity log is a single subscriber persisting to one
table"); `phased-build-plan.md`'s Phase 4 is next in the build order and
low-effort given Phase 0's NATS bus already exists.

## What Changes

- Add an `activity` capability: a Postgres-backed activity log, a single
  NATS subscriber that persists every published activity event to it (one
  subject, one table - no service writes its own audit entries directly),
  and a permission-gated API/UI to browse the history (most recent first,
  filterable by category).
- Retrofit `classifier` (node group create/update/delete) and `rbac` (user
  create/delete, role create/update/delete, role assign/unassign, service
  token create/revoke, and OIDC user provisioning/role reconciliation) to
  publish an activity event on every one of those actions.
- Add a new `activity:read` permission, granted to the bootstrap admin role
  like every other fixed permission.
- Add an "Activity" link to primary navigation (already-established pattern
  from the `console-navigation-and-admin-ui` change) - no spec change to
  `web-shell` needed, since its navigation requirement is already generic
  ("links... between the console's top-level sections").

Publishing stays decoupled from the activity log itself: `classifier` and
`rbac` publish to a shared NATS subject through a small injected
publisher/actor-lookup function (mirroring how `authorize` is already
injected into every package's `Register`), not by importing `internal/rbac`
or `internal/activity` directly - preserving this project's existing
loose-coupling-between-capabilities convention.

## Capabilities

### New Capabilities
- `activity`: persists and exposes an audit trail of meaningful actions
  taken through the console (who, what, when).

### Modified Capabilities
- `classifier`: node group create/update/delete now publish an activity
  event.
- `rbac`: user, role, and service token mutations, and OIDC user
  provisioning/role reconciliation, now publish an activity event.

## Impact

- New Postgres migration: `activity_log` table.
- New `internal/activity` package: NATS event envelope + publish helper,
  Postgres-backed store, subscriber (started at boot, before HTTP serving
  begins - same ordering `Revoker.Start` already relies on so no published
  event is missed), and `GET /api/v1/audit-log` handler.
- `internal/classifier`, `internal/rbac`: each mutating handler publishes
  an event after a successful write; `cmd/console/main.go` wires the
  publisher/actor-lookup into both, alongside the existing `authorize`
  wiring.
- `cmd/console/main.go`: add `activity:read` to `allPermissions`.
- `frontend/src/activity.html`/`activity.js`: new page; nav link added to
  every existing page's header (same duplication pattern as the Admin
  link).

## 1. Schema

- [x] 1.1 Add a migration for `activity_log` (id, occurred_at, category,
      action, actor, summary) and verify it applies cleanly and is a
      no-op on rerun

## 2. activity: event envelope, store, and subscriber

- [x] 2.1 Define the `Event` envelope and `activity.events` NATS subject
      in a new `internal/activity` package, and verify round-trip
      JSON encoding/decoding
- [x] 2.2 Implement `Store.RecordEvent` (persist one event to Postgres)
      and `Store.ListEvents` (most recent first), and verify both
      against a real Postgres instance
- [x] 2.3 Implement a subscriber (`Recorder.Start`, mirroring
      `rbac.Revoker.Start`'s ordering) that subscribes to
      `activity.events` and persists every received event, and verify
      with a real embedded NATS bus: publishing an event makes it appear
      in `Store.ListEvents` without any direct store call
- [x] 2.4 Implement `Publisher` (wraps a `messaging.Bus`, exposes
      `Publish(category, action, actor, summary string)`, logs and swallows
      publish errors rather than returning them), and verify a publish
      failure (e.g. a closed bus) is logged, not returned as an error to
      the caller

## 3. activity: API

- [x] 3.1 Implement `GET /api/v1/audit-log` (most recent first), gated by
      a new `activity:read` permission, and verify via real HTTP request
      against a live console that it returns real persisted events and
      rejects a request without `activity:read`
- [x] 3.2 Add `activity:read` to `cmd/console/main.go`'s
      `allPermissions` (bootstrap admin role), and verify a freshly
      bootstrapped admin can call the activity endpoint

## 4. classifier: publish activity events

- [x] 4.1 Add a `recordActivity func(r *http.Request, action, summary string)`
      parameter to `classifier.NewHandlers`, call it after a successful
      create/update/delete in `createGroup`/`updateGroup`/`deleteGroup`,
      wire a real `activity.Publisher`-backed closure in
      `cmd/console/main.go`, and verify via real HTTP requests against a
      live console: creating, updating, and deleting a group each
      produce a matching entry in `GET /api/v1/audit-log`

## 5. rbac: publish activity events

- [x] 5.1 Add the same `recordActivity` parameter to `rbac.NewHandlers`,
      call it after a successful create/delete in `createUser`/
      `deleteUser`, create/update/delete in `createRole`/`updateRole`/
      `deleteRole`, assign/unassign in `assignRole`/`unassignRole`, and
      create/revoke in `createServiceToken`/`revokeServiceToken`, and
      verify via real HTTP requests against a live console that each
      action produces a matching activity entry with the acting
      administrator as actor
- [x] 5.2 Call `recordActivity` (actor `"system"`) from
      `OIDCService.HandleCallback` when a new user is provisioned and
      when role reconciliation actually changes an assignment (skip
      publishing when reconciliation is a no-op), and verify via a real
      browser-driven OIDC login against the test provider
      (`make oidc-up`) that first-login provisioning and a subsequent
      claim-driven role change each produce a matching activity entry

## 6. Frontend: activity page

- [x] 6.1 Add `activity.html`/`activity.js` listing recent events
      (timestamp, category, action, actor, summary), gated by
      client-side `activity:read` redirect (matching the existing admin
      pages' pattern), and verify via browser it renders real recorded
      events from a live console
- [x] 6.2 Add an "Activity" nav link to every existing page's header
      (same pattern as the Nodes/Groups/Admin links), and verify via
      browser it's reachable from every page

## 7. Final verification

- [x] 7.1 Verify, with a token lacking `activity:read`, that
      `GET /api/v1/audit-log` is rejected and direct navigation to
      `/activity.html` redirects away client-side
- [x] 7.2 Verify the full loop end to end: perform one action in each
      instrumented category (create a group, assign a role) through the
      real UI and confirm both appear in the activity UI in the correct
      order without a restart
- [x] 7.3 `gofmt -l .`, `go vet ./...`, `make test` all pass with no
      regressions

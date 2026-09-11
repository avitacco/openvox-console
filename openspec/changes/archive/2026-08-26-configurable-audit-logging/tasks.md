## 1. `internal/auditlog` foundations

- [x] 1.1 Define `Category` (nodes, classifier, rbac, auth, code, orchestrator) and `Level` (off, writes, full, with an ordering so `full` satisfies a `writes` check) types - verify with a unit test asserting `Level` ordering (`full` >= `writes` >= `off`)
- [x] 1.2 Define the `Event` struct (action, actor, category, resourceType, resourceId, before, after) and a function producing its `slog` attributes in the fixed key set from design.md - verify with a unit test asserting the emitted JSON has exactly those keys plus `slog`'s own `time`/`level`/`msg`
- [x] 1.3 Implement `Config` (per-`Category` `Level` map) and `NewEmitter(cfg Config, logger *slog.Logger) *Emitter` - verify with a unit test constructing an `Emitter` from a `Config` with mixed levels per category
- [x] 1.4 Implement `Emitter.Write(category, event)` (emits when level is `writes` or `full`) and `Emitter.Read(category, event)` (emits only when level is `full`) - verify with table-driven unit tests covering all nine (category level x call type) combinations against a test `slog.Handler` that records what was logged

## 2. Configuration

- [x] 2.1 Add `CONSOLE_AUDIT_NODES`, `CONSOLE_AUDIT_CLASSIFIER`, `CONSOLE_AUDIT_RBAC`, `CONSOLE_AUDIT_AUTH`, `CONSOLE_AUDIT_CODE`, `CONSOLE_AUDIT_ORCHESTRATOR` to `internal/runtime.Config`, parsed to `auditlog.Level`, each defaulting to `writes`, rejecting an unrecognized value with a clear config error - verify with unit tests covering the default, an explicit valid value, and an invalid value for each
- [x] 2.2 Add `CONSOLE_AUDIT_LOG_PATH` (optional; unset means stdout) to `internal/runtime.Config` - verify with a unit test covering both the unset (stdout) and set cases

## 3. Console wiring

- [x] 3.1 In `cmd/console/main.go`, open the configured audit output (stdout by default, the configured file path otherwise) and build the `*auditlog.Emitter` - verify `go build ./...` succeeds and the console starts with no audit config set (defaults apply)
- [x] 3.2 A misconfigured `CONSOLE_AUDIT_LOG_PATH` (unwritable path) logs a warning and falls back to stdout rather than failing startup, matching the project's "never fatal at startup" posture - verify with a test or manual run pointing the path at a non-writable location and confirming the console still starts

## 4. Auth category (login/logout)

- [x] 4.1 Emit a `user.login.success` write event on successful password login and a `user.login.failure` write event on a failed attempt (recording the attempted username, not the password) in `internal/rbac.AuthService.Login` - verify with unit tests for both outcomes against a test emitter
- [x] 4.2 Emit a `user.login.success` write event on successful OIDC login in `OIDCService.HandleCallback` - verify with a unit test
- [x] 4.3 Emit a `user.logout` write event in the `logout` handler - verify with a unit test

## 5. RBAC/users category

- [x] 5.1 Wire the emitter alongside every existing `recordActivity` call site in `internal/rbac` (user create/delete, role create/update/delete, role assignment/unassignment, service token create/revoke) so each also produces a `writes`-level audit event - verify with unit tests asserting both the existing activity publish and the new audit emission happen together
- [x] 5.2 Add before/after permission values to the role-updated audit event (the existing `internal/activity` summary stays prose-only; the audit event carries the structured diff) - verify with a unit test asserting `before`/`after` reflect the actual permission sets
- [x] 5.3 Emit a `user.profile.updated` write event from `updateMe`, and an `admin.user.updated` write event from the admin `updateUser` handler (both currently unlogged anywhere) - verify with unit tests for both, asserting no password value appears in the emitted event
- [x] 5.4 Add read-level instrumentation (`Emitter.Read`) to `listUsers`, `getUser`, `listRoles`, `getRole`, `listServiceTokens`, and the classifier group read handlers's non-classifier counterparts as applicable - verify with a unit test confirming no event is emitted when the category is `writes` and one is emitted when `full`

## 6. Classifier/groups category

- [x] 6.1 Wire the emitter alongside existing group create/update/delete `recordActivity` call sites - verify with unit tests
- [x] 6.2 Add read-level instrumentation to `listGroups`, `getGroup`, and `groupnodes`'s matching-nodes endpoint - verify with a unit test confirming the full-vs-writes boundary

## 7. Nodes/inventory category

- [x] 7.1 Add read-level instrumentation to `internal/inventory`'s node list/detail (facts) handlers and `internal/reporting`'s report/event handlers - previously entirely uninstrumented by design (phase-4 excluded read-only capabilities) - verify with unit tests confirming events appear only at `full` and carry the viewed certname/report identifier

## 8. Code deploys category

- [x] 8.1 Wire the emitter alongside existing deploy-triggered/succeeded/failed `recordActivity` call sites in `internal/codemanager` - verify with unit tests
- [x] 8.2 Add read-level instrumentation to the deploy history list/detail handlers - verify with a unit test confirming the full-vs-writes boundary

## 9. Orchestrator jobs category

- [x] 9.1 Wire the emitter alongside existing job-triggered/completed `recordActivity` call sites in `internal/orchestrator` - verify with unit tests
- [x] 9.2 Add read-level instrumentation to `listJobs`/`getJob` - verify with a unit test confirming the full-vs-writes boundary

## 10. End-to-end verification

- [x] 10.1 Run the console with every category at its default (`writes`) and confirm, against real traffic, that mutating actions and login/logout produce audit events while ordinary reads do not
- [x] 10.2 Raise one category to `full` and confirm reads in that category now produce events while every other category's behavior is unchanged
- [x] 10.3 Set `CONSOLE_AUDIT_LOG_PATH` to a real file and confirm audit events land there and no longer appear mixed into the console's ordinary stdout log output
- [x] 10.4 Run `make test` and confirm the full suite passes, and `gofmt -l .` / `go vet ./...` are clean

## 1. The model and its storage

- [x] 1.1 Migration 000034: `task_patterns text[] NOT NULL DEFAULT '{}'` on `roles` and `service_tokens`; down migration. Verify by applying up, down and up against the dev database with a migration test, and that existing rows read as an empty list
- [x] 1.2 New package `internal/taskscope`: pattern validation (task name, `module::*`, `*`; 20 patterns of 64 characters), pattern matching, `Grant`, and `Scope` (grants resolved to node sets, with `Allows(task)` and `NodesFor(task)`); add `nodescope.Scope.Union`. Verify with table tests for every pattern scenario under "A role carries a task allowlist", including `patch::*` against `patch`, `patching::x`, `patch2::x` and `Patch::x`
- [x] 1.3 `taskscope.Scope.NodesFor` pairing: a table test over the two-role example in "An allowlist applies on the role's own node groups", a role with no node groups, a role with an empty list, an unrestricted scope, and a fleet-wide grant
- [x] 1.4 Roles: `Role.TaskPatterns`, stored and read with the role, validated on create and update (400 naming `taskPatterns`), an omitted field on update keeping the stored list. Verify with handler tests for each scenario under "A role carries a task allowlist"
- [x] 1.5 Service tokens: the same field set at creation and returned with the token. Verify with handler tests for "A service token may carry a task allowlist" and that a token created without one reads as empty

## 2. Tokens and the request's task scope

- [x] 2.1 `Store.UserTaskGrants`: one grant per role carrying `orchestrator:run` (an empty list as `*`), and none at all when no such role has a list. Verify with store tests against the test database, including a user with a restricted and an unrestricted role and a role without run
- [x] 2.2 `Claims.TaskGrants` written at issuance for users (login, refresh, OIDC) and for service tokens (one fleet-wide grant), omitted when unrestricted. Verify with token tests, and that a token issued without the field is read as unrestricted
- [x] 2.3 `internal/app`: a task scope function built from the request's claims and the node scope resolver, denying every task when there are no claims and not restricting a caller without `orchestrator:run`. Verify with unit tests for each case, including a read-only caller
- [x] 2.4 `scheduleAuthority` returns the node scope and the task scope together, from the owner's roles or the service token's list at that moment. Verify with tests for a user whose role is narrowed between two calls, a user who loses the permission, and a service token owner

## 3. Enforcement in the orchestrator

- [x] 3.1 `StartRequest.Tasks`, `TaskNotPermittedError` (task, step) and its `403` response with `task` and `step`; `IsRefusal` includes it. Verify with a response test for a task and for a plan step
- [x] 3.2 `Starter`: match the task's name before the catalogue is read, for `StartTask`, `CheckTask` and each task step of `StartPlan` and `CheckPlan`. Verify with tests for every scenario under "A task is started only if the caller may run it", including that a task that does not exist and one that is not allowed give identical refusals
- [x] 3.3 `Starter`: apply each task's node set to the resolved target: a node list outside it refused naming the nodes (naming the step in a plan), a query or group narrowed, narrowing to nothing starting nothing, and a plan narrowed to the intersection over its task steps. Verify with tests for "Nodes the task is not allowed on", "A query or group is narrowed", "Nodes outside a step's task scope" and "A query is narrowed for every step"
- [x] 3.4 `CheckPlanTemplate` takes the caller's task scope and checks names only. Verify with a test that a saved-plan check refuses a step by name and passes a run step
- [x] 3.5 Handlers: `SetTaskScope` on the orchestrator handlers, and an audit entry `task.denied` (actor, task, step, no parameters) for a refused task or plan start. Verify with handler tests on the recorded entry, asserting no parameter value appears in it
- [x] 3.6 Task catalogue: `SetTaskScope` on its handlers; the list and the details route offer only tasks the caller may run and answer others as unknown, for a caller holding `orchestrator:run`. Verify with tests for "The picker", "A task not offered" and "A read-only caller"

## 4. Saved plans and schedules

- [x] 4.1 `internal/plans`: saving checks task steps against the saver's scope, and reading marks a step the reader may not run in `problem` while still showing it. Verify with tests for each scenario in the `plans` delta
- [x] 4.2 `internal/scheduler`: saving and replacing check the saver's task scope; firing checks the owner's current scope and records a skipped firing naming the task and step. Verify with tests for each scenario in the `scheduled-jobs` delta, including a plan schedule and another caller replacing a schedule
- [x] 4.3 Role and service token audit payloads carry the allowlist, a role change before and after. Verify with tests on the recorded entries, including "A role is narrowed"
- [x] 4.4 Wiring and guard: `internal/app` calls `SetTaskScope` on the orchestrator, task catalogue, saved plan and schedule handlers, and a test fails for any handler set that starts, saves or lists tasks without it. Verify by removing one call locally and seeing the test fail
- [x] 4.5 Cluster test in `internal/app`: a user restricted on one instance is refused the same task on another instance. Verify beside the existing two-instance tests
- [x] 4.6 Node-scope sweep: add a case to the sweep test that a caller allowed a task on one tier cannot run it by naming a node of another, and that existing role fixtures still pass. Verify with the sweep test

## 5. Frontend

- [x] 5.1 Role dialog and Roles page: a task allowlist field (one pattern per line, with the note that an empty list allows every task), a refused pattern shown on the field with the entry kept, and each role's list shown, an empty one as "Every task". Verify in the browser: add `patch::*`, a refused `pa*ch`, and an empty list
- [x] 5.2 Service token form and page: the same field at creation and the list shown with the token. Verify in the browser
- [x] 5.3 Task picker, plan editor and schedule form show only permitted tasks (no change to their code expected, since the server filters), and a refused start shows the task, and the step, in the existing error display. Verify in the browser as a restricted operator: the picker, a refused plan, and a saved plan with a step the reader may not run
- [x] 5.4 Extract and merge strings; translate all 15 languages. Verify with `go run ./i18n status` reporting every language complete and the frontend tests

## 6. Documentation

- [x] 6.1 `use/access.md`: the allowlist, the patterns, how a user's roles combine and the node group pairing with the two-role example, service token lists, that existing roles are unaffected, and the proxy note about token size. Verify with `make marketing-build`
- [x] 6.2 `use/run-jobs.md`, `use/run-plans.md`, `use/schedule-jobs.md`: what a refused task looks like, what a schedule does when its owner loses a task, and what a reader sees on a plan with a task they may not run. Verify with `make marketing-build`
- [x] 6.3 `operations.md` and `architecture-summary.md`: the columns, the token claim, `task.denied`, and where the check lives. Verify the names match the code

## 7. Verification

- [x] 7.1 `go vet ./...`, `gofmt`, `go test ./...` with the database configured, the frontend tests and `openspec validate add-task-permissions --strict` all pass
- [x] 7.2 Live on the dev stack with the test node: a user holding two roles (`patch::*` on one group, every task on another) is refused a task on the first group and allowed it on the second, by node list, by group and in a plan; a schedule is skipped after its owner's role is narrowed; a service token limited to `deploy::*` is refused another task. Record results and screenshots under `evidence/`

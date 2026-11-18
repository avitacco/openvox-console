## 1. Queries

- [x] 1.1 `internal/openvoxdb`: a typed error for a query openvoxdb rejects (4xx, carrying its message), distinct from unreachable; `Query` returns it. Verify with `httptest` tests for 400, 500 and a refused connection
- [x] 1.2 Query shape checks: empty, over 4,000 characters, AST form, and `HasSubquery` (more than one `{` outside string literals, honouring escaped quotes). Verify with table tests covering both subquery forms, braces inside strings and regex literals, and single-entity queries
- [x] 1.3 Confirm against the dev openvoxdb that the two subquery forms and `inventory[certname] { facts.os.family = … }` behave as the design assumes, and record the responses used as fixtures in 1.1

## 2. Target resolution

- [x] 2.1 `orchestrator.Target` and `TargetSource`; "exactly one of nodes, query, group" validation. Verify with unit tests for each combination
- [x] 2.2 `orchestrator.Resolve(ctx, target, scope)`: node list all or nothing; query run with a 30-second timeout, rows reduced to distinct sorted certnames, a row without `certname` refused; group resolved through an injected group resolver, missing group refused; query and group results intersected with scope; subquery refused for a scope that is not everything, before openvoxdb is called. Verify with tests using fake query and group sources, one per scenario in the two delta specs
- [x] 2.3 Migration: `target_source`, `schedule_id`, `schedule_name` on `jobs`, plus the two schedule tables from the design, with a down migration. Verify by applying it up, down and up again against the dev database with the existing migration tests
- [x] 2.4 Store: write and read `targetSource` and `schedule` on jobs (list and detail); `GetJobInScope` blanks the query text when targets are hidden. Verify with store tests, including a job whose schedule was deleted

## 3. Starting jobs

- [x] 3.1 Extract `StartRun` and `StartTask` from the HTTP handlers, with typed errors for out of scope, invalid target and no nodes matched; handlers map them to today's statuses. Verify that the existing handler, scope and run-option tests pass unchanged
- [x] 3.2 `query` and `group` on `POST /orchestrator/runs` and `/tasks`; empty resolution is a 400. Verify with handler tests for every scenario under "Targets chosen by a query", "A query that cannot be used is refused", "Targets chosen by a node group" and "A target that resolves to no nodes starts nothing"
- [x] 3.3 `POST /api/v1/orchestrator/targets/resolve`. Verify with handler tests: a scoped preview lists in-scope nodes only, an empty result is a 200, a caller with read only is rejected
- [x] 3.4 Wire the group resolver and the openvoxdb query into the orchestrator in `internal/app`; add the new route to the node-scope sweep as an action. Verify the sweep test passes and fails when the route is removed from it

## 4. Schedules: storage and rules

- [x] 4.1 `internal/scheduler` types and store: create, replace, get, list (all or by owner), delete, pause, resume; firings insert, finish and trim to 100. Verify with store tests against the test database
- [x] 4.2 Next-start arithmetic: one-off, repeating from a future start, repeating from a past start, each unit, the five-minute minimum, resume of a repeating and of a passed one-off. Verify with table tests on a fixed clock, including an interval spanning a daylight-saving change in UTC terms
- [x] 4.3 Save-time validation: the dry run of authority, target and action with the caller's scope, accepting an empty resolution; schedule field checks; plans refused. Verify with tests for each scenario under "A schedule is checked when it is saved" and "Start times and repetition"
- [x] 4.4 Sensitive parameters: seal on save bound to the schedule id, refuse without a secrets key, keep on replace only for the same owner and unchanged task, redacted on every read. Verify with tests for the four scenarios, and that a sealed value copied to another schedule's row fails to open

## 5. Schedules: firing

- [x] 5.1 Claim: the compare-and-set update plus the `firing` row in one transaction, judged by the database clock. Verify with a test running many concurrent claimers against one due schedule and asserting exactly one wins
- [x] 5.2 Owner authority lookup in `internal/app` over `internal/rbac`: user and service token, permission and scope as they are now. Verify with tests for a user who lost the permission, a deleted user, a revoked token, and a user whose scope narrowed
- [x] 5.3 The firing steps: late, overlap, authority, target, action, start; each outcome written with its reason; the job carries the schedule and the owner as "triggered by". Verify with tests, on a fake clock and fake dispatcher, for every scenario under "A firing is a request made at that moment", "A firing that cannot start is recorded, not retried", "A late firing is missed" and "A schedule fires with its owner's current scope"
- [x] 5.4 The `orchestrator-scheduler` worker: 20-second tick, added to the run-mode surface beside the dispatcher, started only with migrations applied. Verify with the run-mode surface tests and a test that an instance without the worker fires nothing
- [x] 5.5 Activity and audit records for create, change, pause, resume, delete, skipped and missed; the job-start summary names the schedule. Verify with tests on the recorded entries
- [x] 5.6 Cluster test: two app instances on one database, one due schedule, exactly one job. Verify in `internal/app` beside the existing cluster tests

## 6. Schedules: API

- [x] 6.1 The seven routes from the design, with `orchestrator:read` and `orchestrator:run`; visibility by owner or fleet-wide scope; 404 for everything else; saving takes ownership, pause and resume do not. Verify with handler tests for each scenario under "Schedules are limited to their owner and fleet-wide callers", "Pausing and resuming" and "Changing a schedule"
- [x] 6.2 Schedule responses: contents, owner, derived state, next start, recent firings with outcome (including `interrupted`), reason and job. Verify with handler tests
- [x] 6.3 Register the routes in `internal/app` and the node-scope sweep. Verify the sweep test passes

## 7. Frontend

- [x] 7.1 `target-picker.js`: node list, query, node group; preview with count and list; refusal shown beside the field. Verify in the browser against the dev stack for each kind, as a fleet-wide and as a scoped user
- [x] 7.2 Run Puppet and Run task buttons on the Jobs page (hidden without `orchestrator:run`), opening the dialogs with the picker; existing entry points unchanged. Verify in the browser, including that the node, selection and group dialogs look as before
- [x] 7.3 Extract the task environment/task/parameter form from `run-task.js` for reuse. Verify the Run task dialog still validates and marks fields as before
- [x] 7.4 Scheduled jobs list, schedule detail with firings and pause/resume/delete, and the schedule form (action, target, start, repetition, local times with the zone, the "will run as you" notice); sub-navigation between Jobs and Scheduled jobs. Verify in the browser: create, edit, pause, resume, delete, a refused save keeping what was typed
- [x] 7.5 "Schedule instead" from the run and task dialogs, carrying the filled-in action and target. Verify from a group's page
- [x] 7.6 Job list and job page: query or group shown, "Scheduled" badge linking to the schedule, hidden query for a partly hidden job. Verify in the browser
- [x] 7.7 Extract and merge strings; translate German; report the count outstanding in the other locales. Verify with `go run ./i18n status` and the frontend tests

## 8. Documentation

- [x] 8.1 `marketing/guides/use/run-jobs.md`: targets by query and by node group, the preview, starting from the Jobs page, API examples. Verify with `make marketing-build`
- [x] 8.2 New `marketing/guides/use/schedule-jobs.md`: creating a schedule, repetition and time zones, what skipped, missed and interrupted mean, ownership, sensitive parameters and the secrets key, the API; linked from the guides index and from run-jobs. Verify with `make marketing-build`
- [x] 8.3 `marketing/guides/use/access.md`: narrowing of queries and groups, the subquery rule, who sees and changes a schedule, and that a schedule runs with its owner's current access. Verify with `make marketing-build`
- [x] 8.4 Run-modes guide, `operations.md` and `architecture-summary.md`: the scheduler worker, at-most-once firing, the lateness limit. Verify the worker name matches `internal/app/surface.go`

## 9. Verification

- [x] 9.1 `go vet ./...`, `gofmt`, and `go test ./...` with the database configured all pass; `openspec validate add-pql-targeting-and-scheduled-jobs --strict` passes
- [x] 9.2 Live on the dev stack with the test node: a run by query and by group; a scoped user's query narrowed and their subquery refused; a schedule a few minutes ahead fires once with two console instances running; a schedule left to pass while the console is stopped for over fifteen minutes is recorded as missed; an owner whose role is removed is skipped; a scheduled task with a sensitive parameter runs and shows redacted. Record the results and screenshots under `evidence/`

## 1. Plan start

- [x] 1.1 Extend `PlanStep` with `environment`, `timeoutSeconds` and `options`; a task step without an environment uses the default. Verify with unit tests on decoding today's request shape and the new one
- [x] 1.2 `Starter.CheckPlan` and `Starter.StartPlan` over `PlanRequest`: target resolved once, each step checked with the run and task checks, a refused step wrapped with its index; 1 to 20 steps. Verify with tests for every scenario under "Task and plan execution" and "A plan's target is scoped like a run's"
- [x] 1.3 `POST /api/v1/orchestrator/plans` becomes a caller of `StartPlan`, accepts `query` and `group`, and answers a refused step with `step`. Verify the existing plan handler and scope tests pass unchanged, plus new handler tests for query, group and a bad third step
- [x] 1.4 Run steps are dispatched with their run options and task steps with their environment and timeout. Verify with dispatcher tests on the requests sent to the node
- [x] 1.5 `completePlan` closes the steps after a failed one as not run, in one transaction with the plan's completion. Verify with tests for both scenarios under "A failed step ends the plan", including that nothing is dispatched for a closed step
- [x] 1.6 Job reads: `steps` on a plan (with derived `state`, filtered to scope), `plan` and position on a step, `parent_job_id IS NULL` in the list with a `parent` filter. Verify with store and handler tests for "A plan's job is read with its steps" and the existing "Plan steps" scope scenario
- [x] 1.7 Plan jobs record `target_source` and the schedule; activity summaries name them. Verify with tests on the stored job and the recorded entries

## 2. Saved plans

- [x] 2.1 Migration 000032: `plans` table, `scheduled_jobs.plan_name` and `steps`, `kind` check widened; down migration. Verify by applying up, down and up against the dev database with the migration tests
- [x] 2.2 `internal/plans` store: create, replace, get, list, delete; unique-name error typed. Verify with store tests against the test database
- [x] 2.3 Service: step validation on save through the starter's checks with sensitive-required parameters exempt; sensitive values stripped; `needs` and `problem` computed on read. Verify with tests for each scenario under "A saved plan's steps are checked when it is saved" and "Sensitive values are not kept in a saved plan", including that the stored row holds no sensitive value
- [x] 2.4 Handlers for the five routes with `orchestrator:read` and `orchestrator:run`; refusals carry `step` and `fields`. Verify with handler tests for each scenario under "Saved plans"
- [x] 2.5 Activity and audit entries for created, changed, deleted. Verify with tests on the recorded entries
- [x] 2.6 Register in `internal/app` (route group, surface) and add the routes to the node-scope sweep as not node-scoped, with a check that a scoped caller lists every plan. Verify the sweep and surface tests pass

## 3. Scheduled plans

- [x] 3.1 Scheduler types and store: `kind: "plan"`, `planName`, `steps`; sealed blob keyed by step index. Verify with store tests, and that a sealed value copied to another schedule fails to open
- [x] 3.2 Save-time check through `CheckPlan`; keep-on-replace rule compares steps' tasks and environments; redaction on every read. Verify with tests for "Scheduling a plan" and "A sensitive value in a scheduled plan"; remove the test that plans are refused
- [x] 3.3 Firing through `StartPlan` with the owner's current scope; a step that can no longer run is a skipped firing naming the step; overlap rule on the plan job. Verify with tests for each scenario under "A scheduled plan keeps its own steps"
- [x] 3.4 Cluster test: two instances, one due plan schedule, exactly one plan job. Verify beside the existing schedule cluster test in `internal/app`

## 4. Account policy: rules

- [x] 4.1 Migration 000033: `account_policy` table (single row), `users.failed_logins`, `users.locked_until`; down migration. Verify up, down and up against the dev database
- [x] 4.2 Policy store and type with defaults when no row exists; range validation naming the field. Verify with tests for each scenario under "The account policy is stored and changed in the console"
- [x] 4.3 `rbac.CheckPassword`: minimum length in characters, the four kinds, 72-byte maximum, not the username ignoring case; returns every failed rule. Verify with table tests including multi-byte characters at both length limits
- [x] 4.4 Apply the check in create-user, administrator set-password and change-own-password; 400 with `fields` and `rules`. Verify with handler tests for each scenario under "Password rules for local accounts" and "Existing passwords are not rechecked"
- [x] 4.5 `bootstrapAdmin` refuses a password failing the default rules and logs the rules, never the password. Verify with tests for both scenarios under "The first administrator's password meets the rules"
- [x] 4.6 Lengthen any example, seed or harness password under 12 characters (`.env.example`, compose secrets, demo seed, screenshots script, test harness). Verify with `go test ./...` and by starting the dev stack from an empty database

## 5. Account policy: lockout

- [x] 5.1 Store: the atomic failed-login update, reset on success, unlock. Verify with tests on a controllable database clock for each scenario under "Login lockout", and a test with many concurrent failures asserting the count and a single lock time
- [x] 5.2 `AuthService.Login`: comparison always run, locked refusal, counting, reset; every refusal is `ErrInvalidCredentials`. Verify with a handler test comparing status and body for locked, wrong password and unknown username
- [x] 5.3 `PUT /api/v1/me`: a wrong current password counts, a change is refused while locked, the profile part still applies. Verify with tests for "Guessing the current password from an open session"
- [x] 5.4 Refresh, identity-provider login and service tokens are unaffected by a lock. Verify with tests for each scenario under "Lockout covers password login only"
- [x] 5.5 `POST /api/v1/users/{id}/unlock`; administrator set-password unlocks; `lockedUntil` on user responses. Verify with handler tests for "An administrator can unlock an account"
- [x] 5.6 `GET` and `PUT /api/v1/account-policy`. Verify with handler tests, including a non-administrator reading and being refused the change
- [x] 5.7 Audit and activity entries from decision 15. Verify with tests on the recorded entries, asserting no entry contains the password
- [x] 5.8 Cluster test: failed logins spread over two instances lock the account at the threshold. Verify in `internal/app`

## 6. Frontend: plans

- [x] 6.1 Plans tab in the Jobs sub-navigation; `plans.js` list with step counts; controls hidden without `orchestrator:run`. Verify in the browser as an operator and as a reader
- [x] 6.2 `plan-edit.js`: add run and task steps with the existing option and task forms, move up and down, remove; a refusal shown on its step with everything kept. Verify in the browser: create, edit, reorder, a refused save
- [x] 6.3 `run-plan.js`: target chooser with preview, sensitive values per step, start opening the job page, "Schedule instead". Verify in the browser for a node list, a query and a group, as a fleet-wide and a scoped user
- [x] 6.4 Job page: a plan's steps with state, times and node results, refreshing while running; a step links back to its plan; "Not run" shown for closed steps. Verify in the browser with a succeeding and a failing plan
- [x] 6.5 Schedule form and schedule page for a plan action: copied steps shown read-only with the "own copy" note; sensitive values entered and shown redacted. Verify in the browser from "Schedule instead"

## 7. Frontend: account policy

- [x] 7.1 `account-policy.js` page in the administration navigation, restricted to `rbac:admin`; range errors shown on their fields. Verify in the browser
- [x] 7.2 `password-rules.js` used in the create-user, set-password and preferences forms: rules beside the field, failed rules after a refusal. Verify in the browser with a password failing two rules
- [x] 7.3 Users page: "Locked until" badge and Unlock. Verify in the browser by locking a test account
- [x] 7.4 Login page failure message. Verify in the browser
- [x] 7.5 Extract and merge strings; translate German; report the count outstanding in the other locales. Verify with `go run ./i18n status` and the frontend tests

## 8. Documentation

- [x] 8.1 New `marketing/guides/use/run-plans.md`: building a plan, what a step can be, running and watching one, what "Not run" means, sensitive values, scheduling and the schedule's own copy, the API, and the `web`-mode limitation; linked from the guides index, `run-jobs.md` and `schedule-jobs.md`. Verify with `make marketing-build`
- [x] 8.2 `use/schedule-jobs.md`: scheduling a plan. `use/run-jobs.md`: pointer to plans. Verify with `make marketing-build`
- [x] 8.3 `use/access.md`: password rules, lockout, unlocking, what lockout does not cover, what to do when the only administrator is locked, that saved plans are shared by everyone who can run jobs. Verify with `make marketing-build`
- [x] 8.4 `install/packages.md` and `install/containers.md`: the first administrator's password must meet the default rules, and what the log says when it does not. Verify with `make marketing-build`
- [x] 8.5 `operations.md` and `architecture-summary.md`: saved plans, scheduled plan copies, account policy storage and lockout counting. Verify the route and table names match the code

## 9. Verification

- [x] 9.1 `go vet ./...`, `gofmt`, `go test ./...` with the database configured, the frontend tests and `openspec validate add-plans-ui-and-account-policy --strict` all pass
- [x] 9.2 Live on the dev stack with the test node: build a plan in the editor and run it on a group; watch a plan succeed and a plan fail at its second step; a scoped user's plan on a query is narrowed; schedule a plan with a sensitive value and see it fire once with two instances; edit the saved plan and confirm the schedule still runs its copy. Record results and screenshots under `evidence/`
- [x] 9.3 Live: lock an account by failed logins spread over two instances; compare the three login responses byte for byte; wait out a short lock; unlock from the Users page; change the policy and see it apply on the other instance; start from an empty database with a short first-start password. Record results and screenshots under `evidence/`

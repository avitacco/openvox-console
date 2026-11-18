# Design

## Context

**Plans today.** `POST /api/v1/orchestrator/plans` takes `targets`,
`name` and `steps` (`kind`, `taskName`, `params`). The handler checks
scope on the node list, resolves every task step in the default
environment, and calls `Store.CreatePlan`, which inserts the plan row
and one child job per step, all with status `running` and their targets
`pending`. `Dispatcher.DispatchPlan` sends the first step;
`advancePlan` sends the next when a step succeeds and fails the plan
when one fails. The steps after a failed one are never closed. Plan
start is the one start path not moved into `orchestrator.Starter`, so it
has no query or group target and the scheduler refuses it. A job read
returns no steps, and the frontend shows only the plan's name.

**Accounts today.** `Store.CreateUser` and `UpdatePassword` hash
whatever they are given. `AuthService.Login` compares the password
(against a dummy hash for an unknown username, to keep timing equal),
audits success or failure, and the handler answers 401 `invalid
credentials` for every failure. `PUT /api/v1/me` checks the current
password before a change. The first administrator is created at start
by `bootstrapAdmin` only when the users table is empty. Identity
provider users get a random, unusable password hash.

**Constraints.** No internal HTTP hops; instances are stateless and
share Postgres; no new permissions (per-area permissions stay as they
are); user-facing flows must be reflected in the site guides.

## Goals / Non-Goals

**Goals:**
- Build, keep, run, watch and schedule a plan from the console.
- One start path for plans, shared by the API and the scheduler, with
  the target and scope rules runs and tasks already have.
- Password rules and login lockout that behave the same on every
  instance, are changed in the console, and are audited.
- Keep the login endpoint from revealing whether a username exists.

**Non-Goals:**
- Puppet-language or module plans, parallel steps, conditions, per-step
  targets, continuing past a failed step, retrying a step.
- Password expiry, history, forced change, multi-factor login, idle
  timeout, lockout by client address.
- Fixing jobs started through a `web`-mode instance (see Risks).

## Decisions

### Plans

**1. A saved plan is a stored template, not a new way to start.**
Table `plans` (`id`, `name` unique, `description`, `steps JSONB`,
`created_by`, `updated_by`, timestamps), package `internal/plans` with a
store, service and handlers:

| Route | Permission |
|---|---|
| `GET /api/v1/orchestrator/saved-plans` | `orchestrator:read` |
| `GET /api/v1/orchestrator/saved-plans/{id}` | `orchestrator:read` |
| `POST /api/v1/orchestrator/saved-plans` | `orchestrator:run` |
| `PUT /api/v1/orchestrator/saved-plans/{id}` | `orchestrator:run` |
| `DELETE /api/v1/orchestrator/saved-plans/{id}` | `orchestrator:run` |

Running one is the existing `POST /api/v1/orchestrator/plans` with the
plan's name and steps: the console reads the saved plan, adds the
sensitive values and the target, and posts. There is no "run by id"
route, so there is one start path and the caller always sends, and can
see, exactly what will run.
*Alternative:* `POST …/saved-plans/{id}/run`. Rejected: a second start
path, and the steps run would be whatever the row holds at that instant
rather than what the operator was shown.

**2. The step shape.** One type for requests, saved plans and
schedules:

```json
{"kind": "run",  "options": {"noop": true, "environment": "staging"}}
{"kind": "task", "taskName": "service", "environment": "production",
 "params": {"name": "nginx", "action": "restart"}, "timeoutSeconds": 300}
```

`taskName` and `params` are today's field names, so existing API callers
keep working; a task step without `environment` uses the default
environment as now. Limits: 1 to 20 steps, name up to 100 characters,
description up to 500.

**3. Plan start moves into `Starter`.** `PlanRequest{StartRequest, Name,
Steps}`, `Starter.CheckPlan` and `Starter.StartPlan`. Targets are
resolved once through the existing `TargetResolver` (so query, group,
narrowing, the subquery rule and "no nodes matched" come for free), then
each step is checked with the code behind `CheckRun` and `CheckTask`. A
refused step is wrapped as `step N (<what>): <reason>`, carrying the
step index so the API can answer with `step` beside `error` and
`fields`. The handler becomes a thin caller, as the run and task
handlers already are. The plan job records `target_source` and, when
fired by a schedule, the schedule, as other jobs do.

**4. Steps are closed when a plan ends early.** `completePlan` on
failure marks every step after the failed one: job status `failed`, each
target `failed` with the result message `not run: step N failed`, in one
transaction with the plan's own completion. A step read reports
`state: "not_run"` when its targets carry that marker; the job status
set in the database is unchanged (`running`, `succeeded`, `failed`), so
no constraint or reaper logic changes.
*Alternative:* a new `skipped` job status. Rejected: it widens a CHECK
constraint and every status switch in the store, dispatcher, reaper and
frontend for something only plan steps need.

Steps not yet reached in a running plan are `state: "waiting"`: today
they are stored as `running` from creation. The state is derived on
read (a step is waiting when the plan is running and an earlier step is
not finished); stored rows are not changed.

**5. A plan read carries its steps; the list does not carry steps as
rows.** `GET …/jobs/{id}` for a plan adds `steps`: each with `id`,
`stepOrder`, `kind`, `taskName`, `environment`, run options, `state`,
times and targets, filtered to the caller's scope exactly as
`GetJobInScope` filters a job today (a step with no node in scope is
omitted, per the existing "Plan steps" scenario). `ListJobs` adds
`parent_job_id IS NULL` unless a `parent` filter is given. A step's own
read gains `plan: {id, name}` and its position.

**6. Sensitive values are stripped from a saved plan, by task
metadata.** On save, each task step is prepared as an on-demand task
would be, with one difference: a missing required parameter is allowed
when the task marks it sensitive. Values for sensitive parameters are
removed before the row is written. On read, each task step lists
`needs`: the sensitive parameters of its task and whether each is
required, taken from the task catalogue at read time. A task that has
since gone is reported on the step (`problem`) rather than failing the
read.
*Alternative:* seal them with the secrets key, as schedules do.
Rejected: a saved plan is shared by everyone with `orchestrator:run`,
so a stored secret would be usable by people who never knew it.

**7. A scheduled plan is a copy.** `scheduled_jobs` gains `plan_name`
and `steps JSONB`; `kind` accepts `plan`. The schedule form, opened from
a saved plan, copies its steps. The schedule does not reference the
saved plan.
*Why:* a schedule fires with its owner's access. If it followed the
saved plan, anyone with `orchestrator:run` on one node group could edit
a plan that an administrator scheduled fleet-wide and so run any task
anywhere. A copy closes that. It also removes the "plan was deleted"
case.
Sensitive values: one sealed blob per schedule, as today, now holding a
map of step index to that step's sensitive parameters. The existing
keep-on-replace rule (same owner, same action) compares the steps'
tasks and environments in order.
Save-time check and firing use `Starter.CheckPlan` and `StartPlan`; the
overlap rule ("a job of this schedule is still running") already works
on the plan job.

**8. Frontend.** Sub-navigation becomes Jobs, Scheduled jobs, Plans.
New modules: `plans.js` (list), `plan-edit.js` (editor; one card per
step built from `createRunOptions` and `createTaskForm`, with up, down
and remove buttons, no drag and drop), `run-plan.js` (dialog: target
picker, one field per needed sensitive value labelled "Step N: task /
parameter", Start, Schedule instead through the existing
`schedule-draft.js`). `job.js` renders steps for a plan and re-reads the
job every few seconds while it is running, as it already does for other
jobs. `schedule-edit.js` and `schedule.js` gain the plan action: a
read-only list of the copied steps with a note that the schedule keeps
its own copy; steps are edited by re-creating the schedule from the
plan.

### Account policy

**9. The policy is one row in Postgres.** Table `account_policy` with a
single row (`id = 1` enforced by a CHECK): `min_length`,
`require_lowercase`, `require_uppercase`, `require_digit`,
`require_symbol`, `lockout_threshold`, `lockout_minutes`, `updated_by`,
`updated_at`. No row means the defaults, so the migration inserts
nothing and an untouched installation needs no write. It is read from
the database each time a password is set or a login fails; both are
rare, so there is no cache to invalidate across instances.
*Alternative:* environment variables. Rejected: every instance would
need the same values and a restart, and a change would leave no audit
entry.

Routes: `GET /api/v1/account-policy` (any authenticated caller: the
change-password form needs the rules), `PUT /api/v1/account-policy`
(`rbac:admin`), `POST /api/v1/users/{id}/unlock` (`rbac:admin`).

**10. Rules are checked in one function, `rbac.CheckPassword(policy,
username, password) []RuleFailure`**, called by the create-user,
set-password and change-own-password handlers and by `bootstrapAdmin`
(with the default policy when no row exists). Length is counted in
characters (runes) for the minimum and in bytes for the 72 maximum,
because bcrypt ignores everything after 72 bytes; today a longer
password errors or is silently cut, depending on the library version. A
"symbol" is any character that is not a letter, digit or space. A
refusal is a 400 with `error`, `fields: {password: …}` and `rules`, a
list of codes (`min_length`, `lowercase`, `uppercase`, `digit`,
`symbol`, `max_length`, `username`) the frontend turns into translated
text.

**11. Lockout state lives on the user row.** `users` gains
`failed_logins INT NOT NULL DEFAULT 0` and `locked_until TIMESTAMPTZ`.
A failed login runs one statement:

```sql
UPDATE users
   SET failed_logins = CASE WHEN locked_until IS NOT NULL AND locked_until <= now() THEN 1
                            ELSE failed_logins + 1 END,
       locked_until  = CASE WHEN locked_until > now() THEN locked_until
                            WHEN <new count> >= $threshold THEN now() + $period
                            ELSE NULL END
 WHERE id = $1 AND (locked_until IS NULL OR locked_until <= now())
RETURNING failed_logins, locked_until
```

so counting is atomic across instances, uses the database clock, and an
attempt during a lock changes nothing. A success sets both back to zero
and null. Unlock and an administrator's password set do the same.

**12. Login order, and what is revealed.** Look up the user; always run
the bcrypt comparison (dummy hash for an unknown name, real hash
otherwise, including when locked) so timing does not show the lock; if
locked, refuse and audit `user.login.locked`; if the password is wrong,
count it; otherwise reset and issue tokens. All refusals return the
existing `ErrInvalidCredentials`, so the handler's 401 body is
byte-for-byte the same.
*Alternative:* tell the user "your account is locked until 14:05".
Friendlier, and what Puppet Enterprise does, but it tells an attacker
which usernames exist, which the `rbac` spec forbids. The login page
instead says, on every failure, that accounts lock for a time after
repeated attempts.

**13. Temporary lock, with unlock.** Puppet Enterprise locks until an
administrator resets the password. Here the lock expires, because a
permanent lock lets anyone who knows the administrator's username shut
them out with ten requests. With a 15-minute lock an attacker gets at
most 40 guesses an hour per account, and the owner gets back in without
help.

**14. `PUT /api/v1/me` joins the same counter.** A wrong current
password counts as a failed login and the change is refused while
locked; otherwise a stolen session could guess the password without
limit. The profile part of that request is unaffected.

**15. Audit and activity.** Audit actions: `user.locked`,
`user.unlocked`, `user.login.locked`, `user.password.rejected` (with the
rule codes), `account_policy.updated` (old and new values). Activity
events: `user.locked`, `user.unlocked`, `account_policy.updated`.
`user.login.failure` stays as it is.

**16. Frontend.** New `account-policy.js` page under the administration
navigation. A shared `password-rules.js` fetches the policy and renders
the rule list beside a password field and the failed rules after a
refusal; used in `users.js` (create, set password) and
`preferences.js`. `users.js` shows a "Locked until …" badge and an
Unlock button. `login.js` changes its failure message.

## Risks / Trade-offs

- **An attacker can keep a known account locked** by failing ten logins
  every fifteen minutes. → The lock is temporary, identity-provider
  login is unaffected, another administrator can unlock, and the
  threshold can be set to 0. Lockout by client address is left out
  because the console usually sits behind a proxy.
- **If the only administrator is locked out and nobody else can
  unlock,** they wait out the period. → Stated in the guide. No
  command-line unlock is added.
- **The login message no longer says only "wrong username or
  password".** → Accepted as the cost of not revealing accounts.
- **First-start password refused.** A new installation with a short
  `CONSOLE_BOOTSTRAP_ADMIN_PASSWORD` starts with no administrator. →
  The log line names the rule; install guides and example files are
  updated; upgrades are untouched because the bootstrap runs only on an
  empty users table.
- **A saved plan can go stale** when a task or environment it names is
  removed. → Reported on the step when the plan is read, and refused
  with the step named when it is run.
- **Jobs started through a `web`-mode instance never complete**, which
  now also affects plans started from the Plans page in a split
  deployment. This fault predates this change and is recorded in
  `operations.md`. → Not fixed here; it needs its own change (hand the
  job to an instance running the dispatcher). Flagged in the Run plans
  guide.
- **The job list changes for API callers** that relied on plan steps
  appearing as rows. → The `parent` filter returns them.
- **Twenty steps of large task parameters** make a wide JSONB row. →
  Bounded by the step limit and the task parameter size limit that
  already applies to each step.

## Migration Plan

- `000032_plans`: `plans` table; `scheduled_jobs.plan_name`, `steps`;
  widen the `scheduled_jobs.kind` check to include `plan`.
- `000033_account_policy`: `account_policy` table; `users.failed_logins`,
  `users.locked_until`.
- Both have down migrations. Rolling back 000032 with plan schedules
  present deletes those schedules (stated in the down file); rolling
  back 000033 drops the policy and all lock state.
- No data backfill. Existing passwords are untouched.

## Open Questions

- None blocking. The `web`-mode fault above is the natural next change.

## Implementation Notes

Where the build differs from, or adds to, the decisions above.

- **Decision 8 was wrong about the job page.** It did not refresh by
  itself before this change. It now re-reads any job that is still
  running every three seconds, and redraws only when something changed,
  so an opened result stays open.
- **A step that mixes a run's fields with a task's is refused**
  (`taskName` on a run step, `options` on a task step), so nothing a
  caller wrote on a step is silently ignored. Not in decision 2.
- **A listed plan shows its nodes.** A plan row has no targets of its
  own, so the job list would have shown none; it now shows its first
  step's, narrowed to the reader's scope like any job's.
- **The Users page gained a Set password dialog.** The `rbac` delta
  requires the set-password form to show the rules, and the page had no
  such form: an administrator could set a password only through the API.
  It is also the way to let a locked-out user back in with a new
  password.
- **`POST /api/v1/users/{id}/unlock` returns the user**, not an empty
  204, so the page can redraw the row from the answer.
- **Task 4.6 found nothing to lengthen.** The demo seed, the test
  harness, the screenshots script and the development `.env` already use
  passwords of 12 characters or more; the compose stack reads the
  password from a secret file the operator writes. The install guides
  now state the minimum.
- **`internal/testapp.Config` gained `Log`**, so a test can read what an
  instance logs. Used by the first-start password test.
- **An identity-provider login was not testable before.** The lockout
  test for it needed a whole login, so `internal/rbac` now has a small
  fake OpenID Connect provider for tests (`oidc_lockout_test.go`).
- **Other locales:** German is complete (606 of 606). The other 14
  languages each lack 260 strings: 170 from before this change and 90
  new.

### Since this change was implemented

- The `web`-mode fault is fixed: `web` now runs `orchestrator-dispatcher`,
  so a job or plan started through a `web` instance is followed to its
  end. `web` also keeps a node connection registry of its own. See
  `operations.md`.
- The other 14 locales are translated (606/606 each). A runtime fault that
  left every multi-line `data-i18n` paragraph in English, in every
  language, was fixed with them (`frontend/src/i18n.js`).

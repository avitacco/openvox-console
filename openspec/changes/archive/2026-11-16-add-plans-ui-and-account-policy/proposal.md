## Why

Two gaps remain from the Puppet Enterprise comparison that this step closes.

**Plans.** The orchestrator can already run a plan (an ordered list of
Puppet runs and tasks) through `POST /api/v1/orchestrator/plans`, but
only through the API. Nothing in the console builds one, starts one or
shows its steps: a plan's job page shows a name and nothing else. A plan
also cannot be kept for reuse, cannot be scheduled, cannot be targeted
by query or node group, and its steps cannot carry run options or a task
environment, which runs and tasks gained in the last two steps.

**Account policy.** Any password is accepted for a local account,
including an empty one, and nothing limits how many times a password can
be guessed at the login endpoint. Puppet Enterprise has both a password
policy and a failed-attempt lockout.

## What Changes

### Plans

- **Saved plans.** A named, described, ordered list of steps kept in the
  console. A step is a Puppet run with its run options, or a task with
  its environment, parameters and timeout. Create, change, delete and
  list through the API and a new Plans page. Reading needs
  `orchestrator:read`, changing needs `orchestrator:run`; no new
  permission.
- **Sensitive task values are never kept in a saved plan.** They are
  asked for when the plan is run.
- **Starting a plan takes a target like a run or task does**: a list of
  nodes, a query, or a node group, with the same scope rules and the
  same preview.
- **Plan steps carry what runs and tasks carry**: run options on a run
  step; environment and timeout on a task step. Every step is checked
  before anything starts.
- **A failed step ends the plan.** The steps after it are closed and
  shown as "Not run", instead of being left as they are today.
- **Watching a plan.** A plan's job page lists its steps in order with
  each step's state and node results, and updates while the plan runs.
  A step is reached through its plan; the job list shows the plan.
- **Scheduling a plan.** A schedule's action can be a plan. The schedule
  keeps its own copy of the steps, taken when it is saved, so a later
  edit to the saved plan by someone else cannot change what an existing
  schedule runs with its owner's access.
- **Console UI.** A Plans tab beside Jobs and Scheduled jobs; a plan
  editor (add, reorder and remove steps); a Run plan dialog with the
  target chooser, the sensitive values, and "Schedule instead".

### Account policy

- **Password rules** for local accounts: a minimum length (default 12)
  and optional required character kinds (lowercase, uppercase, digit,
  symbol). Always: at most 72 bytes, and not the username. Applied
  wherever a password is set: creating a user, an administrator setting
  one, a user changing their own, and the first-start administrator.
- **Login lockout.** After a number of consecutive failed password
  logins (default 10) the account is locked for a period (default 15
  minutes). An administrator can unlock it early. Lockout covers
  password login only; identity-provider login, existing sessions and
  service tokens are not affected.
- **A locked account answers exactly as a wrong password does**, so the
  login endpoint still does not reveal whether a username exists.
- **The policy is stored in the database and edited in the console**
  (Account policy page, `rbac:admin`), so every instance in a cluster
  applies the same one without a restart.
- **Audited.** Lockouts, unlocks, logins refused while locked, passwords
  refused by the policy, and policy changes are written to the audit
  log; lockouts, unlocks and policy changes also to the activity log.
- **BREAKING (first start only):** a first-start administrator password
  (`CONSOLE_BOOTSTRAP_ADMIN_PASSWORD`) that does not meet the default
  rules is refused and no administrator is created. Existing
  installations are not affected: existing passwords keep working and
  are checked only when next changed.
- **BREAKING (API):** creating a user or setting a password with one
  that fails the policy now answers 400.

### Not in this change

- Plans written in the Puppet language or shipped in modules (Bolt
  plans), parallel steps, conditions, per-step targets, continuing past
  a failed step.
- Password expiry, password history, forced change at next login,
  multi-factor login, idle session timeout.
- Fixing jobs started through a `web`-mode instance (an existing fault,
  see design).

## Capabilities

### New Capabilities

- `plans`: saved plans, starting one against a target, watching its
  steps, and the console pages for them.
- `account-policy`: password rules, login lockout, unlock, the stored
  policy and its page, and what is audited.

### Modified Capabilities

- `orchestrator`: plan steps carry run options, task environment and
  timeout; a failed step closes the remaining steps; a plan job is read
  with its steps.
- `scheduled-jobs`: a schedule's action may be a plan, held as a copy of
  its steps.
- `node-access-scope`: a plan's target is narrowed or refused like a
  run's; saved plans are not limited by node groups.
- `rbac`: the user management page shows a locked account and unlocks
  it, and shows the password rules.

## Impact

- **Code:** `internal/orchestrator` (plan start moved into `Starter`,
  step options, closing steps, steps on a job read), new
  `internal/plans`, `internal/scheduler` (plan action), `internal/rbac`
  (policy, lockout, handlers), `internal/app` wiring, migrations 000032
  and 000033.
- **API:** new `/api/v1/orchestrator/saved-plans` routes;
  `POST /api/v1/orchestrator/plans` gains `query`, `group` and step
  fields; `GET /api/v1/orchestrator/jobs/{id}` gains `steps` for a plan;
  schedules accept `kind: "plan"`; new `GET`/`PUT
  /api/v1/account-policy` and `POST /api/v1/users/{id}/unlock`; user
  responses gain `lockedUntil`.
- **Frontend:** new plans list, plan editor and run dialog; job page
  steps; schedule form and page for plans; account policy page; users,
  preferences and login pages; strings in every locale (German
  translated, the rest reported).
- **Guides:** new `marketing/guides/use/run-plans.md`; updates to
  `use/run-jobs.md`, `use/schedule-jobs.md`, `use/access.md` (password
  rules, lockout, unlocking), `install/packages.md` and
  `install/containers.md` (the first administrator's password must meet
  the rules).
- **Development files:** any example or seed password shorter than 12
  characters (`.env.example`, compose secrets, demo seed, screenshots
  script) is lengthened.

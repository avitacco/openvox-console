## Why

`orchestrator:run` is all or nothing. A role that may run anything on a
set of nodes may run every task the Puppet server knows about on them:
a task that restarts a service, one that deletes files, one that sets a
password. Puppet Enterprise lets an administrator limit a role to named
tasks, so that a team can be given "run the patch tasks on the web tier"
without being given a shell. Without it, the only safe answer to "can
this person run one particular task" is to give them nothing, or to give
them everything.

This is week 7 of the Puppet Enterprise parity plan, and the first step
of the closing group: patch management, agentless targets and code-based
plans all add tasks that need to be grantable one at a time.

## What Changes

- **A role carries a task allowlist.** A list of task names, each either
  exact (`package::install`) or ending in a wildcard (`patch::*`, or `*`
  for every task). An empty list means every task, so every existing
  role keeps working unchanged.
- **The allowlist is paired with the role's node groups.** A user holding
  one role that allows `patch::*` on the web tier and another that allows
  every task on the database tier may run patch tasks on the web tier and
  anything on the database tier, and may not run anything else on the
  web tier.
- **Starting a task is refused when no role allows it.** This applies to
  a task run, to every task step of a plan, to saving a plan, and to
  saving or firing a schedule. A refused plan names the step and the
  task, and nothing starts.
- **A schedule is checked again each time it fires**, against its owner's
  roles as they are then, as its node scope already is.
- **A service token can carry an allowlist too.** Service tokens have no
  roles, so the list sits on the token; a token without one may run
  every task, as today.
- **The task picker shows only the tasks the caller may run**, in the
  Run task dialog, the plan editor and the schedule form.
- **The role editor and the service token form get the allowlist
  control**; the Roles page shows each role's allowlist.
- **Refusals are audited.** A task start refused for task permission is an
  audit entry naming the actor, the task and the step.
- Puppet runs are not tasks and are unaffected. Reading jobs and plans is
  unaffected: the allowlist limits what a caller may start, not what
  they may see in a job's history, though the task picker offers only
  the tasks they may run.

**Not in this change:** user groups (week 8), per-environment task
restrictions, per-parameter restrictions, and any limit on Puppet runs.

## Capabilities

### New Capabilities

- `task-permissions`: which tasks a role or service token may run, how
  that is paired with node groups, where it is enforced, how the task
  picker and the role editor present it, and how refusals are recorded.

### Modified Capabilities

- `scheduled-jobs`: a schedule is saved and fired only for tasks its
  owner may run.
- `plans`: a saved plan is saved only with tasks its editor may run, and
  reading one shows a step its reader may not run.

## Impact

- **Code:** `internal/rbac` (role and service token allowlists, token
  claims, the pattern rules), `internal/nodescope` (an intersection of
  scopes), `internal/orchestrator` (the check in `Starter`, the task
  listing filter), `internal/scheduler` and `internal/plans` (callers),
  `internal/taskcatalog` (the listing), `internal/app` (wiring,
  `scheduleAuthority`, the sweep test).
- **Database:** migration 000034 adds a text-array column to `roles` and
  to `service_tokens`, empty by default.
- **API:** `taskPatterns` on roles and service tokens (read and write);
  `GET /api/v1/tasks` omits tasks the caller may not run; a refusal is
  `403` with `step` and `task`. No existing field changes.
- **Frontend:** the role and service token dialogs, the Roles page,
  the task picker, the plan editor and the schedule form; every new
  string in all 15 languages.
- **Site guides:** `use/access.md` (the allowlist, how roles combine, the
  node group pairing), `use/run-jobs.md`, `use/run-plans.md` and
  `use/schedule-jobs.md` (what a refusal looks like and what a
  schedule does when its owner loses a task), and `operations.md`.
- **Compatibility:** nothing changes for a role or token without an
  allowlist. Access tokens issued before the upgrade carry no task
  grants and read as unrestricted until they expire (15 minutes).

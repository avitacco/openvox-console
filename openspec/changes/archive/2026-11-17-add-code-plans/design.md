# Design

## Context

- Saved plans (`internal/plans`, `orchestrator.PlanStep`) are a fixed list; the
  dispatcher advances their child jobs (`advancePlan`) and all steps share one
  target set resolved at start.
- Tasks start through `Starter.StartTask`, which resolves targets, checks node
  scope and task permissions, and creates a job; agentless targets route inside
  the transport (week 12).
- Patch runs (week 11) added a coordinator following a job with a heartbeat,
  and a reaper rule by silence for kind `patch`.
- The console deploys code to `CONSOLE_CODE_DIR_PATH` (`environments/<env>`).
  The Puppet Server has a plans API (since 6.6, see
  [Puppet Server API: Plans](https://vox-docs.pages.dev/openvox-server/latest/puppet-api/v3/plans)),
  which lists plans and their parameter metadata; it does not return a YAML
  plan's steps. `internal/taskcatalog` already has a parameter type validator.
- `gopkg.in/yaml.v3` is already a dependency.

## Goals / Non-Goals

**Goals:** run YAML plans from deployed code faithfully for a stated subset,
with the same security as running the tasks by hand.

**Non-Goals:** the Puppet language, a full Bolt compatibility, parallelism,
scheduling, plans stored anywhere but the code directory.

## Decisions

1. **Plan files come from the code directory on disk.** The environment's
   module path is read from `environment.conf` (`modulepath`, default
   `modules`; the dev fixture uses `modules:site`), entries relative to the
   environment, `$basemodulepath` ignored. A plan file is `<module>/plans/<name>.yaml`
   (`init.yaml` is the module's name). Only the console instance holding the
   deployed code can load plans; on others the feature answers "not available
   here". Reads are bounded (256 KiB), stay inside the module (no `..`, symlinks
   resolved and checked), and the deploy's atomic directory swap means a read is
   consistent. The plan is parsed once per run and its SHA-256 recorded on the
   job. Alternative: ask the Puppet Server - rejected, it does not return steps.
   **Decision point for you:** this ties code-plan execution to the primary's
   code directory; shipping plan files to other instances (or into Postgres on
   deploy) is a later step if multi-instance running is wanted.

2. **A named subset of Bolt YAML plans.** Top level: `description`,
   `parameters`, `steps`, `return`; anything else is refused. Parameters:
   `type`, `default`, `description`. Types: `String`, `Integer`, `Float`,
   `Numeric`, `Boolean`, `Any`, `Array[T]`, `Hash`, `Optional[T]`, `Enum[...]`,
   `Sensitive[T]` and `TargetSpec`, reusing the task parameter validator where
   it applies. Steps carry exactly one of `task`, `plan`, `message`, plus
   `name`, `description`, `targets` (task steps), `parameters`. `command`,
   `script`, `upload`, `download`, `resources` and `eval` are refused. A step's
   `name` (a Puppet variable name) makes its result available as `$name`.
   Alternative: a larger subset with an expression evaluator - rejected; the
   Puppet language is the optional later week.

3. **Expressions are references, not code.** A string that is exactly `$name`,
   `$name.field.sub` or `$name[0]` is replaced by that value, keeping its type;
   `"text ${name.field} more"` is interpolation into text; `$$` is a literal
   `$`. Nothing else after a `$` is accepted at load time (no operators or
   function calls). Maps and lists are walked recursively. A step result is:
   `{ok, results: [{target, ok, value, error}], targets, ok_targets,
   error_targets, value}` where `value` is the single target's value for a
   one-target step. A plan's `return` is evaluated the same way.

4. **Execution is a coordinator in the starting instance**, like patch runs: it
   evaluates steps in order, and each task step is started through
   `Starter.StartTask` with the user's captured scope and task scope, creating a
   child job that the coordinator waits on (it polls the job every 500 ms,
   updating the plan's heartbeat). Checks therefore happen when a step is
   reached, with the same refusals as on demand. Before the job exists a static
   pass refuses obviously bad requests: parameters, tasks named in task steps
   (exist and are allowed for the caller by name), and literal targets in scope.
   A step failing any target fails the plan; later steps are recorded not run.
   If the instance dies, the reaper fails the plan by silence (decision 6).

5. **The record.** New kind `codeplan` (migration 40) for the plan job:
   `plan_name` = `module::plan`, `environment`, `params` = given parameters with
   sensitive ones redacted, new `jobs.plan_trace` (JSONB list of steps: index,
   name, kind, state, times, message, job id, error) and `jobs.result` (the
   return value, redacted). Task steps are child jobs with `parent_job_id`
   and `step_order` (the step's position among executed steps); a nested plan
   is a child `codeplan` job. `Dispatcher.advancePlan` is skipped for a child of
   a `codeplan` so saved-plan machinery is untouched. Job reads show the trace;
   scope hides children and results of nodes the reader may not see, as plans do.

6. **Heartbeat generalised.** `jobs.heartbeat_at` already exists; the reaper's
   silence rule becomes `heartbeat_at IS NOT NULL` and its age rule
   `heartbeat_at IS NULL`, so any job kind that reports progress is judged by
   silence. Patch runs behave as before.

7. **Sensitive values.** The evaluator wraps a `Sensitive[T]` parameter in a
   value that formats as `[redacted]`; it is unwrapped only when a task
   parameter is built (where the task's metadata and the existing redaction
   apply). Stored parameters, trace, messages and results are written through
   the same redaction, and an audit event carries parameter names only.

8. **Limits.** At most 200 steps executed, plans nested at most 5 deep, a run at
   most 24 hours, a step's own task timeout as for tasks, a plan file 256 KiB.
   Reaching one fails the plan, naming it.

9. **Targets.** A `TargetSpec` parameter takes node names, `{"group": id}` or
   `{"query": "..."}`, resolved once at start by `TargetResolver` within the
   caller's scope. Names outside scope refuse the request whole (the caller
   typed them); a group or query is narrowed silently.

10. **Permissions.** Listing and reading plans: `orchestrator:read`. Running:
    `orchestrator:run`. No new permission: the plan itself runs nothing but the
    tasks and plans it names, each checked as the caller. Task allowlists
    (`taskPatterns`) already decide which tasks; a separate plan allowlist is
    not added. Scheduling a code plan is out of scope.

## Risks / Trade-offs

- [A plan's own logic is limited] -> the subset is documented and refusals say
  where; extending is additive.
- [A malicious plan file in code] -> code that can be deployed already runs
  tasks on nodes; a plan only sequences tasks the caller may run, checked per
  step, and cannot name files or commands.
- [Large fan-out] -> per-step target and step limits, checks per step.
- [Console restart mid-plan] -> reaped by silence; started tasks continue on
  nodes (as for any job).

## Migration Plan

Migration 000040 is additive (new kind, two nullable columns); down deletes
code plan jobs first. No backfill. Existing saved plans are untouched.

## Open Questions

- Whether a step should be able to opt out of failing the plan (Bolt's
  `catch_errors`); not in this version, additive later.

## Implementation Notes

- The task list has 16 tracked tasks (the proposal summary said 18).
- **A nested plan is not a job of its own.** The design said a plan called by a
  plan would be a child `codeplan` job with its own trace. It is instead part of
  the same job and the same trace, one level deeper (`path` `1.2`, `depth` 1),
  and its task steps are children of the outer plan job. This keeps the scope
  rule for reading a plan (found through its step jobs) correct at any depth
  without a recursive query, and gives one record to read.
- The check of task names (that each exists and is allowed) happens *after*
  target parameters are resolved, so the answer to a request never depends on
  something other than the caller's own nodes until their nodes are known to be
  theirs.
- A reader limited to some nodes, for a plan that also ran on others, gets the
  steps and their states but no messages, result, error or parameters.
- `taskcatalog.ParamType` is a new exported wrapper of the existing type
  checker, so plan parameters and task parameters agree.
- `jobs.heartbeat_at` decides how the reaper judges a job (silence if set, age
  if not), replacing the earlier test for kind `patch`.
- A `Sensitive` value passed to another plan must go to a `Sensitive`
  parameter, or the run fails at that step.

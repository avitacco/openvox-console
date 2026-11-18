# Proposal

## Why

Saved plans are a fixed list of Puppet runs and tasks. Real orchestration
passes one step's results to the next, names its own inputs, and ships with
the code that owns it: Puppet Enterprise and Bolt run **plans** that live in
modules. Week 13 lets the console discover and run plans from the deployed
control repository, starting with Bolt's YAML plan format, which needs only an
interpreter and not the Puppet language.

## What Changes

- The console **discovers plans** in each environment's modules
  (`<module>/plans/*.yaml`) and shows their parameters and descriptions.
  Plan files are read from the code directory the console deploys
  (`CONSOLE_CODE_DIR_PATH`): the Puppet Server's plans API lists plans but does
  not return a YAML plan's steps.
- A built-in **YAML plan interpreter** runs a defined subset of Bolt YAML
  plans: typed parameters with defaults; steps that run a **task**, run
  another **plan**, or print a **message**; `$variable` references to
  parameters and to earlier steps' results; and a `return` value. A plan using
  anything outside the subset (commands, scripts, uploads, `eval`, `resources`,
  Puppet-language expressions) is refused when it is loaded, naming the step.
- A plan is started with `orchestrator:run`. Every task step is started
  through the existing task path when it is reached, so **node scope, task
  permissions and agentless targets work unchanged**, step by step. Targets can
  be given as node names, a node group or a query, resolved once at start.
- A run is a new **code plan job** with a trace of its steps (what ran, with
  which job, what it printed, what it returned) and its own heartbeat, so a plan
  that runs for hours is judged by silence rather than age.
- Sensitive parameters (`Sensitive[...]`) never reach storage, the trace, a
  message or the audit log.
- Audit and activity entries, a code plans section on the Plans page and a
  trace on the job page, translations in all 15 locales, guides and
  `operations.md`.
- **Not included:** scheduling a code plan, Puppet-language plans, `command`,
  `script`, `upload`, `download`, `eval` and `resources` steps, parallel steps,
  retry and error catching, and Bolt inventory.

## Capabilities

### New Capabilities
- `code-plans`: discovering plans in environment code, the supported YAML plan
  subset and its semantics, running a plan step by step with scope and
  permission checks, the run's record, sensitive values, and the console pages.

### Modified Capabilities
- `orchestrator`: a code plan is a job kind whose steps are child jobs; a job
  that reports progress, by heartbeat, is judged by its silence whatever its kind.

## Impact

- New `internal/codeplans` (loader, parser, evaluator, coordinator); changes in
  `internal/orchestrator` (job kind, trace and result storage, reaper rule);
  API under `/api/v1/code-plans` and `/api/v1/orchestrator/code-plans`.
- Migration 000040: job kind, `jobs.result`, `jobs.plan_trace`.
- Frontend: Plans page, job page; docs under `marketing/guides/`; `operations.md`.
- The instance that runs a plan needs the deployed code directory.

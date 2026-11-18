---
title: Run plans from your code
summary: Run YAML plans that live in your modules, with typed parameters, results passed between steps, and every task checked against your node groups.
order: 10
---

A **code plan** is a YAML file in a module's `plans` directory, in the format
Bolt and Puppet Enterprise use. The console reads it from the code it
deployed and runs it itself: one step after another, passing results from
step to step. It does not need Bolt or the Puppet language.

This is different from a saved plan, which is a fixed list of Puppet runs and
tasks that you build in the console and that runs on one set of nodes. A code
plan lives with your code, takes its own parameters, and decides each step's
targets from the steps before it.

Reading the list of plans needs `orchestrator:read`. Running one needs
`orchestrator:run`. No new permission is involved.

## Where plans come from

The console looks in each environment's modules, in the order of the
`modulepath` in `environment.conf` (`modules` if there is none), for
`<module>/plans/<name>.yaml`. A plan is named `module::name`, or just
`module` for `init.yaml`. If a name appears in two directories, the first
wins, as in Puppet.

The files are read from the code directory of the console instance that
deploys your code (`CONSOLE_CODE_DIR_PATH`). **An instance that does not hold
the deployed code cannot list or run plans**, and says so rather than showing
an empty list. Run plans from the instance that deploys.

Open **Jobs**, then **Plans**: the **Plans in code** section lists an
environment's plans. A plan the console cannot run is listed with the reason,
naming the step.

## What a plan can do

A plan has a `description`, `parameters`, `steps` and a `return`:

```yaml
description: Restart a service, then check it
parameters:
  targets:
    type: TargetSpec
    description: Where to restart it
  service:
    type: String[1]
    default: nginx
  token:
    type: Optional[Sensitive[String]]
steps:
  - name: restart
    description: Restart the service
    task: service::restart
    targets: $targets
    parameters:
      name: $service
  - name: status
    task: service::status
    targets: $restart.ok_targets
    parameters:
      name: $service
  - message: "checked ${status.targets}"
return:
  restarted: $restart.ok_targets
  healthy: $status.ok
```

**Parameters** declare a `type`, an optional `default` and a `description`.
Types are those tasks use (`String`, `Integer[1, 10]`, `Boolean`,
`Array[String]`, `Enum['a','b']`, `Optional[...]`, ...) plus `TargetSpec`
(nodes) and `Sensitive[...]`. A parameter with no default that is not
`Optional` is required. The console refuses a run with a missing, unknown or
wrongly typed parameter before anything starts, naming it.

**Steps** do exactly one of:

- `task: module::task`, with `targets` and `parameters`. Runs the task.
- `plan: module::plan`, with `parameters`. Runs another plan as part of this
  one. A plan may call plans up to five deep, and never itself.
- `message: text`. Prints text into the run's record.

A step may have a `name`, which makes its result available as `$name` to later
steps, and a `description`.

**References** are `$name`, `$name.field`, `$name[0]` and `$name['field']`, to
a parameter or to an earlier step. A string that is only a reference keeps the
value's type (a list stays a list). In text, write `${name.field}`; write
`$$` for a dollar sign. That is all the syntax: there are no operators, no
conditionals, no functions. Inside a YAML list or mapping written on one
line, put a reference that contains `[` or `,` in quotes.

A step's result has:

| | |
|---|---|
| `ok` | every target succeeded |
| `results` | each target's `target`, `ok`, `value` and `error` |
| `targets`, `ok_targets`, `error_targets` | node names |
| `value` | the task's result, for a step on a single node |

A task's `value` is what it printed as JSON, or its text as `_output`.

If a task step fails on any target, **the plan fails there**: the steps after
it are recorded as not run and nothing more is sent. A step whose targets are
empty (say, an earlier step's successes were none) runs nothing and counts as
having succeeded.

**Not supported**: `command`, `script`, `upload`, `download`, `resources` and
`eval` steps, Puppet-language expressions, running steps in parallel, retrying
and catching errors, and scheduling a code plan. A plan using one is listed as
unusable with the reason. Bolt's Puppet-language plans (`.pp`) are not run.

## Run one

Choose **Run** next to a plan. The form is built from its parameters; leave a
field empty to use the default. Nodes (`TargetSpec`) are given as a list of
names, a **query** or a **node group**, resolved once when the plan starts,
and previewed first. The run opens as a job; its page shows each step as it
happens: its status, the job it started, what a message printed, and why a step
failed, with the value the plan returned and the parameters it was given.

## Scope and permissions

A plan can do nothing you could not do by hand. When you start it:

- the task each step names must exist and be one your roles allow, or the
  request is refused naming the step;
- nodes you name must be in your node groups, or the whole request is refused;
- as each step is reached, its targets are checked against your node groups and
  the task against your allowlist, exactly as a task you started yourself. A
  step that would be refused fails the plan there, with the reason.

Agentless targets can be used in task steps.

A reader limited to some node groups sees a plan run only if it ran on their
nodes, and not what it printed, returned or was given when it also ran on
others.

## Sensitive values

A parameter typed `Sensitive[...]` is never written down: it is `[redacted]`
in the stored parameters, the trace, messages, the returned value, logs and
audit entries, and reaches only the task parameter it is given to. A sensitive
value can be passed on only to a parameter that is itself `Sensitive`.

## Long runs

A plan can run for hours, up to 24. The console tells the job it is alive as
it goes. If the console instance following it stops, the plan is failed ten
minutes after it was last heard from; steps already started carry on on their
nodes. A run is limited to 200 steps and a step to 1000 targets; a limit
fails the plan, naming it.

Starting a plan, and its end, are audit entries with who, which plan and
environment and the *names* of the parameters given, never their values.

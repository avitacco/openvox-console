---
title: Run plans
summary: Build a plan - a list of Puppet runs and tasks carried out in order - keep it, run it on the nodes you choose, and watch each step.
order: 4
---

A **plan** is a list of steps carried out in order on the same nodes.
Each step is a Puppet run or a task, exactly as you would start one by
hand - see [Run jobs](run-jobs.html). A plan is kept in the console so
it can be run again, on different nodes each time.

Seeing plans needs `orchestrator:read`. Creating, changing, deleting
and running them needs `orchestrator:run`.

> [!NOTE]
> A plan here is a list of steps built in the console. It is not a plan
> written in the Puppet language and shipped in a module, which the
> console does not run.

## Build a plan

Open **Jobs**, then **Plans**, then **New plan**.

1. Give it a **Name** and, if you like, a description. A name can be
   used by one plan only.
2. Add steps with **Add a Puppet run step** and **Add a task step**. A
   plan has between 1 and 20 steps.
   - A Puppet run step takes the [run options](run-jobs.html#run-options):
     no-op, debug and an environment.
   - A task step takes an environment, a task, its parameters and a
     timeout, as in [Run a task](run-jobs.html#run-a-task).
3. Put the steps in order with **Move up** and **Move down**. What you
   have typed in a step moves with it.
4. **Save plan**.

The console checks every step when you save, as it would check the run
or the task if you started it now. A step it refuses is marked with the
reason, and nothing you typed is lost.

A plan does not say which nodes it runs on. That is chosen each time it
is run.

### Sensitive task values

A task parameter marked `sensitive` - a password, a token - is never
kept in a plan. Its field is left out of the editor, and the value is
asked for each time the plan is run. This is because a plan is shared:
everyone who can run jobs can open it.

### Who can change a plan

Everyone with `orchestrator:run` can change or delete any plan,
whichever node groups their role is granted on. A plan names no nodes,
so there is nothing in it for node groups to limit. What a person can
run a plan *on* is limited as for any job. A person whose role is
limited to some tasks can save a plan only with tasks they may run.

## Run a plan

On the **Plans** page, choose **Run** beside a plan, or open the plan
and choose **Run**.

1. Choose the nodes: a list, a query, or a node group, as in
   [Choose the nodes](run-jobs.html#choose-the-nodes). **Preview nodes**
   shows what it would run on.
2. Enter any sensitive values the steps ask for. Each is labelled with
   its step.
3. **Run plan**. The plan's job page opens.

The nodes are worked out once, when the plan starts, and every step
runs on those nodes. If your role is granted on particular node groups,
a query or a group is narrowed to your nodes, and a list that names a
node outside them is refused whole, as for any job.

Every step is checked before anything starts. If one would be refused -
a task that has since been removed from the code, say - the plan is
refused and names the step, and nothing runs.

A plan with a step that can no longer run shows a warning on that step.
Edit the plan before running it.

### Tasks your roles do not allow

If your role is limited to some tasks - see
[Limit a role to some tasks](access.html#limit-a-role-to-some-tasks) -
a plan someone else saved may contain a task you may not run. You still
see every step, and the ones you may not run are marked **Your roles do
not allow the task ...**. **Run** is switched off for that plan, and
the API refuses it with `403`, naming the step and the task. Editing the
plan will not help unless you remove those steps; saving it with them
is refused the same way.

When the target is a query or a node group, the plan runs on the nodes
that **every** task step may be run on by you, so all its steps still
share one set of nodes. Puppet-run steps are not limited.

## Watch a plan

A plan's job page lists its steps in order. Each shows what it does,
its state, when it started and finished, and each node's result. The
page updates by itself while the plan runs.

| State | Means |
| --- | --- |
| Waiting | an earlier step has not finished yet |
| Running | sent to the nodes, and not all have answered |
| Succeeded | finished on every node |
| Failed | failed on at least one node |
| Not run | an earlier step failed, so this one was never sent to any node |

A step's number links to that step's own job page, which links back to
the plan. The **Jobs** list shows a plan as one line, not one for each
step.

### When a step fails

A step fails if it fails on any of its nodes, including a node that was
not connected. The plan stops there: the steps after it are marked
**Not run**, and nothing is sent to any node for them. The plan's job
is failed.

There is no option to carry on past a failed step, to skip the nodes
that failed, or to try a step again. Fix what failed and run the plan
again; a plan that should not repeat its first steps can be split into
two.

## Schedule a plan

In the Run plan dialog choose **Schedule instead**, or create a
schedule and choose **Run a plan** - see
[Schedule jobs](schedule-jobs.html).

A schedule keeps **its own copy** of the plan's steps, taken when the
schedule is saved. If the plan is changed or deleted afterwards, the
schedule goes on running the steps it was saved with. To pick up a
change, edit the schedule, choose the plan again, and save.

This is deliberate. A schedule runs with its owner's access, and
everyone who can run jobs can change a plan. If a schedule followed the
plan, someone limited to a few nodes could change what an
administrator's schedule runs on every node.

## Through the API

Saved plans:

| Request | Does |
| --- | --- |
| `GET /api/v1/orchestrator/saved-plans` | lists plans, without their steps |
| `GET /api/v1/orchestrator/saved-plans/<id>` | one plan with its steps |
| `POST /api/v1/orchestrator/saved-plans` | creates a plan |
| `PUT /api/v1/orchestrator/saved-plans/<id>` | replaces a plan |
| `DELETE /api/v1/orchestrator/saved-plans/<id>` | deletes a plan |

```sh
curl -X POST https://console.example.com/api/v1/orchestrator/saved-plans \
  -H "Authorization: Bearer <access token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Restart web tier",
       "description":"Check first, then restart",
       "steps":[
         {"kind":"run","options":{"noop":true}},
         {"kind":"task","taskName":"service","environment":"production",
          "params":{"name":"nginx","action":"restart"},"timeoutSeconds":300}
       ]}'
```

A plan as read lists, on each task step, the sensitive parameters to
supply when running it (`needs`), and a `problem` if its task can no
longer be found.

There is no request that runs a plan by its id. To run one, send its
steps - with the sensitive values added to each step's `params` - and
the nodes:

```sh
curl -X POST https://console.example.com/api/v1/orchestrator/plans \
  -H "Authorization: Bearer <access token>" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Restart web tier",
       "group":12,
       "steps":[
         {"kind":"run","options":{"noop":true}},
         {"kind":"task","taskName":"service",
          "params":{"name":"nginx","action":"restart"}}
       ]}'
```

The nodes are `targets`, `query` or `group`. The reply is the plan's
job. Read it with `GET /api/v1/orchestrator/jobs/<id>`, which returns
its `steps`, each with a `state` of `waiting`, `running`, `succeeded`,
`failed` or `not_run`.

A refused request answers `400` (or `403` for a node outside your node
groups) with an `error`. When one step is the cause it also carries
`step`, counted from 1, and for a task's parameters `fields`.

`GET /api/v1/orchestrator/jobs` no longer lists a plan's steps as jobs
of their own. Add `?parent=<plan job id>` to list them.

## In a split deployment

A plan started in the console is sent and followed, step by step, by
the instance that took the request: a `web` instance in a split
deployment (see [run modes](run-modes.html)). If that instance stops
before the plan ends, the steps still running are marked failed and the
rest are closed as not run; start the plan again. A scheduled plan is
started and followed by an `orchestrator` instance in the same way.

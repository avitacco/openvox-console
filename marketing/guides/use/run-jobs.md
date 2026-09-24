---
title: Run jobs
summary: Run Puppet on demand on one node, a selection, or every node a group matches, and see what each node did.
order: 3
---

The console can run Puppet on nodes right now, rather than at their next
scheduled run. Each request is a **job**, recorded with every node it
targeted and what happened on each.

Starting a job needs `orchestrator:run`; seeing jobs needs
`orchestrator:read`. A node must be connected to be reached: see
[Add nodes](add.html).

## Run Puppet

From wherever you are looking at the nodes you want:

- **One node:** on the node's page, run Puppet.
- **A selection:** on the **Nodes** page, select nodes, then run Puppet
  on the selection.
- **Every node a group matches:** on the group's page, run Puppet on its
  matching nodes.

Each starts a job and takes you to it.

Through the API, with an access token:

```sh
curl -X POST https://console.example.com/api/v1/orchestrator/runs \
  -H "Authorization: Bearer <access token>" \
  -H 'Content-Type: application/json' \
  -d '{"targets":["web01.example.com","web02.example.com"]}'
```

## Follow a job

The **Jobs** page lists every job, newest first. A job's page shows each
target on its own line - its status, exit code and output - so a job that
worked on nine nodes and failed on one says exactly that.

A node that was not connected when the job started fails at once with
"agent not connected", instead of holding the job open.

When a run finishes, its line links to the report that run produced, with
every resource it changed.

Every job's start and outcome is also recorded on the **Activity** page.

> [!NOTE]
> The API also accepts Puppet tasks and plans
> (`/api/v1/orchestrator/tasks` and `/api/v1/orchestrator/plans`), and
> records them as jobs the same way. Running them on nodes has not yet
> been verified end to end, so this guide covers Puppet runs only.

## A job whose instance stopped

A job is followed to the end by the console instance that started it. If
that instance stops before its targets finish, the job would otherwise
show as running forever; instead, it is marked failed once it has been
running far longer than any run could take.

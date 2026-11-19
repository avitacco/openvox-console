---
title: Let AI agents use the console
summary: Give an AI agent its own limited token, let it read the fleet and ask for changes, and approve each change yourself before anything runs.
order: 11
---

The console has an endpoint for AI agents, built on MCP (the Model Context
Protocol), at `/mcp`. An agent connected to it can look at your nodes, node
groups, tasks and jobs. When it wants to change something - run a task, run
Puppet, run a plan or patch nodes - it does not start anything. It **asks**,
and the request waits on the **Approvals** page until a person approves it.

Everything an agent can see or ask for is limited by its own token: which
permissions it has, which node groups it can reach, and which tasks it may
ask to run. Everything it does is in the audit log under its own name.

## Turn the endpoint on

The endpoint is off. Set `CONSOLE_MCP_ENABLED=true` on the console and
restart it. Until then `/mcp` does not exist: it answers as any unknown
path does.

| Setting | Meaning | Default |
| --- | --- | --- |
| `CONSOLE_MCP_ENABLED` | serve `/mcp` | off |
| `CONSOLE_MCP_RATE_LIMIT` | calls a minute one token may make | 60 |
| `CONSOLE_AGENT_REQUEST_TTL` | how long a request waits for a decision, such as `30m` | `1h` |
| `CONSOLE_AGENT_TOKEN_MAX_DAYS` | the longest an agent token may last | 90 |

The rate limit is kept by each console instance, so a token may make that
many calls a minute on every instance. Put the endpoint behind TLS, as you
do the rest of the console: the token is a bearer credential.

## Issue a token for an agent

An agent token is its own kind of token. It is not a service token: it
expires, it is limited to node groups, and it works only at `/mcp`. A leaked
one cannot be used against the rest of the console's API.

1. Open **Admin**, then **Agent tokens**.
2. Name the token for the agent it is for, such as `ops-assistant`.
3. Choose its **permissions**. An agent token can carry only
   `nodes:read`, `classifier:read`, `orchestrator:read`, `orchestrator:run`
   and `vulnerabilities:read`.
   `orchestrator:run` does not let the agent run anything: it lets it
   **ask**.
4. Choose the **node groups** it may reach. A token with none reaches no
   nodes. Everything the agent reads or asks for is limited to those groups
   and the groups beneath them.
5. Optionally, under **Tasks it may ask to run**, list the tasks it may ask
   for, one pattern per line, as for a [role](access.html#limit-a-role-to-some-tasks).
   Leave it empty to allow every task. Both the node groups and the tasks
   are fixed when the token is created.
6. Set how many days it lasts. A token must expire, and cannot last longer
   than `CONSOLE_AGENT_TOKEN_MAX_DAYS`. Thirty days is a good start.
7. Choose **Create agent token** and copy the token: it is shown once.

**Revoke** on the same page ends the token at once, on every console
instance. Its requests stay on the Approvals page as a record, and any it
had waiting are marked failed.

## Connect an agent

Give the agent the console's address with `/mcp` on the end, and the token
as a bearer token. The endpoint speaks streamable HTTP, and answers each
POSTed message directly. For Claude Code, for example:

```
claude mcp add --transport http openvox https://console.example.com/mcp \
  --header "Authorization: Bearer <the token>"
```

The agent is offered only the tools its token allows.

| Tool | Needs | What it does |
| --- | --- | --- |
| `list_nodes`, `get_node`, `get_node_reports` | `nodes:read` | look at nodes, their facts and reports |
| `get_node_patches`, `get_patch_inventory` | `nodes:read` | pending operating system updates |
| `list_groups`, `get_group`, `get_group_nodes` | `classifier:read` | node groups |
| `list_tasks`, `get_task`, `list_code_plans`, `get_code_plan` | `orchestrator:read` | what the deployed code offers, and each one's parameters |
| `list_jobs`, `get_job` | `orchestrator:read` | jobs and their results |
| `resolve_targets` | `orchestrator:run` | which nodes a target would name |
| `request_task`, `request_puppet_run`, `request_plan`, `request_patch_run`, `request_code_plan` | `orchestrator:run` | ask to run something (see below) |
| `get_request`, `list_requests` | `orchestrator:run` | the agent's own requests and how they were decided |

Everything an agent reads comes through the console's own endpoints with its
token, so it sees exactly what a person with that token's permissions and node
groups would, and sensitive values are masked as everywhere else. A long
result is cut, with a note telling the agent how to ask for less.

## Approve what an agent asks for

When an agent calls one of the `request_` tools, the console checks the
request exactly as it would check yours - the task and its parameters, the
nodes, the agent's node groups and task list - and, if it passes, stores it
and tells the agent it is awaiting approval. Nothing runs. A request that
fails a check is refused with the reason, so the agent can correct it.

Open **Jobs**, then **Approvals** (the tab shows how many are waiting, as does
the navigation). **Review** a request to see:

- which agent asked and when, and when the request expires
- the task, plan or kind of run, and its environment
- the **exact nodes**, resolved when the agent asked
- the parameters, with sensitive values masked

Choose **Approve and run** to start the job, or **Reject**, with a note if you
like: the agent is told. Approving starts exactly what you saw - parameters
and nodes are frozen when the request is made - as the agent's token. The job
shows that an AI agent asked for it and who approved it, and **Jobs** can be
limited to jobs started that way.

A request that nobody decides expires, after an hour unless you set
`CONSOLE_AGENT_REQUEST_TTL`, and can no longer be approved. An agent can hold
only so many waiting requests at once.

### Who can approve

Two permissions, which go on roles for people and cannot be given to an agent
token:

| Permission | Lets someone |
| --- | --- |
| `agent-requests:read` | see requests on the Approvals page |
| `agent-requests:approve` | approve and reject them |

An approver also needs `orchestrator:run` over every node the request acts on,
and may run the task. You cannot approve a request on nodes you could not run
the task on yourself. Someone who can see only some node groups sees only the
requests that name nodes in them.

The person who issued an agent's token cannot approve its requests, though
they can reject them. With only one administrator, create a second account
for approving.

> [!NOTE]
> A permission added in a newer console version is not added to existing roles
> for you. After an upgrade, grant `agent-requests:read` and
> `agent-requests:approve` on the **Roles** page to the roles that should have
> them. The bootstrap administrator of a new installation has both.

If an agent's token is revoked or has expired by the time you approve, nothing
is started, and the request says why.

## Plans from code

`request_code_plan` asks to run a [plan from your code](code-plans.html).
Such a plan chooses its own nodes while it runs, within what the agent's token
reaches, so approving it needs `orchestrator:run` over everything that token
reaches. Plans run only on the console instance that deploys your code, so
approve them there, or the request fails with that reason.

## What is recorded

The audit log, at the orchestrator category's level, records each request, each
approval, rejection and expiry, naming the agent as `agent:<token name>` and the
person who decided, with the request's number. Reads an agent makes are
audited as any read is, under the same name. Sensitive values never appear in
the audit log, the activity log or the request itself: they are kept encrypted
with the console's secrets key (`CONSOLE_SECRETS_KEY`), without which a request
that carries one is refused.

## The API description

`/api/v1/openapi.json` describes the REST API for programs you write yourself,
for anyone who is signed in. Agent tokens cannot call that API, and a program
using a service token is not asked to wait for approval. To let an agent act
with a person's approval in the loop, connect it to `/mcp`.

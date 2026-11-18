## Why

A job can only be started on nodes somebody has already picked by name, and only right now. Puppet Enterprise lets an operator say "every node this query matches" or "every node in this group", and say "tonight at two" or "every Sunday"; without both, the console cannot be used for a maintenance window or for a routine run across a set of nodes that changes from week to week.

## What Changes

- **Targets by query.** A Puppet run or a task can name its targets with a PQL query instead of a list of nodes. The console runs the query against openvoxdb and the job runs on the nodes it returns.
- **Targets by node group.** A run or a task can name a node group, and runs on the nodes in it when the job starts. Today the browser resolves a group to a node list before it asks.
- **A preview** of the nodes a query or a group resolves to, before anything is started.
- **Starting a job from the Jobs page**, with a target chooser (node list, query, or node group). The existing entry points on a node, a node selection and a group stay as they are.
- **Scheduled jobs.** A run (with its run options) or a task (with its parameters) can be saved to start once at a chosen time, or repeatedly every so many minutes, hours, days or weeks. A schedule can be paused, resumed, changed and deleted, and shows what happened each time it came due.
- **A schedule's targets are worked out when it fires**, not when it is saved, so a query or a group follows the fleet as it changes.
- **A schedule runs as its owner, with the owner's permissions and node groups as they are when it fires.** An owner who has since lost `orchestrator:run` starts nothing.
- **A late firing is not run.** If no console instance was running when a schedule came due, and it is found more than fifteen minutes late, that firing is recorded as missed. A repeating schedule carries on from its next time.
- **Node scope for selectors.** A named node outside the caller's node groups still refuses the whole request, as now. A query or a group is a question rather than a list of names, so its answer is narrowed to the caller's nodes. A caller limited to node groups may not use a query that reads across nodes (a subquery), because such a query could be used to learn about nodes they cannot see.
- **A job records where its targets came from** (the query or group, and the schedule that started it), and the Jobs pages show it.
- **Site guides**: `marketing/guides/use/run-jobs.md` (query and group targets), a new `marketing/guides/use/schedule-jobs.md`, `marketing/guides/use/access.md` (how node groups limit queries and schedules), and the run-modes guide (the new scheduler worker).

Not in this change: scheduling plans (plans get their UI in week 6); cron expressions or calendar rules such as "the first Monday"; PQL as a general search on the Nodes page; a "run now" button on a schedule; running in batches or with a concurrency limit; LDAP/SAML.

## Capabilities

### New Capabilities
- `scheduled-jobs`: saving a run or a task to start later or repeatedly, firing it exactly once across console instances, the authority it fires with, what happens when it cannot fire, and the API and pages for managing schedules.

### Modified Capabilities
- `orchestrator`: runs and tasks accept a query or a node group as their target; a preview of what a target resolves to; jobs record their target source and the schedule that started them; the Jobs page can start a job.
- `node-access-scope`: targets chosen by a query or a group are narrowed to the caller's scope; scoped callers may not use subqueries; a schedule fires with its owner's current scope; who can see and change a schedule.

## Impact

- **Code:** `internal/orchestrator` (target resolution, new `targets/resolve` route, job columns), a new `internal/scheduler` package (store, firing loop, handlers), `internal/app` (wiring, a new `orchestrator-scheduler` worker in the run-mode surface, owner authority lookup through `internal/rbac`), `internal/openvoxdb` (query shape check), `internal/groupnodes` (reused as the group resolver).
- **Database:** one migration: `scheduled_jobs`, `scheduled_job_firings`, and `target_source` and `schedule_id` on `jobs`.
- **API:** `query` and `group` on `POST /api/v1/orchestrator/runs` and `/tasks`; `POST /api/v1/orchestrator/targets/resolve`; `/api/v1/orchestrator/schedules` (list, create, read, replace, delete, pause, resume). No new permissions: `orchestrator:run` and `orchestrator:read`.
- **Secrets:** a scheduled task's sensitive parameters are stored sealed with the existing secrets key (`CONSOLE_SECRETS_KEY`). Without a key, a schedule that carries a sensitive value is refused.
- **Frontend:** a target chooser, a Scheduled jobs list and a schedule form, run and task buttons on the Jobs page, target source and schedule shown on jobs; new strings in every locale.
- **Node agent client:** unchanged. A scheduled job reaches a node as the same request an on-demand one does.
- **Docs:** the guides named above, `operations.md`, `architecture-summary.md`.

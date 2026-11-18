## Context

`add-group-scoped-node-access` put the mechanism in place and applied
it to reads:

- roles name node groups; `Claims.NodeScopes` carries, per permission,
  the group ids; a token with no scope at all is fleet-wide;
- `internal/app` wraps `authorize` so each request carries the caller's
  groups for the permission that gated it;
- `nodescope.Resolver.FromRequest` resolves that to a `Scope`
  (`All`, `Allows`, `Certnames`), cached 30 s; handler sets take a
  `nodescope.Func` through `SetScope` and default to unscoped;
- a route sweep test fails for any GET route that leaks.

The action endpoints go through the same `authorize`, so the grant for
`orchestrator:run`, `nodes:manage` or `nodes:certs:manage` is already on
the request. They ignore it.

The actions and what an unknown node answers today:

| Endpoint | Permission | Unknown node today |
| --- | --- | --- |
| `POST /orchestrator/runs`, `/tasks`, `/plans` (`targets: []`) | `orchestrator:run` | job created; target fails "not connected" |
| `PUT /nodes/{name}/packages/reporting` | `orchestrator:run` | 409 offline |
| `POST /nodes/{name}/agent-upgrade` | `nodes:manage` | 409 not-connected |
| `DELETE /nodes/{name}` | `nodes:manage` | 404 |
| `POST/DELETE /nodes/{certname}/cert…` | `nodes:certs:manage` | 404 from the CA |

Jobs live in Postgres: `jobs` and `job_targets(job_id, certname, …)`,
with an index on `certname`. `ListJobs` paginates in SQL and loads a
target count and preview per job; `GetJob` loads every target; a plan
is a parent job with step jobs.

## Goals / Non-Goals

**Goals:**
- No action reaches a node outside the caller's groups for that
  action's permission.
- Jobs show a scoped caller only their nodes.
- No new mechanism: same claims, same resolver, same `SetScope` shape,
  same sweep.
- Fleet-wide callers and service tokens behave exactly as before.

**Non-Goals:**
- Scoping the activity log, group definitions, service tokens, code
  deploys or the ENC endpoint.
- Partial execution of a mixed target list.
- Hiding action buttons in the UI according to scope (see decision 6).
- Changing how certificate signing itself works.

## Decisions

### 1. Refuse with 403, and decide before looking anything up
An action on a node outside scope answers `403` with
`{"error": …, "outOfScope": [certnames]}`, for single-node and
multi-node endpoints alike. The check runs first, before openvoxdb,
the CA or the node transport is touched.

*Why 403 and not "what an unknown node answers"*, which is what reads
do: for reads the two are the same empty answer and saying nothing is
natural. For actions the unknown-node answers are a mix of 404, 409
"offline" and a job that fails later, and a user who can *see* a node
but not act on it would be told it is offline or missing, which is
false and undebuggable.

*Why this still leaks nothing:* a name no node has is in no group, so
for a scoped caller it is out of scope too and gets the same 403. Only
a fleet-wide caller ever reaches the existence check, and they may see
everything anyway. The spec states this as a requirement and a test
compares the two refusals.

### 2. Multi-target actions are all or nothing
If any target is out of scope, nothing is created or dispatched.

*Why not run the allowed subset:* a silent partial run is the worst
outcome for an operator ("I ran it on the group" - on 40 of 60), and a
reported partial run needs a new response shape and UI for little
gain. The refusal lists the offending nodes; the caller removes them
and retries.

`nodescope` gains one helper used by every action handler:

```go
// Outside returns the certnames not in scope, in the order given.
func (s Scope) Outside(certnames []string) []string
```

### 3. Jobs: visible by any in-scope node, trimmed to in-scope nodes
For a scoped caller:

- **List:** `ListJobs` gains `JobFilter.Certnames`; the query adds
  `EXISTS (SELECT 1 FROM job_targets t WHERE t.job_id = j.id AND
  t.certname = ANY($n))`, so paging and the total are right. Target
  counts and previews are computed over in-scope targets only.
- **Detail:** `GetJob` returns only in-scope targets; with none, the
  handler answers the existing 404.
- **Plans:** steps are jobs and go through the same filter; a step with
  no in-scope target is omitted. A plan is visible if any step is.

*Alternative considered:* show a job only if **all** its nodes are in
scope. Rejected: a fleet-wide job (a monthly patch run) would vanish
for every team, including the nodes that are theirs.

The job's own status is left as stored. A job that failed only on a
hidden node therefore shows "failed" with no failing node visible.
Recomputing status per viewer would make the same job read differently
to two people looking at it together; the detail page instead says that
some nodes are not shown (decision 6).

Scope is evaluated against **current** membership, like everything
else: a node that leaves a group takes its job history out of that
group's view, and a removed node's history is visible to fleet-wide
callers only. This is the simple rule and matches reads; recording
group membership at job time was rejected as a second source of truth.

### 4. Nodes with no facts: pins, not a new mechanism
A pending certificate request has no facts and is in no group but the
root. The resolver already counts a pinned certname as in a group when
the group's ancestors hold it too (which, for a node with no facts,
means the group sits directly under the root or the node is pinned up
the chain). So a team signs its own new nodes when the certname is
pinned into the team's group first; this change adds the test and the
guide step and nothing else.

*Alternatives considered:* trusting a CSR attribute to name the team's
group (a new enrolment contract, and the requester chooses its own
scope); letting any `nodes:certs:manage` holder sign any *pending*
request (signing is the most sensitive action there is - equivalent to
CA admin - and should not be the one exception). Both rejected.

Fleet-wide roles sign exactly as they do today.

### 5. The activity log stays an administrator surface
Closing the open question from the read-side design. Activity events
are prose summaries with a subject, not rows keyed by node; filtering
them reliably means reclassifying every event type, and an audit trail
with holes is worse than one a narrower audience can read. `activity:read`
is documented as fleet-wide and the roles page already shows it as a
separate permission.

### 6. Frontend: report the refusal, do not predict it
The console does not hide or disable Run, Remove or Sign per node. To
do so it would need each permission's node set in the browser, for
every page that offers an action. Instead:

- every action call shows the 403 in place, naming the nodes, with one
  shared helper in `app.js`;
- for a multi-node action the message offers nothing automatic - the
  user deselects those nodes;
- the job page shows a note when the caller's scope is narrower than
  the job ("Only the nodes in your node groups are shown"), driven by a
  `targetsHidden` flag the detail response sets for scoped callers
  whose view was trimmed.

`GET /api/v1/me` is unchanged.

### 7. Wiring and the sweep
Each action handler set already has, or gets, `SetScope`; `internal/app`
passes the same `nodeScope`. `orchestrator.Handlers` and
`agentupgrade.Handlers` gain it; `inventory`, `nodeconnectivity` and
`packageinventory` already hold one and start using it on their action
routes (the grant on the request is the action permission's, because
`authorize` records the gating permission's groups).

The sweep test is extended:

- the route pattern covers every method, so each permission-gated
  route must be in the swept set or the exempt set;
- for each action route it sends the request as the scoped user
  against the hidden node and requires 403 and no side effect: no row
  in `jobs`, no call recorded by the stand-in CA and node transport, no
  deactivation command at the stand-in openvoxdb;
- job routes move from exempt to swept, with seeded jobs on each node.

## Risks / Trade-offs

- **A narrowed role loses actions on upgrade.** That is the point, but
  it is a behaviour change for anyone who narrowed a role last release.
  → Called out as breaking in the proposal, in `operations.md` and the
  guide; roles on All Nodes are untouched.
- **A group-wide run is refused if one node is out of the caller's
  action scope.** → The refusal names the nodes. Running on "the nodes
  of a group I am granted on" always passes, which is the common case.
- **Job status can reflect a hidden node.** → Stated; the job page says
  nodes are hidden.
- **Job history follows current membership.** → Stated in the guide.
- **A missed action endpoint is worse than a missed read.** → The sweep
  now requires every gated route to be classified, and checks for side
  effects, not just response bodies.
- **Scope resolution failure on an action** → 502, never proceed.

## Migration Plan

No schema change. Deploy; fleet-wide roles and service tokens are
unaffected. Before deploying, an operator who narrowed roles under the
previous release should check those roles' action permissions, since
their members stop being able to act outside their groups.

Rollback: deploy the previous build. Actions become fleet-wide again
for narrowed roles; `operations.md` already says rolling back widens
access.

## Open Questions

- Whether a later change should let the console offer "run on the nodes
  I am allowed to" for a mixed selection, once there is evidence people
  hit the all-or-nothing refusal often.

## Implementation Notes

- **One entry point for every action:** `nodescope.Permit(w, r, scope,
  certnames...)` returns whether the handler may go on, having written
  the 403 (or a 502 when the scope cannot be resolved) itself. Every
  action handler calls it first.
- **Certificate actions are checked before "not configured".** A
  deployment without the certificate-admin credential answered 503
  before anything else; the scope check now comes first, so a scoped
  caller gets the same 403 whether or not the deployment has a CA
  client.
- **Plans have no targets of their own.** The plan row carries none;
  its steps do. So a plan is in scope when any step ran on an in-scope
  node, and its `targetsHidden` is computed over its steps' targets.
  Steps are ordinary jobs and are filtered like any other.
- **The refusal message is produced once, in `fetchJSON`** (decision
  6's "shared helper"): a 403 carrying `outOfScope` becomes a
  translated message on the thrown error, so every page that already
  shows a failed action's error shows this one, with no per-page code.
- **Sweep stand-ins:** the stand-in openvoxdb also serves as the CA and
  records every state-changing request. The node transport is not
  stood in for: the hidden node is not connected, so the evidence that
  nothing was dispatched is the 403 itself plus the unchanged job
  count. Removing the scope check from node removal was confirmed to
  fail the sweep on both the status and the recorded openvoxdb and CA
  calls.


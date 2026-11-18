## Context

`orchestrator:run` is granted through roles, and each role is granted on
node groups (`role_node_groups`). When a token is issued, the console
works out for each permission the union of the groups of every role that
carries it, and puts that in the access token as `nodeScopes`. A request
is then limited to the nodes in those groups, resolved to certnames by
`nodescope.Resolver` and carried into the orchestrator as
`StartRequest.Scope`. A schedule has no token, so `scheduleAuthority`
works the same scope out from the owner's roles at each firing. Service
tokens have no roles: they carry permissions directly and are
fleet-wide.

Every job start goes through `orchestrator.Starter`: `StartRun`,
`StartTask`, `StartPlan`, their `Check*` forms, and `CheckPlanTemplate`
for saved plans. That is the one place a check can be put and be sure no
caller bypasses it. See proposal.md for why.

The scope model is a union per permission. That is fine for "which
nodes", but it loses the pairing between a role's node groups and what
the role allows, which is exactly what a task allowlist needs: with a
plain union, a role allowing only `patch::*` on the web tier plus a role
allowing everything on the database tier would together allow
`reboot` on the web tier.

## Goals / Non-Goals

**Goals:**

- A role or service token can be limited to named tasks, with no change
  for those that are not.
- The limit is paired with the role's own node groups.
- Every path that starts, saves or fires a task goes through one check.
- A refusal names the task, and the step for a plan, and reveals nothing
  about whether a task exists.

**Non-Goals:**

- Limiting Puppet runs, or limiting what a caller may read.
- Per-environment or per-parameter limits.
- A new permission. The allowlist narrows `orchestrator:run`; it never
  grants anything on its own.

## Decisions

### 1. The allowlist is a column on the role, not a permission

A role gets `task_patterns text[] NOT NULL DEFAULT '{}'`, as does
`service_tokens`. An empty array means every task.

*Alternatives.* A permission per task (`orchestrator:run:patch::apply`)
would put an open-ended set into the permission vocabulary, the Roles
page's permission list and the sweep. A separate table
(`role_task_patterns`) matches how permissions and node groups are
stored, but there is no query that wants one pattern at a time, and an
array keeps a role's list one row read. Empty-means-all is the only
reading that leaves every existing role unchanged without a data
migration.

### 2. Patterns: a task name, `module::*`, or `*`

Task names are `module` (a module's init task) or `module::task`, with
the lowercase-letters-digits-underscores rule Puppet applies. `module::*`
matches `module` and every `module::task`. Anything else (a wildcard
elsewhere, an empty pattern, upper case) is refused when the role is
saved. Matching is exact and case-sensitive; there is no regular
expression.

*Alternatives.* Globs or regular expressions are more flexible and make
it easy to grant by accident (`patch*` also matching `patching`). The
cost of this grammar is that two modules need two patterns, which is
the safer way round for an access control list.

Limits are 20 patterns of 64 characters per role or token, because the
grants travel in the access token (decision 4).

### 3. Pairing: a grant is (patterns, node groups), one per role

A caller's task grants are the list of `{tasks, groups}` pairs, one for
each role that carries `orchestrator:run`; a role with an empty list
contributes `{["*"], its groups}`. A task `T` may be run on the nodes in
the union, over the grants whose patterns match `T`, of each grant's
groups. A service token contributes one fleet-wide grant with its
patterns, or none when it has none.

*Alternatives.* Keep the per-permission union and apply the union of all
allowlists on top: simple, and the escalation described in Context.
Computing the pairing at request time from the database instead would
mean a lookup on every request; the token already carries the node
scopes this way, and a token that is a quarter of a second stale is not
a new exposure.

When no role of a user has an allowlist, no grants are written to the
token at all, and an absent list means unlimited. That keeps tokens as
small as they are today for every installation that does not use the
feature, and is also what makes a token issued before the upgrade
unlimited (spec: "Existing roles and tokens keep every task").

### 4. The grants are embedded in the access token

`Claims` gains `taskGrants`, written at issuance from the same role
lookup that produces `nodeScopes`, and read by the same middleware. A
role change therefore takes effect at the next refresh, exactly as a
permission or node group change does.

Size is the cost: 20 patterns of 64 characters is about 1.3 KB per role
in the worst case, and a user with several restricted roles approaches
the 8 KB request header limit some reverse proxies apply. Typical use is
a handful of short patterns; the limits in decision 2 exist to bound the
worst case, and the guide says so.

### 5. One package for the model: `internal/taskscope`

The pattern rules, the grant type, `Scope` (grants resolved to node sets)
and the request context carrier live in a new leaf package. `rbac` needs
the rules to validate a role and the type for claims; `orchestrator`
needs `Scope` to check a start; neither can import the other. The
package depends only on `nodescope`, which gains `Scope.Union`.

### 6. The check sits in `Starter`, before the catalogue is read

`StartRequest` gains `Tasks taskscope.Scope`. In a task start, and in
each task step of a plan in order:

1. the task's name is matched against the grants. No match is a
   `TaskNotPermittedError` (`403`, names the task, and the step for a
   plan);
2. only then is the task looked up in the catalogue.

Matching before the lookup is what keeps "not allowed" and "does not
exist" indistinguishable to a caller who is not allowed (spec: "Existence
is not revealed"); the lookup's own "unknown task" error is reached only
by a caller who is allowed to ask about the name.

Targets are resolved first, with the caller's `orchestrator:run` scope,
unchanged: a refusal for a node outside the caller's groups still comes
before anything else, so nothing a caller learns about nodes depends on
the rest of the request. The task's own node set is then applied to what
resolved:

- **Single task.** The node set is the run scope intersected with the
  nodes the task is allowed on. A node list with a node outside it is
  refused as any node outside the scope is. A query or group is
  narrowed, as it already is to the run scope.
- **Plan.** One target is resolved once for every step. Each task step
  has its own allowed node set. A node list naming a node outside any
  step's set is refused naming the step and the node; a query or group
  is narrowed to the nodes every task step may run on (the intersection
  of the sets), so every step still runs on the same nodes. Narrowing to
  nothing starts nothing, as a target that matches no nodes does.

*Alternative.* Refuse a plan whose query matches any node outside a
step's set, instead of narrowing. Easier to explain, but it is stricter
than a run, which narrows silently, and would make group targets
unusable for anyone with a partial grant.

### 7. Callers: a `TaskScope` function beside `nodescope.Func`

Handlers get `SetTaskScope(func(*http.Request) (taskscope.Scope, error))`,
built in `internal/app` from the request's claims and the same
`nodescope.Resolver`. It reads the claims rather than the per-permission
grant the `authorize` middleware records, because the catalogue is gated
on `orchestrator:read` while the grants belong to `orchestrator:run`.
With no claims it denies every task; with no `SetTaskScope` call a
handler set allows every task, as `nodescope.Unscoped` does, and a test
in `internal/app` fails for any handler set that starts or saves tasks
without one (the same arrangement as the node-scope sweep).

The scheduler's `Authority` returns the owner's node scope and task
scope together, from the owner's roles or token as they are at that
moment.

### 8. The catalogue hides tasks the caller may not run

`GET /api/v1/tasks` and `GET /api/v1/tasks/{module}/{task}` filter with
the caller's task scope, so a restricted caller's picker offers only
what they can run and a hidden task is answered as unknown. A caller
who holds `orchestrator:read` but not `orchestrator:run` is not
restricted, since an allowlist limits starting and they cannot start
anything.

### 9. Saved plans and schedules: the editor is checked, not the reader

Saving a plan checks each task step by name against the saver's scope
(no nodes, there being no target). Reading marks a step the reader may
not run in the plan's existing `problem` field, computed per caller at
read time as it is now, and the step stays visible. Saving a schedule,
or replacing one (which makes the replacer its owner), checks the
saver; firing checks the owner at that moment and records a skipped
firing naming the task.

A user with a narrower list than a plan's author therefore cannot edit
that plan without removing the step, and cannot run it. That is the
conservative reading, and it follows from "a plan is checked as a whole".

### 10. Audit: starts that are refused, not saves

A refused task start or plan start writes an audit entry
(`task.denied`: actor, task, step; never parameters), from the handler
that answers the refusal. A refused save is a validation failure, like
an unknown task, and is not audited; a schedule's refusal at firing is
its firing record. Role and service token changes already write audit
entries; their payloads gain the allowlist (before and after for a role).

### 11. Service tokens carry the allowlist themselves

The request called for "service tokens' roles", but service tokens have
no roles. The allowlist is therefore a field of the token, set at
creation and fixed for its life, as its permissions are.

## Risks / Trade-offs

- **A pairing bug is a privilege escalation.** → The node-set function is
  small and has a table test over every combination in the spec's
  scenarios, plus a live check with the two-role user from the spec on
  the dev stack.
- **Token size.** → Limits of 20 patterns and 64 characters, omission
  when unused, and a note in the guide about proxies.
- **A stale token.** A narrowed role takes up to 15 minutes to reach an
  open session. → Same as every other permission change; a revocation
  is available.
- **A plan can be unrunnable by someone who could read and schedule
  around it.** → Marked on read, named on refusal.
- **A handler set that forgets `SetTaskScope` allows everything.** → The
  test in `internal/app` fails for it; the default is the pre-existing
  behaviour so existing tests keep passing.

## Migration Plan

Migration 000034 adds the two columns with an empty default, so nothing
changes for existing rows. The down migration drops them. No data is
backfilled. Roll back by dropping the columns; an older binary ignores
unknown token claims.

## Implementation Notes

What differs from the design above, or was decided while building it.

- **A task refusal that is about the node names the task.** Decision 6 refuses
  a node list that names a node the task may not run on as an out-of-scope
  node. Live, that read as though the node were hidden from the caller, and a
  skipped schedule firing named only the node, though the spec says it names
  the task. `OutOfScopeError` therefore carries the task when it is a
  task-specific refusal, and says `your roles do not allow the task "X" on this
  node: N`, with `task` in the `403` body. The `outOfScope` list is unchanged.
- **The picker uses "may run on at least one node", not "named by a grant".**
  `taskscope.Scope.AllowsAnywhere` is what the catalogue filters with, so a
  grant on a role that covers no node (or an empty group) does not offer the
  task, while `Allows` (by name, no node) is what the start check uses first.
- **`UserTaskGrants` returns nil, never an empty list**, when no role carrying
  `orchestrator:run` has an allowlist, and the issuer refuses an empty non-nil
  list: JSON drops it, which would turn "may run nothing" into "no limit".
- **Service token allowlists** are a field of the token, as decision 11 says;
  they are written to the token's claims as one fleet-wide grant.
- **`rbac.WithClaims`** was added so a test can act as a caller it did not
  authenticate. **`internal/taskscope.Func`** and `UnrestrictedFunc` mirror
  `nodescope.Func` and `Unscoped`.
- **The saved plan step carries `notPermitted`** beside `problem`, so the
  console can say "your roles do not allow ..." rather than "can no longer run
  as saved", and can switch off Run without suggesting an edit would help. The
  catalogue is not read for such a step.
- **The plan editor** shows a step whose task the user may not run as an
  unchosen task, since the picker does not offer it, and the server refuses
  the save naming the step and the task.
- **The tasks list holds 30 tasks**, not the 29 stated when it was proposed.
- **An unrelated test was made independent of earlier runs**:
  `internal/activity`'s `TestListEvents_MostRecentFirst` compared its events
  with stale rows of the same category left by earlier runs in the shared
  database, and failed once enough had accumulated.

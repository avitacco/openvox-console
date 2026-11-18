## Context

See proposal.md for motivation. What exists today:

- `POST /api/v1/orchestrator/runs` and `/tasks` take `targets`, a list of certnames. `nodescope.Permit` refuses the whole request if any is outside the caller's `orchestrator:run` scope, before anything else is looked at. A job is created and dispatched in the request.
- A group's page resolves its matching nodes in the browser and sends them as `targets`. `groupnodes.Resolver.GroupCertnames` does the same on the server (one fleet fact query).
- `openvoxdb.Client.Query(ctx, pql)` runs any PQL string and returns rows. Its only check is balanced brackets and quotes.
- The caller is known to the orchestrator as a string, `claims.Subject`: a username, or a service token's name. Node scope arrives on the request context from the token.
- Background work that must run on one instance uses `internal/leases` (job reaper, vulnerability sync). Run modes pick which workers an instance starts (`internal/app/surface.go`).
- A task's sensitive parameters are redacted before the job is stored; the real values live only in the dispatching instance's memory. `internal/sealer` seals secrets at rest for vulnerability provider credentials, with `CONSOLE_SECRETS_KEY`.
- Run options (week 4) are stored in `jobs.params` and `jobs.environment`.

## Goals / Non-Goals

**Goals:**
- One target resolver used by on-demand requests, the preview and scheduled firings, so the three can never disagree about what a target means or what a caller may reach.
- A firing goes through the same checks and the same job creation as an on-demand request. The scheduler adds "when" and "as whom" and nothing else.
- At most one job per due time across instances, without a leader.
- No new way for a scoped caller to learn about nodes outside their scope.

**Non-Goals:**
- A PQL parser. The console recognises just enough of the syntax to count brace blocks.
- Calendar-aware recurrence (cron, "last day of the month", local-time anchoring across daylight-saving changes).
- Retrying a failed or skipped firing.
- Scheduling plans, batching, concurrency limits.
- Any change to the node agent client or the wire protocol.

## Decisions

### 1. One target type, three kinds

Requests gain `query` (string) and `group` (node group id) beside `targets`. Exactly one must be present. Internally:

```go
type Target struct {
    Nodes []string `json:"nodes,omitempty"`
    Query string   `json:"query,omitempty"`
    Group *int64   `json:"group,omitempty"`
}
```

`orchestrator.Resolve(ctx, target, scope) (certnames []string, source *TargetSource, err error)` is the single entry point. `TargetSource` is what the job stores: `{kind: "query", query}` or `{kind: "group", groupId, groupName}`; nil for a node list.

The existing `targets` field keeps its name and meaning, so nothing that calls the API today changes.

*Alternative considered:* a nested `target: {…}` object on the request. Cleaner, but it either breaks `targets` or leaves two spellings of the same thing.

### 2. Names are all or nothing; selectors are narrowed

A node list is checked with `scope.Outside` as now: one name outside refuses everything and the refusal names it. The caller typed those names, so naming them back reveals nothing.

A query or a group is resolved, then intersected with the scope (`scope.Intersect`). The caller did not name any node, so refusing and listing "nodes outside your scope" would hand them the names of other teams' nodes that match an arbitrary fact. Narrowing gives them the answer to "which of my nodes match". Nothing in the response says whether anything was left out; a query matching only other teams' nodes gets the same "no nodes matched" as one matching nothing.

### 3. Scoped callers get single-entity queries only

Narrowing the result is not enough by itself. A subquery lets a query about a caller's own node depend on a fact of somebody else's:

```
nodes[certname] { certname = "mine" and facts_environment in nodes[facts_environment] { certname = "theirs" } }
```

One bit per query, but a patient caller can read facts that way. So when the scope is not `All()`, a query with more than one `{` outside string literals is refused before it is sent. Both subquery forms in PQL (explicit `in entity[...] { }` and implicit `entity { }` nesting) need a second brace block, so this check is complete without parsing the language. A single-entity query yields rows that each describe one node, and rows for out-of-scope nodes are dropped.

`inventory[certname] { facts.os.family = "Debian" and trusted.extensions.pp_role = "web" }` needs no subquery, so the common cases remain available to scoped callers.

Fleet-wide callers (including service tokens) may send any query.

*Alternative considered:* rewriting the query to add `and certname in [...]`. Does not stop the oracle above (the outer clause is already about the caller's node), and needs real parsing to do safely.

### 4. What makes a query usable

In order, each a 400 with its own message:

1. Not empty; at most 4,000 characters.
2. Does not start with `[` (AST form is refused; the console's brace check assumes PQL).
3. The existing balance check.
4. The scoped-caller subquery check (decision 3).
5. openvoxdb accepts it. A 4xx from openvoxdb becomes a 400 carrying its message; a transport failure or 5xx stays a 502.
6. Every row has a string `certname`. `nodes[count()] {}` fails here.

Rows are reduced to distinct certnames, sorted. The query runs with a 30-second timeout. A typed `InvalidQueryError` in `internal/openvoxdb` distinguishes 4xx from unreachable; today `Query` returns one undifferentiated error.

### 5. Preview is the resolver with nothing after it

`POST /api/v1/orchestrator/targets/resolve` (`orchestrator:run`) takes a `Target` and returns `{"nodes": [...], "count": n}`. It calls `Resolve` with the caller's scope and returns what a job would have run on, or the error a job would have got. An empty result is a 200 with no nodes here (the preview should be able to say "nothing matches"); the run and task endpoints turn empty into a 400.

It is `POST` because a query can be long and should not land in access logs as a URL.

### 6. What a job remembers

Migration adds to `jobs`:

- `target_source JSONB` (null for a node list),
- `schedule_id BIGINT REFERENCES scheduled_jobs ON DELETE SET NULL`,
- `schedule_name TEXT` (kept so a job still names a deleted schedule).

`Job` gains `targetSource` and `schedule: {id, name}`. In `GetJobInScope`, when targets are hidden from the reader, `targetSource.query` is blanked: the query text may name nodes the reader cannot see. The group name stays (group definitions are not scoped).

### 7. Schedule storage

```
scheduled_jobs
  id, name
  kind                  'run' | 'task'
  run_options  JSONB    run: RunOptions
  task_name, environment, timeout_seconds
  params       JSONB    task: parameters, sensitive ones redacted
  sealed_params BYTEA   task: the sensitive values, sealed; null if none
  target       JSONB    Target
  start_at     TIMESTAMPTZ
  repeat_every INT, repeat_unit TEXT   null/null = once; unit in minute|hour|day|week
  next_run_at  TIMESTAMPTZ             null = nothing further due
  enabled      BOOL
  owner TEXT, owner_kind TEXT          'user' | 'service_token'
  created_at, updated_at

scheduled_job_firings
  id, schedule_id (ON DELETE CASCADE)
  due_at, fired_at
  outcome   'firing' | 'started' | 'skipped' | 'missed'
  detail    TEXT
  job_id    BIGINT REFERENCES jobs ON DELETE SET NULL
```

State shown to users is derived: `paused` when not enabled, `finished` when enabled with no `next_run_at`, otherwise `active`. A firing still `firing` after ten minutes is shown as `interrupted`. Firings beyond the newest 100 per schedule are deleted when a new one is written.

`owner_kind` is needed because a service token's name and a username share one string space in `claims.Subject`.

This lives in a new `internal/scheduler` package: it depends on `internal/orchestrator` (to resolve, validate and create), not the other way round.

### 8. Firing: compare-and-set, not a lease

Every instance with the `orchestrator-scheduler` worker ticks every 20 seconds and reads schedules with `enabled AND next_run_at <= now()`. For each, in one transaction:

```sql
UPDATE scheduled_jobs SET next_run_at = $next
 WHERE id = $id AND next_run_at = $due AND enabled
INSERT INTO scheduled_job_firings (schedule_id, due_at, fired_at, outcome) VALUES (…, 'firing')
```

The instance whose `UPDATE` changes a row owns that firing; the others see zero rows and move on. Due-ness and lateness are judged by the database clock (`now()` in SQL), as leases are, so instances with skewed clocks agree.

`$next` for a repeating schedule is the smallest `start_at + k × interval` greater than the database's `now()`, so however much time passed, one firing is taken and the schedule is back on its grid. For a one-off it is null.

*Why not a lease:* a lease serialises all firing through one instance and, if that instance stalls, delays every schedule until the lease expires. Compare-and-set makes each firing independently claimable and needs no renewal.

*At-most-once:* the claim commits before the job is created. If the instance dies in between, the firing stays `firing` (shown as interrupted) and is not picked up by anyone. Starting a Puppet run twice, or a task such as a reboot twice, is worse than not starting it.

### 9. What the claiming instance does next

1. **Late?** `now() - due_at > 15 minutes` → `missed`. Stop.
2. **Overlap?** The newest earlier firing of this schedule has a `job_id` whose job is still `running` → `skipped`.
3. **Authority.** `Authority(ctx, owner, ownerKind) (nodescope.Scope, error)`, injected from `internal/app`:
   - user: the user exists; `rbac.Store.UserPermissions` includes `orchestrator:run`; scope from `UserNodeScopes["orchestrator:run"]` through the existing `nodescope.Resolver.For`.
   - service token: a token of that name still exists and carries `orchestrator:run`; scope is everything.
   - otherwise `skipped`: "owner no longer holds orchestrator:run".
4. **Target.** `orchestrator.Resolve(target, scope)`. Refusal or empty → `skipped` with the reason. For a node list outside scope, the reason names the nodes (the firing is visible only to the owner, who typed them, and to fleet-wide callers).
5. **Action.** Run: `decodeRunOptions` and the environment-exists check. Task: `prepareTask` against the catalogue as deployed now, with sealed values opened and merged into the parameters first. Failure → `skipped`.
6. **Start.** `CreateRunJob` / `CreateTaskJob` with `triggeredBy = owner`, the target source and the schedule, then `DispatchRun` / `DispatchTask`. Firing → `started` with `job_id`.

Steps 4 to 6 are the bodies of `triggerRun` and `triggerTask` today. They are pulled out of the HTTP handlers into `orchestrator.StartRun(ctx, StartRequest)` and `StartTask`, which the handlers and the scheduler both call. `StartRequest` carries the target, the scope, the actor and an optional schedule reference. Errors are typed (`OutOfScopeError`, `InvalidTargetError`, the existing task and run-option errors) so the handler maps them to today's statuses and the scheduler maps them to a firing's `detail`.

Dispatch from any instance already works: requests go over NATS to whichever instance holds the node's connection.

### 10. Saving is a dry run of firing

Create and replace run steps 3 to 5 with the caller's request scope, and store only if they pass, with one difference: an empty resolution is accepted (the fleet may not match yet). Then schedule fields are checked: name present and at most 100 characters; `start_at` in the future for a one-off; `repeat_every ≥ 1` and the interval at least five minutes; unit one of the four.

### 11. Recurrence is a fixed interval from the start time

`every 1 day` is 24 hours, anchored at `start_at`. In a zone with daylight saving, a daily 02:00 schedule becomes 03:00 or 01:00 local for half the year. The form and the guide say so, and show the next start in local time with the zone, so it is visible rather than surprising.

*Alternative considered:* storing a zone and anchoring in local time. Correct, but it brings the questions of nonexistent and repeated local times, and with them most of a cron implementation. Left for a later change if it is asked for.

### 12. Saving takes ownership

Whoever saves a schedule's contents owns it. A schedule therefore always runs with the authority of the person who last read and approved what it does, and an administrator takes over a departed user's schedule by opening and saving it. Pause and resume do not change ownership, since they do not change what runs.

No escalation is possible in either direction: a scoped caller can save only their own schedules, and a fleet-wide caller already has at least the authority of any owner.

### 13. Sensitive parameters

On save, sensitive values (by the task's metadata, as `redactSensitive` decides today) are taken out of `params`, replaced by the redaction marker, and sealed as one JSON object into `sealed_params`, with associated data `scheduled_job:<id>`. Without a sealer the save is refused with a message naming `CONSOLE_SECRETS_KEY`.

On replace, a sensitive parameter absent from the request keeps its sealed value when the caller is the current owner and `task_name` and `environment` are unchanged. Otherwise it is simply absent, and validation reports it if it is required. A different user therefore cannot inherit a secret and redirect it to a node they control.

The job a firing creates stores redacted parameters, exactly as an on-demand task job does.

### 14. Visibility

`GET /schedules` and `GET /schedules/{id}` (`orchestrator:read`): all schedules for a caller whose read scope is `All()`, otherwise those where `owner` and `owner_kind` match the caller. Mutations (`orchestrator:run`): the owner, or a caller whose run scope is `All()`. Anything else is 404, the same body as a missing id.

Routes:

```
GET    /api/v1/orchestrator/schedules
POST   /api/v1/orchestrator/schedules
GET    /api/v1/orchestrator/schedules/{id}
PUT    /api/v1/orchestrator/schedules/{id}
DELETE /api/v1/orchestrator/schedules/{id}
POST   /api/v1/orchestrator/schedules/{id}/pause
POST   /api/v1/orchestrator/schedules/{id}/resume
```

All go into the node-scope route sweep (`internal/app/node_scope_sweep_test.go`): the two reads as scoped reads, the mutations and `targets/resolve` as actions.

### 15. Run mode

A new worker, `orchestrator-scheduler`, in the modes that run `orchestrator-dispatcher` today. It needs the dispatcher on the same instance, because a task's resolved payload is held in the dispatching instance's memory.

### 16. Frontend

- `target-picker.js`: a segmented choice of Node list / Query / Node group; a textarea, a query box, or a group select; a Preview button calling `targets/resolve`; the result as a count and a scrolling list. Exposes `getTarget()` and `isPreviewed()`.
- `run-puppet.js` and `run-task.js`: when opened without preset targets (from the Jobs page), embed the picker. Both gain a "Schedule instead" link that opens the schedule form carrying what has been filled in.
- `run-task.js`'s environment, task and parameter form is extracted so the schedule form can embed it.
- `schedules.html` (list), `schedule.html?id=` (detail with firings and controls), `schedule-edit.html` (form). The Jobs and Scheduled jobs pages share a two-item sub-navigation.
- Times use `<input type="datetime-local">` (native, as the datalist fields are), converted to RFC 3339 with the browser's offset, and are shown with `Intl.DateTimeFormat` including the zone name.
- `job.js` and `jobs.js` show the target source and a "Scheduled" badge linking to the schedule.

## Risks / Trade-offs

- [The brace count is a heuristic for "has a subquery"] → It errs toward refusing: a `{` inside a regex literal is inside a string and is not counted, and no valid single-entity query has two blocks. Tests cover both subquery forms, braces in strings, and escaped quotes. If PQL grows another cross-entity form, scoped callers need the check extended.
- [A scoped caller's query still runs against the whole fleet in openvoxdb] → Cost only; rows outside scope are dropped before anything is returned. Query time is bounded at 30 seconds.
- [A firing lost to a crash is not retried] → Chosen (decision 8). It is visible as interrupted on the schedule and in the list.
- [A deleted node group silently stops a schedule] → Each firing is recorded as skipped with the reason, the list marks the schedule, and the activity log has it. Deleting a group does not warn about schedules in this change.
- [The fifteen-minute lateness limit is fixed] → A constant, not configuration, until somebody needs otherwise.
- [Fixed-interval recurrence drifts against local time] → Decision 11; stated in the form and the guide.
- [Ownership by save could surprise an administrator who only meant to fix a typo] → The form says "this schedule will run as you" when the saver is not the owner.
- [Sealed parameters are unreadable if `CONSOLE_SECRETS_KEY` changes] → The firing is skipped with that reason; re-entering the values fixes it. Same exposure as provider credentials today.
- [Every scheduler instance polls every 20 seconds] → One indexed query on `next_run_at` per tick per instance.

## Migration Plan

One additive migration (two tables, three nullable columns on `jobs`). Existing jobs read with no target source and no schedule. No data to convert, no client change, no configuration required; sensitive scheduled parameters need the secrets key that vulnerability providers already use.

Rollback: stop the new binary and run the down migration, which drops the tables and columns. Jobs keep everything else.

In a rolling upgrade, an older instance never fires a schedule (it has no scheduler), and a job created by a newer instance reads correctly on an older one, which ignores the new columns.

## Open Questions

None that change the specs or the tasks.

## Implementation Notes

What was built differs from the decisions above in these ways, none of which changes a requirement.

- **Starter, not free functions (decision 9).** `StartRun` and `StartTask` are methods of `orchestrator.Starter`, which the HTTP handlers own and hand to the scheduler (`Handlers.Starter()`). `CheckRun` and `CheckTask` are the same preparation with an empty resolution allowed; saving a schedule calls those.
- **`TargetResolver` (decision 1)** is a type holding the query and group sources, rather than a function taking them.
- **Overlap (decision 9, step 2)** is "any job this schedule started is still running", read from `jobs.schedule_id`, rather than only the job of the newest earlier firing. It is the same answer in practice and one query.
- **The preview is shown before a job starts (decision 16).** In the dialogs opened from the Jobs page, choosing Run with no preview on screen runs the preview and stops, saying to check the nodes; choosing Run again starts the job. Any change to the choice clears the preview.
- **"Schedule instead"** hands the half-filled form over in `sessionStorage`, read once and removed, because it can hold a task's sensitive value and must not go in a URL. From a group's page it carries the group, not the nodes in it at that moment.
- **`scheduler.TickInterval` is a variable**, so the application-level tests can fire schedules in a fraction of a second. Production never sets it.
- **Refusals carry `field`** (`name`, `startAt`, `repeat`, `kind`, `params`, `target`, `options`, `task`) so the form can place them. `fetchJSON` passes it through as `err.field`.
- **Single-quoted PQL strings** are recognised by the subquery check as well as double-quoted ones. Confirmed against the dev openvoxdb that it accepts both, so a brace inside either is inside a string.
- **Schedule times are returned in the server's zone offset**, not forced to UTC. They are RFC 3339 either way and the browser renders them in its own zone.
- **A "finished" schedule offers no Pause button.** The API still accepts the call.

Found on the way and left alone, because it is outside this change: a job started through a `web`-mode instance never completes (see `operations.md`). The scheduler runs only in modes that also run the dispatcher, so scheduled jobs do not depend on it.

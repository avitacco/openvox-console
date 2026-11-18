## Context

- A group's classes are `[]Class{Name, Parameters map[string]any}`,
  edited on the group page as a JSON textarea (`frontend/src/group.js`).
  The group's environment is a free-text field; `tree.go` resolves the
  effective environment down the hierarchy.
- The console already talks to openvoxserver's `/puppet/v3` API for
  tasks: `internal/taskcatalog.Client` (mTLS, `CONSOLE_PUPPET_SERVER_URL`
  with the openvoxdb certificate as the default credential), with typed
  not-found and unreachable errors and a parameter validator for the
  subset of Puppet types task metadata uses.
- A run is `POST /api/v1/orchestrator/runs {targets}` →
  `Dispatcher.DispatchRun` → node-agent request `{module, action: "run",
  params: null}` → `puppet agent --onetime --no-daemonize
  --detailed-exitcodes`. The old client ignores `params` on `run`.
- Jobs already have `environment` and `params` columns (used by tasks)
  and per-target `output`.
- A successful deploy publishes `codemanager.deployed` on the bus.
- Checked against the dev stack: `GET /puppet/v3/environment_classes`
  with the console certificate answers **403**. The server's default
  `auth.conf` has no rule for it; `/puppet/v3/tasks` and
  `/puppet/v3/environments` are allowed to any certificate.

## Goals / Non-Goals

**Goals:**
- Pick classes and parameters instead of typing them; see when a group
  no longer matches its code.
- No-op, environment and debug on a console run, with the output where
  the user is already looking.
- Nothing about either feature can break classification or turn a no-op
  into a real run.

**Non-Goals:**
- Parsing Puppet code in the console.
- Full Puppet type checking.
- Forcing an environment over a node's classification.
- Options on plan steps, initial runs or the package-toggle run.

## Decisions

### 1. Classes come from `environment_classes`, behind one auth rule

The server's `environment_classes` endpoint is the only source that is
correct (it uses Puppet's own parser) and complete (names, parameters,
types, defaults). It needs an `auth.conf` rule the default server does
not have.

Alternatives considered:
- **Parse manifests in the console.** No Go parser for the Puppet
  language; a partial one would be wrong in ways users cannot see.
- **Classes seen in openvoxdb catalogs.** Only classes already in use,
  no parameters, types or defaults. Useless for adding a new class.

The rule allows the console's certificate (the one it presents to the
server, by default its openvoxdb certificate) to `GET
/puppet/v3/environment_classes`. The install guides and the dev compose
stack carry it. An installation without it loses nothing it has today:
the editor falls back to typed names and says the server refused.

### 2. A new `internal/classcatalog` package over the existing server client

`taskcatalog.Client` gains `EnvironmentClasses(ctx, env, etag)`; the
connection, credentials and error types are shared, not duplicated. It
adds a typed forbidden error so "the rule is missing" is told apart from
"unreachable".

`classcatalog` owns the cache, the shape served to the frontend, and
checking a group's classes against an environment. It does not import
`classifier`; `classifier` handlers call it through a small interface
(`Classes(ctx, env)`), nil-safe like `TaskCatalog`.

Routes (in `classcatalog`):
- `GET /api/v1/classes/environments` - `classifier:read`
- `GET /api/v1/classes?environment=` - `classifier:read`
- `POST /api/v1/classes/refresh?environment=` - `classifier:write`

A separate environments route is needed because the existing
`/api/v1/tasks/environments` is gated by `orchestrator:read`, which a
classification-only user need not hold.

Response for classes:

```json
{
  "environment": "production",
  "fetchedAt": "2026-11-14T20:45:02Z",
  "stale": false,
  "classes": [
    {"name": "ntp", "parameters": [
      {"name": "servers", "type": "Array[String]", "required": false,
       "defaultSource": "['pool.ntp.org']", "default": ["pool.ntp.org"]}
    ]}
  ],
  "errors": [{"file": "site/profile/manifests/bad.pp", "error": "Syntax error at ..."}]
}
```

`default` is present only when the server gives a literal default;
`defaultSource` is the expression text, shown as the placeholder.
Unavailable is `503` with `{"error", "reason"}`, `reason` one of
`not_configured`, `forbidden`, `unreachable`.

### 3. Per-instance cache, five minutes, invalidated by deploys

Held in memory per console instance, keyed by environment:
- fresh for five minutes, then re-read on the next request, sending the
  server's `ETag` back as `If-None-Match` when it gave one (the server
  only does so with its class cache enabled; without it the read is a
  full parse, which five minutes makes tolerable);
- every entry is marked expired on `codemanager.deployed`, which each
  instance receives from the bus;
- `POST .../refresh` re-reads at once on the instance that serves it;
  other instances catch up within five minutes;
- on a failed re-read, the old entry is served with `stale: true`.

Not stored in Postgres: it is derived data the server can always
reproduce, an instance that restarts simply re-reads it, and a table
would add a migration and a sync job for no gain. This is the same
shape as `nodescope.Resolver`'s cache.

Concurrent requests for the same environment share one server read.

### 4. Flags are warnings, computed on read, never stored

`classifier` already returns `warnings` on a group. Three kinds are
added: `class-missing`, `parameter-unknown`, `parameter-type`. They are
computed when a group is read, from the cache for the group's effective
environment.

- The group **list** includes a `classWarnings` count per group so the
  list can mark it, using only environments already cached or readable;
  it never fails because discovery does.
- Nothing is flagged when the answer is stale or unavailable: a stale
  list after a deploy would flag exactly the classes that were just
  added.
- A class defined in a manifest that failed to parse cannot be known;
  when the environment has parse errors, `class-missing` is suppressed
  for it and the page shows the parse errors instead.
- Type checks reuse `taskcatalog`'s type validator, extended only as far
  as is cheap. A type it does not understand passes. `Sensitive[...]`,
  `Optional[...]` and `Variant[...]` of known types are handled; the
  rest are not.
- Save is never blocked and the ENC path does not call `classcatalog`
  at all, which is what guarantees discovery cannot affect
  classification.

### 5. Effective environment for the editor

Own environment, else nearest ancestor's, else `production`. This is
what `tree.go` already resolves for a node in that group *when no other
matched group overrides it*. A node can match other groups that win;
the editor cannot know that per node, and says "classes from
`<environment>`" next to the picker so the basis is visible. On an
unsaved change of the environment field the picker reloads for the new
value.

### 6. Run options ride on a new agent action

`run` with `params` would be the obvious wire shape, but every deployed
client ignores `params` on `run` and performs an enforcing run. A no-op
request executed as a real run is the one failure this feature must not
have. So:

- no options → action `run`, `params` null: byte-for-byte today's
  request, for every client version;
- any option → action `run_with_options`, `params`
  `{"noop":bool,"environment":string,"debug":bool}`. An old client
  answers `unsupported action "run_with_options"`; the dispatcher turns
  that into a failed target with "this node's agent client is too old
  for run options; upgrade it" (the console can already upgrade
  clients).

The client decodes `params` strictly (unknown field → error) so a
future option is likewise never dropped silently.

Alternative considered: check each target's recorded client version in
the console first. Rejected as the only guard - the record can be out of
date - though the dialog may use it to warn.

### 7. What the client runs

```
puppet agent --onetime --no-daemonize --detailed-exitcodes
             [--noop] [--environment <env>] [--debug | --verbose]
```

- `--verbose` is added whenever there are options and no `--debug`, so a
  no-op run prints the changes it would make. A plain run stays silent,
  as today.
- `--environment` is one argv element; the name is validated in the
  console and again in the client against `^[a-z0-9_]+$`i, Puppet's own
  rule. No shell is involved.
- Output is captured with the same bounded buffer tasks use and returned
  in the existing `output` field.
- `--detailed-exitcodes` meanings are unchanged; a no-op run with
  pending changes exits 0.
- The single in-flight request rule is unchanged.

### 8. Storing and showing options

- `jobs.params` holds the options JSON for a run with options;
  `jobs.environment` holds the requested environment. No migration.
  `Job` gains `runOptions` in its JSON, derived from `params` for run
  jobs.
- Console validation: environment name pattern always; existence checked
  against the server's environment list when that list can be read. If
  it cannot be read the run proceeds (an unreachable server already
  means the run will report its own failure).
- Activity/audit: `run.triggered` summary gains "no-op", "environment
  X", "debug".
- Jobs list: a "No-op" badge and the environment. Job page: the options,
  per-target output (already rendered for tasks), and the mismatch mark.

### 9. Environment mismatch is read from the report

Puppet lets the server's answer win: when the ENC returns an
environment, the agent switches to it. The console's ENC returns one
whenever a matched group sets it. Forcing the request would mean the ENC
answering differently for one run - state that outlives a failed job and
changes what a concurrent scheduled run compiles. Not done here.

Instead the truth is shown: targets already carry `reportHash` from the
report correlator; the job detail adds `reportEnvironment` per target
from the report, and the page marks a target where it differs from the
requested one. No report, no mark. The dialog carries one sentence
stating the rule.

### 10. Frontend

- Classes editor: rows of class → parameters, built from voxblocks
  inputs; class and parameter names use a filterable picker that also
  accepts free text. Value inputs by type: `Boolean` → select
  (unset/true/false), `Integer`/`Float` → number, `Enum[...]` → select,
  `String` and similar → text, anything else → JSON text. JSON view
  toggle as on match rules; "Editing a group never saves over unloaded
  values" continues to hold because the editor round-trips the full
  classes array, including classes and parameters it does not recognise.
- Run dialog: one shared module used by nodes, node and group pages.
- New strings extracted; German translated; other locales reported.

## Risks / Trade-offs

- **Installations that skip the `auth.conf` step get no picker.** →
  Specific on-page message naming the cause; guides carry the step;
  nothing else degrades.
- **Full parse on each read when the server's class cache is off.** →
  Five-minute reuse, shared in-flight reads, deploy-driven invalidation
  rather than polling.
- **Instances disagree for up to five minutes after a manual refresh.**
  → Accepted; deploys invalidate everywhere.
- **Flags can be wrong for a node whose environment is set by another
  group.** → The page names the environment the flags are based on.
- **Requested environment silently not used.** → Stated in the dialog,
  marked on the job. Forcing it is left as an open question.
- **Debug output is large.** → Bounded buffer with a truncation notice.
- **Old clients fail runs with options.** → Intended; the reason names
  the fix, and plain runs are untouched.

## Migration Plan

- No schema change. Deploy console; add the `auth.conf` rule and reload
  openvoxserver when convenient; upgrade node agent clients (automatic
  where auto-upgrade is on) to use run options.
- Rollback: the previous console ignores nothing new in the database;
  jobs created with options show as plain runs with params.

## Open Questions

- Should a later change let a run force its environment over
  classification (as PE does through an "agent-specified" environment
  group)? Not needed for no-op and debug, which are the common cases.
- Should the System page report class discovery's state alongside the
  other server checks? Left out; the group page already reports it.

## Implementation Notes

Where the build departed from the decisions above, and what it found.

- **Class warnings are their own field, not three more kinds of
  `warnings`** (decision 4). The existing `warnings` entries describe an
  ancestor override and carry `ancestorId`; a class mismatch has no
  ancestor. Group responses gained `classWarnings`, `classEnvironment`
  and `classesChecked` instead. The list carries the warnings
  themselves rather than a count; the page counts them.
- **The broken manifest is not part of the fixture module** (task 1.2).
  It was in the module when the response was recorded, and is kept at
  `internal/classcatalog/testdata/broken.pp`. Left in the module it
  would put a permanent parse-error notice on every group page of
  anyone using the fixture control repo, and suppress every "not
  defined" mark.
- **`--color=false`** was added to a run with options (decision 7):
  Puppet writes ANSI colour codes even when stdout is a pipe.
- **A refusing server drops the earlier answer** (decision 3). Stale
  classes are served only when the server is unreachable. A 403 or 404
  is the server's answer, and is reported as such.
- **A failed read is remembered for 30 seconds**, so a server that is
  down delays one group-list request in that window, not every one.
- **`Sensitive[T]` is now checked as `T`** by the shared type checker,
  which also applies to task parameters. A task's sensitive parameter
  given a value of the wrong type is now refused by the console rather
  than by the task.
- **The rule's certificate name is configurable**
  (`CLASS_DISCOVERY_CERTNAME` on the server container, default
  `console`), since the rule names one certificate rather than any.
- **The run dialog replaces the bulk-run confirmation** on the nodes
  page and adds a confirmation step to the node and group pages, which
  used to start the run on the first click.
- **A run's output was already stored**, and never shown. The job page
  now shows it for every run that has any, not only runs with options.

Found, not fixed here:

- **The server's environment cache.** With `environment_timeout =
  unlimited` (the container image) openvoxserver keeps serving the
  classes it first loaded until its environment cache is flushed. The
  console does not flush it after a deploy, so on that image neither
  catalogs nor the class list see new code until an operator does. The
  guides say so. Having the console flush it after a deploy would fix
  both, and is a code-manager change.
- **The dev stack has no code source configured**, so the
  deploy-expires-the-cache path was verified by test, not live.

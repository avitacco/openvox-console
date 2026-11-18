## Why

Two places where the console still asks for more than Puppet Enterprise's
does, both met every day:

- **Classes are typed as JSON.** A node group's classes are a JSON
  textarea. The console does not know which classes an environment's code
  defines, what parameters they take, or their types and defaults, so a
  misspelt class name or parameter is found only when a node's catalog
  fails to compile. A class removed from the code leaves the group naming
  it with nothing to say so.
- **A Puppet run has no options.** "Run Puppet" always does a normal
  enforcing run in the node's own environment with no output. The
  everyday checks - "what would this change?" (no-op), "try this node
  against my branch" (environment), "why did it fail?" (debug output) -
  need a shell on the node.

This is week 4 of the PE-parity plan. It builds on the group hierarchy
and the group-scoped access changes, and changes neither.

## What Changes

### Class discovery

- The console reads each environment's classes from openvoxserver
  (`GET /puppet/v3/environment_classes`), with each class's parameters,
  their Puppet types and their defaults, through the server connection
  it already uses for tasks.
- New API, gated by `classifier:read`: the environments the server has,
  and one environment's classes. Gated by `classifier:write`: refresh an
  environment's classes now.
- Classes are held per console instance and refreshed when they are
  older than five minutes, when a code deploy finishes, and on request.
  When the server cannot be reached the last good answer is served and
  marked stale.
- The group page's classes section becomes a structured editor: pick a
  class from the group's environment, add its parameters from the
  declared list, and enter each value in an input that fits its type,
  with the default shown. The JSON view stays, as it does for match
  rules. The environment field becomes a picker of the server's
  environments.
- A group's environment for this purpose is its own, else its nearest
  ancestor's, else `production`.
- **Flagged, never dropped.** A group that names a class the environment
  does not define, or a parameter the class does not declare, or a value
  that does not fit a simple declared type, shows a warning on the group
  page, and the groups list marks the group. Saving is never blocked and
  the ENC answer is unchanged - the code may simply not be deployed yet.
- When discovery is unavailable - no server connection configured, or
  the server refuses the console's certificate - the editor falls back
  to typing class names, says why, and nothing is flagged.
- **Server setup:** openvoxserver refuses `environment_classes` by
  default (confirmed against the dev stack: 403). Installation gains one
  step, an `auth.conf` rule allowing the console's certificate to read
  it. An existing installation that skips the step keeps working exactly
  as today, with the notice above.

### Run options

- `POST /api/v1/orchestrator/runs` accepts optional `options`:
  `noop` (report what would change, change nothing), `environment` (run
  against this environment) and `debug` (debug-level output).
- The node agent client gains a run-with-options request and passes the
  options to `puppet agent`. It is a **new** request, not new parameters
  on the old one: a client that predates it answers "unsupported", so a
  no-op request can never be carried out as an enforcing run by an old
  client.
- A run with options records the agent's output on the job, as tasks do,
  so a no-op run's would-be changes and a debug run's log are read on
  the job page.
- The options are recorded on the job and shown on the jobs list, the
  job page and the activity log.
- The Run Puppet buttons on the nodes page, node page and group page
  open a dialog with the three options. Running with none is the request
  made today.
- **Environment is a request, not an override.** When a node's
  classification sets an environment, Puppet uses that one whatever the
  agent asks for. The dialog says so, and the job page marks each node
  whose run used a different environment from the one requested.
- Scope, permission and all-or-nothing behaviour are unchanged: options
  ride on the existing `orchestrator:run` path.

### Not in this change

- PQL targeting and scheduled jobs (week 5); plans UI and account policy
  (week 6); patch workflow; LDAP/SAML.
- Run options on plan steps, the initial run of a new node, and the run
  that follows a package-reporting toggle.
- Forcing an environment on a node whose classification sets one.
- Refusing to save a group over an unknown class or parameter.
- Reading classes by parsing code in the console.

## Capabilities

### New Capabilities

- `class-discovery`: reading, caching, refreshing and serving the classes
  each environment defines.

### Modified Capabilities

- `classifier`: structured class editor; groups flag classes, parameters
  and values that do not match their environment's code.
- `orchestrator`: Puppet run options, their record on the job, and the
  environment-mismatch mark.
- `node-agent`: executes a run request with options.

## Impact

- **Code:** new `internal/classcatalog`; `internal/taskcatalog` client
  (shared server connection) or a sibling client; `internal/classifier`
  (warnings, handlers); `internal/orchestrator` (handlers, dispatcher,
  job); `internal/nodeagent`; `internal/app` wiring; frontend
  `group.js`, `groups.js`, `nodes.js`, `node.js`, `job.js`, `jobs.js`
  and templates; locales.
- **Database:** none. Run options use the job's existing `params` and
  `environment` columns.
- **Settings:** none new.
- **Wire:** one new node-agent request. Older clients reject it; plain
  runs are unchanged for every client version.
- **Server setup:** one `auth.conf` rule on openvoxserver; the dev
  `docker-compose.yml` stack carries it.
- **Guides:** `use/classify.md` (class picker, warnings, refresh),
  `use/run-jobs.md` (run options, environment rule, client version),
  `install/containers.md` and `install/packages.md` (the `auth.conf`
  step). Also `operations.md` and `architecture-summary.md`.
- **Tests:** the node-scope route sweep lists the new class routes as
  exempt (they carry no node data).

## Context

See proposal.md - Why. The design-relevant state:

- `internal/nodeagent/handler.go` handles `run_task` by calling
  `puppet task run <task> --params <json>` through `CommandRunner`, which
  takes only a command and arguments: no stdin, no environment, no
  working directory. The agent has no `task` subcommand, so this always
  fails.
- `internal/orchestrator` sends `taskParams{Task, Params}` over the node's
  dispatch subject. Plans store their steps as child jobs and dispatch
  each step later from the stored row (`dispatchStep`), so anything a step
  needs at dispatch time must either be in Postgres or in memory on the
  dispatching instance.
- `dispatchTimeout` is one hour; the reaper fails a job still running
  after `2 * dispatchTimeout`. A job is followed to completion by the
  instance that started it.
- The console holds no credential meant for openvoxserver's `/puppet/v3`
  API. It has an openvoxdb client certificate (a plain CA-issued
  certificate) and an optional CA-client certificate carrying
  `pp_cli_auth`, which is full CA admin.
- openvoxserver's default `auth.conf` allows any CA-issued certificate to
  read `/puppet/v3/tasks` and `/puppet/v3/file_content` (checked against
  the dev stack). `GET /puppet/v3/tasks/<module>/<task>?environment=`
  returns the task's `metadata` and a `files` list, each with `filename`,
  `uri` (`path` and `params`), `sha256` and `size_bytes`.
- `node-agent-client` knows the transport address and its certificate
  paths but not its Puppet server's address.
- Nodes run Linux, macOS and Windows (agent-distribution).

## Goals / Non-Goals

**Goals:**

- Tasks that work as they do under Bolt and PE for the common cases:
  single-file and multi-file tasks, shell, PowerShell and Ruby
  implementations, all four input methods, structured output.
- Nothing runs on a node unless every file matches the checksum the
  console resolved from openvoxserver.
- No new mandatory configuration for existing deployments.

**Non-Goals:**

- Running tasks as another user (`run_as`), or noop for tasks.
- Agentless targets; plans beyond today's step lists.
- A stored or cached task catalogue in the console.
- Interpreter configuration on nodes (Bolt's `interpreters` setting).

## Decisions

### 1. The console reads tasks from openvoxserver's API, not from disk

A new package, `internal/taskcatalog`, is an HTTPS client for
`/puppet/v3/environments` and `/puppet/v3/tasks`. It parses metadata,
validates parameters, and resolves a task into the dispatch payload.

*Alternative:* read `modules/*/tasks/*.json` from the code directory the
console deploys with g10k. Rejected: only instances running the code
manager have that directory (not `web` or `orchestrator` run modes),
code deployed by something other than this console would be invisible,
and openvoxserver is what nodes will download from, so it should also be
what the console resolves against.

### 2. New optional settings, reusing the openvoxdb credential by default

- `CONSOLE_PUPPET_SERVER_URL` - the console's own view of openvoxserver
  (often loopback or container-internal, like `CONSOLE_CA_CLIENT_URL`),
  distinct from the node-facing `CONSOLE_PUPPET_SERVER_PUBLIC_ADDR`.
  Unset disables the catalogue and task runs.
- `CONSOLE_PUPPET_SERVER_CERT_FILE` / `_KEY_FILE` / `_CA_FILE` -
  optional; each defaults to the matching `CONSOLE_OPENVOXDB_*` value.

*Alternative:* reuse the CA-client credential. Rejected: it carries full
CA admin rights, and `operations.md` keeps it separate from every other
credential on purpose. Reading tasks needs only an ordinary certificate.

### 3. The console resolves; the node verifies

On a task request, the console fetches the task from openvoxserver,
validates parameters, and dispatches:

```json
{
  "task": "mymod::restart",
  "environment": "production",
  "input_method": "both",
  "implementations": [
    {"name": "restart.sh", "requirements": ["shell"], "input_method": "",
     "files": ["mymod/lib/helper.sh"]}
  ],
  "files": [
    {"filename": "restart.sh", "path": "/puppet/v3/file_content/tasks/mymod/restart.sh",
     "params": {"environment": "production"}, "sha256": "...", "size": 123}
  ],
  "params": {"service": "nginx"},
  "timeout_seconds": 1800
}
```

The node chooses the implementation (decision 5), downloads and verifies
the files, and runs the task. Each implementation's supporting files are
listed with their module-relative names (`module/lib/...`,
`module/files/...`), and the `files` list carries every file the task
can need.

*Alternative:* the console chooses the implementation from the node's
facts. Rejected: facts can be stale or missing (a node with no report
yet), and the node knows its own OS for certain.

*Alternative:* the console sends file contents over NATS. Rejected:
message size limits, and nodes already trust their Puppet server for
code. The console only vouches for checksums.

### 4. The node finds its Puppet server through Puppet's own settings

The node agent runs `puppet config print server_list server serverport
--section agent` once per task and uses the first `server_list` entry,
falling back to `server:serverport`. A new optional
`NODE_AGENT_PUPPET_SERVER` setting (`host:port`) overrides this.

*Alternative:* the console sends `CONSOLE_PUPPET_SERVER_PUBLIC_ADDR`.
Rejected: it is optional, and wrong for nodes pointed at a compiler. The
server the node's agent already uses is the one that serves its
environment's code.

### 5. Implementation selection and interpreters

Features: Windows provides `powershell` and `puppet-agent`; other OSes
provide `shell` and `puppet-agent`. The first implementation whose
`requirements` are a subset wins. A task with no `implementations` key
uses its single file, as Puppet does.

How a file runs:

- `.ps1` on Windows - through a PowerShell wrapper (see decision 6).
- `.rb` on any OS - the agent's bundled Ruby
  (`/opt/puppetlabs/puppet/bin/ruby`, or `ruby.exe` under the Windows
  install), since every node has `puppet-agent`.
- `.pp` - `puppet apply` with the file.
- Anything else on Unix - executed directly after `chmod 0700`, relying on
  its shebang. On Windows, `.bat`/`.cmd` through `cmd.exe /c`; other
  extensions fail as unsupported.

### 6. Input methods

- `stdin`: parameters as one JSON object on stdin.
- `environment`: each parameter as `PT_<name>`, strings verbatim and
  other values JSON-encoded, as Bolt does.
- `powershell`: a generated wrapper reads the JSON from stdin, converts
  it to a hashtable and splats it into the script, so the task gets named
  parameters.
- `both` or none: stdin and environment together, except a `.ps1` task,
  which defaults to `powershell` as in Bolt.

Parameters a request omits get their declared `default`, as Bolt
fills them, so a task sees the same values under the console as under
Bolt. `_task` (the task name) is always added; `_installdir` is added when the
task declares supporting files.

### 7. Files: a checksum-keyed cache and a per-run directory

Downloads go to `<cache>/<sha256>/<filename>`. The SHA-256 is verified
while streaming to a temporary file, which is renamed into place only
on a match, so a cache entry is always a verified file. Default cache
paths are `/opt/puppetlabs/node-agent/task-cache` (Linux and macOS) and
`%ProgramData%\PuppetLabs\node-agent\task-cache` (Windows), overridable
with `NODE_AGENT_TASK_CACHE_DIR`. Entries unused for 14 days are removed
when a task starts.

Each run gets a fresh `0700` directory laid out as Bolt does
(`<installdir>/<module>/tasks/<file>` plus supporting files at their
module-relative paths). Files are copied from the cache, and the
directory is removed when the run ends.

### 8. Running, timeouts and killing the process tree

A new `TaskRunner` replaces `CommandRunner` for tasks. It takes stdin,
environment, working directory and a deadline. On Unix the task starts
in its own process group, and a timeout kills the group. On Windows it is
put in a job object (`golang.org/x/sys/windows`, already a dependency)
and the job is terminated. In both cases the busy flag is released only
after the tree is gone.

Default timeout is 30 minutes. The maximum is `dispatchTimeout` minus 5
minutes, so the node's timeout always fires before the console stops
waiting.

### 9. Results and storage

`responseData` gains `result` (raw JSON object). Migration `000027`
adds `jobs.environment TEXT` and `job_targets.result JSONB`. The
dispatcher records `_error.msg` (or a generic message for a non-zero exit
with no `_error`) as `error_detail`. Output stays in `output` for
non-task jobs.

### 10. Sensitive parameters live only in memory

Before a job is stored, each sensitive parameter's value is replaced by
`"Sensitive [value redacted]"`. The real values stay in a map on the
`Dispatcher`, keyed by job ID (child job ID for plan steps), and are
merged back into the payload at dispatch time, then dropped when the job
completes.

This works because a job is already followed only by the instance that
started it. If that instance dies, the reaper fails the job, and the
lost values cannot matter.

*Alternative:* store them encrypted with `internal/sealer`. Rejected:
nothing needs them after the job ends, and a value that is never
written cannot leak from a backup.

### 11. Type checking: a small Puppet type parser

Validation parses the parameter type strings that occur in practice:
`String`, `Integer`, `Float`, `Numeric`, `Boolean`, `Array`, `Hash`,
`Optional`, `Variant`, `Enum`, `Pattern`, `NotUndef`, `Undef`, `Data`,
`Any`, size and range arguments, and `Array[T]` / `Hash[K, V]`. Any
other name (type aliases, `Struct`, `Tuple`, and so on) parses as
"unchecked", so it accepts any value (spec: "A type the system cannot
check").

The frontend reuses the same type summary, returned with the task's
details, to choose inputs: checkbox for `Boolean`, number for
`Integer`/`Float`, select for `Enum`, masked text for sensitive, and a
JSON text area for `Array`/`Hash`/unchecked types. The server stays
authoritative.

### 12. API

- `GET /api/v1/tasks/environments`
- `GET /api/v1/tasks?environment=production`
- `GET /api/v1/tasks/{module}/{task}?environment=production` (a module's
  `init` task is `{module}/init`)
- `POST /api/v1/orchestrator/tasks` gains `environment` and
  `timeoutSeconds`. Validation failures return `400` with the offending
  parameter named. "Not configured" returns `503`, the same way
  certificate actions report missing configuration.

Plan task steps use the same resolve/validate path from `production`,
and a plan is rejected before any job is created if any step is invalid.

### 13. Frontend

One shared module, `frontend/src/run-task.js`, opens a modal (voxblocks
modal, per the modal-confirmation convention). It lists environments
and tasks, then renders the parameter form. `node.js`, `nodes.js` and
`group.js` each add a "Run task" action next to "Run Puppet", gated on
`orchestrator:run`. `job.js` shows the job's environment and parameters
with sensitive values redacted, and each target's result as formatted
JSON in an expandable row.

## Risks / Trade-offs

- [A node configured with `use_srv_records` has no `server_list` to
  read] → falls back to `server`. `NODE_AGENT_PUPPET_SERVER` covers the
  rest, and the guide says so.
- [A node agent older than this change gets the new payload] → it still
  calls `puppet task run` and fails as it does today. The install script
  already upgrades in place; the guide says to upgrade agents.
- [A console older than this change sends a bare task name to a new node
  agent] → the agent fails with "task request is missing resolved files;
  upgrade the console", which costs nothing since that path never
  worked.
- [Tasks run with the node agent's full privileges (root/SYSTEM)] → the
  same as pxp-agent. `orchestrator:run` is already the gate, and every
  run is in the activity and audit logs.
- [The type parser rejects a value Puppet would accept] → it is
  deliberately small, and anything unrecognised is unchecked. Tests
  cover the type strings in the voxpupuli and puppetlabs modules most
  likely to be run.
- [Sensitive values in a plan are lost if the instance restarts mid-plan]
  → that job fails through the reaper anyway, the same as today.
- [Private tasks (`"private": true` in metadata) are hidden from
  Bolt's and PE's task lists] → the catalogue lists them too but flags
  them, and the picker hides them. They can still be run by name through
  the API, as in PE.

- [A task's output must fit in one NATS reply (1 MB by default), and the
  reply carries stdout twice: raw, and as the result] → the node agent
  keeps the first 256 KiB of stdout and 64 KiB of stderr, and notes the
  truncation in the output. A task whose JSON is cut off this way is
  reported as plain text under `_output`.
- [The system temp directory is often mounted `noexec`] → run
  directories live beside the cache (`task-runs`, next to
  `task-cache`), not in the temp directory.

## Migration Plan

1. Deploy the console; with `CONSOLE_PUPPET_SERVER_URL` unset, nothing
   changes, except that task requests return "not configured" instead
   of creating jobs that fail.
2. Set `CONSOLE_PUPPET_SERVER_URL` (and the credential if the openvoxdb
   one is not suitable).
3. Upgrade `node-agent-client` on nodes through the install script.

Rollback: the migration only adds nullable columns, so an older console
runs against the new schema.

## Open Questions

- Whether the 14-day cache expiry should be configurable. It can be added
  later without changing the payload or specs.

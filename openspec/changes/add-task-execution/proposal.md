## Why

Puppet tasks are accepted by the API and recorded as jobs, but never run
on a real node. `node-agent-client` executes a task by shelling out to
`puppet task run`, and the agent has no such subcommand - against
openvoxserver 8.29.0 it fails with `Error: Unknown subcommand 'task'`.
In Puppet Enterprise, task execution lived in `pxp-agent`, which this
project deliberately does not ship, so the node side of that work was
never actually built.

Working tasks are also the foundation for the follow-up YAML plans
change: a plan's main building block is a task step, and plans cannot be
useful while every task step fails.

## What Changes

- **Task catalogue.** The console reads the tasks available in each code
  environment from openvoxserver's tasks API (`/puppet/v3/tasks`), and
  shows each task's description and parameters from its metadata. No
  copy of the catalogue is stored: it is read from openvoxserver when
  asked for, so it always matches the deployed code.
- **Validated task requests.** Triggering a task names an environment
  (defaulting to `production`). The console resolves the task's metadata
  and files in that environment before dispatch, rejects an unknown task,
  a missing required parameter, an unknown parameter or a value of the
  wrong type, and sends the node the resolved task - files, checksums,
  implementations and input method - instead of a bare task name.
- **Real execution on the node.** `node-agent-client` stops calling
  `puppet task run` and runs the task itself, the way `pxp-agent` did:
  it downloads the task's files from its own Puppet server with its own
  certificate, verifies each file's SHA-256 against what the console
  sent, chooses the implementation that suits the node's OS, passes
  parameters by the task's input method (stdin JSON, `PT_` environment
  variables, or PowerShell named parameters), enforces a timeout, and
  returns the task's structured result.
- **Structured results.** A task's JSON output (including `_output` and
  `_error`) is stored per target and shown on the job page; non-JSON
  output is wrapped as `_output`, as Bolt and PE do.
- **Console UI.** A task can be run from the same three places a Puppet
  run can - a node's page, a selection on the Nodes page, and a group's
  matching nodes - through a dialog that picks an environment and task
  and builds a parameter form from the task's metadata. The job page
  shows each target's task result.
- **BREAKING (API):** `POST /api/v1/orchestrator/tasks` now validates the
  task and its parameters, and fails a request it previously accepted and
  recorded (an unknown task, invalid parameters, or no task catalogue
  configured). Every such request failed on the node anyway, so no
  working integration breaks.
- Task steps in the existing step-list plans use the same execution path,
  so they start working too; plans are otherwise unchanged.

Out of scope: agentless (SSH/WinRM) targets, scheduled jobs, YAML or
Puppet-language plans, run options such as noop, and stopping a running
task.

## Capabilities

### New Capabilities

- `task-catalog`: listing the tasks available in a code environment and
  reading a task's metadata and files from openvoxserver, and validating
  a set of parameters against that metadata.

### Modified Capabilities

- `orchestrator`: the "Task and plan execution" requirement gains an
  environment, validation before dispatch, dispatch of the resolved task,
  and a structured per-target result; the console UI gains a way to run
  a task.
- `node-agent`: the "Executes a task request" requirement is rewritten
  around downloading and verifying task files, choosing an
  implementation, passing input by input method, a timeout, and
  structured output.

## Impact

- `internal/nodeagent` - replaces `handleRunTask` with a task runner:
  file download over HTTPS with the node's certificate, a checksum-keyed
  local cache, implementation selection, input methods, timeout, and
  output parsing. Learns its Puppet server address from the node's own
  Puppet configuration.
- `internal/orchestrator` - the task request and wire shapes, validation
  in `triggerTask`, and a structured result stored per target.
- New Puppet server client for the console (`/puppet/v3/tasks`), with new
  optional settings for its URL and credentials; unset, the task
  catalogue and task runs report "not configured" rather than the
  console failing to start.
- `internal/persistence` - migration adding the task environment to jobs
  and a structured result to job targets.
- `frontend/src` - a run-task dialog on `node.js`, `nodes.js` and
  `group.js`, and task results on `job.js`; new strings in the gettext
  catalogues.
- No new external dependency: openvoxserver's default `auth.conf`
  already lets any CA-issued certificate read `/puppet/v3/tasks` and
  `/puppet/v3/file_content`.
- Guides: `marketing/guides/use/run-jobs.md` gains a "Run a task"
  section and drops its note that tasks are unverified;
  `marketing/guides/install/containers.md` and
  `marketing/guides/install/packages.md` document the new Puppet server
  settings.

# Proposal

## Why

Every node the console can act on today runs its node agent. Plenty of
machines cannot or should not: network gear with a shell, appliances, locked-down
servers, hosts not yet enrolled. Puppet Enterprise reaches these over SSH
without an agent. Week 12 gives the console the same for Linux hosts, using the
orchestrator, task catalogue, node scope and sealed secrets it already has.

## What Changes

- An operator registers **agentless targets**: a name, address and port, and
  how to log in (a private key or a password, with an optional `sudo` run-as
  user). Credentials can belong to one target or be a **shared credential**
  several targets use. They are sealed with the console's secrets key and are
  never returned by the API, logged, audited or stored in a job.
- Host keys are **pinned**: the first check records the key the host presents
  for an operator to accept; after that a different key is refused until it is
  explicitly replaced.
- A **connectivity check** connects, authenticates and reports who it logged in
  as, so a target can be proven before a job depends on it.
- **Tasks run on agentless targets** through the existing `orchestrator:run`
  path, node scope and task permissions, unchanged. The console fetches the
  task's files from the Puppet server itself, copies them to a private
  directory on the host over the SSH session, runs the task, collects the
  result as it does for an agent, and removes the files. Plans and scheduled
  jobs with task steps work the same way.
- A Puppet run, a patch run or a plan **run step** against an agentless target
  is refused, naming the target: they need the agent.
- Agentless targets appear on the Nodes page, in target pickers and in node
  groups (by pin), marked **agentless**. They have no facts, so a group's
  rule cannot match them; pins can.
- A new permission, `agentless:manage`, covers registering and changing
  targets and credentials and accepting host keys. Everything else reuses
  existing permissions.
- Audit for target, credential and host-key changes; console pages,
  translations in all 15 locales, guides and `operations.md`.
- **Not included:** WinRM and Windows, jump hosts, SSH certificates, Puppet
  runs, patch scan and apply over SSH (they are Go code in the agent, not a
  shell script - a later change), facts collection, Bolt and Puppet-language
  plans.

## Capabilities

### New Capabilities
- `agentless-targets`: registering targets and credentials, sealing, host key
  pinning, the connectivity check, visibility and scope, audit.
- `agentless-tasks`: running tasks on agentless targets - fetching and copying
  files, input methods, results, timeouts, limits, and what is refused.

### Modified Capabilities
- `orchestrator`: a task job can target an agentless node; a run, patch run or
  plan run step against one is refused whole.

## Impact

- New `internal/agentless` (store, SSH executor, handlers); a composite
  transport in the orchestrator that routes a dispatch by node name; shared
  task logic moved out of `internal/nodeagent` so both executors use it;
  `taskcatalog.Client` gains a file download.
- Migration 000039 (`agentless_credentials`, `agentless_targets`).
- API under `/api/v1/agentless/`; node list gains an `agentless` marker.
- Frontend: Nodes page, an Agentless section under Admin or Nodes, pickers.
- Dependency: `golang.org/x/crypto/ssh` (already a direct dependency).

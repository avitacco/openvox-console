# Design

## Context

- Nodes are plain names (certnames) in `job_targets`, node groups (by pin or
  rule) and node scope. Pinned names are in scope even if openvoxdb has never
  heard of them (`groupnodes`), so an agentless target is reachable by scope
  with no special case.
- The node list is the union of openvoxdb's inventory and the CA/connectivity
  registry; neither knows an agentless target.
- A task job is dispatched by the `Dispatcher` over a `Transport.Dispatch(ctx,
  certname, payload, timeout)` and the reply is a `wireResponse` that
  `handleTaskResult` already turns into a target result.
- The agent resolves a task itself (`nodeagent/taskrun.go`: choose an
  implementation, download files with the node certificate, build stdin/env,
  run, parse the output). The console has `taskcatalog.Client` with mutual TLS
  to the Puppet server, but no file download.
- Sealed secrets exist (`internal/sealer`, AES-GCM, associated data), keyed by
  `CONSOLE_SECRETS_KEY_FILE`. `golang.org/x/crypto` is already a direct dependency.

## Goals / Non-Goals

**Goals:** run tasks on SSH-only Linux hosts with the same permissions, scope,
results and audit as an agent node; never expose or leak a credential; make a
changed host key impossible to miss.

**Non-Goals:** Windows, jump hosts, SSH certificates, Puppet and patch runs,
facts, keeping a connection open between jobs.

## Decisions

1. **A target is a console-owned record in the same name space as nodes.**
   `agentless_targets.name` is unique and refused if it equals a node the
   inventory or CA registry already knows. A real node enrolling later under the
   same name is not prevented (the console does not control enrolment); the
   node list shows the clash and the target wins for task routing until it is
   deleted. Alternative: a separate name space with a prefix - rejected, it makes
   every picker, group pin and scope check special.

2. **Route by name inside the transport, not in the dispatcher.** A composite
   `Transport` checks whether the name is an agentless target (cached, refreshed
   on change) and either dispatches over NATS or calls the SSH executor, which
   returns the same JSON `wireResponse` an agent would. The dispatcher,
   `handleTaskResult`, plans and schedules need no change. A Puppet run, patch
   run or plan run step never reaches it: `Starter` refuses them first, for every
   entry (HTTP, schedule, plan) because they all go through it.

3. **Share the task logic.** The pure parts of `nodeagent/taskrun.go`
   (implementation choice, input method, output parsing, caps) move to
   `internal/taskexec` and both executors use them; the agent's tests must pass
   unchanged. For an agentless target `featuresFor` is `["shell"]`: no Puppet.

4. **SSH with `x/crypto/ssh`, files over the session, no SFTP.** Each
   connection is made for one task and closed after. Steps, each its own
   session: `mktemp -d` (private, 0700); for each file `cat > <dir>/<name>`
   fed from stdin, then `chmod`; run the implementation with its input; `rm -rf`
   the directory (deferred, also on timeout). No SFTP subsystem is needed, which
   many appliances lack. A sensitive parameter is passed on the task's stdin or
   environment of the session, never as an argument and never written to disk
   (the agent behaves the same). Run-as is `sudo -n -u <user> -- ...`; a sudo that
   needs a password fails with that reason. Password sudo is not supported.

5. **The console downloads task files.** `taskcatalog.Client.File` fetches a
   file with the console's existing client certificate and verifies size and
   SHA-256 before anything is sent; files are held in memory per job (a task's
   files are small) and shared by all its agentless targets.

6. **Sealing.** Secrets are sealed with `sealer` using associated data
   `agentless:<table>:<id>:<field>`, so a ciphertext copied to another row will
   not open. Stored: `private_key`, `passphrase`, `password`. API reads return
   `hasKey`, `hasPassword`, and a key's public fingerprint, never material.
   Updates are write-only; omitted means unchanged. Without a secrets key,
   creating a credentialed target is refused with a clear message.

7. **Credentials.** `agentless_credentials` (shared, named) holds the login
   (`username`, `run_as`, sealed secrets). A target either references one
   (`credential_id`) or carries its own login fields; its own wins. Deleting a
   credential in use is refused. Alternative: always a credential row, hiding a
   per-target one - rejected, it clutters the shared list with one-off entries.

8. **Host keys.** The pinned key is stored as the SHA-256 fingerprint plus the
   key's type and the full public key (not secret). A check against a target
   with no pin connects without authenticating (the host key callback captures
   it and aborts) and stores it as `proposed`; the operator accepts it, after
   seeing the fingerprint, which sets the pin. A pinned target that presents
   another key is refused before any credential is sent and flagged
   `key_changed`; only an explicit "replace pin" (audited with both
   fingerprints) clears it. Jobs never trust-on-first-use by themselves: an
   unpinned target fails as "not trusted". This is the one place the design is
   stricter than "TOFU" in the brief, on purpose.

9. **Permission.** One new permission, `agentless:manage`, for creating,
   changing and deleting targets and credentials and for accepting or replacing
   host keys. Listing and reading (without secrets) is `nodes:read`; checking a
   connection and running tasks are `orchestrator:run`; all scoped by node
   groups as those permissions are. Reason for a new one: `nodes:manage` already
   lets someone delete nodes, and storing a key that logs in as root is a
   different level of trust. A caller whose `agentless:manage` is limited to
   some groups may manage only targets whose names are in scope (a new name must
   be pinned into their scope to be created by them) - an out-of-scope target
   reads as unknown. **Decision point for you:** if you would rather reuse
   `nodes:manage`, it is a one-line change before apply.

10. **Limits.** Connect timeout 15 s; a task by its timeout (default 30 min, as
    now); output capped as the agent's; one task at a time per target (a second
    fails "busy"); at most 20 concurrent SSH tasks across the console, queued
    beyond that. A failure carries its stage: connect, host key, authenticate,
    become, transfer, task.

11. **Display.** `GET /api/v1/nodes` and the connectivity list stay as they are;
    the console adds `/api/v1/agentless/targets` and the Nodes page merges it,
    marking each row. Facts and reports do not exist for a target, and its node
    page says so. Rule-based group membership cannot include a target (no
    facts); pins do. Showing the list through one endpoint instead would change
    the openvoxdb-backed handlers' contract for a minority case.

12. **Patch scan/apply over SSH is deferred.** They are Go code parsing package
    manager output on the node; running them agentless means shipping a script
    equivalent, which is its own change. Refused with a clear reason now.

## Risks / Trade-offs

- [A credential that logs in as root is stolen from Postgres] -> sealed with a
  key that is not in the database; associated data binds each to its row; reads
  never return it.
- [Host key not checked] -> never connect unpinned for a job; changed key
  refused before authentication.
- [Task output contains a secret] -> same as agent tasks: output is stored;
  sensitive parameters are redacted from params, not from what a task prints.
- [Many agentless targets exhaust the console] -> global cap with queueing.
- [Task files on a shared /tmp] -> private `mktemp -d` (0700), removed after.

## Migration Plan

Migration 000039 adds two tables; down drops them. No backfill. Ship with
`CONSOLE_SECRETS_KEY_FILE` set to use credentials; without it everything else
still works.

## Open Questions

- Whether to collect minimal facts (OS release) over SSH at check time so a
  target can show an OS; deferred, nothing here depends on it.

## Implementation Notes

- The task list has 16 tracked tasks (the proposal summary said 15).
- `agentless:manage` was added as designed. `rbac` gives it to the bootstrap
  admin only at creation, so an existing admin role needs it granted once.
- Environment parameters are not on a command line: they are sent ahead of
  the task's stdin as `export` lines that a small `sh -c` bootstrap reads with
  `dd bs=1 count=N` and `eval`s, then `exec`s the task. A task taking its
  input on stdin (no environment) is run directly, without the bootstrap and
  without `exec` when under `sudo`.
- Updating a target keeps a port or secret that is left out; a changed
  address or port clears the host key pin.
- Shared credentials need a caller whose `agentless:manage` covers every node
  (a scoped caller may manage only their own targets and own logins).
- `TestCappedBuffer` moved with the type to `internal/taskexec`; the agent's
  other task tests are unchanged.
- A task killed on timeout is sent SIGKILL on its session; a process that
  ignores a closed session may outlive it. Its directory is removed anyway.

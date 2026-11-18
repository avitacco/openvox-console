# Design

## Context

- A Puppet run, task or plan is one blocking request-reply per node
  (`dispatchOne`), bounded by `dispatchTimeout` (1h); the node's permission to
  reply expires after `nodetransport.ResponseWindow` (2h). The dispatcher that
  sent a request is the only one that can correlate its answer.
- The reaper fails any `running` job whose `started_at` is over `2 * dispatchTimeout`
  old, every 5 minutes, under a lease.
- The agent runs one request at a time (`busy`); only `agent_status` is answered
  while busy. Upgrades reply first and act after (`afterReply`).
- Week 10 already gives `patch_scan` (apt/dnf/yum parsing in
  `internal/nodeagent/patchscan.go`), a snapshot store with `Save`, and a
  scanner that talks to the transport.
- Targets, scope and refusals (`Starter`, `TargetResolver`, `IsRefusal`) are
  reused unchanged. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:** patch many Linux nodes safely, observably, for longer than one
request; keep every existing job kind behaving exactly as before.

**Non-Goals:** schedules, windows, rollback, plan steps, patch groups,
resuming a run on another console instance after the follower dies.

## Decisions

1. **A job kind `patch`, not a task.** A task is a user-supplied script with
   its own permissions; a patch is console behaviour with batching. New kind in
   `jobs.kind`; per-package results reuse `job_targets.result` (JSONB, already
   used by tasks). Options live in `jobs.params`:
   `{scope: security|all|packages, packages[], reboot, dryRun, batchSize,
   stopAfterFailures}`. Defaults: scope security, batch size 10 (1-100),
   stopAfterFailures 3 (0 = never stop, max 1000), reboot false, dryRun false.
   Alternative considered: patch as a task over the existing task runner - rejected,
   it inherits the 1h timeout and gives no batching or package results.

2. **Start-and-poll, not one long request.** `patch_apply` replies
   `{accepted}` immediately and runs in a goroutine, holding the agent busy.
   `patch_status` is added to `answeredWhileBusy` and returns
   `{state: running|done|failed|interrupted, phase, packages[], exit, output,
   reboot_required, rebooting, scan}`. The console polls every 15s. Alternative:
   the agent publishing progress to the bus - rejected, nodes may only publish
   replies today and widening node permissions is a security change this week
   does not need. Polling also makes the heartbeat free: an answer is a beat.
   The agent keeps the finished result (and a state file, so an agent restart
   mid-run reports `interrupted` rather than nothing) until the next patch
   request.

3. **Coordinator beside the dispatcher.** A `PatchCoordinator` (new, in
   `internal/orchestrator`) owns a patch job: it starts batch *n*, one goroutine
   per node polling its agent, and when the batch is terminal checks the
   threshold and starts the next. State is in Postgres (`job_targets.status`,
   new `batch`); the rest is in memory. If the instance dies the job is reaped
   (decision 4) - same trade-off as `taskPayloads`, accepted for week 11.
   Sensible limits: a node is failed 4h after its own start; the start request's
   timeout is 2 minutes; a poll's is 30 seconds.

4. **Heartbeat reaper.** Migration adds `jobs.heartbeat_at` and
   `job_targets.heartbeat_at`. The coordinator sets the job's heartbeat when it
   starts, on every poll answer and on every batch change. The reaper adds a
   second rule: `kind = 'patch' AND status = 'running' AND heartbeat_at < now() - 10 min`
   fails the job and its pending/running targets with a distinct reason
   ("progress stopped being reported"). The existing 2h rule is restricted to
   `kind <> 'patch'`, so every other kind is untouched. A node target stops
   answering for 10 minutes is failed by the coordinator itself, sooner than
   the reaper would reach it; the reaper is the backstop for a dead
   coordinator. Alternative: raise the 2h cap - rejected, it would only move
   the problem and delay detection of a dead tracker.

5. **Batching semantics.** Batch size is also concurrency; batches run one
   after another in target order (sorted by certname, so a run is repeatable).
   After a batch, if failed targets >= stopAfterFailures (and it is non-zero),
   remaining pending targets are set `failed` with the reason "not started:
   the run stopped after N failed nodes", matching how a failed plan closes
   later steps ("not run"). The job is failed. The reason text carries the
   distinction; no new target status, so no constraint change on `job_targets.status`.

6. **What the agent runs.** apt: `all` = `apt-get -y -o Dpkg::Options::=--force-confold dist-upgrade`
   with `DEBIAN_FRONTEND=noninteractive` (matches the scan's `dist-upgrade`
   simulation); `security`/`packages` = the scan's simulation picks the names,
   then `apt-get -y --only-upgrade install <names>`. dnf/yum: `all` = `upgrade -y`;
   `security` = `upgrade -y --security`; `packages` = `upgrade -y <names>`.
   Dry run: apt `-s`, dnf/yum the `check-update` listing filtered the same way -
   nothing executed that changes the node. Per-package results come from the
   pre-list (what was pending and selected) against a post scan (what is still
   pending): not-pending afterwards = upgraded, with its from/to versions. The
   post scan is the `scan` returned in the final status and saved by the
   console (`patchinventory.Store.Save`) - no second dispatch.

7. **Reboot.** After a successful, non-dry run with `reboot` set and
   `reboot_required` true, the agent marks `rebooting`, persists its result,
   waits 30 seconds so the console's next poll collects it, then runs
   `systemctl reboot` (`shutdown -r now` as fallback). The target is succeeded
   as soon as the final status is read; a background step waits up to 10
   minutes for the node to reconnect and then scans it (`patchinventory`
   scanner), recording a failed attempt if it does not return. A reboot never
   fails the run.

8. **Concurrency guard.** Starting a run checks `job_targets` for the nodes in
   a running patch job and refuses naming them; the agent's `busy` is the
   second line. The check names only nodes already within the caller's scope.

9. **Scope and audit.** `orchestrator:run` and the existing resolver/scope
   path, unchanged; reads follow the job scope rules in `GetJobInScope`. Audit
   `patch.triggered` carries the options in `After`; end events
   `patch.succeeded`/`patch.failed`, using the dispatcher's recorders.
   Activity summaries follow the run wording.

10. **Old agents.** `unsupported action "patch_apply"` becomes the target
    reason "this node's agent client is too old to patch; upgrade it. Nothing
    was installed", as run options do. A node with no apt/dnf/yum answers
    `unsupported`; failed with that reason.

11. **API.** `POST /api/v1/orchestrator/patches` with `{target, options}`;
    400 on bad options (field named), 409 on nodes already being patched, 403
    out of scope as runs. Job reads add `patchOptions`, per-target `batch`,
    `heartbeatAt`. No new permission.

## Risks / Trade-offs

- [The following instance dies mid-run] -> reaper fails the job within ~15
  minutes with the reason; nodes keep their agent-side result, which the next
  scan reflects. Resuming is a later week.
- [A package manager hangs forever] -> per-node 4h limit fails the target; the
  agent is left busy until the process ends, so a later request answers busy.
- [Patching the agent's own package or the node's network] -> out of scope; a
  node that drops mid-run is failed after 10 minutes of silence.
- [dnf `--security` needs updateinfo] -> a node without it fails clearly rather
  than patching everything.
- [Reboot during polling] -> reboot is only after the final status is read, with
  a grace delay.

## Migration Plan

Migration 000038 is additive (new kind value, nullable columns); down removes
them (patch jobs deleted first). Deploy console, then roll agents; until a node
has the new agent it fails as too old. No data to backfill.

## Open Questions

- Whether `stopAfterFailures` should also allow a percentage; a count is
  shipped and a percentage can be added without changing stored runs.

## Implementation Notes

- The task list has 17 tracked tasks (the proposal summary said 14).
- Starting a run is serialized with a Postgres advisory lock inside
  `CreatePatchJob`, so the "node already being patched" check and the insert
  are atomic; the agent being busy is the second line.
- The agent keeps its status in `patch-run.json` beside the task cache and
  reports `interrupted` after a restart mid-run.
- The coordinator saves the agent's post-run scan even for a node that will
  reboot (accurate until it does), then scans again after it returns.
- A node refused by the agent for having no supported package manager is
  failed with the agent's own wording rather than a console-side text.
- The job-list `kind` filter and the jobs page accept `patch`.
- Not exercised live: an actual reboot (no init system in the test
  container); covered by tests.

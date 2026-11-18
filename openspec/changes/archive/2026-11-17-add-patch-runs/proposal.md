# Proposal

## Why

Week 10 shows which updates each node is missing, but an operator still has to
leave the console to install them. A patch run closes that loop for Linux
nodes - applying updates across many nodes safely, in batches, with a stop when
too many fail - and the console's current job tracking cannot carry it: a
single request-reply per node is capped at an hour, and the stale-job reaper
fails any job two hours after it started, however alive it is.

## What Changes

- A new job kind, the **patch run**, started with `orchestrator:run` through
  the existing target choosing (nodes, query, node group) and node-scope
  checks, unchanged. It applies security updates only, all updates, or named
  packages; optionally reboots a node that needs it; and can be a dry run that
  installs nothing.
- Patch runs are **batched**: nodes are patched a batch at a time (batch size
  is also the concurrency), and the run stops starting new batches once a
  chosen number of nodes have failed. Nodes not reached are recorded as not
  started.
- The node agent gains two actions: `patch_apply`, which starts a run and
  answers at once, and `patch_status`, which reports progress and is answered
  even while the run is in progress. The console polls it; each answer is a
  **heartbeat**.
- The stale-job reaper learns heartbeats: a patch job or target is failed when
  its heartbeat stops for ten minutes, not when it is two hours old. Every
  existing job kind keeps the existing rule.
- After a run the node's pending-update inventory is refreshed from the scan
  the agent takes when it finishes (and again after a reboot, once the node is
  back), so the Patching page shows the result.
- Per-node and per-package results show on the job, the node's Patches tab and
  the Patching page. Runs are audited and appear in the activity log.
- A patch-capable node refuses a second run while one is in progress, and the
  console refuses to start a run on nodes already being patched.
- Console pages, translations in all 15 locales, site guide updates.
- **Not included:** patch windows, blackouts or scheduled patch runs; patch
  groups; rollback; patch steps in plans; Windows and macOS; zypper; CVE
  correlation.

## Capabilities

### New Capabilities
- `patch-runs`: applying pending updates across nodes - the run's options,
  batching and stop threshold, the agent exchange and its heartbeat, results,
  inventory refresh, reboot, scope and audit rules.

### Modified Capabilities
- `orchestrator`: a job that reports progress is judged stale by its silence,
  not its age; a patch job is a job kind with per-target results.

## Impact

- `internal/orchestrator`: new patch coordinator beside the dispatcher, store
  columns, reaper rule, handler route; `internal/nodeagent`: `patch_apply`,
  `patch_status`; `internal/patchinventory`: store a scan the agent returns.
- Migration 000038 (job kind, heartbeat columns, per-target batch).
- API: `POST /api/v1/orchestrator/patches`; job reads gain patch fields.
- Frontend: Patching page action, job detail, node Patches tab; docs under
  `marketing/guides/`; `operations.md`.
- Agents older than this build answer "unsupported action" and are reported as
  too old; nothing is installed on them.

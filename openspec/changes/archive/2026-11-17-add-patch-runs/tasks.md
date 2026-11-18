# Tasks

## 1. Storage and reaper

- [x] 1.1 Migration 000038: add kind `patch`, `jobs.heartbeat_at`, `job_targets.heartbeat_at`, `job_targets.batch`, with down migration; verify by a migration test that applies up/down and rejects an unknown kind.
- [x] 1.2 Store: create a patch job (params, targets with batch numbers), record heartbeats, close not-started targets, list nodes with a running patch target; verify by store tests against a scratch database.
- [x] 1.3 Reaper: fail a patch job by heartbeat staleness (10 min) and restrict the age rule to other kinds; verify by reaper tests - long job with fresh heartbeat survives, silent one is failed, a stale task is still failed at 2h.

## 2. Node agent

- [x] 2.1 `patch_apply` for apt and dnf/yum (scopes security/all/packages, dry run, non-interactive), running in the background and holding the agent busy; verify by unit tests with a fake command runner for each manager and scope.
- [x] 2.2 `patch_status`, answered while busy, with per-package results from the post scan, state file and `interrupted` after restart; verify by client tests that status is answered mid-run and a second apply is refused.
- [x] 2.3 Reboot after the result is delivered, only when asked for and required; verify by tests that a dry run, failed run or unrequested reboot never reboots.

## 3. Console orchestration

- [x] 3.1 Option decoding and validation (scope, packages, batch size, threshold, reboot, dry run) with refusals naming the field; verify by unit tests.
- [x] 3.2 Patch coordinator: batches, per-node polling, heartbeat, 10-minute silence and 4-hour limits, stop threshold with "not started" targets, too-old and unsupported reasons; verify by tests with a fake transport.
- [x] 3.3 Refresh the inventory from the final scan and, after a reboot, rescan on reconnect; verify by tests that the snapshot changes and a node that never returns records a failed scan.
- [x] 3.4 Start path: `POST /api/v1/orchestrator/patches` through the existing target resolution and scope, 409 for nodes already being patched, audit and activity entries; verify by handler tests and that the node-scope sweep passes with the route classified.
- [x] 3.5 Job reads show patch options, batch, heartbeat and per-package results, scoped like other jobs; verify by tests including a scoped reader.

## 4. Console UI, translations and docs

- [x] 4.1 Patching page: start a run (group or nodes, options, preview), and the job detail view with batches and per-package results; verify by frontend tests and screenshots.
- [x] 4.2 Node Patches tab shows the last run; verify by screenshot.
- [x] 4.3 Translate every new string in all 15 locales; verify with `go run ./i18n status` showing complete.
- [x] 4.4 Update `marketing/guides/use/patching.md`, `access.md` and `operations.md` (reaper rule, polling, reboot, limits); verify `make marketing-build` succeeds.

## 5. Verification

- [x] 5.1 `go build`, `go vet`, `gofmt`, `go test ./... -count=1` and `openspec validate add-patch-runs --strict` all pass.
- [x] 5.2 Live on the dev stack with `task-e2e-agent`: a dry run, then a real security run upgrading real packages, the Patching page updating, a run longer than the old reaper window kept alive (heartbeat reaper tested with a shortened threshold), the stop threshold with a failing node, and a scoped user refused; record in `evidence/`.

# Tasks

## 1. Agent scan action

- [x] 1.1 Add `patch_scan` to the node agent: apt and dnf/yum parsers over command output, security flag, reboot flag, unsupported-manager result. Verify with table tests over recorded apt/dnf outputs, including an empty result and a failing manager.
- [x] 1.2 Add `refresh` param and the unsupported-action path for older agents. Verify with a handler test.

## 2. Storage and scans

- [x] 2.1 Migration 000037 (scans, updates, settings) with up/down. Verify with a migration test applying up and down.
- [x] 2.2 `internal/patchinventory` store: replace-snapshot in one transaction, record failed attempt keeping the old snapshot, purge, settings read/write. Verify with store tests, including a reader during replacement.
- [x] 2.3 Scanner: dispatch to a node, decode, store; worker pool for groups. Verify with a fake transport (success, offline, error, unsupported).

## 3. API

- [x] 3.1 Read endpoints: fleet list with group/OS/package filters and staleness, node detail, package search; `nodes:read`, node-scoped. Verify with handler and scope tests; the node-scope sweep in `internal/app` passes.
- [x] 3.2 Scan endpoints for a node and a group (`orchestrator:run`), per-node outcomes, skipped-out-of-scope count; one audit entry per request. Verify with tests for scope, offline, 403 and the audit entry.
- [x] 3.3 Interval setting endpoints (`nodes:manage`), range 15 min to 7 days or off. Verify with tests for range, 403 and audit.

## 4. Periodic scan

- [x] 4.1 Leased scheduler wired into `internal/app`, in the same modes as the vulnerability scheduler; no per-node audit. Verify with a test using a fake clock for due, not-due, off, and failed-attempt backoff.

## 5. Console

- [x] 5.1 Patching page (fleet list, filters, package search, scan control, interval setting) and node page patch panel. Verify in a browser: screenshots of both, and a refused interval shown on the field.
- [x] 5.2 Translate all new strings in the 15 locales. Verify `go run ./i18n status` reads complete and `node --test frontend/src/` passes.

## 6. Docs and live verification

- [x] 6.1 Site guide for patching under `marketing/guides/`, plus `operations.md`; `make marketing-build` passes.
- [x] 6.2 Live check on the dev stack with `task-e2e-agent`: a real scan showing real pending updates, a scoped user not seeing it, an offline node, and a periodic pass. Evidence in `evidence/`.
- [x] 6.3 `go build`, `go vet`, `gofmt`, `go test ./...`, frontend tests, and `openspec validate add-patch-inventory --strict` all pass.

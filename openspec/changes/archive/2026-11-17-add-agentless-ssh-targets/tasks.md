# Tasks

## 1. Storage

- [x] 1.1 Migration 000039: `agentless_credentials` and `agentless_targets` (name, address, port, login or credential reference, run-as, host-key pin and proposed key, key-changed flag, last check result), with down migration; verify by a migration up/down test including uniqueness and the in-use credential guard.
- [x] 1.2 Store with sealing: create, update (write-only secrets), read without secrets, list, delete, shared-credential reference counts; verify by tests that no read returns secret material and a ciphertext moved to another row will not open.

## 2. Shared task logic and file download

- [x] 2.1 Move implementation choice, input method, output parsing and caps into `internal/taskexec`, used by the agent; verify that the existing `nodeagent` task tests pass unchanged.
- [x] 2.2 `taskcatalog.Client.File` fetching a resolved file and verifying size and SHA-256; verify by tests for success, mismatch and a missing file.

## 3. SSH executor

- [x] 3.1 Connect with host-key pin (proposed, trusted, changed) and key or password auth, with stage-specific errors that never contain a secret; verify against an in-process test SSH server.
- [x] 3.2 Run a task: private temp directory, file copy, chmod, run-as with `sudo -n`, input by stdin/environment, timeout, output caps, cleanup in every outcome; verify against the test server including timeout and cleanup.
- [x] 3.3 Per-target and global concurrency limits; verify a second task on a busy target fails busy and the global cap queues.

## 4. Orchestrator

- [x] 4.1 Composite transport routing by target name, returning the agent's response shape, with the target cache refreshed on change; verify a mixed task job gives each target its own result.
- [x] 4.2 Refuse Puppet runs, patch runs and plan run steps that include an agentless target, before anything is created (HTTP, schedules, plans); verify by tests naming the target.

## 5. API

- [x] 5.1 Endpoints for targets, shared credentials, check, accept and replace host key, with `agentless:manage`, node scope, name-clash refusal and "unknown" for out-of-scope targets; verify by handler tests and the node-scope sweep passing with the routes classified.
- [x] 5.2 Audit entries for every change and host-key action, never a secret; verify by tests that secrets appear in no audit entry, log line or job.

## 6. Console UI, translations and docs

- [x] 6.1 Targets and credentials pages (add, edit, check, accept key, delete) and the agentless marker on the Nodes page, pickers and group pins; verify by frontend tests and screenshots.
- [x] 6.2 Translate every new string in all 15 locales; verify with `go run ./i18n status` complete.
- [x] 6.3 Guides under `marketing/guides/` (new agentless guide, `access.md` permission table) and `operations.md`; verify `make marketing-build` succeeds.

## 7. Verification

- [x] 7.1 `go build`, `go vet`, `gofmt`, `go test ./... -count=1` and `openspec validate add-agentless-ssh-targets --strict` pass.
- [x] 7.2 Live on the dev stack against an sshd container: register by key and by password, check, accept the key, a changed key refused, a shell task with parameters including a sensitive one, a timeout, run-as, a mixed job with `task-e2e-agent`, a Puppet run refused, a scoped user refused; secrets absent from the API, logs, audit and job rows; evidence in `evidence/`.

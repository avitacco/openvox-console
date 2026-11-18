## 1. Puppet server client and configuration

- [x] 1.1 Add `CONSOLE_PUPPET_SERVER_URL` and `CONSOLE_PUPPET_SERVER_CERT_FILE` / `_KEY_FILE` / `_CA_FILE` to `internal/runtime/config.go`, with the credential defaulting to the matching `CONSOLE_OPENVOXDB_*` values, and verify with config tests for unset (disabled), set with defaults, and set with an explicit credential
- [x] 1.2 Create `internal/taskcatalog` with an mTLS client for `GET /puppet/v3/environments`, `GET /puppet/v3/tasks?environment=` and `GET /puppet/v3/tasks/<module>/<task>?environment=`, mapping 404s to typed not-found errors and connection failures to an unreachable error, and verify with `httptest` TLS servers using `internal/testca`
- [x] 1.3 Verify the client against the dev stack: add a fixture module with tasks (single-file shell, multi-file with `lib/` supporting files, shell and PowerShell implementations, a sensitive parameter) to the `control-repo-fixture` Make target, deploy it, and record the real `/puppet/v3/tasks/...` responses as test fixtures for 1.2

## 2. Metadata, type checking and resolution

- [x] 2.1 Parse task metadata (description, parameters, `input_method`, `implementations`, `files`, `supports_noop`) and verify against the recorded fixtures, including a task with no `parameters` key
- [x] 2.2 Implement the Puppet type parser from design decision 11 and a validator for parameter sets (missing required, undeclared, wrong type, unchecked types accept anything, no-`parameters` accepts anything), and verify with table tests covering every scenario in the `task-catalog` spec plus type strings taken from popular voxpupuli and puppetlabs task metadata
- [x] 2.3 Implement resolution into the dispatch payload of design decision 3 (implementations with module-relative supporting files, the full `files` list with path, params, SHA-256 and size), and verify against the single-file, multi-file and multi-implementation fixtures

## 3. Task catalogue API

- [x] 3.1 Add `GET /api/v1/tasks/environments`, `GET /api/v1/tasks` and `GET /api/v1/tasks/{module}/{task}` behind `orchestrator:read`, returning `503` when not configured and naming openvoxserver when unreachable, and verify with handler tests for each `task-catalog` scenario, including 401 and 403
- [x] 3.2 Wire the catalogue into `cmd/console/main.go` for the run modes that serve the API, and verify the run-mode table test still asserts the expected subsystems

## 4. Orchestrator: validation, sensitive values, timeout, results

- [x] 4.1 Add migration `000027` (up and down) adding `jobs.environment TEXT` and `job_targets.result JSONB`, and verify it applies and reverses against Postgres
- [x] 4.2 Extend `triggerTask` with `environment` (default `production`) and `timeoutSeconds` (default 30 minutes, maximum `dispatchTimeout` minus 5 minutes), resolving and validating before `CreateJob`, and verify with handler tests for unknown task, invalid parameters, over-long timeout and not configured, each creating no job
- [x] 4.3 Redact sensitive parameter values before storing, keep the real values in the `Dispatcher`'s in-memory map keyed by job ID, merge them in at dispatch and drop them on completion, and verify with tests that the stored params, the job endpoints, the activity entry and the audit event all show the redaction while the transport receives the real value
- [x] 4.4 Send the resolved payload over the wire, and record `result`, plus `_error.msg` (or a generic message for a non-zero exit) as `error_detail`, per target, and verify with dispatcher tests for JSON, plain-text, `_error` and non-zero-exit responses
- [x] 4.5 Resolve and validate every task step of a plan before creating it, rejecting the whole plan if any step is invalid, and dispatch steps with the resolved payload, and verify with handler and dispatcher tests including a plan whose second step names an unknown task
- [x] 4.6 Update the `orchestrator` guide sections that describe tasks in `marketing/guides/use/run-jobs.md`: an API example for `POST /api/v1/orchestrator/tasks` with `environment` and `timeoutSeconds`, and verify `make marketing-build` succeeds

## 5. Node agent: fetching and verifying files

- [x] 5.1 Add `NODE_AGENT_PUPPET_SERVER` and `NODE_AGENT_TASK_CACHE_DIR` to `internal/nodeagent/config.go`, with per-OS cache defaults, and verify with config tests
- [x] 5.2 Discover the Puppet server via `puppet config print server_list server serverport --section agent` (first `server_list` entry, else `server:serverport`), with the setting overriding it, and verify with tests using a fake `CommandRunner`
- [x] 5.3 Implement the checksum-keyed cache: stream each download over mTLS with the node's certificate into a temporary file, verify the SHA-256, rename into place only on a match, reuse verified entries, and expire entries unused for 14 days; verify with tests for a fresh download, a cache hit making no request, a checksum mismatch running nothing, and an unreachable server
- [x] 5.4 Build the per-run `0700` install directory (`<installdir>/<module>/tasks/<file>` plus supporting files at their module-relative paths), removed after the run, and verify its layout and cleanup in a test

## 6. Node agent: running the task

- [x] 6.1 Implement implementation selection (features per OS, first match, single-file fallback, failure when none fits) and verify with the `node-agent` scenarios, including a PowerShell-only task on Linux
- [x] 6.2 Implement interpreter choice from design decision 5 (`.rb` through the agent's Ruby, `.pp` through `puppet apply`, `.ps1` and `.bat`/`.cmd` on Windows, direct execution elsewhere on Unix) and verify with table tests of the resulting command lines
- [x] 6.3 Add a `TaskRunner` taking stdin, environment, working directory and a deadline, which kills the process group on Unix and terminates a job object on Windows when the deadline passes, and verify on Linux with a task that starts a child process and outlives its timeout, checking both processes are gone and the busy flag is released
- [x] 6.4 Implement the input methods (`stdin`, `environment` with `PT_` names, the `powershell` wrapper, `both`/default, and `.ps1` defaulting to `powershell`), plus `_task` and `_installdir`, and verify on Linux with scripts that echo what they received; cross-compile the Windows build and unit-test the generated PowerShell wrapper text
- [x] 6.5 Parse output into a result (a JSON object as-is, anything else under `_output`, failure on `_error` or a non-zero exit) and return it in `responseData.result`, and verify with tests for each `node-agent` output scenario
- [x] 6.6 Replace `handleRunTask`'s `puppet task run` call with the new runner, fail a payload without resolved files with an "upgrade the console" message, and verify the existing handler tests are updated and the single-in-flight test still passes

## 7. Console UI

- [x] 7.1 Add `frontend/src/run-task.js`: a voxblocks modal that lists environments and tasks, fetches the picked task's details, renders the parameter form from design decision 11 (masked input for sensitive values, required markers, defaults shown), reports the server's `400` against the named parameter, states when tasks are not configured, and opens the created job; verify in the running app against the fixture tasks
- [x] 7.2 Add a "Run task" action beside "Run Puppet" on `node.js`, `nodes.js` (for the selection) and `group.js` (for its matching nodes), gated on `orchestrator:run`, and verify in the running app as a user with and without the permission
- [x] 7.3 Show the job's environment and redacted parameters on `job.js`, and each target's result as formatted JSON in an expandable row, and verify in the running app for a successful, a plain-text and an `_error` task
- [x] 7.4 Run `go run ./i18n extract && go run ./i18n merge` in `frontend`, add the German translations for the new strings, and verify `go run ./i18n status` shows German complete and the i18n lint test passes

## 8. Guides and documentation

- [x] 8.1 Add a "Run a task" section to `marketing/guides/use/run-jobs.md` (picking an environment and task, parameters, sensitive values, timeouts, reading results), remove the note that tasks are unverified, and verify `make marketing-build` succeeds
- [x] 8.2 Document `CONSOLE_PUPPET_SERVER_URL` and its credential settings in `marketing/guides/install/containers.md` and `marketing/guides/install/packages.md`, and `NODE_AGENT_PUPPET_SERVER` for nodes using SRV records, and verify `make marketing-build` succeeds (it fails on settings the console does not read)
- [x] 8.3 Update the `operations.md` credential notes to cover the Puppet server credential and why it is not the CA-client one, and verify by reading it against design decision 2

## 9. End-to-end check

- [x] 9.1 Against the dev stack with a connected agent node, run each fixture task from the UI - a single-file shell task, a multi-file task using `_installdir`, a task with a sensitive parameter, and a task that exceeds its timeout - and verify each job's results, that the sensitive value appears nowhere in the database, API or logs, and that a re-run makes no new downloads
- [x] 9.2 Run a step-list plan with two task steps through the API and verify both steps run in order with their results recorded
- [x] 9.3 Run `make test` and `openspec validate add-task-execution --strict` and verify both pass

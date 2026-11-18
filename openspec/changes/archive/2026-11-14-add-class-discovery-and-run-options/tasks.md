## 1. Server access

- [x] 1.1 Add the `auth.conf` rule allowing the console's certificate to `GET /puppet/v3/environment_classes` to the dev stack (`docker-compose.yml` / server config), restart the dev openvoxserver, and confirm the request answers 200 with the console certificate
- [x] 1.2 Add a class with typed parameters (Boolean, Integer, Enum, Array, one with no default, one with an expression default) and one manifest with a syntax error to the control-repo fixture; deploy; record the real `environment_classes` response as a test fixture
- [x] 1.3 `taskcatalog.Client.EnvironmentClasses(ctx, environment, etag)`: decode files, classes, parameters, per-file errors; `If-None-Match`/304; typed forbidden error distinct from unreachable and not-found; tests with `httptest` TLS servers and the recorded fixture

## 2. Class catalogue

- [x] 2.1 `internal/classcatalog`: types served to the frontend (class, parameter with type, required, default, defaultSource; file errors; fetchedAt; stale)
- [x] 2.2 Per-environment cache: five-minute freshness, ETag revalidation, shared in-flight reads, stale-on-error, expire-all, refresh-one; tests with a fake clock
- [x] 2.3 Subscribe to `codemanager.deployed` and expire the cache; test
- [x] 2.4 Handlers: `GET /api/v1/classes/environments`, `GET /api/v1/classes?environment=` (`classifier:read`), `POST /api/v1/classes/refresh?environment=` (`classifier:write`); 404 unknown environment; 503 with `reason` `not_configured` / `forbidden` / `unreachable`; tests
- [x] 2.5 Wire in `internal/app`; add the three routes to the node-scope sweep's exempt list with the reason

## 3. Group warnings

- [x] 3.1 Check a group's classes against an environment's classes: `class-missing`, `parameter-unknown`, `parameter-type`; suppress `class-missing` when the environment has parse errors; nothing when stale or unavailable; unit tests per scenario
- [x] 3.2 Type check by extending the existing task parameter validator (`Optional`, `Variant`, `Sensitive` of known types); unknown types pass; tests
- [x] 3.3 Group detail returns these warnings with the environment they were checked against; group list returns a `classWarnings` count per group and never fails when discovery does; handler tests
- [x] 3.4 Test that the ENC answer for a node is identical with discovery available, unavailable, and reporting a class missing

## 4. Run options: console

- [x] 4.1 `options` on `POST /api/v1/orchestrator/runs`: validate environment name; refuse an environment the server is known not to have; proceed when the environment list cannot be read; no job created on refusal; scope check unchanged and still first
- [x] 4.2 Store options in the job's `params` and `environment`; `Job.runOptions` in list and detail responses
- [x] 4.3 Dispatcher: no options sends today's `run` request unchanged (assert the bytes); options send `run_with_options`; an "unsupported action" answer becomes a failed target with the client-upgrade reason
- [x] 4.4 Record each target's output for a run with options
- [x] 4.5 Activity and audit summaries name the options
- [x] 4.6 Job detail adds each target's report environment; tests for differs, matches and no report
- [x] 4.7 Handler, store and dispatcher tests for every scenario in the orchestrator delta, including mixed client versions

## 5. Run options: node agent client

- [x] 5.1 `run_with_options` action: strict decode (unknown field fails), environment name validation, argv built per the design, bounded output; plain `run` untouched
- [x] 5.2 Tests with the injected runner: argv for each option and combination; invalid environment and unknown option fail without calling the runner; truncation notice; plain run argv unchanged
- [x] 5.3 Confirm against a real agent on the dev stack that `--verbose`/`--debug` output reaches stdout under `--onetime --no-daemonize`, and adjust the argv if not

## 6. Frontend

- [x] 6.1 Classes editor on the group page: class picker for the effective environment, declared-parameter picker, typed value inputs with defaults shown, free-text fallback, JSON view toggle, round-trip of unrecognised classes and parameters
- [x] 6.2 Environment field offers the server's environments and still accepts any name; picker reloads when it changes
- [x] 6.3 Warnings on the group page (naming the environment checked), parse errors, stale and unavailable notices with the reason, and a refresh control for `classifier:write`
- [x] 6.4 Groups list marks groups with class warnings
- [x] 6.5 Shared run dialog (no-op, environment, debug, the environment rule) used by the nodes, node and group pages; no options sends the plain request
- [x] 6.6 Jobs list and job page: options, per-target output for runs, environment mismatch mark, client-upgrade failure reason
- [x] 6.7 Extract and merge strings; translate German; report the status of the other locales; `frontend/build.sh`

## 7. Docs

- [x] 7.1 Guides `install/containers.md` and `install/packages.md`: the `auth.conf` step, how to check it, and what the console shows without it
- [x] 7.2 Guide `use/classify.md`: picking classes and parameters, the warnings and what they do not do, refresh, the fallback
- [x] 7.3 Guide `use/run-jobs.md`: run options, reading no-op and debug output, the environment rule and the mismatch mark, the client version needed
- [x] 7.4 `operations.md` and `architecture-summary.md`: class discovery source, cache and invalidation; the `run_with_options` request and why it is separate
- [x] 7.5 `make marketing` (or `make marketing-build`) passes, checked by its exit status

## 8. Verification

- [x] 8.1 `make test`, `go vet ./...`, gofmt, `openspec validate add-class-discovery-and-run-options --strict`
- [x] 8.2 Live on the dev stack, recorded in `evidence/`: pick a class and typed parameters and see the ENC answer; remove the class from the fixture, deploy, and see the flag with the group unchanged; remove the `auth.conf` rule and see the fallback; restore it
- [x] 8.3 Live: no-op run shows would-be changes and changes nothing on the node; debug run shows debug output; environment run on a node with and without a classified environment shows the mark only for the first; a run with options against an old client fails with the upgrade reason and does not run Puppet; a plain run is unchanged
- [x] 8.4 Screenshots of the classes editor, the warnings, the run dialog and a no-op job page

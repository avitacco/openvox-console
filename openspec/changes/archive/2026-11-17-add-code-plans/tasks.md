# Tasks

## 1. Loader and parser

- [x] 1.1 Discover plans in an environment: module path from `environment.conf`, `plans/*.yaml`, names, path-escape and size guards, and "not available" without a code directory; verify by tests over a temp code tree including symlink escape and `init.yaml`.
- [x] 1.2 Parse the YAML subset into a checked plan (parameters and types, steps, references) and refuse everything else with reasons naming the step; verify by table tests over accepted and refused plans, including `command`, `eval` and a bad `$` expression.
- [x] 1.3 Parameter type checking and defaults (reusing the task type validator where it fits, plus `TargetSpec` and `Sensitive`); verify by tests naming the parameter on each refusal.

## 2. Evaluator

- [x] 2.1 Evaluate references and interpolation against parameters and step results, preserving types, with a `Sensitive` value that prints as redacted; verify by tests including result paths, `$$`, and redaction in text.
- [x] 2.2 Step result shape (`ok`, `results`, `targets`, `ok_targets`, `error_targets`, `value`) built from a child job; verify by tests with one and many targets.

## 3. Execution and record

- [x] 3.1 Migration 000040 (kind `codeplan`, `jobs.result`, `jobs.plan_trace`) and store methods for the trace and result; verify by a migration up/down test and store tests.
- [x] 3.2 Reaper: judge any job with a heartbeat by silence and any without by age; verify the patch tests still pass and a code plan job with a fresh heartbeat survives.
- [x] 3.3 Coordinator: run steps in order, start each task step through `Starter.StartTask` with the captured scope, wait on the child job with heartbeat, fail and record not-run steps, nested plans, and limits; verify by tests with a fake transport including a result feeding the next step, a refused step, depth and step limits.
- [x] 3.4 Skip `advancePlan` for children of a code plan; verify a saved plan still advances and a code plan's step does not trigger it.

## 4. API

- [x] 4.1 `GET /api/v1/code-plans` (and one plan) and `POST /api/v1/orchestrator/code-plans` with the static pass, target resolution, audit and activity entries; verify by handler tests including scope refusal, a missing parameter, a task the user may not run, and audit holding names only.
- [x] 4.2 Job reads show the trace and result, scoped like other jobs; the node-scope sweep passes with the routes classified; verify by tests including a scoped reader.

## 5. Console UI, translations and docs

- [x] 5.1 Code plans on the Plans page (list, unusable reasons, parameter form, run) and the trace on the job page; verify by frontend tests and screenshots.
- [x] 5.2 Translate every new string in all 15 locales; verify with `go run ./i18n status` complete.
- [x] 5.3 A guide under `marketing/guides/` (the supported subset, examples, scope and permissions) and `operations.md`; verify `make marketing-build` succeeds.

## 6. Verification

- [x] 6.1 `go build`, `go vet`, `gofmt`, `go test ./... -count=1` and `openspec validate add-code-plans --strict` pass.
- [x] 6.2 Live on the dev stack with a real plan under the fixture module: parameters, a result feeding a second step, a message with a sensitive value redacted, an agentless target, a scoped user refused at a step, a failing step with later steps not run, and a long-lived run kept alive past the old reaper window (shortened threshold); evidence in `evidence/`.

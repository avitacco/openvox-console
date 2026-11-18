# Tasks

## 1. Build identity and monotonic package versions

- [x] 1.1 Confirm the uncommitted `add-task-execution` changes in `internal/nodeagent` and `internal/orchestrator` are committed or otherwise settled before editing those packages; verify with `git status` that this change starts from a known base
- [x] 1.2 Add stamped `Version` and `BuildTime` variables to the client and a `--build-info` flag that prints version, build time and the executable's SHA-256 and exits; verify with a unit test and by running a stamped local build
- [x] 1.3 Stamp version and build time in `make agent-binaries` and the Dockerfile's client build loop, and write `internal/agentdist/bin/builds.json` (per platform: digest, build time, version); verify `make agent-binaries` produces the file and `--build-info` on the linux/amd64 binary matches it
- [x] 1.4 Change `cmd/build-agent-packages` to take the build time and emit `<base>+<timestamp>.<sha>` (deb) and `<base>` with release `<timestamp>.<sha>` (rpm), with `0.1.0` as the base when untagged; verify with unit tests that the new versions compare greater than `0.0.0+bbba663-dirty` under dpkg ordering and `bbba663_dirty-1` under rpm ordering, and that a later timestamp orders after an earlier one
- [x] 1.5 Update `internal/agentdist/repo.go` metadata for the rpm release field and its tests; verify `go test ./internal/agentdist/` passes and, in a Debian and a Rocky container holding the previously built package, that the install script upgrades to the new one
- [x] 1.7 Make `postrm.sh` act only on a real removal and run `postinst.sh` as the rpm's `%posttrans`, so an rpm upgrade no longer leaves the service stopped; verify with script tests for every dpkg and rpm argument and live on Rocky, including an upgrade from a package carrying the old script (see evidence/1.5 and 5.5)
- [x] 1.6 Document the version scheme in the agent-distribution section of `operations.md`

## 2. Client: status and upgrade actions

- [x] 2.1 Add `NODE_AGENT_ACCEPT_UPGRADES` and the upgrade directory to `nodeagent.Config` with per-OS defaults; verify with config tests
- [x] 2.2 Implement install-method detection (deb, rpm, binary, unknown) behind an injectable command runner; verify with unit tests for each outcome including a script-managed Linux install
- [x] 2.3 Implement the `agent_status` action returning digest, build time, version, os, arch, install method and `acceptsUpgrades`; verify with a handler test and that an unstamped build reports an empty build time
- [x] 2.4 Implement artifact download with a size cap into the 0700 upgrade directory, size and SHA-256 verification, and cleanup on every exit path; verify with tests for a good artifact, a digest mismatch, an oversized body, and a failed download
- [x] 2.5 Implement pre-install validation: extract the client binary from a verified deb/rpm when needed, run it with `--build-info`, and require the requested build; verify with tests for a non-executable artifact and a wrong-build artifact
- [x] 2.6 Implement the Linux installer: `dpkg -i` / `rpm -U --oldpackage` launched through `systemd-run` in a transient unit, with the new-session fallback; verify with unit tests on the constructed commands and live in Debian and Rocky systemd containers that the service comes back on the new build and the package database shows the new version
- [x] 2.7 Implement the macOS and Windows installers (atomic rename; rename-aside, move into place, non-zero exit; removal of a leftover `.old` on start) behind the same interface; verify with unit tests against a temporary directory on each build tag and `GOOS=windows`/`GOOS=darwin go vet`
- [x] 2.8 Implement the `agent_upgrade` action tying 2.4-2.7 together, refusing when upgrades are declined or the install method is unknown, and replying before the restart; verify with handler tests for started, declined, unknown install method and each failure reason
- [x] 2.9 Add `sc.exe failure` restart actions to the Windows install script and confirm the macOS plist's `KeepAlive` covers a clean exit; verify with the existing script tests plus PSScriptAnalyzer and shellcheck
- [x] 2.10 Update the node-agent section of `operations.md` with the two actions, the upgrade directory and the opt-out variable
- [x] 2.11 Handle each request on its own goroutine so a second request during a run or task is refused as busy at once instead of queuing behind it, and answer a build-status request whatever else is running; verify with client tests over a real transport (second request refused within 2 s and never run; status answered during a run) and live on a Debian node (see evidence/5.2)

## 3. Console: artifacts, state and coordinator

- [x] 3.1 Load `builds.json` and compute package artifact size and digest per platform and format in `internal/agentdist`, exposed to other packages; verify with tests including the "packages not built" case
- [x] 3.2 Add migration `000028` for the per-node agent build and upgrade state with a store offering upsert-status, claim-due (conditional update honouring the concurrency limit), record-outcome and list; verify with store tests including two concurrent claimers getting disjoint nodes
- [x] 3.3 Add `CONSOLE_AGENT_AUTO_UPGRADE` to `internal/runtime` config, default on; verify with config tests
- [x] 3.4 Implement out-of-date determination (unknown build; differing digest with an earlier build time; never when the console build is unstamped or the client is newer); verify with a table-driven test covering every scenario in the agent-upgrade spec
- [x] 3.5 Implement the coordinator: `agent_status` on connect, the sweep, the upgrade dispatch, busy/error/timeout outcomes, backoff, exhaustion, and reset on a new console build; verify with tests using a fake dispatcher and clock for each outcome
- [x] 3.6 Wire the coordinator into `internal/app` on the node-connect fan-out and register its worker in `surface.go` beside the initial-run trigger; verify `internal/app` mode tests pass and that modes without a node listener do not start it
- [x] 3.7 Add `POST /api/v1/nodes/{name}/agent-upgrade` with `nodes:manage`, audit emission and the 409 reasons; verify with handler tests for success, missing permission, disconnected, current and non-upgradable nodes
- [x] 3.8 Document the setting, the retry behaviour and the manual endpoint in `operations.md` and the relevant guide under `marketing/guides`
- [x] 3.9 Report a client's busy refusal from the manual upgrade endpoint as 409 `busy` by waiting briefly for the upgrade's reply, releasing the claim without counting an attempt; verify with the coordinator test and live (409 in 20 ms during a task, 202 after it; see evidence/5.2)
- [x] 3.10 Read a node's build reliably: retry a just-connected node that is not yet listening and re-ask at the sweep, ask every connected node when the coordinator starts, and log a reading that fails; verify with coordinator tests and live (0 of 14 restarts missed, against 1 in 3 before)

## 4. Console: surfacing

- [x] 4.1 Add the `agent` object to the node status response in `internal/nodeconnectivity`; verify with handler tests for current, outdated, unknown, unreported and failed nodes
- [x] 4.2 Show the out-of-date badge on the nodes page and the build, state, failure reason and "Upgrade client" action on the node page, hiding the action without `nodes:manage`; verify by loading both pages against a dev stack with a stale and a current node
- [x] 4.3 Add current / out-of-date / failed / needs-install-script counts and the auto-upgrade state to the Fleet Status tab; verify against the same dev stack
- [x] 4.4 Add the new strings to `frontend/locales` and regenerate `internal/web/dist`; verify the i18n lint test passes
- [x] 4.5 Add a "Keeping node agents current" section to the guides, including the one-time install-script re-run for existing nodes

## 5. End-to-end verification

- [x] 5.1 In the compose stack, enroll a Debian and a Rocky node with the current client, rebuild the console with a later-stamped client, restart it, and confirm both nodes upgrade without intervention and report the new build; save the evidence under the change's `evidence/` directory
- [x] 5.2 Start an upgrade while a long task runs on the node and confirm the task completes and the upgrade follows; save the evidence
- [x] 5.3 Serve a corrupted artifact and confirm the node keeps its client, the failure reason appears on the node page, and retries back off and stop; save the evidence
- [x] 5.4 With two console instances (`docker-compose.cluster.yml`) on different client builds, confirm a node is never downgraded and is instructed by only one instance; save the evidence
- [x] 5.5 Connect a client built from the commit before this change and confirm it is shown as needing the install script, then re-run the install script and confirm it becomes upgrade-capable; save the evidence
- [x] 5.6 Run `go test ./...` and the frontend tests and confirm they pass

## Why

Every prior phase has assumed code already exists on disk
(`openvox-code/environments/production`, hand-populated so far). Phase 5
closes that gap: the console deploys Puppet code from a control repo
(Puppetfile + manifests) into the live environment directory
`openvoxserver` actually reads from, on a webhook push or a manual
trigger, so classification and catalog compilation work against code the
console itself manages end to end.

## What Changes

- Add a `code-manager` capability: deploy trigger (webhook and manual),
  running g10k against a configured control repo; atomic directory swap
  so `openvoxserver` never reads a partially-deployed environment;
  deploy status/history persisted and exposed via API/UI; a
  "deployment ready" NATS event published on every successful deploy
  (the event contract Phase 6+'s compiler-distribution logic will
  subscribe to - this phase defines and verifies the contract itself,
  which is unchanged whether zero or several subscribers exist yet).
- Deploy actions are permission-gated (`code:deploy` to trigger,
  `code:read` to view status/history), and every deploy publishes an
  activity event (create the group/user/role handler equivalent for
  deploys - see the `activity` capability).
- **Deliberate architecture deviation, explicitly confirmed with the
  user**: `architecture-summary.md`'s "Code Manager: g10k in-process,
  not r10k" section calls for vendoring g10k as an in-process Go
  library. I confirmed this is not achievable as written: every file in
  g10k (`github.com/xorpaul/g10k`, Apache-2.0) is `package main` with
  package-level mutable global state (config, mutex, counters) - Go's
  compiler cannot import `package main` from another package, and this
  isn't a refactor-later gap, it's a language rule. Presented with fork-
  and-refactor / subprocess / reimplement-natively as the three real
  options, the user chose to shell out to the g10k binary as a
  subprocess. This supersedes that section's "in-process, not r10k"
  reasoning specifically for g10k - but the *original* reason r10k was
  avoided (needing a Ruby runtime alongside the Go binary) does not
  apply here: g10k is itself a Go binary, so this deviation only gives
  up true single-binary distribution, not the Ruby-runtime-avoidance
  goal that motivated picking g10k over r10k in the first place.
- The Puppetfile-compatibility risk `architecture-summary.md` already
  flags (g10k's parser has gaps vs r10k's Ruby-eval semantics) remains
  relevant regardless of in-process-vs-subprocess and is carried forward
  as this phase's documented side-track, not solved by this proposal.

## Capabilities

### New Capabilities
- `code-manager`: control-repo-driven code deployment - triggering,
  running, and tracking g10k deploys into the live environment
  directory, and publishing the deployment-ready event other components
  will subscribe to.

### Modified Capabilities
(none - this is additive; existing capabilities are unaffected)

## Impact

- New Postgres migration: a `deploys` table (id, control repo ref/SHA,
  status, started/finished timestamps, triggered-by, error detail).
- New `internal/codemanager` package: g10k subprocess invocation, control
  repo fetch (git clone/pull), atomic directory swap, deploy
  status store, `POST /api/v1/code-deploys` (manual trigger),
  `POST /api/v1/code-deploys/webhook` (git-hosting webhook, HMAC-verified),
  `GET /api/v1/code-deploys` (history/status).
- `cmd/console/main.go`: wires the new capability, adds `code:deploy` and
  `code:read` to `allPermissions`, wires its `recordActivity` closure
  the same way `classifier`/`rbac` already are.
- New `CONSOLE_G10K_BIN_PATH`, `CONSOLE_CONTROL_REPO_URL`, and
  `CONSOLE_CODE_WEBHOOK_SECRET` config (all in `internal/runtime.Config`),
  following the existing optional-until-configured pattern used for OIDC.
- `Makefile`: a target to install a real g10k binary for local dev
  (`go install github.com/voxpupuli/g10k@latest`), matching how
  `rbac-keys` already handles one-time local dev setup.
- `docker-compose.yml`: a real local git server (or bare repo volume) so
  deploys can be verified against a genuine control repo, not a mock -
  matching this project's established verification practice throughout
  every prior phase.
- `frontend/src/deploys.html`/`deploys.js`: new page showing deploy
  status/history; nav link added to every existing page's header.

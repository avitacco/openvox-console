## Why

The console deploys from exactly one control repo: `internal/runtime.Config`
carries a single `ControlRepoURL`, and `codemanager.Deployer.Run` writes a
g10k config with one hardcoded source named `control`. Organisations that
split Puppet code across repos - a platform team's control repo plus a
per-team or per-business-unit one - cannot express that, even though the
only thing standing in the way is console-side: g10k's own config format
already takes a map of sources, each with its own remote, basedir, prefix
and private key.

Without this, the workaround is to funnel every team through one repo's
branches, which makes the control repo a permissions chokepoint precisely
where teams most want autonomy.

## What Changes

- **Multiple control repos.** A new optional YAML sources file (pointed at
  by `CONSOLE_CODE_SOURCES_PATH`) declares any number of named sources,
  each with a remote and optional prefix, deploy key and webhook secret.
  The existing `CONSOLE_CONTROL_REPO_URL` continues to work unchanged and
  defines a source named `control`; the two are mutually exclusive, so
  there is exactly one way to read any given deployment's source list.

- **Prefixes resolve environment name collisions.** Two repos may each
  have a `production` branch. A source's `prefix` decides the Puppet
  environment name its branches produce (`production` unprefixed,
  `team_a_production` when prefixed), so both can be live simultaneously.
  Prefix is per-source and defaults to off, which is what keeps existing
  installations' environment paths stable.

- **Deploys are scoped to a source.** The manual trigger and the deploy
  history gain a source field. Deploying `production` in one repo no
  longer deploys every repo's `production` branch, which is what the
  current `g10k -branch` invocation would do once more than one source
  exists.

- **Per-source webhooks.** A new `POST /api/v1/code-deploys/webhook/{source}`
  endpoint verifies that source's own secret. The existing unsuffixed
  webhook route keeps working for the default source.

- **Deploy history records which source deployed.** The `deploys` table
  gains a `source` column, and the list API/UI gain a source filter.
  `ref` alone stops being a unique description of what was deployed.

- **Deployment-ready events name the source.** The published event gains
  the source name alongside the environment it already carries.

- Not breaking: every change above is additive. An installation with only
  `CONSOLE_CONTROL_REPO_URL` set deploys the same branches to the same
  paths, through the same routes, as before.

## Capabilities

### New Capabilities

None. This extends how an existing capability is configured and scoped
rather than introducing a distinct area of behavior.

### Modified Capabilities

- `code-manager`: control-repo deploys become multi-source. "The
  configured control repo" becomes one of possibly several named sources;
  deploy execution, triggers (both webhook and manual), deploy
  history/status, and the deployment-ready event all gain a source
  dimension. Adds a requirement covering environment-name collision
  handling via prefixes, which has no equivalent today because a single
  source cannot collide with itself.

## Impact

**Configuration**
- `internal/runtime/config.go`: new `CodeSourcesPath`; `ControlRepoURL`,
  `CodeWebhookSecret` keep their meaning as the single-source form.
- New YAML sources file format (`gopkg.in/yaml.v3` is already a direct
  dependency).

**Code**
- `internal/codemanager/deploy.go`: emit one g10k source per configured
  source; switch per-source targeting from `-branch` to `-environment`.
- `internal/codemanager/handlers.go`: source on the manual trigger, new
  per-source webhook route, source filter on the list endpoint.
- `internal/codemanager/store.go` + a new migration: `source` column,
  filtering, and `DistinctRefs`' source equivalent.
- `internal/codemanager/event.go`: source on `DeployedEvent`.
- `internal/codemanager/activation.go`: unchanged - it activates by
  environment name, and prefixes keep those distinct.

**API**
- `POST /api/v1/code-deploys` accepts an optional `source`.
- `POST /api/v1/code-deploys/webhook/{source}` added; existing webhook
  route retained.
- `GET /api/v1/code-deploys` accepts a `source` filter and returns
  `source` on each deploy.
- New `GET /api/v1/code-deploys/sources` for the UI's filter and picker.

**Frontend**
- `frontend/src/deploys.js`, `frontend/templates/pages/deploys.tmpl`:
  source column, source filter, source picker on manual deploy.

**Docs**
- `README.md` and the setup/code-repo runbooks describe one control repo
  throughout.

## Context

See proposal.md for the g10k in-process-vs-subprocess deviation and why
it was decided the way it was. The console reads Puppet code from
`openvox-code/environments/production` today (see `docker-compose.yml`'s
`openvoxserver` service, which bind-mounts that host directory to
`/etc/puppetlabs/code`) - hand-populated for Phase 1-4 testing. This
phase makes that directory the *output* of a managed deploy process
instead.

`architecture-summary.md`'s own dependency table already names
`voxpupuli/g10k` (not `xorpaul/g10k`, the upstream it's forked from) as
the intended module - matching this project's existing convention of
using VoxPupuli's own OpenVox-branded forks/images throughout
(`openvoxserver`, `openvoxdb`, `openvoxagent`). This design uses that
same fork as the subprocess binary.

The existing `authorize`/`recordActivity` injected-closure pattern
(`internal/classifier`, `internal/rbac`, wired in `cmd/console/main.go`)
and the `internal/activity` event-publishing convention (one `Publisher`
per category, injected, never imported by the capability itself) both
carry forward unchanged - see those packages' own design docs for why.

## Goals / Non-Goals

**Goals:**
- A deploy is atomic from any concurrent reader's perspective - no
  request ever observes a half-written environment directory.
- Deploy triggers (webhook, manual), status/history, and the
  deployment-ready event are all genuinely verified against a real git
  control repo and a real g10k binary - no mocking the deploy mechanism
  itself, matching this project's practice in every prior phase.
- The deployment-ready event's contract is correct and tested now, even
  though this phase has no second "compiler" instance in this project's
  dev topology yet to actually consume it - see proposal.md.

**Non-Goals:**
- Consuming the deployment-ready event to run a second, distributed g10k
  deploy (that's the compiler-distribution mechanism itself - a later
  phase, per `phased-build-plan.md`'s dependency graph). This phase only
  defines and publishes the event.
- Multiple simultaneous control repos / multi-tenant deploy targets - one
  configured control repo, matching the phase's stated scope.
- GitLab-style shared-token webhook auth - GitHub's HMAC-signature
  convention (`X-Hub-Signature-256`) is the v1 target; a second webhook
  auth style is a small, separable addition later if needed.
- Solving the Puppetfile-compatibility gap itself (contributing a proper
  parser upstream to g10k) - carried forward as the documented
  side-track per `phased-build-plan.md`, not part of this phase's
  exit criteria.

## Decisions

**g10k is invoked as a subprocess (`os/exec`), config-file-driven.**
Per proposal.md's confirmed deviation. The console writes a g10k YAML
config (`sources`, target `basedir`) to a temp file per deploy attempt
and runs `g10k -config <path>` (optionally `-environment <branch>` for a
single-environment deploy), with a bounded `context.Context` timeout so
a hung deploy can't block the console indefinitely. Stdout/stderr are
captured into the deploy record's error detail on failure.

**The console manages atomic activation itself - it does not assume
g10k does.** g10k's documented behavior deploys directly into
`<basedir>/<environment>`; whether it internally stages-then-renames
that top-level directory atomically is not something to take on faith
(this exact kind of unverified assumption has bitten this project before
- see the OIDC change's `SameSite` cookie and ID-token-claims surprises,
both only caught by testing against the real thing). So the console
wraps it regardless: each deploy attempt gets a fresh versioned basedir
(`openvox-code/.deploys/<environment>/<git-sha>-<timestamp>/`), g10k
deploys into that, and only on g10k's success does the console do the
swap itself - `os.Symlink` to a temp name in the same directory, then
`os.Rename` over `openvox-code/environments/<environment>` (rename over
an existing path is atomic on the same POSIX filesystem, and a symlink
target change is what every reader actually resolves through). This
must be verified empirically during apply, not assumed correct from
reading g10k's docs - if g10k turns out to already do this safely on its
own, the wrapper is redundant but still correct and harmless; if it
doesn't, the wrapper is the only thing making the atomicity requirement
true. `openvox-code/environments/production`'s current hand-placed
content is superseded by the first real deploy against it - documented
in Risks below, not migrated (there's nothing there worth preserving,
it was Phase 1-4 test fixture data).

**Control repo fixture for dev/testing is a local bare git repo, not a
git-server container.** g10k clones over whatever transport its `remote`
config says, including a plain filesystem path - a `git init --bare`
directory (created once, populated with a real Puppetfile and
`site.pp`/manifests, committed and pushed to normally with `git`) is a
genuine control repo, not a mock, and needs no server process at all.
Webhook delivery itself is simulated directly in tests (a real HMAC-
signed POST, computed the same way a real git host would) rather than
running an actual GitHub/GitLab instance - the thing being tested is the
console's signature verification and deploy trigger, not a third-party
service's webhook delivery mechanism.

**Webhook signature verification uses GitHub's `X-Hub-Signature-256`
convention** (`sha256=<hex hmac>` over the raw request body, keyed by
`CONSOLE_CODE_WEBHOOK_SECRET`) - the most widely supported convention
across git hosts (GitHub natively, GitLab and others via generic webhook
relays), and simple to verify without a git-host-specific SDK.

**Deploy status lives in a `deploys` table**, one row per attempt:
`id, ref, status ('running'|'succeeded'|'failed'), started_at,
finished_at, triggered_by, error_detail`. `triggered_by` is `"webhook"`
or the acting admin's username (manual trigger) - mirrors how
`activity_log.actor` already distinguishes `"system"` from a real
username.

**Permissions: `code:deploy` and `code:read`**, following the existing
`<capability>:<verb>` convention (`classifier:read`/`write`,
`enc:read`, `activity:read`). Both added to `allPermissions` in
`cmd/console/main.go`, same as every prior phase's new permissions.

**The deployment-ready event is `codemanager.deployed`** (NATS subject),
payload `{environment, ref, deployedAt}` - published only after the
atomic swap completes successfully, mirroring `internal/activity`'s and
`internal/rbac`'s existing "publish only on genuine success" pattern
(e.g. OIDC's reconciliation event is skipped entirely on a no-op).

**g10k's config always sets `cachedir` inside the same directory tree as
`basedir`.** Confirmed empirically during apply: g10k hardlinks Forge
module files from its cache into the target directory, and hardlinking
fails outright across filesystem devices ("Can't hardlink Forge module
files over different devices"). g10k's own default cachedir
(`~/.cache/g10k`) is not guaranteed to share a device with wherever the
console's deploy basedir lives, so the generated config always pins
`cachedir` under `openvox-code/.deploys/.cache` (same tree, same
device), never left at g10k's default.

## Risks / Trade-offs

- [g10k's own internal atomicity behavior for a top-level environment
  directory is currently unverified] → Mitigated by design: the console
  never relies on it, wrapping every deploy in its own stage-then-swap
  regardless of what g10k does underneath. Verify empirically during
  apply rather than trusting documentation, consistent with this
  project's practice throughout.
- [Shelling out to g10k means the console's distribution now depends on
  a second binary, not one] → Explicitly accepted trade-off from the
  user's confirmed choice in proposal.md; document the `make g10k-install`
  step in README.md the same way `make rbac-keys`/`make oidc-up` already
  document one-time local dev setup.
- [`openvox-code/environments/production`'s existing hand-placed test
  fixture content is superseded, not preserved, by the first real
  deploy] → Accepted; it was scratch test data from Phase 1-4, not
  anything meant to persist.
- [A hung or slow control-repo fetch could tie up a deploy indefinitely
  without a timeout] → Mitigated: the subprocess invocation uses a
  bounded `context.Context` timeout, and a timed-out deploy is recorded
  as `failed` like any other failure.

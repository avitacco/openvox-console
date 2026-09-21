## Context

See proposal.md - Why. The mechanics that constrain the approach:

`Deployer.Run` writes a fresh `g10k.yaml` per deploy attempt into that
attempt's own directory, pointing g10k at a fresh empty `basedir`, then
runs `g10k -config <path> -branch <environment>` and symlinks the result
into `environments/<name>`. Because every attempt gets its own empty
basedir and only the deployed environment is linked live, g10k's
stale-environment purging never has anything to act on - a fact this
design depends on and preserves.

Two details of g10k's own behavior shape the decisions below, both read
from its source rather than its docs:

- `-branch <name>` matches the **raw branch name across every configured
  source**. With more than one source in a config, `-branch production`
  deploys production from all of them in a single run.
- `-environment <name>` matches per-source, but it compares against a
  literal `source + "_" + branch`, **regardless of that source's
  configured prefix**, while the directory it writes is
  `basedir/<prefix><branch>`. With `prefix: false` or a custom prefix
  string, the selector and the output directory name disagree.

`gopkg.in/yaml.v3` is already a direct dependency, so the sources file
needs no new module.

## Goals / Non-Goals

**Goals:**

- One deploy attempt maps to exactly one source and exactly one
  environment directory, structurally - not by convention.
- An installation that never configures a second source is bit-for-bit
  unaffected: same routes, same environment paths, same g10k invocation.
- Configuration errors that could cause one source to overwrite
  another's live code are caught at startup, not at deploy time.

**Non-Goals:**

- Deploying every source in one action. Each deploy attempt has one
  status and one activation; a fan-out trigger can be layered on later
  by calling the per-source path repeatedly.
- Removing a live environment when its branch or its source disappears.
  Nothing does that today (stale purging never applies, see Context) and
  this change does not add it.
- Managing sources through the UI. The sources file is the interface;
  see Decisions for why.
- Per-source RBAC. `code:deploy` remains fleet-wide - a user who may
  deploy may deploy any source. Scoping deploy rights per source is a
  real ask but it is an RBAC change, not a code-manager one.

## Decisions

### One source per g10k invocation, keeping `-branch`

The console writes a per-attempt config containing **only the source
being deployed**, and keeps invoking `g10k -branch <branch>` exactly as
it does today. The prefix still applies to the directory g10k writes, so
collision handling comes for free.

Alternative considered: put every source in the config and target one
with `-environment <source>_<branch>`. Rejected on both correctness and
blast radius. Correctness: as noted in Context, `-environment` compares
against `source_branch` while the output directory is `prefix+branch`,
so with `prefix: false` the console would have to pass a name that does
not match the directory it then has to go and find - two different
name derivations for one deploy, and a latent bug the first time
someone sets a custom prefix string. Blast radius: a config listing
every source means a single mistake - an empty selector, a future g10k
flag change - deploys every repo at once. Listing one source makes that
impossible rather than merely unlikely.

This also keeps the single-source case byte-identical to today's
behavior, which is the main compatibility claim this change makes.

Cost: deploying N sources means N g10k runs and N clones of shared
modules on a cold cache. They share one cachedir, so this is a
first-run cost only.

### Sources declared in a YAML file, not env vars or the database

`CONSOLE_CODE_SOURCES_PATH` points at a YAML file mapping source names
to their settings. Per-source secrets follow the existing
`CONSOLE_*_FILE` convention: `webhook_secret_file` alongside
`webhook_secret`, so a secret can stay in a Docker/systemd secret file
rather than the config.

Env vars were rejected because a source carries four or five settings,
and `CONSOLE_CODE_SOURCE_TEAM_A_WEBHOOK_SECRET_FILE` scales badly past
two repos while making the set of configured sources something you have
to reconstruct by scanning the environment.

A database table with an admin UI was rejected for this change, not
forever. It is the better operator experience - add a repo without host
access - but it means a migration, CRUD API, permission model, deploy
key storage, and UI, which is more work than everything else here
combined, and it moves deploy configuration out of git review. The YAML
file is a strict subset of that work: the loader produces the same
in-memory source list either way, so a later DB-backed source store
replaces the loader and nothing downstream of it.

### The two configuration forms are mutually exclusive

Setting both `CONSOLE_CONTROL_REPO_URL` and `CONSOLE_CODE_SOURCES_PATH`
is a startup error. The alternative - merge the single repo in as a
source named `control` - creates a configuration where the same repo can
be declared twice with different prefixes, and where reading the config
file alone does not tell you what will deploy. Refusing is one line and
the error message can say exactly which to remove.

`CONSOLE_CONTROL_REPO_URL` alone remains fully supported. It is not
deprecated: for the single-repo case it stays the simpler configuration.

### Collisions are prevented by requiring distinct prefixes

A collision needs two sources sharing both an effective prefix and a
branch name. Branch names are not knowable at startup, so the check is
on the prefix: **the effective prefixes of all configured sources must
be pairwise distinct**, which in practice means at most one source may
be unprefixed.

Alternative considered: detect the collision at deploy time, when the
branch is known. Rejected because the failure it prevents is one source
silently overwriting another's live production code, and by deploy time
the only thing left to do is refuse - so the same refusal may as well
happen at startup, where it is visible before anything is at risk.

Effective prefix follows g10k's own resolution (`prefix: true` ->
`<source>_`, a string -> `<string>_`, `false`/unset -> empty) so the
console and g10k agree on the environment name without the console
reimplementing anything.

Source names are restricted to `[a-z0-9_]+`. They become part of a
Puppet environment name, and Puppet rejects environment names outside
that character set - a source named `team-a` would deploy into a
directory openvoxserver refuses to load.

### Webhooks route by path segment with per-source secrets

`POST /api/v1/code-deploys/webhook/{source}` verifies that source's own
secret. The pre-existing unsuffixed route continues to work and targets
the default source.

Alternatives considered. Reading the repository URL out of the push
payload keeps one URL but requires parsing per-forge payload shapes
(GitHub, GitLab and Gitea differ) and leaves every repo sharing one
secret, so a compromised secret in a low-trust team repo can trigger a
deploy of the platform control repo. A `?source=` parameter has the same
shared-secret weakness with attacker-controlled routing on top. The path
segment costs one route and gives each repo an independently revocable
credential.

A request whose source segment is unknown is rejected **after** the
signature check fails, not with a distinct "no such source" response, so
the endpoint does not enumerate configured source names to an
unauthenticated caller.

### `deploys.source` is `NOT NULL DEFAULT 'control'`

A new migration adds the column and backfills existing rows with
`control`, which is precisely what they were. Keeping the `DEFAULT`
afterwards - rather than dropping it once backfilled - is deliberate: a
rollback to the previous binary leaves `INSERT INTO deploys (ref,
status, started_at, triggered_by)` running against a table with the new
column, which succeeds only because of that default. It costs nothing
and makes the migration safely reversible without a down-migration race.

## Risks / Trade-offs

**Two sources deploying concurrently share one g10k cachedir** → This
already happens today for two branches of one repo, so it is
pre-existing rather than introduced, and g10k takes its own per-module
locks. Multi-source makes it more likely by widening what can be
in flight at once. Not mitigated here; if it proves to bite, serialising
deploys behind a per-cachedir lock is a contained follow-up.

**A removed source leaves its environments live** → Deleting a source
from the config stops future deploys but leaves `environments/team_a_*`
in place, and openvoxserver keeps compiling from it. Documented as an
operator step rather than automated, because the alternative - deleting
Puppet environments as a side effect of a config edit - is a far worse
failure when someone edits the file wrongly.

**Per-source deploy keys widen what the console's process can reach** →
Each `private_key` is read by g10k as the console's service user, so the
console host becomes a place where every team's deploy key sits. Keys
should be read-only deploy keys scoped to their own repo. This is a
documentation matter; the console cannot enforce it.

**A prefixed environment name changes node classification** → Adding a
prefixed source does not move existing environments, but nodes must be
classified into `team_a_production` explicitly to get that code. Nothing
auto-assigns them, so a newly added source deploys successfully while
appearing to do nothing. Worth calling out in the runbook.

## Migration Plan

Every step is additive; there is no cutover.

1. Ship the migration. Existing rows backfill to `control`. The previous
   binary continues to work against the migrated schema (see the
   `DEFAULT` decision above), so this step is independently reversible.
2. Ship the code. With no `CONSOLE_CODE_SOURCES_PATH` set, behavior is
   unchanged: one source named `control`, unprefixed, same routes, same
   environment paths, same g10k invocation.
3. An operator adopting multiple sources writes the sources file, moves
   their existing remote into it as the unprefixed `control` source,
   unsets `CONSOLE_CONTROL_REPO_URL`, and restarts. Environment paths do
   not move, so no node reclassification is needed for existing code.

Rollback: revert the binary. The extra column is inert to the old code
and the old configuration path never stopped working.

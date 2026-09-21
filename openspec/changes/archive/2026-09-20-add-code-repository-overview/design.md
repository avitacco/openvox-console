## Context

See proposal.md - Why. What the data actually looks like, since three of
the four reported fields come from somewhere other than the code
manager:

- **Config** gives name, remote and prefix. `codemanager.Sources`
  already holds all three.
- **openvoxdb's `nodes` entity already returns `report_environment`** -
  verified live against this project's stack. No new query entity is
  needed, only a struct field, exactly as
  `latest_report_corrective_change` was added.
  `catalog_environment` is null across that same data, so it is not a
  usable signal here.
- **`classifier.Classify` already resolves a node's effective
  environment**, merging every matching group by priority. The assigned
  count is that function applied fleet-wide, not a `node_groups`
  aggregate.
- **The `deploys` table cannot currently say which environment an
  attempt deployed.** `ref` holds the pushed commit SHA for
  webhook-triggered deploys and the branch name for manual ones, so the
  environment is not recoverable from a row.

`Deployer.Run` stages into `.deploys/<environment>/<timestamp>/basedir/
<environment>` and activates by symlinking `environments/<name>` at it.
g10k hardlinks Forge module files from the shared cachedir into that
tree - which is what makes "size" ambiguous below.

This change depends on `add-multi-source-code-deploy`, which is
implemented but not yet archived.

## Goals / Non-Goals

**Goals:**

- One request answers "is this repository doing anything?" without the
  operator correlating the Deploys page against node inventory by hand.
- Reporting a repository never walks a deployed module tree.
- A failure in one input degrades that field, not the page.

**Non-Goals:**

- Editing sources from this page. The sources file remains the
  interface; this is a read-only view.
- Per-environment drill-down (which nodes, which modules). The unit here
  is the repository; the Deploys page already covers per-attempt detail.
- Reclaiming disk. Size is reported, not acted on; nothing prunes
  `.deploys` today and this change does not add pruning.
- Recomputing sizes for historical deploys. They report as absent.

## Decisions

### Size is measured once, at deploy time, and stored

After g10k succeeds and before the result is activated, the staging tree
is walked once and its size recorded on the deploy row. The page then
reads a number.

Alternative considered: walk `environments/` per request. Rejected on
cost - a control repo with a real Puppetfile is tens of thousands of
files, and every viewer would pay for it on every load, to answer a
question whose value is entirely trend-shaped.

**Apparent size, not disk usage.** g10k hardlinks module files from the
shared cachedir, so two environments depending on the same module
version share those bytes on disk. The recorded number sums file sizes
without deduplicating by inode, which overstates real disk consumption
when environments overlap. That is the deliberate choice: the question
is "how big is this repository's deployed code", and an inode-dedup
figure would make one repository's reported size change depending on
which *other* repositories happen to be deployed beside it - a number
that moves for reasons having nothing to do with the repository it
describes. The UI labels it as content size for that reason.

### `deploys` gains an `environment` column alongside `size_bytes`

Both in one migration. `size_bytes` is what the size decision needs;
`environment` is what makes "which environments has this repo deployed"
answerable at all, given `ref` holds a commit SHA for webhook deploys.

Alternative considered: derive environments by listing `environments/`
and attributing each name to a source by longest-matching prefix.
Rejected because the unprefixed source would absorb every name no other
prefix claims - including hand-placed directories and environments left
behind by a source since removed from the config - and silently
overstate that one repository. Recording what was actually deployed
replaces a guess with a fact.

This also improves the existing Deploys page, where a webhook-triggered
row currently shows a bare SHA with no indication of what it deployed.

Both columns are nullable: every existing row predates them, and a
deploy that fails before producing a tree has no size to record.

### Two counts, derived from different sources, reported separately

*Assigned* runs `classifier.Classify` over every node's facts (one
fleet-wide facts query, the same one the package catalogue uses) and
maps each node's resolved environment to a repository. Using the
resolved result rather than a `node_groups` aggregate is what makes a
node matching three groups count once, against the environment it would
actually be sent to.

*Reporting* reads `report_environment` from the nodes query and maps it
the same way.

They are never reconciled into one number. The whole diagnostic value is
in the gap: assigned-but-not-reporting is a repository nothing has run
yet; reporting-but-not-assigned is nodes running code no group directs
them to, which usually means an environment left over from a removed
group or a node with a hardcoded `environment` in its `puppet.conf`.

**Environment-to-repository attribution** uses the environments
recorded in deploy history, not prefix matching, for the reason given
above. An environment no repository has deployed is counted against
none of them, and the page reports that total separately rather than
attributing it to the unprefixed source.

### Each field degrades on its own

The overview assembles four independent inputs. openvoxdb being
unreachable makes both node counts unavailable; it does not fail the
request, blank the remote, or hide the last-deploy time - all of which
come from config and Postgres. Counts are therefore nullable in the
response and render as "unavailable", distinct from a real zero, which
is itself meaningful.

### Credentials are stripped by parsing, not by regex

The remote is parsed as a URL and its userinfo replaced. A remote that
does not parse (`git@host:org/repo.git` is not a URL) is passed through
unchanged, which is safe: the SCP-like form has no place to put a
password. A regex over the raw string would risk either leaking an
unanticipated shape or mangling a legitimate one.

The current sources endpoint returns no remote at all, so this is a
deliberate widening of what `code:read` discloses. It is worth stating
plainly: an operator who puts a token in a remote has put it somewhere
every `code:read` user can see the *shape* of, and redaction protects
the secret, not the fact of its existence.

## Risks / Trade-offs

**The assigned count needs every node's facts on every page load** →
This is the same fleet-wide `inventory[certname, facts]{}` query the
package catalogue already issues, so the pattern and its cost are
established rather than new. On a large fleet it is still the most
expensive part of this page. Not mitigated now; if it bites, the
resolved-environment-per-node map is a natural thing to cache, since it
only changes when groups or facts change.

**Reported size will read as zero-ish for a while** → Only deploys made
after this ships record a size, so a busy repository can show "absent"
until its next deploy. Reported as absent rather than 0 specifically so
it does not read as an empty repository.

**Apparent size overstates disk usage** → See the size decision.
Mitigated by labelling, not by arithmetic.

**Two unarchived changes now modify the same requirement** →
`add-multi-source-code-deploy` and this change both carry a MODIFIED
"Deploy status and history". This change's version is a superset (source
*and* size), so applying them in either order converges, but they should
be archived oldest-first to keep the main spec's history sensible.

## Migration Plan

1. Ship the migration adding nullable `deploys.size_bytes` and
   `deploys.environment`. Nullable and additive, so the previous binary
   keeps inserting successfully and the step is independently
   reversible.
2. Ship the code. Existing rows report both fields as absent; new
   deploys populate them.
3. No operator action. The page appears; its size column fills in as
   repositories redeploy.

Rollback: revert the binary. Both columns are inert to the old code.

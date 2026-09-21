## Why

Once the console can deploy from several control repos, nothing in the
UI answers "what repos do I have, and is each one actually doing
anything?". The Deploys page lists attempts, not repositories: it shows
that `team_a` deployed at 14:05, but not that no node has ever run
against `team_a`'s environment, that its remote points somewhere
unexpected, or that it has quietly grown to 400MB on disk.

The gap matters most right after adding a source. A prefixed source
deploys successfully and changes nothing until nodes are classified into
its new environment - documented behavior, but invisible, and it looks
identical to a working repo.

## What Changes

- **A Code Repositories page** listing every configured source with its
  remote, prefix, environments, node counts, size and last successful
  deploy.

- **Two node counts per repository, side by side.** *Assigned* is how
  many nodes the classifier resolves into one of that repo's
  environments; *reporting* is how many nodes last reported from one.
  Neither alone is the answer: the divergence between them is what
  reveals a repo nothing uses, or nodes running code no group assigns.

- **Deploy size is measured at deploy time**, not on page load. The
  `deploys` table records the deployed tree's size, so the page reads a
  number rather than walking a module tree per request.

- **Remotes are shown with embedded credentials redacted.** A remote can
  carry a token (`https://x-access-token:ghp_...@github.com/...`), and
  this page is visible to every `code:read` user. This widens what the
  API exposes - the current sources endpoint deliberately returns no
  remote at all - so redaction is part of the requirement, not a detail.

- Repositories with no deploy history, and configured-but-never-deployed
  repos, still appear. A repo that has never worked is the one most
  worth seeing.

## Capabilities

### New Capabilities

None. This reports on control repo sources, which `code-manager`
already owns; a separate capability would split one subject across two
specs.

### Modified Capabilities

- `code-manager`: adds a repository overview requirement (per-source
  remote, prefix, environments, node counts, size, last deploy, with
  credential redaction), and extends the existing deploy status/history
  requirement to record the deployed tree's size.

## Impact

**Code**
- `internal/codemanager`: a repositories endpoint assembling config,
  deploy history, classifier-resolved environments and openvoxdb report
  environments; size measurement in the deploy pipeline; remote
  redaction.
- `internal/codemanager/store.go` + a migration: `deploys.size_bytes`,
  and a per-source "last successful deploy" query.
- `internal/openvoxdb`: a query returning each node's
  `report_environment` (the nodes entity already carries it; no new
  entity needed).
- `internal/classifier`: reuses `Classify` to resolve each node's
  effective environment - no change to classification itself.

**API**
- New `GET /api/v1/code-repositories` behind `code:read`.

**Frontend**
- New page + template, and a nav entry alongside Deploys.

**Docs**
- `README.md`: what the page reports and what the two node counts mean.

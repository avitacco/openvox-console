## Context

`internal/certstatus.Client` already holds the CA-client credential and
calls openvoxserver's bulk `GET /puppet-ca/v1/certificate_statuses/all`
for read-only status. Its package doc is explicit that it "does not
implement sign/revoke/clean, even though the credential could" - this
change is exactly that follow-on, referenced there as a known future
step.

Puppet Server's CA API also exposes a per-node endpoint,
`/puppet-ca/v1/certificate_status/:certname` (singular), already
confirmed live against the real CA:
- `GET` - single-node detail (state, fingerprints, validity window)
- `PUT` with `{"desired_state": "signed"}` - sign a pending request
- `PUT` with `{"desired_state": "revoked"}` - revoke a signed cert
- `DELETE` - clean (revoke if needed, then remove the CA's record
  entirely, freeing the certname to re-request)

All three write actions are gated behind the same `pp_cli_auth`
extension as the bulk read - no new credential or server-side
authorization change is needed, only new console-side code paths that
exercise a privilege the credential already has.

See proposal.md for motivation; see
`operations.md`'s "Certificate status reporting (CA-client credential)"
section for the credential's provisioning and its already-documented
full-CA-admin privilege.

## Goals / Non-Goals

**Goals:**
- Expose sign/revoke/clean as console API actions, reusing the existing
  CA-client credential.
- Keep the credential's blast radius visible and gated: a distinct
  permission for write actions, separate from read-only status
  visibility.
- Audit every action, success or rejection reason distinguishable from
  the log alone.

**Non-Goals:**
- No new credential, no scoped/narrower CA-client role (Puppet Server
  has none to offer - see `internal/certstatus`'s package doc).
- No bulk/multi-node actions (sign-all, revoke-all) in this change - one
  action targets one certname. A bulk variant, if ever wanted, is a
  separate proposal given the larger blast radius of a single mis-click.
- No change to how certificate status is *read* - `Statuses` is
  untouched; this only adds new methods alongside it.

## Decisions

**Extend `internal/certstatus.Client` with `Sign`/`Revoke`/`Clean`
methods, rather than a new package.** The package already exists
specifically to isolate this credential (see its doc comment); a second
package holding the same credential would just be two places to audit
instead of one, for no isolation benefit. The package's own doc comment
already anticipated and named this exact extension.

**Add the new HTTP handlers to `internal/nodeconnectivity` rather than a
new package.** `nodeconnectivity.Handlers` already owns the `/api/v1/
node-connectivity` surface and already holds a `CertStatusClient`
dependency; the new actions are naturally adjacent REST endpoints on the
same node-oriented resource, not a distinct capability boundary the way
the credential itself is. (The delta spec still lives in its own
`node-certificate-management` capability file per proposal.md, since its
requirements - and its permission - are distinct from
`node-connectivity`'s read-only ones; capability-file granularity and
package granularity aren't required to match.)

**Endpoints:**
```
POST   /api/v1/nodes/{certname}/cert/sign
POST   /api/v1/nodes/{certname}/cert/revoke
DELETE /api/v1/nodes/{certname}/cert
```
`sign`/`revoke` are `POST` (they don't fit plain REST verbs on the
resource, and matches this project's existing `orchestrator:run`-style
action endpoints); `clean` maps naturally to `DELETE` on the cert
sub-resource. All three require `nodes:certs:manage`.

**New permission `nodes:certs:manage`, not reusing `nodes:read` or an
existing write permission.** Matches the confirmed decision: read access
to the Nodes page (connectivity + cert status) must not imply the
ability to revoke or clean a node's identity. No schema change is
needed - permissions are free-form strings assigned to roles through the
existing role management endpoints (`internal/rbac`'s "Role and
permission management" requirement); this is purely a new string
convention, documented in the new capability's spec.

**Extend `CertStatusClient`'s narrow interface** (in
`nodeconnectivity`) with the three new methods, mirroring the existing
`Registry`/`CertStatusClient` narrow-interface pattern already used
there - `nodeconnectivity`'s tests get fakes for the new methods the
same way they already fake `Statuses`.

**State validation happens against a fresh single-node lookup, not the
handler's own assumption.** Before sign/revoke, the handler calls the
new `Client` method, which itself calls the per-node `GET` to check
current state, then issues the `PUT`/`DELETE` - avoiding a
check-then-act race against a state the console isn't authoritative
over (the CA is). Puppet Server's own API independently rejects an
invalid transition (e.g. signing an already-signed cert), so this is
belt-and-suspenders correctness, not the only guard.

**Audit category: reuse `auditlog.CategoryNodes`**, with
`auditWrite(auditlog.CategoryNodes)` (mirrors `classifier`'s
`auditWrite`/`auditRead` pair in `cmd/console/main.go`). New actions:
`node.certificate.signed`, `node.certificate.revoked`,
`node.certificate.cleaned`, each with `ResourceType: "node"`,
`ResourceID: <certname>`. A rejected action (unauthorized, not
configured, wrong state) returns an HTTP error and emits no audit entry
- consistent with "a rejected action is not falsely logged as
successful" in the spec.

**Frontend: `window.confirm()` for the revoke/clean confirmation step.**
This codebase has no existing modal/dialog component to reach for
(checked - no `vox-dialog` or similar in `frontend/src`); introducing
one is more UI-framework work than a confirmation step warrants. `sign`
gets no confirmation (approving a pending request is the common,
low-risk path); revoke and clean each get a native `confirm()` naming
the certname and, for clean, explicitly stating the certname must
re-enroll from scratch. Action buttons are omitted entirely (not just
disabled) for a viewer without `nodes:certs:manage`, matching this
project's existing permission-gated UI convention.

## Risks / Trade-offs

- **A mis-click revokes or cleans a real, in-use node's identity.**
  Mitigated by: dedicated permission (not everyone with Nodes page
  access can act), confirmation dialog naming the certname, and the
  action being individually audit-logged with the acting user. Not
  mitigated: there's no undo - revoking/cleaning is exactly as
  irreversible from the console as it already was from a shell, this
  change doesn't add or remove that risk, only reachability.
- **`nodes:certs:manage` is effectively full CA admin, gated by a
  console-level permission string rather than anything openvoxserver
  itself understands.** If a role is misconfigured to grant this
  broadly, the console can't warn about it - openvoxserver's own
  authorization has no visibility into console roles. Mitigation is
  entirely procedural (grant narrowly, per proposal.md's Security
  note) - flagged explicitly in the new capability's spec and in
  `operations.md`, not solved in code.
- **Revoking a node the orchestrator or node transport currently has a
  live session with** - the transport connection isn't forcibly torn
  down synchronously by this change; the spec only requires the *next*
  authentication attempt to be rejected (matching how TLS revocation
  normally takes effect). An already-established connection may remain
  briefly usable until it naturally reconnects. Acceptable for v1;
  forcibly closing live sessions on revoke would require new
  coordination between `certstatus` and `nodetransport` this change
  doesn't otherwise need.

## Migration Plan

No data migration. Deploying this change adds new endpoints and a new
permission string that starts ungranted on every existing role - no
existing user gains new capability on upgrade. Rollback is a plain
revert (remove the endpoints/permission); nothing persisted depends on
their existence.

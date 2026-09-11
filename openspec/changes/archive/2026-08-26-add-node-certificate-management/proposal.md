## Why

The console can already report a node's certificate status
(`node-connectivity`'s "Certificate status reporting" requirement) but
cannot act on it. An operator who spots a `requested` (pending) or
`revoked` cert on the Nodes page still has to drop to a shell on the
openvoxserver host to run `puppetserver ca sign`/`revoke`/`clean`
directly - the console's CA-client credential (see `operations.md`'s
"Certificate status reporting (CA-client credential)" runbook) is already
privileged enough to do this, it's just never exercised for anything but
reads. Exposing sign/revoke/clean through the console closes that gap and
finishes the certificate lifecycle the Nodes page already started
surfacing.

## What Changes

- Add a new `internal/certstatus` capability: `Sign`, `Revoke`, and
  `Clean` methods against openvoxserver's per-node CA endpoint
  (`PUT`/`DELETE /puppet-ca/v1/certificate_status/:certname`), alongside
  the existing bulk-read `Statuses` method.
- Add three new console API endpoints for node certificate actions (sign
  a pending request, revoke a signed cert, clean a cert), each requiring
  certificate status reporting to be configured (same nil-safe
  `CONSOLE_CA_CLIENT_*` gate as reads) and rejecting the action with a
  clear error when it is not.
- Gate all three behind a new, dedicated `nodes:certs:manage` permission,
  separate from the existing `nodes:read` permission that already governs
  connectivity/cert-status *viewing* - so read-only Nodes page access
  never implies the ability to revoke or clean a node's identity.
- Add sign/revoke/clean actions to the Nodes page UI, each gated on the
  viewer actually holding `nodes:certs:manage` (buttons hidden rather than
  disabled for a `nodes:read`-only viewer, consistent with this project's
  existing permission-gated UI pattern), with a confirmation step before
  revoke/clean given their irreversibility (clean, in particular,
  invalidates the node's current identity - it cannot re-enroll under the
  same certname without generating a new key).
- Every action emits an audit log entry (`node.certificate.signed`/
  `.revoked`/`.cleaned`), matching the existing `node.connectivity.viewed`
  audit pattern.
- Every action is recorded with a proceed/cancel state, not silently
  auto-confirmed, given a mis-click on revoke/clean can knock a real node
  off the fleet or force it to re-enroll.

## Capabilities

### New Capabilities
- `node-certificate-management`: sign/revoke/clean actions against a
  node's Puppet certificate, gated by a dedicated permission, each
  audit-logged.

### Modified Capabilities
(none - `node-connectivity`'s existing read-only certificate status
reporting requirement is unchanged; this change only adds new write
actions alongside it. `node-connectivity` itself is still an unsynced
delta from `add-nodes-page`/`add-node-connectivity-timestamp-and-cert-status`
at the time of this proposal - this change's UI work depends on that
capability's Nodes page existing, but does not modify its requirements.)

## Impact

- **Code**: `internal/certstatus` (new `Sign`/`Revoke`/`Clean` methods on
  the existing client), a new handler set (either within
  `internal/nodeconnectivity` or a small new package - see design.md),
  `cmd/console/main.go` wiring, `internal/rbac` (new permission string,
  no schema change - permissions are free-form strings assigned to
  roles), `frontend/src/nodes.js` (action buttons + confirmation),
  `operations.md` (documenting that the CA-client credential is now
  exercised for writes, not just reads - the privilege was already there,
  but this is the first feature that uses it).
- **APIs**: three new endpoints under `/api/v1/nodes/{certname}/cert/...`
  (exact paths in design.md).
- **Security**: no new credential or privilege escalation - reuses the
  existing CA-client credential (already full CA admin per
  `operations.md`) - but is the first time that privilege is reachable
  from the console UI/API rather than only from a shell on the
  openvoxserver host. The new `nodes:certs:manage` permission is the
  control point; it should be granted narrowly.
- **Dependencies**: none new.

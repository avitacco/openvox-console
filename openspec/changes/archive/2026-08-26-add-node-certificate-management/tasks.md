## 1. CA-client actions

- [x] 1.1 Add `Sign`, `Revoke`, and `Clean` methods to `internal/certstatus.Client`, each first calling the per-node `GET /puppet-ca/v1/certificate_status/:certname` to check current state, then issuing the appropriate `PUT`/`DELETE`, returning a distinguishable error when the certificate isn't in the required state (e.g. signing an already-signed cert) or the CA has no record of the certname - verify with unit tests against a fake HTTP server covering: successful sign/revoke/clean, wrong-state rejection for sign and revoke, and clean of an unknown certname
- [x] 1.2 Verify live against the real openvoxserver: sign a real pending request, revoke a real signed cert, clean a real cert, confirming each via the existing bulk `Statuses` read - use a scratch/throwaway node certname for this, not any real node currently in use

## 2. API endpoints and permission

- [x] 2.1 Extend `nodeconnectivity`'s `CertStatusClient` interface with `Sign`/`Revoke`/`Clean`, and add `POST /api/v1/nodes/{certname}/cert/sign`, `POST /api/v1/nodes/{certname}/cert/revoke`, and `DELETE /api/v1/nodes/{certname}/cert` handlers, each gated by a new `nodes:certs:manage` permission and rejecting with a clear "not configured" error when no `CertStatusClient` is wired (mirrors the existing nil-safe pattern for reads) - verify with unit tests using a fake `CertStatusClient`, covering success, wrong-state rejection, and the not-configured case for all three actions
- [x] 2.2 Wire each successful action to an audit log entry (`node.certificate.signed`/`.revoked`/`.cleaned`, `ResourceType: "node"`, `ResourceID: <certname>`) via `auditWrite(auditlog.CategoryNodes)`, and confirm no audit entry is emitted for a rejected action - verify with a unit test asserting the audit recorder is invoked only on success
- [x] 2.3 Confirm `go build ./...` succeeds with the new endpoints wired into `cmd/console/main.go`

## 3. Frontend

- [x] 3.1 Add sign/revoke/clean action buttons to `frontend/src/nodes.js`'s cert status column, visible only when the logged-in user holds `nodes:certs:manage` (omitted, not merely disabled, otherwise), with a `confirm()` step before revoke/clean naming the certname and, for clean, stating the certname must re-enroll from scratch - verify `./frontend/build.sh` succeeds
- [x] 3.2 Live: as a user granted `nodes:certs:manage`, sign a real pending request and confirm its cert status updates to `signed` on the page; as a user without that permission, confirm the action buttons don't appear - screenshot both as evidence, matching this project's established verification style

## 4. Documentation and end-to-end verification

- [x] 4.1 Document the new `nodes:certs:manage` permission and the sign/revoke/clean actions in `operations.md`'s existing "Certificate status reporting (CA-client credential)" section - note explicitly that granting this permission is equivalent to granting shell-level CA admin access, and should be scoped as narrowly as the credential itself
- [x] 4.2 Run the full test suite (`make test`, or `go test -p 1 ./...` if package-level NATS test contention reappears), confirm it passes, `gofmt -l .` and `go vet ./...` clean

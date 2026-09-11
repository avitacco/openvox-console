## 1. Certname derivation

- [x] 1.1 Create `internal/infracert` with `SelfCertname(certFile string) (string, error)` (parse a leaf cert's Subject CN) and `PeerCertname(ctx, addr, certFile, keyFile, caFile string) (string, error)` (raw TLS dial, return the server's presented cert's CN) - verify with unit tests: `SelfCertname` against a real generated test cert, `PeerCertname` against an `httptest.NewTLSServer`-style real TLS listener (or equivalent raw `tls.Listen`) presenting a known cert
- [x] 1.2 Add a `Set` type to `internal/infracert` with a `Lookup(certname string) (reason string, ok bool)` method and a way to populate it (e.g. `Add(certname, reason string)`) - verify with a unit test covering a populated lookup, a miss, and an empty Set
- [x] 1.3 Wire `internal/infracert.Set` construction into `cmd/console/main.go`: `SelfCertname` for `CONSOLE_OPENVOXDB_CERT_FILE`/`CONSOLE_CA_CLIENT_CERT_FILE` (if configured)/`CONSOLE_NODE_TRANSPORT_CERT_FILE` (if configured), `PeerCertname` for `CONSOLE_OPENVOXDB_URL` and `CONSOLE_CA_CLIENT_URL` (if configured) - each failure logged as a warning, not fatal to console startup - verify `go build ./...` succeeds and a live console start logs a warning (not a crash) when one of these is deliberately misconfigured

## 2. API

- [x] 2.1 Add `IsInfrastructure`/`InfrastructureReason` to `nodeconnectivity`'s response type and a new `InfrastructureCertnames` narrow interface (`Lookup(certname string) (string, bool)`) that `*infracert.Set` satisfies directly, wired as a fourth `Handlers` dependency (nil-safe: every certname reports `isInfrastructure: false` when unset) - verify with unit tests using a fake `InfrastructureCertnames`, covering a flagged certname, an unflagged one, and the nil case
- [x] 2.2 Confirm `go build ./...` succeeds with the new dependency wired into `cmd/console/main.go`

## 3. Frontend

- [x] 3.1 Add a "Show infrastructure certs" checkbox to the Nodes page filters, defaulting unchecked; `filteredCertnames()` excludes any `isInfrastructure: true` row unless it's checked - verify `./frontend/build.sh` succeeds
- [x] 3.2 When shown, render an infrastructure row with a small distinguishing badge next to its name - verify visually via a live screenshot
- [x] 3.3 Update the Revoke/Clean `confirm()` dialogs to use `infrastructureReason`-aware wording when the target `isInfrastructure` - verify live: triggering Revoke/Clean on a real infrastructure cert shows the specific reason in the confirmation text, and on a real managed node shows the existing generic wording unchanged

## 4. Live verification

- [x] 4.1 Live: confirm the real Nodes page hides `console`, `console-ca-client`, `openvoxdb`, `openvoxserver`, and `node-transport` by default, and that the three real managed nodes (`openvox-testing-agent`, `f5259b0430e8`, `85bdf3470e9e`) remain visible - screenshot as evidence
- [x] 4.2 Live: enable "Show infrastructure certs" and confirm all five infrastructure rows appear with the distinguishing badge, each showing a real, specific reason (not a generic placeholder) - screenshot as evidence
- [x] 4.3 Live: confirm openvoxserver's own certname is correctly flagged as infrastructure (requires `CONSOLE_CA_CLIENT_*` to be configured, which it already is in this dev environment) - if it is NOT flagged, treat this as a bug to fix, not an acceptable gap, since this environment has exactly the config the design says should catch it

## 5. Documentation and full-suite verification

- [x] 5.1 Note in `operations.md` how infrastructure-cert detection works and its stated limitation (can't detect a server identity with no direct console connection to it) - point specifically at the `node-transport` case as the concrete example of a certname caught only incidentally via cert-file reuse
- [x] 5.2 Run the full test suite (`make test`), confirm it passes, `gofmt -l .` and `go vet ./...` clean

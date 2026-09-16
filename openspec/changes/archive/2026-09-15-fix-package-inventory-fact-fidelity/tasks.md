## 1. Fact scripts

- [x] 1.1 Change `internal/nodeagent/factassets/package_inventory_rpm.sh` to query `%{NAME}\t%|EPOCH?{%{EPOCH}:}|%{VERSION}-%{RELEASE}\n` and additionally emit `"console_package_inventory":{"format":2}` alongside `_puppet_inventory_1` - verify with `shellcheck` clean and a unit test that runs the script against a stubbed `rpm` on `PATH` (one epoch-bearing and one epoch-less package) and asserts the output parses as JSON with `1:3.0.7-24.el9` and `2.34-100.el9` versions and `format: 2`
- [x] 1.2 Change `package_inventory_apt.sh` to query `${Package}\t${Version}\t${source:Package}\t${source:Version}`, keep the `_puppet_inventory_1` tuples as binary name/version/`apt`, and emit `console_package_inventory` with `format: 2` and a `sources` map containing only binaries whose source name or version differs - verify with `shellcheck` clean and a unit test with a stubbed `dpkg-query` (same-name package, `libssl3`→`openssl`, and a binNMU where only the version differs) asserting valid JSON and exactly the expected `sources` entries
- [x] 1.3 Confirm existing `internal/nodeagent` toggle tests still pass unchanged (`go test ./internal/nodeagent/...`)

## 2. Refresh a stale fact on client start

- [x] 2.1 Add a `nodeagent` function that, given the facts directory and `fileExists`, rewrites `package_inventory.sh` with the embedded script for the detected family only when the file exists and its bytes differ, returning whether it changed - verify with table tests covering stale→rewritten, current→untouched (mtime unchanged), absent→not created, unknown family→untouched, and unwritable directory→error returned
- [x] 2.2 Call it from `cmd/node-agent-client` before connecting to the node transport, logging the outcome and continuing on error - verify with a test (or a run against a temp facts dir) that a refresh error is logged and startup proceeds, and `go build ./...` succeeds for linux, windows, and darwin targets

## 3. Console: source packages in the node package list

- [x] 3.1 Add an `internal/openvoxdb` method returning a single named fact for a certname (`facts { certname = ... and name = ... }`), returning nil when absent - verify with a unit test against the existing fake-server pattern in `queries_test.go` for present and absent cases
- [x] 3.2 Extend `internal/packageinventory`'s node package list to read `console_package_inventory` and add `sourcePackage`/`sourceVersion` to every `apt` entry when the fact exists (mapped source, else the binary's own name/version), omitting both fields when it does not - verify with handler tests for: fact present with mapped and unmapped apt packages, fact absent, rpm packages never getting the fields, and a fact-lookup error surfacing as the existing 502 behaviour
- [x] 3.3 Show the source package on the node detail page's package table when present (e.g. `libssl3` with a secondary `openssl 3.0.11-1~deb12u1` line), leaving rows without it unchanged - verify `./frontend/build.sh` succeeds and by the live check in 4.3

## 4. Live verification

- [x] 4.1 Live: build agent packages and the console; on a fresh Debian-family container enrolled via `install.sh`, enable package-inventory reporting, run `puppet agent -t`, and confirm via PQL that `package_inventory` rows are unchanged in shape and `facts { name = "console_package_inventory" }` has `format: 2` and a `sources` entry for `libssl3` (or another library package) - record the query output as evidence
- [x] 4.2 Live: repeat on a fresh `rockylinux/rockylinux:9` container, confirming an epoch-bearing package (e.g. `openssl`) now reports `1:...` in `package_inventory` and the companion fact has `format: 2`
- [x] 4.3 Live: on a container that already had reporting enabled with the *old* script, upgrade node-agent-client to the new package, confirm the service restart rewrote `facts.d/package_inventory.sh` without a Puppet run being triggered, then after the next `puppet agent -t` confirm corrected data in openvoxdb; view the node detail page and confirm source packages render - screenshot as evidence

## 5. Documentation and full-suite verification

- [x] 5.1 Update `operations.md`'s package-inventory section: RPM epoch format (and the exact-version search consequence), the `console_package_inventory` companion fact and its `format` marker, and that upgrading node-agent-client refreshes an enabled fact on restart - verify by reading the rendered section for accuracy against the live results in section 4
- [x] 5.2 Run `gofmt -l .`, `go vet ./...`, and `go test -p 1 ./...`; confirm all clean and passing

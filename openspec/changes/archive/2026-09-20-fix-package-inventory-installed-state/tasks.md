## 1. Apt fact: report only installed packages

- [x] 1.1 Add the install-status column to the `dpkg-query` format string in `internal/nodeagent/factassets/package_inventory_apt.sh` and filter to rows equal to `installed` before the awk aggregation, shifting every existing awk field index by one including the multi-arch dedupe key - verify by running the script inside a Debian 12 container that has a package in `config-files` state (install a package, then `dpkg -r` it without purging) and confirming that package is absent from both `_puppet_inventory_1.packages` and `console_package_inventory.sources`
- [x] 1.2 Verify the filter does not drop held packages - in the same container, `apt-mark hold` an installed package, run the script, and confirm the package still appears in `_puppet_inventory_1.packages` with its correct version
- [x] 1.3 Verify the source-package map is unchanged for installed packages - confirm a binary whose source differs in name and version (for example `libssl3` from `openssl`) still appears in `console_package_inventory.sources` with the correct source name and version, and that a multi-arch duplicate pair still emits exactly one source entry

## 2. Rpm fact: exclude signing-key pseudo-packages

- [x] 2.1 Exclude entries named `gpg-pubkey` in `internal/nodeagent/factassets/package_inventory_rpm.sh` - verify by running the script inside a Rocky 9 container after importing a repository signing key and confirming no `gpg-pubkey` row is emitted
- [x] 2.2 Verify real rpm packages are unaffected by the exclusion - confirm in the same container that the emitted package count drops by exactly the number of imported keys and that a package with an epoch still reports `epoch:version-release`

## 3. Tests

- [x] 3.1 Add a test covering the apt script against sample `dpkg-query` output containing a `config-files` row, a held row, a multi-arch duplicate pair, and a row whose source package differs in name and version, asserting the emitted JSON - verify the test fails against the pre-fix script and passes against the fixed one, so the test is proven to catch the defect
- [x] 3.2 Add a test covering the rpm script asserting `gpg-pubkey` rows are dropped while epoch formatting is unchanged - verify with `go test ./internal/nodeagent/...`

## 4. Documentation

- [x] 4.1 Document in `operations.md` that findings raised against packages which were never installed clear on convergence rather than immediately, and that stale dpkg entries are removed from a node with `apt purge '~c'` and specifically not with `apt autoremove` - verify the section names both commands and states the convergence path (client upgrade rewrites the fact, next Puppet run republishes, engine closes the findings)

## 5. Verification

- [x] 5.1 Run `gofmt`, `go vet`, and the full test suite and verify exit 0 across all packages
- [x] 5.2 Verify end to end against a live node - upgrade the agent on a node that already reports package inventory, confirm the stale fact is rewritten in place without triggering a Puppet run, and confirm that after the node's next Puppet run its reported inventory no longer contains the non-installed packages
- [x] 5.3 Verify the vulnerability findings converge - confirm that findings which existed only because of non-installed packages are closed after that node re-reports, and that findings for genuinely installed packages are retained

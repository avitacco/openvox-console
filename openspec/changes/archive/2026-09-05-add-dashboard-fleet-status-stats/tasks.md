## 1. Backend classification

- [x] 1.1 Add `LatestReportCorrectiveChange *bool` (json `latest_report_corrective_change`) to `internal/openvoxdb.Node` - no query change needed, the field is already present in the existing `nodes {}` response - verify with a unit test decoding a realistic response body that includes the field
- [x] 1.2 Rewrite `internal/inventory`'s `nodesSummary` handler to classify each node into `failed`/`corrected`/`intentional`/`unchanged` per design.md's rule, excluding `noop` and unreported nodes from all four, while `total` keeps counting every known node - verify with unit tests covering all four categories, the two exclusion cases, and that `total` still counts an excluded node
- [x] 1.3 Confirm `go build ./...` succeeds

## 2. Frontend

- [x] 2.1 Replace `frontend/src/index.js`'s dynamic `orderedStatuses`/per-status rendering with exactly four fixed `vox-stat` cards (Failed, Corrected, Intentional changes, Unchanged) in that order, dropping the "Total nodes" card - verify `./frontend/build.sh` succeeds
- [x] 2.2 Live: view the real Dashboard and confirm all four stat cards render horizontally with real counts (0 is fine) and no leftover "Total nodes" card or dynamic status list - screenshot as evidence

## 3. Live verification with real corrective-change data

- [x] 3.1 ~~Enable `corrective_change = true`~~ - superseded during implementation: confirmed live against the real `openvox-testing-agent` container that no such setting exists in FOSS/OpenVox Puppet (grepped `defaults.rb`, `puppet agent --help`, `puppet apply --help` - no match). Corrective-change detection is automatic, driven by `transactionstorefile` (`$statedir/transactionstore.yaml`) persisted across runs - see design.md's corrected Decisions entry. Nothing to enable; proceeding directly to 3.2's drift-and-correct test, which requires no config change
- [x] 3.2 Ran the agent cleanly, deliberately drifted `openvox-testing-agent`'s managed file (`/tmp/enterprise-console-code-manager-test-marker`, reversible/scratch), and ran the agent again - confirmed via `last_run_report.yaml` and the agent's own log (`(corrective)`) that Puppet genuinely detects the corrective change. Confirmed via direct, repeated PQL queries against the real openvoxdb (event level, report level, `nodes{}` projection, including an 11s wait to rule out lag) that `latest_report_corrective_change` is `null` regardless - traced the root cause to `openvoxserver`'s report submission using PDB command `store_report` version 8, which doesn't carry the field. **Cannot get `latest_report_corrective_change: true` in this environment** - this is a confirmed upstream limitation of the vendored images, not fixable in this project's code. See design.md's expanded Decisions section for the full trace, including a reverted attempt to upgrade to the newest published image tags (blocked by an unrelated ENC/entrypoint incompatibility in the newer openvoxserver build, plus a volume-ownership issue from the attempt itself - both fully diagnosed and recovered, no data lost)
- [x] 3.3 Superseded by 3.2's finding: the real Dashboard correctly shows this node under "Intentional changes" (not "Corrected"), which is the *correct* behavior given real data that never confirms a corrective change - matches the design's explicit "never guess corrected" rule. There is no real state to screenshot moving in and out of "Corrected" in this environment, since that category can never be populated here regardless of what the node actually did

## 4. Documentation and full-suite verification

- [x] 4.1 Note in `operations.md` (or the nearest existing relevant section) that `openvox-testing-agent` has corrective-change tracking enabled for this reason, so a future reader isn't surprised by it
- [x] 4.2 Run the full test suite (`make test`), confirm it passes, `gofmt -l .` and `go vet ./...` clean

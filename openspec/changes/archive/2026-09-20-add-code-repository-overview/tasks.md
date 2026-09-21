## 1. Deploy record: size and environment

- [x] 1.1 Add a migration adding nullable `deploys.size_bytes BIGINT` and `deploys.environment TEXT`, with a matching down migration; verify it applies, reverts and re-applies, and that an insert omitting both columns still succeeds (the previous binary's insert)
- [x] 1.2 Add a `treeSize` helper walking a directory and summing file sizes, skipping symlinks so an activated environment is never followed back into staging; verify with a unit test over a temp tree containing a nested dir, a symlink and an empty file
- [x] 1.3 Measure the staged tree in `Deployer.Run` and return it on `Deployment`; verify a test asserts a real deploy reports a non-zero size matching the tree it produced
- [x] 1.4 Record size and environment on deploy completion, and return them from `GetDeploy`/`ListDeploys` as nullable fields; verify `store_test.go` round-trips a deploy with both set and one with both absent, asserting absent stays absent rather than becoming 0

## 2. Repository overview data

- [x] 2.1 Add `ReportEnvironment` to `openvoxdb.Node` (the nodes entity already returns it - no query change); verify against the live stack that a real node row populates it
- [x] 2.2 Add a store query returning, per source, the environments deployed, the last successful deploy time, and the most recent recorded size; verify a test covers a source with several environments, one with none, and one whose only deploys failed
- [x] 2.3 Add a `redactRemote` helper replacing URL userinfo, passing SCP-style remotes through unchanged; verify unit tests cover an HTTPS remote with a token, one without, an SSH remote, and an unparseable string
- [x] 2.4 Add environment-to-source attribution from deployed environments (not prefix matching), returning unattributed environments separately; verify a test asserts an environment no source deployed is not attributed to the unprefixed source

## 3. Node counts

- [x] 3.1 Add an assigned-count resolver running `classifier.Classify` over fleet facts and bucketing each node by its resolved environment; verify a test asserts a node matching several groups of differing priority counts once, against the winning group's environment
- [x] 3.2 Add a reporting-count resolver bucketing nodes by `report_environment`; verify a test covers a node whose report environment belongs to no configured source
- [x] 3.3 Make both counts independently nullable so an openvoxdb failure degrades only them; verify a test with a failing openvoxdb asserts the response still carries remote, prefix and last-deploy, with counts reported unavailable rather than zero

## 4. HTTP API

- [x] 4.1 Add `GET /api/v1/code-repositories` behind `code:read`, returning name, redacted remote, prefix, environments, both counts, size and last deploy for every configured source; verify a handler test asserts the permission is enforced and that no secret or private key path appears in the response
- [x] 4.2 Ensure a configured-but-never-deployed source appears with absent rather than zero size/last-deploy; verify with a handler test
- [x] 4.3 Report environments belonging to no configured source as a separate total; verify a handler test asserts they are not folded into the unprefixed source's counts

## 5. Frontend

- [x] 5.1 Add a Code Repositories page and template listing each repository with its fields, rendering absent values distinctly from zero; verify against a console with two sources, one deployed and one not
- [x] 5.2 Surface the assigned/reporting divergence visibly (not two bare numbers side by side), so "deployed but nothing uses it" reads as a state rather than something to notice; verify against a source that has deployed with no nodes classified into it
- [x] 5.3 Add the nav entry alongside Deploys, and show the deployed environment on the Deploys page now that it is recorded; verify both pages render for a `code:read` user
- [x] 5.4 Rebuild the embedded bundle with `make frontend` and verify `internal/web/dist` reflects the change

## 6. Documentation

- [x] 6.1 Document the page in `README.md`: what the two node counts mean, why they are reported separately, and that size is content size measured at deploy time rather than disk usage
- [x] 6.2 Note that the remote is shown with credentials redacted, and that `code:read` therefore discloses each repository's host and path

## 7. End-to-end verification

- [x] 7.1 Run `go test ./...` and `make frontend` clean, accounting for the known pre-existing `TestNodeCoverage` failure
- [x] 7.2 Against the two-source fixture, deploy both, and verify the page reports each repository's remote, environments, size and last deploy, with counts reflecting the real node inventory
- [x] 7.3 Verify graceful degradation by pointing the console at an unreachable openvoxdb: the page must still render every repository with counts unavailable

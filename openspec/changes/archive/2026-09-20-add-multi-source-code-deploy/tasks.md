## 1. Source configuration model and loader

- [x] 1.1 Add a `Source` type to `internal/codemanager` (name, remote, prefix, private key path, webhook secret) with an `EffectivePrefix()` method following g10k's own resolution (`true` -> `<name>_`, a string -> `<string>_`, `false`/unset -> empty); verify with unit tests covering all three forms plus the unset case
- [x] 1.2 Add a YAML loader for the sources file, including `webhook_secret_file` reading the secret from disk the way `internal/runtime`'s existing `*_FILE` handling does; verify a table test parses a two-source file and resolves a secret from a temp file
- [x] 1.3 Add source-list validation: names must match `[a-z0-9_]+`, names must be unique, remotes must be non-empty, and effective prefixes must be pairwise distinct; verify each rule has a test asserting the error names the offending source
- [x] 1.4 Add `CodeSourcesPath` to `internal/runtime.Config` from `CONSOLE_CODE_SOURCES_PATH`, and make it a startup error when both it and `CONSOLE_CONTROL_REPO_URL` are set; verify with a config test asserting the error mentions both settings as mutually exclusive
- [x] 1.5 Resolve the configured form into one source list in `cmd/console/main.go` - the sources file, or `CONSOLE_CONTROL_REPO_URL` as an unprefixed source named `control`, or empty - and verify the console still starts with each of the three configurations

## 2. Deploy execution

- [x] 2.1 Change `Deployer` to hold the source list and look a source up by name, keeping `Configured()` false when the list is empty; verify existing `deploy_test.go` cases still pass against a single-source deployer
- [x] 2.2 Change `Deployer.Run` to take a source name plus branch, write a per-attempt g10k config containing only that source (with its prefix and private key), and keep the `-branch` invocation; verify a test asserts the generated config contains exactly one source and its prefix
- [x] 2.3 Derive the environment name as `EffectivePrefix() + branch` and use it for both the expected g10k output directory and the activation target; verify a test deploys a prefixed source against a real local git repo and asserts the environment directory is the prefixed name
- [x] 2.4 Return a clear error for a deploy naming an unconfigured source, without running g10k; verify with a unit test
- [x] 2.5 Add an isolation test: two sources each with a `production` branch, deploy one, and assert the other's live environment content is unchanged

## 3. Persistence

- [x] 3.1 Add a migration adding `deploys.source TEXT NOT NULL DEFAULT 'control'`, backfilling existing rows, with a matching down migration; verify `make up` plus the persistence migration test applies and reverts it cleanly
- [x] 3.2 Record source on `CreateDeploy` and return it from `GetDeploy`/`ListDeploys`; verify `store_test.go` asserts a round-tripped deploy carries its source
- [x] 3.3 Add a `Source` field to `DeployFilter` and filter on it, and add a `DistinctSources` query for the UI filter; verify tests cover filtering two sources' attempts apart, including same-ref attempts from different sources

## 4. HTTP API

- [x] 4.1 Accept an optional `source` on `POST /api/v1/code-deploys`, defaulting to the single/default source; verify handler tests cover the named, omitted, and unknown-source cases (the last returning 400, not 502)
- [x] 4.2 Add `POST /api/v1/code-deploys/webhook/{source}` verifying that source's own secret, keeping the unsuffixed route for the default source; verify tests cover a valid signature, a wrong-source signature, and an unknown source - the last two both rejecting with the same unauthorized response so the endpoint does not enumerate source names
- [x] 4.3 Add the `source` query filter to `GET /api/v1/code-deploys` and return `source` on each deploy; verify a handler test filters by source
- [x] 4.4 Add `GET /api/v1/code-deploys/sources` behind `code:read`, returning each configured source's name and effective prefix; verify a handler test asserts the permission is enforced and no secret or key path appears in the response

## 5. Events

- [x] 5.1 Add `Source` to `DeployedEvent` and to `PublishDeployed`'s signature; verify `event_test.go`'s real-subscriber test asserts the source arrives on the wire

## 6. Frontend

- [x] 6.1 Add a source column to the deploys list and a source filter populated from `/api/v1/code-deploys/sources`; verify the page renders both sources' attempts distinctly against a console with two sources configured
- [x] 6.2 Add a source picker to the manual deploy control, hidden when only one source is configured so the single-source UI is unchanged; verify by loading the page under both configurations
- [x] 6.3 Rebuild the embedded bundle with `make frontend` and verify `internal/web/dist` reflects the change

## 7. Documentation

- [x] 7.1 Document the sources file format, the mutual exclusivity with `CONSOLE_CONTROL_REPO_URL`, and the per-source webhook URL in `README.md` and `SETUP.md`; verify by following the written steps to configure a second source from scratch
- [x] 7.2 Note in the operator docs that adding a prefixed source deploys successfully but affects no node until nodes are classified into the new environment name, and that removing a source leaves its environments live on disk

## 8. End-to-end verification

- [x] 8.1 Run `go test ./...` and `make frontend` clean
- [x] 8.2 Against two real local control repo fixtures (extend `make control-repo-fixture` or add a second), configure both sources, deploy each, and verify both environments are live simultaneously, each deploy is recorded against its own source, and each source's webhook triggers only its own deploy
- [x] 8.3 Verify the single-source upgrade path: start with only `CONSOLE_CONTROL_REPO_URL` set against a pre-migration database, migrate, deploy, and confirm the environment path and deploy history are unchanged from before

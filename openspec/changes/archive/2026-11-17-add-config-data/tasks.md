## 1. Model, storage and merge

- [x] 1.1 Migration 000036: `node_group_config_data (group_id, key, value jsonb)`, unique pair, cascading; down migration. Verify with a migration test (up, down, up) that an existing group has no data
- [x] 1.2 `classifier.Group.ConfigData`, validation (key rule, `lookup_options`, 1000 keys, 64 KiB values) and the store: saved with the group, replaced when non-nil, kept when nil, cleared when empty. Verify with store and validation tests for each scenario under "A node group carries configuration data"
- [x] 1.3 `Classification.ConfigData` and the fold in `Tree.Classify`. Verify with table tests for every scenario under "A node's data is its groups' data merged", including a map replaced whole and a node matching no data
- [x] 1.4 Handlers: `configData` on create, update and read (a pointer on the request so absent differs from `{}`), `400` with `fields.configData` naming the key. Verify with handler tests, including that an update that omits it keeps the stored data

## 2. Serving and audit

- [x] 2.1 `GET /api/v1/enc/{certname}/data` in `encapi`, `enc:read`, sharing the group and fact fetch with the classification route. Verify with handler tests for each scenario in the `enc-api` delta
- [x] 2.2 Audit and activity: keys added, changed and removed on a group update, never a value. Verify with a test that sets distinct sentinel values and asserts none appears in the recorded entry
- [x] 2.3 Wiring in `internal/app` and the node-scope sweep exemptions for the new route. Verify by the sweep test passing and an end-to-end cluster test that a token with `enc:read` gets a node's merged data and one without gets `403`

## 3. openvoxserver delivery

- [x] 3.1 `openvoxserver-init/console_config_data.rb` (certname from trusted facts, the data route, token from `ENC_BRIDGE_TOKEN_FILE`, 5 second timeout, raises naming the console on failure) and `30-configure-config-data.sh` (glob for the gem directory, copy, write or leave the global `hiera.yaml` by the three rules in design 6, idempotent); `Dockerfile.openvoxserver` copies both. Verify by running the script against scratch directories for each scenario under "The image wires itself up without overriding an operator", including a second run changing nothing
- [x] 3.2 Rebuild the image and start it on the dev stack. Verify the function is installed and `hiera.yaml` written after a start and after a restart

## 4. Console

- [x] 4.1 Group page: the Configuration data JSON editor beside parameters, field error from the server, opened when populated, the "not for secrets" note. Verify in the browser
- [x] 4.2 Every new string in all 15 locales (`go run ./i18n extract|merge|status`); lint passes. Verify locale status at 100% and `node --test`

## 5. Docs and live verification

- [x] 5.1 `marketing/guides/use/classify.md` (setting data, merge order, precedence over the control repo, not for secrets), `operations.md` (storage, route, image script, the function's location, an operator's own `hiera.yaml`), and the install guides that describe the image; `make marketing-build` passes
- [x] 5.2 Live verification on the dev stack with a real agent catalog run: a key set only in the console, a key set in both (console wins), a key only in the control repo (falls through), two groups with different priorities, and the console stopped (compile fails naming it). Screenshots of the group page in `evidence/`; test data and any changes to the dev server removed afterwards
- [x] 5.3 Full suite, vet, gofmt, frontend tests and `openspec validate add-config-data --strict` pass

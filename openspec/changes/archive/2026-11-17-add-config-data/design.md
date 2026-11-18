## Context

The classifier merges the groups matching a node (`Tree.Matching`, ordered
by `mergeOrder`: rank, then depth, then unique priority) and `Tree.Classify`
folds their classes, parameters and environment in that order, later groups
winning. `encapi` serves the result at `GET /api/v1/enc/{certname}`,
which `cmd/enc-bridge` turns into the exec-terminus YAML. Group updates
replace the whole group: `UpdateGroup` deletes and reinserts classes,
parameters, rule and pins.

The ENC can only give classes parameters and set top-scope variables. A
profile that calls `lookup('ntp::servers')` reads Hiera, so the only way
to feed it from the console is a Hiera backend.

A spike on the dev openvoxserver (8.29) established the following, and the
design rests on it:

- A global `/etc/puppetlabs/puppet/hiera.yaml` with one hierarchy entry
  `data_hash: console_config_data` is consulted before the environment's
  Hiera; the function got the certname from
  `closure_scope['trusted']['certname']`.
- A key present in both the console's answer and `common.yaml` returned the
  console's value; a key only in the control repo fell through.
- The server found the function only in the openvox gem's own
  `lib/puppet/functions` directory
  (`/opt/puppetlabs/server/data/puppetserver/jruby-gems/gems/openvox-<ver>/`).
  It did not find it in `vendor_ruby`, the plugin cache lib
  (`/opt/puppetlabs/puppet/cache/lib`) or the server var dir's `lib`.
- The container's own `puppet lookup` CLI does not exercise this path (it
  has no puppetdb terminus and a different load path); a real agent run
  does, and is how it must be verified.

## Goals / Non-Goals

**Goals:**

- Per-group data, readable by `lookup()`, that wins over control-repo
  Hiera and falls through to it.
- Merge exactly as class parameters merge, so there is one rule to learn.
- A deployment that follows the image gets it with no manual steps, and an
  operator's own `hiera.yaml` is never overwritten.

**Non-Goals:** secrets, `lookup_options`/merge strategies, deep merge,
editing the control repo, override warnings for this data.

## Decisions

### 1. Storage: a table beside the parameters, replaced only when sent

`node_group_config_data (group_id, key, value jsonb)`, unique on
`(group_id, key)`, cascading, mirroring `node_group_parameters`. `Group`
gains `ConfigData map[string]any` (`configData`). Because `UpdateGroup` is a
full replace, `ConfigData == nil` (field absent from the request) means
"keep what is stored" and an empty non-nil map clears it; the handler
decodes into a pointer to tell them apart, and a response always carries an
object. This is the same fail-safe as a role's `taskPatterns`: an older
client must not wipe data it cannot see.

### 2. Validation lives in the classifier package

Key rule `^[A-Za-z0-9_:.\-]{1,255}$`, `lookup_options` refused (it would
silently become a Hiera merge directive, which is out of scope), at most
1000 keys, each value at most 64 KiB once JSON-encoded. Errors name the
field `configData` and the key, like `taskPatterns`.

### 3. Merge: top-level key replace, in mergeOrder

`Classification` gains `ConfigData map[string]any`; `Tree.Classify` folds
each matching group's data in order, so a later (winning) group replaces a
key whole. No deep merge: Hiera's own default for a `data_hash` provider is
first-found with no merge, and replacing whole is the only behaviour a
reader can predict from the page.

### 4. Serving: a second route, not a bigger ENC answer

`GET /api/v1/enc/{certname}/data` returns the merged map. It shares the
group and fact fetch with the classification route, and the same `enc:read`
permission. It is a separate route because `enc-bridge` must keep emitting
only what the exec terminus accepts, and because Hiera calls it at a
different moment (during lookup) than the terminus (before compile).

*Alternative.* Put data in the ENC `parameters`, read by a Hiera
`lookup_key` against top-scope variables. Rejected: it leaks every key into
every class as a top-scope variable and cannot carry non-string keys
cleanly.

### 5. The function runs enc-bridge, once per compile, and fails closed

`console_config_data(options, context)` takes the certname from the trusted
facts and runs `enc-bridge --data <certname>` (a new mode of the program the
ENC already runs, found through puppet.conf's `external_nodes`, which prints
the data route's answer as one JSON object). Hiera calls a `data_hash` once
per lookup invocation, so this is one run per compile. Any failure raises,
failing the compile with a message naming the console: an empty answer
would compile a catalog with the wrong values and apply it.

*Why not call the console from Ruby.* The first version did, reading
`ENC_BRIDGE_URL` and the token from the environment. The live run showed
openvoxserver does not give its own environment to Ruby code (the call went
to the default `localhost:8080`), though it does to the programs it runs.
Running enc-bridge reuses its address and token handling and needs no
server configuration.

### 6. Wiring: a boot script, in the image's existing pattern

`openvoxserver-init/30-configure-config-data.sh` runs on every start (the
gem directory and `hiera.yaml` are not preserved by a recreate; they are by
a restart, and the script is idempotent either way):

1. Find the gem directory by glob
   (`jruby-gems/gems/openvox-*/lib/puppet/functions`, falling back to
   `puppet-*`), so a version bump does not break it, and copy the function
   in. If none is found, log an error and carry on: the server must
   still start, and the first compile then fails with an "unable to find"
   message that the log explains.
2. If `/etc/puppetlabs/puppet/hiera.yaml` does not exist, write it with the
   console entry and a first-line marker comment
   `# Managed by OpenVox Console`.
3. If it exists with that marker, rewrite it (so a new version of the entry
   propagates). If it exists without the marker and without the string
   `console_config_data`, leave it and log the entry to add. If it names
   `console_config_data` already, leave it.

The function file ships in the repo (`openvoxserver-init/console_config_data.rb`)
and is copied into the image by `Dockerfile.openvoxserver`.

### 7. Precedence is the global layer's, and stated

The global layer is the first consulted, so console data beats the
environment's. That matches PE and is what the spike showed. The guide says
so and says the control repo remains the fallback.

### 8. Audit by key name

The existing group-update audit and activity entries gain the lists of
config keys added, changed and removed. Values are never written, because
people put credentials where they should not and the audit log is wider
read than the groups.

### 9. Page: a JSON object field, like parameters

A second JSON textarea beside the parameters one on the group page, using
the same `parseJSONField` and open-if-populated disclosure, and the field
error shown on it from the server's `fields.configData`. A key/value grid
was considered; values are arbitrary JSON, so a typed grid is a larger
build than this week warrants and the JSON field is what the parameters
editor already does.

## Risks / Trade-offs

- **The function needs a new enc-bridge.** `--data` does not exist in older
  builds. → The image builds both together; the install guide says to
  rebuild and reinstall enc-bridge.
- **The function's location is an implementation detail of the openvox
  gem.** → The glob, a loud log line, and a live check in the verification;
  the guide names the path so an operator can place it by hand.
- **An operator's own global `hiera.yaml`** can't be edited safely. → Left
  alone, with the exact entry logged and documented.
- **Console outage fails compiles.** → Intentional and consistent with the
  ENC, which already needs the console for every compile.
- **Clear-text data visible to `classifier:read`.** → Said on the page and
  in the guide; sensitive values are a follow-up.
- **A key shadowing control-repo data surprises people.** → The group page
  is where it is set; precedence is documented, and a future change could
  warn on shadowing.

## Migration Plan

Migration 000036 creates the table; down drops it. No data moves. Roll
back by running it down; an older binary ignores the table. Deployments
rebuild the openvoxserver image to get the boot script; until then Hiera
behaves exactly as before.

## Implementation Notes

What differs from the design above, or was decided while building it.

- **The function runs `enc-bridge --data`** instead of calling the console
  itself (decision 5 is rewritten): found live, see
  `evidence/live-verification.md`. The `ENC_BRIDGE_*` variables are read by
  enc-bridge, not the function.
- **The boot-script glob needs a version** (`openvox-[0-9]*`,
  `puppet-[0-9]*`), since the same directory holds `puppet-resource_api-*`.
- **Audit** records `configDataKeys` for create, update and delete, and
  `configDataChange` (added, changed, removed key names) on an update, through
  an `auditedGroup` wrapper that hides `ConfigData`; a test sets distinct
  sentinel values and asserts none appears.
- **The console-down case** shows the ENC's own failure when the whole console
  is stopped, since the ENC runs first; the function's failure was seen end to
  end in a compile during the run and is covered by `enc-bridge --data`'s
  tests (an unreachable console and a refusal print nothing and exit 1).
- **`openvoxserver-init/config_data_script_test.go`** is a test-only Go
  package that runs the boot script against scratch directories, so the script
  is tested without Docker.

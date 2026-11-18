## Why

A node group can set classes, class parameters and top-scope variables,
but not data that code reads with `lookup()`. A profile that asks for
`ntp::servers` or `profile::dns::resolvers` has to find the answer in the
control repo's Hiera data, so changing one value for one group of nodes
means a commit, a review and a code deploy. Puppet Enterprise lets an
administrator set such data on a node group, and the console's value wins
over Hiera's. This is week 9 of the Puppet Enterprise parity plan.

A spike on the dev stack showed the way: a global-layer `data_hash`
backend in Hiera. A one-entry global `hiera.yaml` calling a small Ruby
function resolved console-supplied keys for the right certname, the
console's value won over a control-repo value for the same key, and keys
the console did not set fell through to the control repo. The ENC cannot
do this: it only sets parameters on classes a node already declares, not
arbitrary keys.

## What Changes

- **A node group carries configuration data**: a map of key to value, a
  value being any JSON value (string, number, boolean, list, map, null).
- **A node's data is its groups' data merged** by the rule that already
  merges class parameters: a descendant beats its ancestors, and priority
  decides otherwise. A key from a higher-ranked group replaces the same
  key from a lower one whole; maps are not deep-merged.
- **The console serves it**: `GET /api/v1/enc/{certname}/data` returns the
  node's merged data, for the same `enc:read` token the ENC uses.
- **openvoxserver reads it through Hiera.** The openvoxserver image gets a
  boot script that installs a `console_config_data` function where the
  server can find it and adds a global `hiera.yaml` entry for it. Console
  data is consulted first, so it wins over control-repo Hiera, as in PE;
  a key it does not set falls through to the control repo.
- **The node group page gets a Configuration data editor**, with the data
  validated on save and shown with the rest of the group.
- **Changes are audited** by key name, never value.
- **Docs**: the classify guide and operations, including how to set it up
  when an operator already has a global `hiera.yaml`.

## Capabilities

### New Capabilities

- `config-data`: what configuration data is, how groups' data merges,
  validation, the page, audit, and how openvoxserver is wired to read it.

### Modified Capabilities

- `enc-api`: serves a node's merged configuration data.

## Impact

- **Code:** `internal/classifier` (group field, store, merge in `Tree`),
  `internal/encapi` (the data route), `internal/app` (wiring).
- **Image and scripts:** `Dockerfile.openvoxserver`, a new
  `openvoxserver-init/` boot script and the Ruby function it installs.
- **Database:** migration 000036 adds `node_group_config_data`.
- **API:** `configData` on groups (read and write); the new `enc` data
  route. No existing field changes meaning.
- **Frontend:** the node group page; every new string in all 15 languages.
- **Site guides:** `use/classify.md`, `operations.md` and the install
  guides that describe the openvoxserver image.
- **Compatibility:** nothing changes for a group with no data, and a
  group saved by a client that does not send `configData` keeps what it
  has. An installation not using the image's boot script is unaffected
  until it adds the Hiera entry itself.
- **Security note:** data is stored and shown in the clear and is
  readable by anyone with `classifier:read`; it is not for secrets.
  Sensitive values are out of scope.
- **Out of scope:** sensitive or encrypted values, per-key `lookup_options`
  and merge strategies (a `lookup_options` key is refused), editing the
  control repo's `hiera.yaml`, override warnings for config data, patch,
  agentless targets, code-based plans.

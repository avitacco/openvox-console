# Live verification on the dev stack

Run 2026-11-17 on the dev stack: the real openvoxserver (8.29.0, recreated
with the new boot script and `enc-bridge`), one connected agent
(`task-e2e-agent`) and the console. Every result below is from a real
`puppet agent -t --noop` catalog compile, because `puppet lookup` on the
server does not exercise the global Hiera layer (no puppetdb terminus, a
different Ruby load path). The test groups, and the control-repo data and
manifest added for the run, were removed afterwards.

## Setup

- Two groups pinning the agent: `cfg9-low` (priority 930) and `cfg9-high`
  (priority 931), each with configuration data.
- Control repo (the dev environment): `hiera.yaml` with a `common.yaml`
  holding `cfg9::both`, `cfg9::repo_only` and `cfg9::console_only`.
- A manifest looking up six keys with `lookup()` and printing each.

## What the agent compiled

| Key | Set in | Result |
| --- | --- | --- |
| `cfg9::group_key` | low group only | `from the low group` |
| `cfg9::both` | low group, high group and the control repo | `high`: the higher priority wins, and the console beats `control-repo` |
| `cfg9::map` | low `{a:1, b:2}`, high `{a:9}` | `{'a' => 9}`: replaced whole, not merged |
| `cfg9::console_only` | high group (a list) and the control repo | `['x', 'y']`: the console wins, with the list typed as a list |
| `cfg9::repo_only` | control repo only | `from the control repo`: falls through |
| `cfg9::nowhere` | nowhere | `NOT FOUND` (the lookup's default) |

## Delivery

- After the container was **recreated** the boot script had written the
  global `hiera.yaml` and copied the function into
  `.../jruby-gems/gems/openvox-8.29.0/lib/puppet/functions/`; after a plain
  **restart** the file was unchanged (same modification time).
- `Dockerfile.openvoxserver` builds, and the script run inside the built image
  finds the gem directory, installs the function and writes `hiera.yaml`; a
  second run prints nothing.
- On the server, `enc-bridge --data task-e2e-agent.example.com` prints the
  merged data as one JSON object. Against an unreachable console it exits 1
  with `enc-bridge: request configuration data from http://127.0.0.1:1: ...
  connection refused` and prints nothing to stdout.

## A console that cannot answer

- With the console stopped, the compile fails and the agent reports it; the
  failure is the ENC's (`Failed to find ... via exec: Execution of
  '/usr/local/bin/enc-bridge ...' returned 1`), which runs first, exactly as
  it did before this change.
- The function's own failure was seen end to end during the run, before the
  function was changed to use `enc-bridge` (see "Things found"): a compile
  that could not reach the console failed with `Server Error: Evaluation
  Error: ... console_config_data: the console at http://localhost:8080 did not
  answer for task-e2e-agent.example.com: Connection refused`, and no catalog
  was built.

## Console

- `group-config-data.png`: the group page with the Configuration data editor
  opened because the group has data.
- `group-config-data-refused.png`: saving `{"has space": 1}` is refused and the
  server's reason is shown on the field, with the disclosure opened.
- Saving `[1, 2]` is refused on the field before any request: "Configuration
  data must be a JSON object: key/value pairs inside { }."

## Things found

- **The function could not read the console's address or token.** The first
  version called the console itself using `ENC_BRIDGE_URL` and the token from
  the server's environment. The compile failed trying `localhost:8080`:
  openvoxserver does not give its own environment to Ruby code, only to the
  programs it runs. The function now runs `enc-bridge --data <certname>`,
  found through `puppet.conf`'s `external_nodes`, so the address and token are
  the same ones classification uses. This changed the design (decision 5),
  the function, `enc-bridge` and the install guide.
- The first live run also confirmed the glob must not match unrelated gems:
  `puppet-resource_api-2.0.1` is in the same directory, so the glob now
  requires a version number after `openvox-` or `puppet-`.

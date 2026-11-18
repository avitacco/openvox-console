# Proposal

## Why

A node's node-agent-client is only ever replaced when someone re-runs the
install script on that node, so clients silently fall behind the console.
A production node running a client from before the package-inventory fix
kept its old fact script, reported removed kernel packages as installed,
and showed 6,817 vulnerabilities on a patched Ubuntu 26.04 host - and
nothing in the console said the client was out of date. Fixes shipped in
the client do not reach the fleet today, and the console cannot tell.

## What Changes

- The client reports its build identity (a digest of its own executable,
  its build time, version, platform and how it was installed) when the
  console asks.
- The console asks every node for that identity when it connects, records
  it, and treats a node as out of date when its client is older than the
  client build the console embeds.
- When automatic upgrades are enabled (the default), the console tells an
  out-of-date client to upgrade. The client downloads the artifact for
  its platform from the console, verifies it against the digest it was
  given over the mutually authenticated node transport, installs it
  through the platform's own mechanism (dpkg/rpm on Linux, in-place
  replacement on Windows and macOS), and restarts under its service
  manager.
- An upgrade never interrupts a run or task in progress, is confirmed by
  the build the client reports after it reconnects, and backs off after a
  failure instead of retrying in a loop.
- Operators can turn automatic upgrades off for the whole console, or
  exempt a single node from its own configuration, and can start or retry
  an upgrade of one node by hand.
- The node list, the node page and the fleet status tab show each node's
  client build, whether it is out of date, and the last upgrade failure.
- Linux package versions become monotonic in build time, so re-running
  the install script (or `ensure => latest` from Puppet) upgrades an
  existing client. Today's `0.0.0+<git sha>` versions do not sort, which
  is why a package-manager upgrade never picked up a newer client. This
  is the one-time path for clients that predate this change and cannot
  upgrade themselves.

Nothing here is breaking: a client that predates this change keeps
working and is simply reported as out of date with an unknown build.

## Capabilities

### New Capabilities
- `agent-upgrade`: the console keeping node-agent-client current across
  the fleet - knowing each node's client build, deciding when it is out
  of date, driving and confirming upgrades, handling failure, the
  operator controls, and what the console shows about it.

### Modified Capabilities
- `node-agent`: adds reporting the client's build identity and carrying
  out a verified upgrade request.
- `agent-distribution`: adds serving the upgrade artifact for every
  supported platform and package versions that order by build time.
- `node-connectivity`: adds the client build and out-of-date state to the
  node status the console reports and shows.

## Impact

- `cmd/node-agent-client`, `internal/nodeagent`: build stamping, two new
  request actions, download/verify/install per platform. Builds on the
  uncommitted `add-task-execution` work in the same package, which must
  land first.
- `internal/agentdist`, `cmd/build-agent-packages`, `Makefile`,
  `Dockerfile`: artifact digests and build time exposed to the console,
  monotonic package versions, stamped client builds.
- New console package for the upgrade coordinator, a migration for the
  per-node build and upgrade state, a worker registered in the run-mode
  surface, and a configuration switch in `internal/runtime`.
- `internal/nodeconnectivity`, node/nodes/status frontend pages and all
  locale catalogs.
- Node-facing API: `GET /packages/node-agent-client` semantics unchanged;
  package versions served by the apt/yum repo change format.
- Guides and `operations.md`: upgrading clients, the one-time manual
  upgrade for existing nodes, and turning the feature off.

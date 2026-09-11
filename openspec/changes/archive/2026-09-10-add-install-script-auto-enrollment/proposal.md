## Why

The install script assumes a node already holds a signed Puppet
certificate. When it doesn't - the common case for a brand-new node -
all three scripts print "Run '<puppet> agent -t' first to enroll this
node, then re-run this script" and exit non-zero. Enrolling a node is
exactly what an install script should be doing for the operator, not
handing back as homework, and the failure lands *after* the script has
already installed openvoxagent, so the operator is left half-installed
and told to go finish it by hand.

Removing the check alone doesn't work: nothing in the install path ever
tells the node which Puppet server to enroll with. `agentdist.Config`
carries only the console's base URL and the node transport address, and
the console's own CA URL is its private view of the CA API (its dev
value is `https://localhost:8140`), meaningless to a remote node. So a
node-facing server address has to exist before the script can enroll
anyone.

## What Changes

- The install script enrolls a node that has no certificate: it submits
  a certificate signing request and waits a bounded time for the
  request to be signed, then continues the rest of the install in the
  same run.
- Add a node-facing Puppet server address to the console's
  configuration, passed through to the generated script, mirroring the
  existing node-facing node-transport address. The script points the
  agent at it before enrolling. When it isn't configured, the script
  leaves whatever server the node already has configured alone rather
  than guessing at one.
- When signing doesn't happen inside the wait window, the script stops
  with a message naming this console's Nodes page as the place to sign
  the request - the console already signs, revokes, and cleans
  certificates behind its own permission - rather than naming a
  CA command line the operator may have no access to.
- An enrollment failure that isn't "waiting to be signed" (CA
  unreachable, DNS failure, TLS mismatch) is reported as itself, not
  misreported as a signing timeout.
- A node that already has a signed certificate keeps skipping all of
  this, so re-running the script on a provisioned node stays a no-op.
- All three platforms (Linux, macOS, Windows). Only Linux is
  live-verifiable in this dev environment; macOS and Windows get the
  static-verification treatment this project already applies to them.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `agent-distribution`: the "Install script route" requirement gains
  certificate enrollment - today it describes installing openvoxagent,
  the node-agent artifact, service registration, and package-inventory
  reporting, but says nothing about how a node without a certificate
  gets one.

## Impact

- `internal/runtime/config.go`: new node-facing Puppet server address
  setting, alongside the existing node-facing transport address.
- `cmd/console/main.go`: pass it into `agentdist.Config`.
- `internal/agentdist/handlers.go`: carry it on `Config`.
- `internal/agentdist/install_script.go`,
  `install_script_macos.go`, `install_script_win.go`: replace the
  "no certificate, go run puppet yourself" exit with enrollment, in
  each platform's own idiom.
- `internal/agentdist/handlers_test.go`: coverage for the rendered
  scripts, including the unset-address case.
- `operations.md`: the new setting, and what an operator sees when a
  node's request is waiting to be signed.
- No change to node-agent-client, the node transport, or any API.

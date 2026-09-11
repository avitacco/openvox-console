## Why

The Nodes page shows every certname the OpenVox CA knows about, which
deliberately includes certs that were never signed by way of a real
Puppet run - necessary so a pending enrollment is visible and signable.
But this same union also surfaces certs that are never going to become
managed nodes at all: this console's own service-to-service TLS
identities (its openvoxdb client cert, its CA-client credential, its
node-transport server cert) and the servers it connects to (openvoxdb's
and openvoxserver's own host certs). These render identically to a
genuine pending node, with the same Revoke/Clean buttons - an operator
can't tell "safe to clean up" from "will break the console" without
already knowing this codebase's internal cert layout by heart.

## What Changes

- The console derives its own "infrastructure certnames" set at
  startup, with no new configuration: every certname it presents as a
  client (from its already-configured cert files) plus every certname
  presented to it by a server it connects to (captured from the real
  TLS peer certificate on that connection), each paired with a short,
  specific reason (e.g. "the certificate this console uses to connect
  to openvoxdb").
- `GET /api/v1/node-connectivity` reports, per node,
  `isInfrastructure`/`infrastructureReason` alongside its existing
  fields.
- The Nodes page hides infrastructure certs by default behind a new
  "Show infrastructure certs" filter checkbox, and - when shown - marks
  them distinctly so they're never visually confused with a real node
  even once revealed (see design.md; this wasn't a separately-confirmed
  requirement, but "toggle to reveal" without any visual distinction
  would just recreate the original confusion for anyone who turns the
  toggle on).
- The Revoke/Clean confirmation dialogs, when the target is an
  infrastructure cert, name the specific reason instead of the generic
  node-focused wording, so the real consequence is stated up front
  rather than discovered after the fact.

## Capabilities

### Modified Capabilities
- `node-connectivity`: the "Node connection status endpoint" and
  "Dedicated nodes page" requirements gain infrastructure-cert
  awareness.

## Impact

- **Code**: a new small package holding the peer/self certname-derivation
  helpers and the resulting infrastructure-certname set (see design.md
  for exactly where - existing clients' public APIs are not changed),
  `internal/nodeconnectivity` (new response fields), `cmd/console/main.go`
  (derive the set at startup from already-configured cert files:
  `CONSOLE_OPENVOXDB_CERT_FILE`, `CONSOLE_CA_CLIENT_CERT_FILE`,
  `CONSOLE_NODE_TRANSPORT_CERT_FILE`, and the already-configured
  `CONSOLE_OPENVOXDB_URL`/`CONSOLE_CA_CLIENT_URL` server addresses),
  `frontend/src/nodes.js` (filter checkbox, distinct styling, reason-
  aware confirmations).
- **APIs**: `GET /api/v1/node-connectivity` response gains two new
  per-node fields; existing fields and every other endpoint are
  unaffected.
- **Config**: none new - this deliberately reuses cert files the
  console already has configured, per the confirmed design decision
  (no `CONSOLE_INFRASTRUCTURE_CERTNAMES`-style list to maintain).
- **Known limitation, stated up front**: this can only identify
  infrastructure certs the console itself has a direct channel to learn
  about. It cannot detect openvoxserver's own certname unless
  `CONSOLE_CA_CLIENT_*` is configured (the only outbound connection this
  console makes to openvoxserver), and it cannot detect a *retired*
  identity nothing connects to/as anymore by this mechanism alone -
  `node-transport` happens to be caught only because its cert file is still
  reused as the node-transport's own server cert today (see
  operations.md), not because the console has any notion of "formerly
  used for the node transport." This is recorded in the spec as an explicit
  scenario, not silently glossed over.

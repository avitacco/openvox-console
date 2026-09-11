## Why

The Nodes page (`add-nodes-page`) shows only a live connected/not-connected
boolean per node. Two follow-up gaps came up immediately after using it:
there's no way to tell *when* a node last connected or disconnected (useful
for "has this been down for five minutes or five days"), and no way to see
a node's Puppet certificate status (signed/requested/revoked) - directly
useful for diagnosing exactly the kind of enrollment problem debugged
live earlier in this project (a node that can't connect because its
certificate was never signed, versus one that's signed but the transport
connection itself is failing).

## What Changes

- Track a last-connected and last-disconnected timestamp per node in
  `internal/nodetransport.Registry`, and surface both on the existing
  node connection status endpoint.
- Add certificate status (signed/requested/revoked/unknown) per node,
  sourced from openvoxserver's real Puppet Server CA API
  (`GET /puppet-ca/v1/certificate_statuses`).
- **Security-relevant**: querying that API requires a client certificate
  carrying the `pp_cli_auth` extension - openvoxserver's `auth.conf` gates
  it that way, not a role this project controls. Minting one
  (`puppetserver ca generate --ca-client`) grants **full CA admin
  access** (sign/revoke/clean any node's certificate), not read-only cert
  status specifically - there is no narrower built-in grant. The console
  will hold this credential to use it read-only, but the credential
  itself is not read-only. This is a real, accepted trade-off (the user
  explicitly chose to proceed after being shown the alternative of
  skipping cert status) - see design.md for how it's isolated and
  documented, not just used inline with existing credentials.
- Add "Last connected" and "Cert status" columns to the existing Nodes
  page.

## Capabilities

### Modified Capabilities
- `node-connectivity` (delta-only today - see `add-nodes-page`, not yet
  archived/synced to a main spec; this change's delta targets the same
  capability and should sync together with or after it): "Node
  connection status endpoint" gains timestamp reporting; "Dedicated
  nodes page" gains the two new columns; a new "Certificate status
  reporting" requirement is added.

## Impact

- **Code**: `internal/nodetransport.Registry` gains timestamp tracking
  alongside its existing boolean state. A new small package (or an
  addition to `internal/nodeconnectivity` - see design.md) calls
  openvoxserver's CA API and exposes cert status via the existing
  `GET /api/v1/node-connectivity` response (or an additive field on it).
  `frontend/src/nodes.js` renders the two new columns.
- **Config**: a new credential (CA-client cert/key, separate from every
  other cert this project uses) and its file paths, config-gated like
  every other optional integration this project has (unset = the
  feature degrades to "unknown" per node, not a startup failure).
- **Operational**: openvoxserver must be stopped briefly to mint the new
  CA-client certificate (`puppetserver ca generate --ca-client` requires
  the CA service to not be running) - a one-time, documented operational
  step, not a code concern.
- **No breaking change** - purely additive: new fields on an existing
  response, two new (optional-to-populate) UI columns.

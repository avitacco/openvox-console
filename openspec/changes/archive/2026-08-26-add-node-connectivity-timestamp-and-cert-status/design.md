## Context

Confirmed by direct investigation, not assumption:

- `internal/nodetransport/registry.go`'s `Registry` currently stores only
  `connected map[string]bool`, fed by `$SYS.ACCOUNT.*.CONNECT`/
  `.DISCONNECT` events (see `markConnected`/`markDisconnected`). No
  timestamp of any kind is recorded today.
- openvoxserver's real CA API (verified live against the actual running
  container): `GET /puppet-ca/v1/certificate_statuses/<ignored>` (the
  plural, bulk-listing form) returns a JSON array of `{name, state,
  not_before, not_after, fingerprint, ...}`, `state` being `"signed"`,
  `"requested"`, or `"revoked"`. This requires a client certificate
  presenting the `pp_cli_auth: true` authorization extension - confirmed
  in `auth.conf` (`"puppetlabs cert statuses"` rule, `"allow": {
  "extensions": { "pp_cli_auth": "true" } }`).
- `puppetserver ca generate --ca-client` (the only way to mint such a
  cert) requires Puppet Server to not be running at generation time, and
  its own `--help` text states plainly: "this can be used for
  regenerating the server's host cert, or for manually setting up other
  nodes to be CA clients... do not distribute certs generated this way
  to any node that you do not intend to have administrative access to
  the CA (e.g. the ability to sign a cert)." There is no narrower grant
  in openvoxserver/Puppet Server - `pp_cli_auth` is all-or-nothing CA
  admin, not a read-only cert-status role. The user was shown this
  trade-off directly (including the alternative of skipping this
  feature, or first investigating a narrower `auth.conf` carve-out) and
  chose to proceed anyway.
- `internal/nodeconnectivity` (from `add-nodes-page`) already has
  `Registry` as a narrow interface (`Certnames() []string`) and a single
  handler. Every other external-service client in this project
  (`internal/openvoxdb`, ENC-bridge-facing code) lives in its own
  package with its own cert/key/CA config triple, matching
  `internal/runtime.Config`'s existing pattern (`OpenvoxdbCertFile` etc.)
  - not folded into the handler package that serves it.

## Goals / Non-Goals

**Goals:**
- Make the CA-client credential's blast radius as small and as visible
  as possible, given it cannot be made narrow at the protocol level -
  this is the central design constraint of this change, not a detail.
- Both new pieces of information (timestamp, cert status) degrade to
  "unknown"/absent rather than erroring when not available (no history
  yet; CA integration not configured) - matching every other optional
  integration in this project (G10K, OIDC, the node transport itself).

**Non-Goals:**
- Any CA *write* action (signing, revoking, cleaning certs) from the
  console - this change is read-only use of a credential that happens to
  be capable of more; exposing sign/revoke/clean via the console UI is a
  separate, much larger RBAC/audit-logging-relevant decision, explicitly
  out of scope here.
- Persisting connection-history beyond "most recent" - a full connect/
  disconnect log (for trend analysis, uptime percentage, ...) is a
  different, larger feature; this change only adds the two most recent
  timestamps, matching the proposal's stated scope.
- Automating the CA-client cert's generation - it's a one-time, manual,
  documented operational step (openvoxserver must be stopped), not
  something the console or its build tooling does for the operator.

## Decisions

**New package `internal/certstatus`, not folded into
`internal/nodeconnectivity`.** Mirrors `internal/openvoxdb`'s existing
shape (its own HTTP client, its own cert/key/CA config) - the CA-client
credential's unusually high privilege is exactly the reason to keep it
in the smallest, most clearly-named, most obviously-privileged package
possible, not mixed into the handler package that happens to serve it
over HTTP. `nodeconnectivity.Handlers` depends on a narrow
`CertStatusClient` interface (`Status(ctx, certname) (string, error)`),
the same "small interface, real implementation elsewhere" pattern this
project already uses throughout (`inventory.queryClient`,
`orchestrator.Transport`, ...).

**New, entirely separate config vars
(`CONSOLE_CA_CLIENT_CERT_FILE`/`_KEY_FILE`/`_URL`), not reusing any
existing cert/key pair.** Every other credential this project holds
(openvoxdb client cert, node-transport server cert, RBAC signing key)
has a bounded, specific purpose; deliberately giving the CA-client
credential its own dedicated, clearly-named config keys - rather than,
say, defaulting it to the same cert used for openvoxdb queries - makes
its presence (and its elevated privilege) obvious in any operator's
environment file or secrets manager, not an accidental side effect of
reusing something already there. Unset (the default) disables
certificate status reporting entirely - every node reports `"unknown"` -
matching this project's established "optional integration, never fatal
at startup" posture.

**Registry timestamps are wall-clock `time.Time`, captured at the
moment `markConnected`/`markDisconnected` run - not derived from the
NATS event payload's own timestamp.** Simpler, and the discrepancy
(NATS event processing delay) is sub-millisecond in practice; no
scenario in the spec requires sub-second precision. Alternative
considered: parse `ClientInfo.Start`/`Stop` from the `$SYS` event
payload itself (`server.ConnectEventMsg`/`DisconnectEventMsg`, already
partially decoded in `registry.go`'s `systemEvent` struct) - rejected as
unnecessary precision for no behavioral benefit, adding a field to a
struct that's currently deliberately minimal.

**Bulk-fetch certificate statuses once per node-connectivity request,
not one HTTP call per node.** openvoxserver's own API is already
bulk-shaped (`certificate_statuses`, plural, returns every cert in one
call) - fetching in bulk and building a `map[certname]string` avoids an
N+1 pattern the upstream API doesn't even require.

## Risks / Trade-offs

- **[Risk]** The CA-client credential, if the console itself were
  compromised, gives an attacker the ability to revoke or sign
  certificates fleet-wide - a materially larger blast radius than any
  credential this project has held before. **Mitigation:** this is
  explicitly accepted by the user with the alternative (skip the
  feature) presented first; documented prominently in `operations.md`
  with the exact privilege named, not buried in a config comment;
  scoped to read-only *use* even though the credential itself permits
  more (see Non-Goals) - a future change adding sign/revoke UI would be
  a new, separately-considered privilege escalation, not an incremental
  extension of this one.
- **[Risk]** Minting the CA-client cert requires stopping openvoxserver,
  a real (if brief) production availability impact for whoever performs
  this one-time setup step. **Mitigation:** documented as an explicit,
  planned maintenance action in `operations.md`, not something to
  discover by surprise.
- **[Trade-off]** Certificate status and connectivity are fetched from
  two different systems (openvoxserver's CA vs. this instance's own
  in-memory `Registry`) with no transactional relationship between them
  - a node's cert status and its connectivity are reported as of two
  slightly different moments. Accepted: both are "as of last refresh"
  values already (see `add-nodes-page`'s own accepted non-live-updating
  trade-off), so this adds no new inconsistency class, just widens an
  existing one slightly.

## Migration Plan

- No database migration - both new pieces of state are either in-memory
  (`Registry` timestamps) or fetched live from an external API (cert
  status), nothing new is persisted.
- Operator setup (one-time, manual, only if this feature is wanted):
  stop openvoxserver, run `puppetserver ca generate --ca-client
  --certname <name>`, restart openvoxserver, configure
  `CONSOLE_CA_CLIENT_CERT_FILE`/`_KEY_FILE`/`_URL` on the console,
  restart the console. Fully documented as a runbook in `operations.md`,
  including the privilege warning from Context above.
- Purely additive and optional otherwise: unset CA-client config means
  cert status reports `"unknown"` for every node; no existing behavior
  changes.

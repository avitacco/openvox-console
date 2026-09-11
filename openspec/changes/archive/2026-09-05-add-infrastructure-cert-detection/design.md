## Context

The Nodes page currently unions two certname sources - openvoxdb
inventory and `internal/nodeconnectivity`'s connectivity/cert-status
data - with no notion that some of those certnames are the console's
own infrastructure rather than managed nodes. See proposal.md for which
certnames this actually affects in a real deployment of this project
(`console`, `console-ca-client`, `openvoxdb`, `openvoxserver`, and
incidentally `node-transport` today).

The console already has every ingredient needed to derive this without
new configuration: `internal/runtime.Config` already holds the cert
files it presents as a client (`OpenvoxdbCertFile`, `CAClientCertFile`,
`NodeTransportCertFile`) and the URLs of the servers it connects to
(`OpenvoxdbURL`, `CAClientURL`).

## Goals / Non-Goals

**Goals:**
- Zero new configuration - derive entirely from what's already
  configured.
- No changes to `internal/openvoxdb` or `internal/certstatus`'s public
  APIs - this is read-only introspection of connections those packages
  already establish in spirit, done alongside them, not through them.

**Non-Goals:**
- A general "is this certname trustworthy" or "is this certname
  malicious" judgment - this only answers "is this certname one of
  *this console's own* known infrastructure identities," nothing about
  any other cert's legitimacy.
- Detecting infrastructure identities this console has no channel to
  observe (see proposal.md's stated limitation - openvoxserver's own
  certname specifically requires `CONSOLE_CA_CLIENT_*` to be
  configured, since that's the only outbound connection this console
  makes to it today).

## Decisions

**New package `internal/infracert`**, not new methods on
`internal/openvoxdb.Client`/`internal/certstatus.Client`. Two kinds of
derivation, both stateless helper functions:

```go
// SelfCertname parses the leaf certificate at certFile and returns its
// Subject Common Name - the certname this console presents as a client
// when using that cert file.
func SelfCertname(certFile string) (string, error)

// PeerCertname dials addr with TLS using the given client
// certificate/CA (mirroring how internal/openvoxdb.Client and
// internal/certstatus.Client each build their own tls.Config) and
// returns the CN of the certificate the server presents - the
// certname of the server this console connects to at addr.
func PeerCertname(ctx context.Context, addr, certFile, keyFile, caFile string) (string, error)
```

`PeerCertname` does a raw `tls.Dial`, not an application-level HTTP
request - it doesn't need openvoxdb's or openvoxserver's query/CA APIs
to succeed, just the TLS handshake, so it can't be broken by an
unrelated API-level issue and doesn't add any real application load.

A `Set` type in the same package holds the result:

```go
type Set struct {
	// certname -> reason, e.g. "the certificate this console uses to
	// connect to openvoxdb"
	reasons map[string]string
}

func (s *Set) Lookup(certname string) (reason string, ok bool)
```

Built once, in `cmd/console/main.go`, after the existing openvoxdb/
CA-client construction: three `SelfCertname` calls (openvoxdb cert,
CA-client cert if configured, node-transport cert) and up to two
`PeerCertname` calls (openvoxdb's server, openvoxserver's server if
CA-client is configured), each logged as a warning and skipped on
failure rather than failing console startup - matches this project's
established nil-safe degradation pattern (a console that can't
determine one infrastructure certname still serves everything else
correctly, it just won't flag that one row).

**Wired into `nodeconnectivity.Handlers` as a fourth narrow
dependency**, mirroring the existing `Registry`/`CertStatusClient`
pattern:

```go
type InfrastructureCertnames interface {
	Lookup(certname string) (reason string, ok bool)
}
```

`*infracert.Set` satisfies this directly - no adapter needed. A nil
value (never realistically nil in production once wired, but keeps the
existing nil-safe convention) means every certname reports
`isInfrastructure: false`.

**Response shape**: `nodeStatus` gains
`IsInfrastructure bool \`json:"isInfrastructure"\`` and
`InfrastructureReason string \`json:"infrastructureReason,omitempty"\``.

**Frontend**: a `<vox-switch id="show-infrastructure">` (unchecked by
default) alongside the existing filter row. `filteredCertnames()` drops
any certname with `isInfrastructure: true` unless that checkbox is
checked. When shown, an infrastructure row renders with a small
`<vox-badge variant="neutral">Infrastructure</vox-badge>` next to the
name (this specific visual-distinction detail wasn't a separately
confirmed option - see proposal.md's note on why it's included
anyway: revealing infrastructure certs with literally no visual
difference from managed nodes would just recreate the exact confusion
this change exists to fix). The Revoke/Clean `confirm()` dialogs check
`isInfrastructure` and, when true, use `infrastructureReason` in place
of the generic wording, e.g. `Revoke "openvoxdb" - the certificate
openvoxdb uses for its own server identity? This will break every
client (including this console) that connects to it.` - the second
sentence per-consequence is written per known reason, matching the
table already established in conversation for the specific
certnames this project's own dev stack produces; a certname whose
reason doesn't match one of those known phrasings falls back to a
generic-but-still-specific "This is infrastructure, not a managed node:
<reason>."

## Risks / Trade-offs

- **`PeerCertname` runs a TLS dial at console startup against services
  that might not be up yet** (e.g. openvoxdb still starting). Mitigated
  the same way CA-client already is: best-effort, logged, non-fatal -
  an infrastructure certname just won't be flagged until the next
  console restart if the dependency wasn't reachable at startup time.
  No retry loop - matches this project's existing "no auto-reconnect
  for one-shot startup checks" posture elsewhere.
- **A future certname rename/rotation on openvoxdb's or openvoxserver's
  side isn't detected until the console restarts** (the Set is built
  once, not refreshed). Acceptable - these are long-lived
  infrastructure identities, not something that rotates during normal
  operation, and a stale "not flagged as infrastructure" is safe by
  construction (it just means the row isn't hidden/warned, not that
  something incorrect is claimed).

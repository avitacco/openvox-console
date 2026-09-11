## Context

See proposal.md - Why. Three facts about the current code shape the
approach:

1. All three scripts derive `CERT_FILE` from `puppet config print
   certname`/`ssldir`, then exit non-zero if it's absent
   (`install_script.go` ~100-104, `install_script_macos.go` ~112-113,
   `install_script_win.go` ~75-76). The exit happens *after*
   openvoxagent is installed, so the node is left half-provisioned.
2. `agentdist.Config` carries `ConsoleBaseURL` and `TransportAddr`
   only. `CONSOLE_CA_CLIENT_URL` exists in `internal/runtime/config.go`
   but is the console's own client credentials for the CA API (dev
   value `https://localhost:8140`), not a node-facing address.
3. `internal/runtime.Config` already distinguishes a bind address from
   a node-facing one for the node transport (`NodeTransportAddr` vs
   `NodeTransportPublicAddr`), with a documented reason: a bind address
   has no useful host part for a remote client. The new setting follows
   that existing precedent rather than inventing a shape.

## Goals / Non-Goals

**Goals:**
- A brand-new node goes from nothing to enrolled and running
  node-agent-client in one invocation of one script.
- A node waiting on a human to sign gets told where the human signs it.
- A real connectivity failure stays distinguishable from waiting.

**Non-Goals:**
- Autosign configuration on openvoxserver. Whether the CA autosigns is
  the operator's policy; this change only has to behave correctly under
  both.
- Signing from inside the install script. The script runs as root on
  the node with no console credentials; signing stays an authenticated
  console action behind `nodes:certs:manage`.
- Certificate renewal, revocation, or re-issue on an already-enrolled
  node.
- Any change to how node-agent-client itself locates or uses the
  certificate once it exists.

## Decisions

**Enrollment uses `puppet ssl bootstrap`, with no fallback.** It exists
precisely for "get this node a certificate and nothing else" and does
not also apply a catalog, which is the wrong side effect for an
installer that hasn't finished setting the node up yet.
- *Alternative considered*: `puppet agent -t --waitforcert`. Rejected -
  a full catalog run mid-install can fail for reasons unrelated to
  enrollment (a broken manifest, a missing class) and would report that
  failure as an enrollment failure.
- *Confirmed in task 2.1, not assumed*: the agent this script installs
  is openvox-agent 8.28.1 (`ghcr.io/openvoxproject/openvoxagent:latest`),
  and its `puppet ssl --help` lists `bootstrap` - documented as
  "Perform all of the steps necessary to request and download a client
  certificate. If autosigning is disabled, then puppet will wait every
  `waitforcert` seconds for its certificate to be signed." That is
  exactly the required behavior, including the bounded wait, with no
  polling loop of our own. `puppet ssl` has shipped since Puppet 6 and
  this script installs the agent itself from the openvox8 repos, so an
  agent without `bootstrap` is not a state this script can produce -
  a fallback path for it would be untestable dead code.

**The three outcomes are told apart by the certificate's presence plus
the command's own error output, never by its exit code.** Confirmed in
task 2.1: `puppet ssl bootstrap` against an unreachable CA exits 1, and
a wait that expires without a signature exits 1 as well - the code
carries no information distinguishing them. What does distinguish them
is what the command printed: an unreachable/unresolvable/refusing CA
produces route-level errors (`No more routes to ca`, `Failed to open
TCP connection`), while a node whose CSR is simply sitting unsigned
produces none of those. So the script captures bootstrap's output,
then: certificate now present -> continue; output carries a
route-level error -> report that, with the captured error; otherwise ->
report the pending-signature case.

**Wait window defaults to 5 minutes total, polled every 15 seconds, via
the agent's own two settings.** Long enough for an operator watching the
Nodes page to sign it while the script runs; short enough that an
unattended install fails rather than hanging a provisioning job
indefinitely. The script does not implement its own polling loop - the
agent already has one, and reimplementing it would mean reimplementing
its retry and TLS behavior too.
- *Alternative considered*: block indefinitely. Rejected as the default
  for the unattended case, per the user's decision recorded in
  proposal.md.
- **Corrected during live testing (task 4.2), after getting this wrong
  first**: `--waitforcert` is the *poll interval*, not a total timeout,
  and `--maxwaitforcert` is the total wait, defaulting to `unlimited`.
  Quoting the agent's own `defaults.rb`: waitforcert is "How frequently
  puppet agent should ask for a signed certificate", and maxwaitforcert
  is "The maximum amount of time the Puppet agent should wait for its
  certificate request to be signed. A value of `unlimited` will cause
  puppet agent to ask for a signed certificate indefinitely." The first
  implementation passed only `--waitforcert 300`, which meant the script
  re-checked just once every five minutes and would have waited
  *forever* - the exact indefinite hang rejected above, reached by
  accident. Caught because a live run kept waiting more than a minute
  after the certificate had actually been signed in the console. Both
  settings are now passed explicitly.

**Timeout message points at this console's Nodes page, not a CA CLI.**
The console already signs certificates (`internal/nodeconnectivity`,
behind `nodes:certs:manage`), and the person running the install script
on a node frequently has no shell on the CA host. The script knows
`CONSOLE_URL` already - it fetches packages from it - so the message
can name the actual page.

**The node-facing server address is a new optional setting, and unset
means "change nothing".** Mirrors `NodeTransportPublicAddr`'s existing
role. Unset must not fall back to `CONSOLE_CA_CLIENT_URL`: that value
is routinely `localhost`, and silently writing it into a remote node's
`puppet.conf` would break a node that was previously working via its
own DNS or pre-seeded config. Leaving the node's setting alone is the
only safe default, and it keeps this change strictly additive for
existing deployments.

**Distinguishing "waiting to be signed" from a real failure keys off
the certificate's presence after the enrollment command returns, not
off parsing the command's output.** Exit codes and log text differ
across agent versions and are a bad contract to depend on; "is there a
signed certificate at the path we already compute" is the same check
the script does today and is unambiguous. So: run enrollment, then
re-check `CERT_FILE`. Present -> continue. Absent *and* the command
reported a connection-level error -> report that error. Absent with no
such error -> report the pending-signature case with the console URL.

**The existing "already has a certificate" fast path stays exactly as
it is**, guarding the whole enrollment block. That is what keeps the
agent-distribution spec's "Running the script on an already-provisioned
node" scenario true, and it means a re-run costs nothing extra.

**Certname stays the node's own default.** The scripts already read
`puppet config print certname` and every downstream consumer (the
package's postinst, node-agent-client's config, the console's
connectivity registry keyed by certname) uses that same value. Letting
the script override it would create a way for the certname in the
certificate to disagree with the one node-agent-client reports, which
is precisely the mismatch this project's node transport identity model
depends on not happening. An operator who needs a different certname
sets it in `puppet.conf` before running the script, the same as with
any Puppet install.

## Risks / Trade-offs

[A node enrolls against the wrong server because the configured
node-facing address is stale or internal] → Mitigation: unset means
"don't touch it", so the setting is opt-in; and the script prints the
server it is enrolling against before it does so, making a wrong value
visible in the install output rather than silent.

[The 5-minute wait makes an unattended install appear to hang] →
Mitigation: the script prints, before waiting, that it is waiting and
for how long, and names the page where signing happens. A CI/
provisioning caller that wants no wait can sign ahead of time or
autosign.

[macOS and Windows enrollment can't be live-verified here] →
Mitigation: this project's established treatment - the equivalent
commands are written from each platform's documented behavior, the
scripts' rendered output is asserted in tests, and operations.md
records that they are static-verified only. Same posture as
add-multi-platform-agent-install and add-native-agent-packaging.

[`puppet ssl bootstrap` absent on an older agent] → Mitigation: the
fallback path, plus task 2.1 confirming against the agent version this
project actually installs rather than assuming availability.

## Migration Plan

Additive. An existing node already has a certificate and takes the
unchanged fast path. An existing console with no node-facing address
configured generates a script that behaves as before for configured
nodes, and now enrolls unconfigured ones against whatever server they
already point at. Rollback is reverting the script templates; nothing
persists on the console side beyond one optional config value.

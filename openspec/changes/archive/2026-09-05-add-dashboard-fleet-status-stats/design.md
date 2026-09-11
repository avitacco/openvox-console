## Context

`GET /api/v1/nodes/summary` (`internal/inventory/handlers.go`) currently
counts nodes by whatever raw `latest_report_status` string openvoxdb
returns (`failed`, `noop`, `changed`, `unchanged`), with a synthesized
`unreported` bucket for nodes with none, and renders one card per
bucket that actually occurs (`frontend/src/index.js`'s
`orderedStatuses`). `internal/openvoxdb.Node` (queries.go) parses only
`certname`, `latest_report_status`, and `report_timestamp` from
`nodes {}` today.

Confirmed live against this project's real openvoxdb: the unprojected
`nodes {}` query already returns `latest_report_corrective_change` in
every response (verified via a direct PQL query against the running
`enterprise-console-openvoxdb-1`) - it's simply never been parsed. Its
value is currently `null` for every real node here, since Puppet's
corrective-change tracking is off by default and none of this
project's test nodes have it enabled. See proposal.md's Impact section.

## Goals / Non-Goals

**Goals:**
- Four fixed stat categories, always in the same order, with zero new
  PQL queries (the field is already in the existing response).
- Real, live-verified `corrected` data, not just plumbing that always
  reads zero.

**Non-Goals:**
- Event-level or report-level detail about *which* resources were
  corrective - `latest_report_corrective_change` is a single
  aggregate boolean per node's latest report (openvoxdb's own
  projection, not derived here), and that's the only granularity this
  change surfaces. Per-resource corrective detail, if ever wanted, is a
  report/event-detail page concern (`internal/reporting`), not the
  Dashboard summary.
- Retroactively enabling corrective-change tracking fleet-wide - only
  `openvox-testing-agent` (this project's designated live-verification
  node, per this session's established pattern) gets it enabled, to
  produce real data for verification. Any other real managed node's
  Puppet configuration is untouched.
- Changing what `/api/v1/nodes` (the paginated node list) or
  `/api/v1/nodes/{name}` (single-node detail) return - only the summary
  endpoint's classification changes.

## Decisions

**Per-node classification uses `latest_report_status` first, then
`latest_report_corrective_change` only to split `changed`:**

```
failed          status == "failed"
unchanged       status == "unchanged"
corrected       status == "changed" && corrective_change == true
intentional     status == "changed" && corrective_change != true   (false OR null)
(excluded)      status == "noop", or no report at all
```

`corrective_change == null` (tracking not enabled, or no reportable
event) falls into `intentional` rather than a separate "unknown"
bucket or `corrected` - the conservative choice: don't claim a node had
a corrective change unless openvoxdb actually confirms one. This means
a node with corrective-change tracking off but a real "changed" report
is - correctly - indistinguishable from an intentional change, which is
exactly the point of live-verifying with tracking enabled (task 3 in
tasks.md) rather than trusting the plumbing alone.

**Reuse the existing `{total, byStatus}` response shape, redefining
`byStatus`'s keys** rather than inventing a new response shape or
adding parallel fields. `byStatus`'s only consumer is
`frontend/src/index.js` (confirmed by search - see proposal.md's
Impact), so this is a safe, self-contained redefinition, and keeps the
Go handler and frontend both simple: `byStatus` becomes exactly
`{failed, corrected, intentional, unchanged}` (each key always present,
even if 0), and `total` keeps its original, unchanged meaning (full
node count, unaffected by exclusions).

**Frontend renders four fixed `vox-stat` cards in a declared order**
(Failed, Corrected, Intentional changes, Unchanged), replacing
`orderedStatuses`'s dynamic list-whatever-exists approach entirely -
this endpoint's output is now a fixed shape, so there's nothing left to
dynamically order. The "Total nodes" card is dropped per the user's
explicit four-stat request; `total` stays in the API response (harmless,
simple) even though the UI no longer renders it.

**Live verification produces a real corrective change**, not a
simulated one - and, per a live check against the real
`openvox-testing-agent` container, requires no config change at all.
`corrective_change = true` (this proposal's original assumption, based
on general Puppet Enterprise familiarity, not verification) is **not a
real setting** - confirmed by grepping the actual FOSS/OpenVox agent's
`defaults.rb`, `puppet agent --help`, and `puppet apply --help` for
"corrective" and finding no such setting or flag anywhere. Corrective-
change detection is built into open-source Puppet automatically: each
run persists the value it applied to every managed resource in
`transactionstorefile` (default `$statedir/transactionstore.yaml`), and
the *next* run flags a resource as a corrective change precisely when
its current value differs from that persisted "last known good" value
(unexpected drift) rather than only from the catalog's desired value (an
intentional catalog change still resolves cleanly against the persisted
state). So: run the agent once cleanly, manually drift a resource that
container's catalog manages (e.g. hand-edit a file Puppet is applying)
outside of Puppet, run the agent again, and confirm openvoxdb reports
`latest_report_corrective_change: true` for that node before checking
the Dashboard - matching this session's established practice of
verifying against real behavior, not assumed behavior (this specific
correction is itself an example of exactly why that practice matters).

**A second, deeper finding from live verification: the corrective flag
never reaches openvoxdb at all, in this environment, regardless of
config.** A real drift-and-correct cycle against `openvox-testing-agent`
confirmed Puppet's own agent genuinely detects it -
`last_run_report.yaml` has `corrective_change: true` at the event,
resource-status, and top-level report keys, and the agent's own log
explicitly prints `(corrective)` on the changed line. But querying
openvoxdb afterward - at the event level, the report level, and the
`nodes{}` projection - returns `null` every time (confirmed repeatedly,
including an 11-second wait to rule out ingestion lag). Root cause,
traced via `openvoxserver`'s access log: report submission goes out as
PDB command `store_report` **version 8**, and that command version
doesn't carry `corrective_change` through, even though openvoxdb's own
query schema already has the column. This is a version gap between the
`puppetdb-termini` gem bundled in `ghcr.io/openvoxproject/openvoxserver`
and what `ghcr.io/openvoxproject/openvoxdb` needs to receive - both
vendored, third-party images this project doesn't build.

An upgrade attempt (`openvoxserver:8.8.1-latest` /
`openvoxdb:8.9.1-latest`, the newest tags published at investigation
time) was tried and reverted: it introduced an *unrelated* break first
(the newer image's entrypoint regenerates `puppet.conf` after this
project's custom ENC-configuration init script runs, not before, so
`node_terminus`/`external_nodes` silently stop being set and ENC
classification breaks server-side) before the original question could
even be answered. Reverting also surfaced a real infrastructure
side-effect worth recording: swapping images changed the owning
UID/GID of the shared `openvoxserver-ssl`/`openvoxserver-ca`/
`openvoxdb-ssl` volumes to the newer images' process user, and
reverting the *image* tag does not revert *file ownership* in a
already-populated volume - `:latest` (uid 64604) couldn't read its own
key material until that ownership was fixed back by hand. Both
containers and their real CA/cert data were fully recovered; no data
was lost.

**Conclusion: ship the correct, unit-tested classification logic as-is.**
This app's code has no way to make openvoxdb persist a field its
current report-ingestion path drops - upgrading the vendored images is
a real fix, but a separate, larger undertaking (needs the ENC/entrypoint
incompatibility resolved first) than this change's scope. `"corrected"`
will correctly read 0 in this environment, and every real "changed"
node correctly falls back to `"intentional"` - the conservative,
never-guess behavior this design already commits to, working exactly as
intended given real data that doesn't confirm a corrective change.

## Risks / Trade-offs

- **A node whose corrective-change tracking is off will never appear
  under "corrected," even if it did in fact drift and get corrected** -
  this is an inherent limitation of the data source (openvoxdb only
  knows what Puppet tells it), not something this change's backend
  logic can work around. Documented explicitly in the spec's
  "intentional" scenario and design decision above, rather than left as
  a silent gap.
- **Redefining `byStatus`'s keys is a breaking change to that
  endpoint's shape** for any future consumer that isn't
  `frontend/src/index.js` - acceptable now (confirmed single consumer),
  but worth remembering if this endpoint ever gets a second caller with
  different needs (at which point a dedicated, purpose-built endpoint
  would be cleaner than overloading this one further).

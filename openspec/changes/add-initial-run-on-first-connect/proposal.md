## Why

A freshly enrolled node is invisible in the console until something
triggers its first Puppet run. Enrolment succeeds, the certificate is
signed, `node-agent-client` installs and connects - and the node still
shows no facts, no packages, no reports, no status, because every one of
those comes from openvoxdb and a node that has never run has submitted
nothing to it. The operator's reward for completing enrolment is a blank
row, with no indication that one more manual step is needed.

The agent's own scheduled run closes the gap eventually (up to 30 minutes
by default), but "wait half an hour, then look again" is a poor answer
during the exact activity - adding a node - where the operator is most
likely to conclude that something is broken.

## What Changes

- When a node connects over the node transport and the console holds no
  openvoxdb inventory record for that certname, the console dispatches a
  Puppet run to it automatically, using the existing orchestrator job
  machinery. The node populates its own inventory within seconds of
  enrolling.
- The dispatched job is a normal orchestrator job: visible in the jobs
  list, recorded in the activity log and the audit log, and correlated to
  the report it produces. It is attributed to the console itself rather
  than to a user.
- The console records that it has attempted an initial run for a
  certname, and never dispatches a second one automatically. A flapping
  or restarting agent cannot produce a dispatch loop, and a failed
  initial run surfaces as a visible failed job rather than a retry.
- The node transport gains a way for other subsystems to observe node
  connections, rather than only to poll current state. This makes
  connection events load-bearing for the first time, which the transport
  currently documents as explicitly not the case.

Not a breaking change: no existing endpoint, payload or configuration
value changes, and a deployment that never enrols a new node sees no
difference in behaviour.

## Capabilities

### New Capabilities

None. This is a new trigger for an existing capability rather than a new
one - the run itself, its job record, its audit trail and its report
correlation are all existing orchestrator behaviour.

### Modified Capabilities

- `orchestrator`: add a requirement that the console dispatches a Puppet
  run when a node with no inventory connects, exactly once per certname,
  attributed to the console rather than a user.
- `node-transport`: add a requirement that node connection events are
  observable by other subsystems, so a connection can trigger work rather
  than only update a status lookup.

## Impact

- `internal/nodetransport` - `Registry` currently documents itself as
  observability-only, fed by `$SYS.ACCOUNT.*.CONNECT` events, with the
  explicit note that "a brief disagreement between this map and reality
  during a connect/disconnect race is never load-bearing for dispatch
  correctness". That note stops being true for the new observer path and
  must be revisited rather than left standing.
- `internal/orchestrator` - reuses `Store.CreateJob` and
  `Dispatcher.DispatchRun` unchanged. The `triggeredBy` field carries the
  console's own identity.
- `internal/openvoxdb` - `NodeByCertname` supplies the "has no inventory"
  test; no new query needed.
- `internal/persistence` - a migration for the record of which certnames
  have had an initial run attempted. Postgres rather than in-process
  state, so the decision survives a restart and is shared across console
  instances, matching the project's stateless-service preference.
- `cmd/console/main.go` - wires the transport's connection observer to
  the orchestrator.
- No frontend change: the job and its report appear through existing
  views.

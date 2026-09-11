## Context

See proposal.md - Why. The existing wire scheme this extends
(confirmed by reading the real code, not assumed): `internal/orchestrator/wire.go`
and `internal/nodeagent/handler.go` each independently define matching
`requestData{Module, Action, Params}` / `wireResponse{Result, Error}`
types - one shared JSON contract, deliberately not a shared Go type,
dispatched over `internal/nodetransport.Dispatch(ctx, certname, payload,
timeout)`, a fully generic opaque-payload RPC with no command shape
baked in at the transport layer. `nodeagent.NewHandler(runner
CommandRunner, puppetBin string)` currently only knows how to run
`puppetBin` with fixed argument sets for `actionRun`/`actionRunTask`;
extending it here is genuinely new capability for that constructor, not
just a new `case` in an existing switch that already had what it needed.

## Goals / Non-Goals

**Goals:**
- Reuse the existing dispatch wire scheme and single-in-flight
  semantics exactly - no new transport mechanism.
- Make the fact-writing logic package-manager-aware without shelling
  out to detect it, where a simple file check suffices.

**Non-Goals:**
- Reworking `internal/orchestrator`'s job-tracking dispatcher - this
  proposal dispatches directly from `internal/packageinventory`, since
  a status/set action has no job history worth persisting (see
  proposal.md - Impact).
- Windows/macOS live verification - same already-established limitation
  as every prior agent-distribution change in this project (no real
  host). The new dispatch action is platform-agnostic in principle
  (node-agent-client already runs cross-platform), so it's implemented
  for all platforms, just verified live on Linux only.

## Decisions

**New `Action` values on the existing `puppet` module,
not a new module.** `actionPackageInventoryStatus` (no params) and
`actionPackageInventorySet` (`{"enabled": bool}` params). Both still
ultimately trigger a real Puppet run on a state change, so this is
recognizably the same kind of operation as `actionRun`/`actionRunTask`,
not a new category of thing the wire scheme needs to model separately.

**Package-manager family detected by file check, not by shelling
out.** `NewHandler` gains this responsibility: check for
`/usr/bin/dpkg-query` vs `/usr/bin/rpm` via `os.Stat` to decide which of
the two embedded fact-script variants to write - mirrors what
`install.sh` already does with `command -v`, translated to Go since
node-agent-client is a Go binary making this decision itself now,
not a shell script. A node with neither is an error (`errorResponse`),
not a silent no-op - see Risks.

**The two fact-script variants move from `internal/agentdist/pkgassets/`
to `internal/nodeagent`, embedded via `go:embed`.** They're unchanged
verbatim (`dpkg-query`/`rpm -qa`, same as today) - only their owner
changes, since node-agent-client writes them now, not the package or
the install script.

**Toggling a state it's already in still replies with success but
performs no filesystem write and triggers no Puppet run.** Cheap to
implement (check current file presence first) and avoids an
operator-visible no-op Puppet run cluttering that node's run history
every time the console re-syncs UI state.

**`internal/packageinventory` dispatches directly**, translating
`nodetransport.ErrNodeNotConnected` into the "node offline, toggle
disabled" response the spec's "Toggling requires the node to be
currently connected" requirement describes, and reports the dispatched
Puppet run's `outputData` alongside the resulting enabled/disabled
state so an operator can see what actually happened, not just a bare
boolean.

**Permissions reused, not invented** (confirmed by reading
`internal/orchestrator/handlers.go` and `internal/packageinventory/handlers.go`
rather than guessed): `nodes:read` for the status endpoint (matching
this capability's existing read endpoints), `orchestrator:run` for the
set endpoint (matching the permission that already gates other
node-affecting dispatched actions).

## Found during implementation

**NATS delivers to a single subscription strictly serially** - a real
`nats.Conn.Subscribe` callback never receives a second message until the
first call returns, confirmed by trying (and failing) to race two real
dispatches through an actual transport round-trip: the second simply
sat queued in NATS's own delivery pipeline rather than ever reaching
`Client.dispatch` concurrently. This means `busy`'s `CompareAndSwap`
guard can only ever matter if something calls `dispatch` itself out of
band - not a real risk with this client's current single-subscription
design, but not dead code either, so the corresponding test
(`TestClient_DispatchRejectsPackageInventoryRequestDuringInFlightRun`)
calls the unexported `dispatch` method directly with two goroutines
instead of relying on transport-level delivery timing.

## Risks / Trade-offs

- [A node has neither `dpkg-query` nor `rpm`] → `errorResponse` with a
  clear message, surfaced to the operator through the same path a busy
  or execution-failure response already takes - no silent no-op.
- [The single-in-flight lock means a toggle request during a long-
  running orchestrator run gets rejected as busy] → Already the
  existing, understood behavior of this mechanism (see specs/node-agent's
  "Single in-flight request per client"); the UI should surface "node
  busy, try again" rather than a generic error, but no new mechanism is
  needed to produce that information - `wireResponse.Error` already
  carries it.
- [Migration surprise: nodes that already have the fact from
  `add-native-agent-packaging`'s package content will have it *removed*
  when they upgrade to the package version built after this change,
  since dpkg/rpm remove a file that a newer package version no longer
  declares] → This is expected package-manager behavior, not a bug, but
  it's a real, visible change: reporting silently goes from
  always-on to off-until-explicitly-enabled for every node that
  upgrades. Documented plainly in `operations.md` rather than left as a
  silent surprise (see tasks.md's documentation step) - there is no
  practical way to distinguish "this node's operator wants reporting
  back on" from "this node never had it enabled" after the file is
  already gone, so no auto-re-enable is attempted.

## Migration Plan

1. Ship `cmd/build-agent-packages` no longer declaring the fact script
   as package content; ship `node-agent-client` with the new dispatch
   actions and embedded fact-script variants.
2. On a node's next `apt-get`/`yum` upgrade to the new package version,
   the package manager removes the now-undeclared fact file as a normal
   part of the upgrade transaction (verify this live - task 5 - rather
   than assume it, even though it's well-documented package-manager
   behavior).
3. An operator who wants reporting back on for a given node uses the
   new toggle - a one-time action per node, not automatic.

Rollback: reverting to a prior console build re-packages with the fact
as static content again; a node re-installing that older package
version gets the fact back unconditionally, same as before this change
existed.

## Open Questions

None - the decisions above cover what would otherwise have been
deferred; nothing here changes the specs, approach, or task breakdown
if answered differently later.

## Context

See proposal.md - Why. The design-relevant state:

- `internal/nodetransport.Registry` is fed by the embedded NATS server's
  `$SYS.ACCOUNT.<acct>.CONNECT` / `.DISCONNECT` events, consumed by an
  internal admin client. It maintains an in-process map plus two
  timestamps per node. Its doc comment states the map "exists purely for
  observability ... dispatch itself does not depend on it (it relies on
  NATS's own no-responders signal instead), so a brief disagreement
  between this map and reality during a connect/disconnect race is never
  load-bearing for dispatch correctness."
- Dispatch reachability is decided by `nats.ErrNoResponders` at send time
  (`internal/nodetransport/dispatch.go`), not by the registry.
- `internal/orchestrator` already has everything needed to run and record
  a job: `Store.CreateJob(ctx, JobKindRun, "", "", nil, targets,
  triggeredBy)` then `Dispatcher.DispatchRun`. `triggeredBy` is a free
  string, and job records already carry trigger source, per-target
  results, activity entries, audit events and report correlation.
- `internal/openvoxdb.NodeByCertname` answers "does this node have an
  inventory record" directly.
- Migrations are numbered SQL files under
  `internal/persistence/migrations/`, currently through `000019`.
- The console is designed to run as more than one instance against one
  Postgres, and the project prefers Postgres over in-process state so
  failover stays simple.

## Goals / Non-Goals

**Goals:**

- Exactly one automatic run per node identity, holding across console
  restarts and across multiple console instances.
- No change to how an operator-triggered run behaves or is recorded.
- Connection observation that cannot affect a node's ability to connect.

**Non-Goals:**

- A general event bus for transport events. One observer hook, shaped for
  this use, with no subscription management or replay.
- Reconciling nodes that connected while the console was down. A node
  that enrols during an outage is picked up by its own scheduled run, or
  by an operator triggering one. Catching up would mean a periodic
  reconciler, which is a larger change with its own failure modes.
- Making the initial run configurable. See Decisions.

## Decisions

### Trigger on "connected and has no inventory record", not "first ever connection"

The condition is evaluated at connect time: the node connected, and
`NodeByCertname` returns nothing.

The alternative - "the first time we ever see this certname connect" -
was rejected because it answers the wrong question. What the operator
cares about is an empty console, and emptiness is a property of inventory,
not of connection history. The two differ in a real case: openvoxdb's
`node-purge-ttl` eventually removes a node that has stopped reporting. If
such a node comes back and reconnects, its inventory is genuinely absent
and dispatching a run is the right behaviour - it repopulates exactly what
the operator would otherwise have to trigger by hand.

That case is bounded by the once-per-certname record below, which is
never cleared: a purged-and-returned node gets a run only if it never had
one dispatched before. This is deliberately conservative. Re-arming on
purge would be more useful, but it needs a purge signal the console does
not currently receive, and would reopen the dispatch-loop risk for a node
that flaps around the TTL boundary. Left for a later change if it proves
necessary.

### Record the attempt in Postgres, keyed by certname, written before dispatch

A table of certnames the console has dispatched an initial run for, with
a timestamp. Inserted with `ON CONFLICT DO NOTHING`, and the dispatch
happens **only if the insert reported a row was actually inserted**.

This makes the check-and-claim a single atomic step, which matters for
the two ways duplicates arise:

- Two console instances observing the same connect event. Both attempt
  the insert; exactly one wins; only the winner dispatches.
- One instance seeing a rapid reconnect. The second insert conflicts and
  dispatches nothing.

Alternatives considered:

- *An in-process set.* Rejected: lost on restart, and wrong the moment a
  second console instance exists - each would dispatch its own run.
- *Query the jobs table for an existing system-triggered run.* Rejected:
  it makes the idempotency key implicit in job history, so deleting or
  archiving old jobs would silently re-arm the behaviour. It is also a
  check-then-act with a race window between the two.
- *Writing the record only after a successful dispatch.* Rejected: a
  crash between dispatch and write re-arms the node, and a dispatch that
  fails slowly leaves the window open for a duplicate. Claiming first
  means a lost claim costs one missed automatic run, which the scheduled
  agent run covers; the opposite error costs unrequested runs on a
  managed machine.

The record is never deleted by normal operation. Node deletion is the one
case worth considering, and it is left alone deliberately: re-enrolling a
deleted node under the same certname is unusual enough that a
hand-triggered run is acceptable, and clearing the record would put the
dispatch-loop risk back.

### Never retry automatically

A failed initial run leaves a failed job and no further action. The node
still has its scheduled runs; an operator can trigger another from the
Nodes page.

Retrying is attractive right up until the failure is systemic - a broken
catalog, a node that accepts a dispatch and dies, a compilation error
affecting every new node. Then a retry policy turns one visible failure
into a stream of them, on every newly enrolled node at once. A single
failed job is a better signal, and the recovery path already exists.

### Attribute the job to the console, not to a user

`triggeredBy` is set to a reserved, clearly non-user value -
`system:initial-run`. No permission check is performed, because no
principal is making a request: this is the console acting on its own
behalf, in the same way it already writes activity and audit entries
without a user.

Two consequences accepted deliberately. First, the audit trail shows a
run nobody asked for, so the actor string must be unmistakably
system-originated rather than something that could collide with a
username - hence the `system:` prefix, which the local login path cannot
produce. Second, a deployment cannot withhold this behaviour by
withholding `orchestrator:run` from users. That is the point: the run is
part of enrolling a node, not a privileged operation a user performed.

### Always on, with no configuration

No new environment variable. The behaviour is part of what enrolling a
node means; a console where a freshly enrolled node stays blank is the
bug this change exists to fix, and an option to re-enable the bug is not
worth the config surface. This follows the project's preference for fewer
knobs, and can be revisited if a real deployment needs it - adding a
toggle later is easy and removing one is not.

### Bound concurrency with a small worker pool

Connect notifications feed a buffered channel drained by a fixed number
of workers (start at 4), each doing the claim-then-dispatch. When the
buffer is full, the notification is dropped rather than blocking the
transport - the node simply does not get an automatic run, which is the
same outcome as enrolling while the console is down, and its scheduled
run still covers it.

Unbounded goroutines were rejected: enrolling a large batch would open
one Postgres transaction and one dispatch per node simultaneously, and the
pool the whole console shares is finite. Queuing rather than bursting also
matches how an operator would stage a batch by hand.

The spec requires that a batch "degrades into a queue rather than a
simultaneous burst" and that no node's run is dropped; the buffer is sized
so that dropping is a genuine overload condition rather than something a
routine batch hits. Drops are logged at warning level with the certname,
so an overloaded console is visible rather than silent.

### Revise the Registry's "never load-bearing" comment rather than leave it

The observer hook makes connect events cause work. The existing comment
explicitly disclaims that, and leaving it would mislead the next reader
into assuming a dropped or duplicated event is harmless. It gets rewritten
to say what is now true: the connected-state *map* remains observability
only and dispatch still relies on the no-responders signal, but connect
*notifications* drive the initial-run path, which is why that path is
written to tolerate duplicates and misses.

## Risks / Trade-offs

- **A node is dispatched a run it did not expect, immediately on
  enrolment.** → This is the intended behaviour, and it is what an
  operator does by hand anyway. Bounded by the once-per-certname claim, so
  it cannot repeat.
- **A systemic catalog failure produces one failed job per newly enrolled
  node.** → No retry, so the count matches the number of new nodes rather
  than growing. Each is a normal failed job an operator can read.
- **The claim is written but the dispatch never happens** (crash between
  the two, or the node disconnected). → The node loses its automatic run
  and falls back to its scheduled run. Chosen deliberately over the
  opposite failure; see the Decisions note on claim ordering.
- **Connect notifications are dropped under load.** → Same outcome as the
  previous point, logged with the certname so it is visible.
- **openvoxdb is unreachable when the inventory check runs.** → Treated as
  "unknown, do nothing": no claim is written and no run is dispatched, so
  the node is still eligible when it next connects. Dispatching on an
  error would mean running Puppet on every connecting node during an
  openvoxdb outage.
- **Two console instances both observe the connect.** → The atomic claim
  means one dispatches. No coordination beyond Postgres is needed.

## Migration Plan

One additive migration (`000020`) creating the claim table. Nothing reads
it before this change, and nothing else writes it, so it applies to a
running deployment with no downtime and no backfill.

Existing nodes are unaffected: they all have inventory records, so none
meets the trigger condition. A deployment that upgrades and never enrols
a node sees no behavioural change at all.

Rollback is the down migration plus the previous binary. Because the
claim table is only ever consulted by this feature, leaving it in place
after a rollback is harmless, and rolling forward again preserves the
record of which nodes were already handled.

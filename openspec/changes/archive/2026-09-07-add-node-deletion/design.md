## Context

See proposal.md - Why. This design went through a real correction
during implementation, not just planning - worth recording accurately
rather than presenting the final answer as if it were obvious from the
start:

**What's actually true, confirmed live against a real openvoxdb
instance:**
- `POST {baseURL}/pdb/cmd/v1?command=deactivate%20node&version=3&certname=<name>`
  with a JSON body `{"certname": "<name>", "producer_timestamp":
  "<RFC3339, current time>"}` deactivates a node.
  `producer_timestamp` must not be stale (older than the node's own
  last-known activity) - an old one is silently ignored ("'deactivate
  node' command ignored (stale)", seen in openvoxdb's own logs), so it
  must be generated at request time, not hardcoded or reused.
- openvoxdb's `nodes` query entity **unconditionally excludes**
  deactivated and expired nodes, with no override found - not even an
  explicit `nodes { deactivated is not null }` or
  `nodes { certname = "..." }` filter clause returns one, confirmed
  live and matching PuppetDB's own documentation ("Deactivated and
  expired nodes aren't included in the response"). This is the
  *opposite* of an earlier assumption made before implementation (that
  the bare `nodes {}` query returned everything and needed an added
  filter to exclude inactive nodes) - that assumption was wrong, caught
  by testing it live rather than trusting the earlier research. The
  practical effect is a wash for this proposal's core goal: a
  deactivated node already stops appearing in the node list for free,
  with zero query change needed.
- The one place a deactivated node's record is still reachable is
  openvoxdb's single-node lookup route
  (`GET /pdb/query/v4/nodes/<certname>`) - confirmed live it returns a
  deactivated node's full record (including its `deactivated`
  timestamp), and 404s only for a certname openvoxdb has never heard of
  at all. This is *not* the same code path as the general PQL query
  endpoint `Client.query` uses.

**What this ruled out:** a "show inactive" list toggle, floated during
design, turned out to be unbuildable against openvoxdb's real query
surface - there's no supported way to get a list of deactivated nodes
back out, only single-certname lookups if you already know the
certname. Rather than reintroduce console-side persistence to fake
that list (a table of "certnames I've deleted"), it was dropped:
deleting a node hides it immediately and completely, with no built-in
way to browse what's been deleted - see proposal.md's What Changes.

**A second, more consequential correction found during implementation
(not planning):** deactivating a node in openvoxdb was verified to
correctly exclude it from `Nodes()`, but the node kept reappearing on
the live rendered page anyway. Root cause, found by reading the actual
frontend code rather than re-guessing: `frontend/src/nodes.js`'s
`allCertnames()` unions `inventoryCertnames` (from `Nodes()`) with the
keys of `connectivityByCertname` (from `GET /api/v1/node-connectivity`,
the separate CA/node-transport connectivity registry) - a certname with
a live certificate record stays in that union regardless of its
openvoxdb deactivation status. This was confirmed by direct
observation, not inferred: a deactivated node correctly vanished from
`/api/v1/nodes`'s own response yet kept rendering on the page, with its
Revoke/Clean/Delete buttons implying an active certificate underneath.
Resolved per the user's direction: deletion now cleans the node's CA
certificate record too (reusing `certstatus.Client.Clean`, the same
operation `internal/nodeconnectivity`'s existing "Clean certificate"
action already performs), not just its openvoxdb record - confirmed
live, end-to-end through the actual UI, with a completely fresh page
reload afterward to rule out any render-timing artifact.

`internal/openvoxdb`'s package doc comment currently states it's
"deliberately thin - a query/response client... not a general-purpose
PQL engine" and openvoxdb-client's spec Purpose says it's "the
console's only path for *querying* openvoxdb." Both need a small
wording update once this ships a command-submission method alongside
the existing query-only one (the Purpose update happens on the main
spec directly at archive time, per this project's normal delta-spec
process for Purpose changes to an existing capability).

## Goals / Non-Goals

**Goals:**
- Get a deleted node off the dashboard immediately and reliably.
- Keep the console's involvement in data lifecycle minimal - it tells
  openvoxdb "this node is gone," not "erase these specific rows."

**Non-Goals:**
- Forcing immediate purge of a deactivated node's historical data
  (facts, catalogs, reports) - explicitly decided against; openvoxdb's
  own `node-purge-ttl`-driven background GC handles this on its own
  schedule, unconfigured/left at its own default in this project. If a
  future need for immediate purge emerges, that's a separate change
  (the admin `POST /pdb/admin/v1/clean` `purge_nodes` operation looked
  plausible for that, but was never actually tested - see the
  conversation this design came out of - and shouldn't be assumed to
  work as expected without live verification then, either).
- Bulk/fleet-wide deletion - one node at a time, matching how the
  existing cert revoke/clean actions also operate on one node at a
  time.

## Decisions

**A new `nodes:manage` permission, not the existing
`nodes:certs:manage`.** Confirmed by reading
`internal/nodeconnectivity/handlers.go`: `nodes:certs:manage` gates
sign/revoke/clean, which is specifically PKI certificate lifecycle. A
node's openvoxdb inventory record is a different resource; conflating
the two would mean granting cert-management rights to someone who
should only be able to delete stale inventory records, or vice versa.

**Command submission added to the existing `internal/openvoxdb.Client`,
not a separate client.** One authenticated connection to openvoxdb,
matching how this project already treats that package as the console's
one path to openvoxdb (per its own doc comment) - expanding its
responsibility from query-only to query+command is a smaller change
than standing up a second client with its own cert/TLS setup.

**`Nodes()` is unchanged; a new `NodeByCertname()` method is added
alongside it**, not a parameter on `Nodes()` - they use genuinely
different openvoxdb routes with different behavior (see Context), not
two modes of the same query, so modeling them as separate methods is
more honest than a boolean flag that would suggest they're the same
operation with a filter toggle.

**The delete handler's "does this node exist" check uses
`NodeByCertname`, not `Nodes()`.** This isn't just style - it's
required for correctness. openvoxdb's "deactivate node" command
succeeds unconditionally (it creates a new, already-deactivated stub
record for an unknown certname rather than rejecting it), so the
console has to distinguish "unknown" from "known" itself. `Nodes()`
can't do this for a certname that's *already* deactivated (its
collection query excludes it either way, indistinguishable from truly
unknown); `NodeByCertname` correctly returns the record for an
already-deactivated node and only 404s for a truly unknown one.

**Delete lives in `internal/inventory`, not a new package.** It's the
same resource (`/api/v1/nodes/{name}`) the existing list/detail
endpoints already serve from that package; a `DELETE` on that path is
a natural extension, not a new capability's worth of surface area.

**The frontend delete confirmation reuses the existing `confirmDialog`
helper** (`frontend/src/app.js`), the same one already used for the
cert revoke/clean actions in `frontend/src/nodes.js` - same
`danger: true` styling, same cancel/confirm semantics, no new dialog
mechanism needed.

**Deletion cleans the CA certificate server-side, inside the same
`DELETE` handler - not as a second frontend-issued request.** A
`certCleaner` interface (just `Clean(ctx, certname) error`) is added to
`internal/inventory`, satisfied structurally by the same
`*certstatus.Client` value `cmd/console/main.go` already wires into
`internal/nodeconnectivity` as `caClient` - no new client, no new
config, just passing the same value to a second package. Doing this
server-side (rather than having the frontend call both `DELETE
/api/v1/nodes/{name}` and `DELETE /api/v1/nodes/{name}/cert` in
sequence) means one confirmation, one request, and one place that
decides what "deleted" actually requires - a future third cleanup step
would only need to change here, not in every caller.

**A `*certstatus.NotFoundError` from `Clean` is tolerated; any other
error is not.** A node can legitimately have no CA certificate record
(e.g., its openvoxdb data arrived without ever completing enrollment),
in which case there's nothing to clean and that's success, not failure.
Any other error (CA unreachable, etc.) is a real failure and is
surfaced rather than swallowed - deletion already deactivated the
node in openvoxdb by this point (not rolled back on a cert-cleaning
failure), so a retried delete just re-attempts the cleanup via the
already-tested "already-deactivated" path.

**`certCleaner` may be nil**, matching `internal/nodeconnectivity`'s
own established pattern for the same underlying client - when no CA
client is configured (`CONSOLE_CA_CLIENT_URL` unset), deletion still
deactivates the openvoxdb record and succeeds; it just can't clean a
certificate that this console has no way to reach.

## Risks / Trade-offs

- [A deactivated node's data lingers for however long openvoxdb's
  default `node-purge-ttl` GC sweep takes] → Explicitly accepted per
  the proposal's Why and confirmed with the user - not a bug, the
  intended behavior. Documented in `operations.md` so a future reader
  doesn't mistake the lag for a defect.
- [`producer_timestamp` staleness is a real, silent failure mode -
  openvoxdb drops a stale command with no HTTP-level error, only a log
  line] → The console always generates this timestamp at request time
  (never accepts one from the caller), so this can't actually occur in
  practice, but it's worth a unit test asserting the timestamp used is
  current, not a fixed/injected value that could go stale.
- [With no CA client configured, a deleted node still deactivates in
  openvoxdb but its certificate is never cleaned, so it can keep
  appearing via the connectivity/CA registry union - the exact problem
  this change set out to fix, just for a console that never had CA
  integration configured at all] → Accepted: this console's CA
  integration is already fully optional everywhere else
  (`internal/nodeconnectivity`'s own sign/revoke/clean already degrade
  the same way), so deletion degrading the same way when unconfigured
  is consistent, not a new gap - and a console without any CA client
  configured has no cert lifecycle management at all regardless of this
  feature.
- [A deleted node is completely unrecoverable from the console's own
  UI - no "show inactive" list, no undo] → Explicitly accepted per the
  corrected design above: openvoxdb doesn't support listing deactivated
  nodes back out, and reintroducing console-side persistence just to
  fake that list was ruled out as the wrong trade-off. The confirmation
  dialog (see specs' "Deletion requires confirmation") is the only
  safeguard - it needs to actually communicate this is not easily
  reversible, not just restate "are you sure?".

## Migration Plan

No migration - purely additive endpoints
(`DELETE /api/v1/nodes/{name}`, `Client.DeactivateNode`,
`Client.NodeByCertname`). `Nodes()`'s existing signature and behavior
are unchanged, so no other call site needs to change.

## Open Questions

None.

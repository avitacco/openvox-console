## Context

See proposal.md - Why for motivation.

What this design has to work with, as built by `add-console-run-modes`:

- `internal/app` has a mode table naming each mode's route groups,
  listeners and workers. The status responder and route are new entries
  in it.
- `internal/messaging.Bus` wraps an embedded NATS server and exposes
  `Publish`, `Subscribe` and `QueueSubscribe`. Instances peer as routed
  cluster members, and `enc` instances attach as leaf nodes.
- `internal/runtime` has a `Checker` interface (`Name()`, `Check(ctx)`)
  already implemented by the Postgres pool and the bus, and consumed by
  the health endpoint.
- `runtime.Version` carries the build version.
- Nav links are `vox-sidenav-item` entries in
  `frontend/templates/layout.html.tmpl`, hidden by default and revealed
  by permission from the page's JS - the pattern `nav-jobs-link`,
  `nav-code-link` and `nav-activity-link` already use.

An important property of the deployment: `enc` instances attach as leaf
nodes, not routed peers, and their connection is outbound-only. Leaf
connections do carry subscription interest and replies in both
directions, so an `enc` instance both receives the status request and
answers it. That is worth stating because it is the one topology where
"can it answer?" is not obvious.

## Goals / Non-Goals

**Goals:**

- A page an operator can open during an incident and trust: if it shows
  four healthy instances, there are four healthy instances.
- Bounded response time regardless of fleet size or a wedged instance.
- No new schema, no new write traffic.

**Non-Goals:**

- History or trends. This is a live view; a time series is a different
  feature with different storage.
- Alerting. The metrics endpoint already exists for that.
- Discovering instances that are running but cannot reach the bus. They
  are undiscoverable by construction here, and the design says so rather
  than pretending otherwise.
- Showing managed infrastructure nodes. The Nodes page owns those.

## Decisions

### Scatter-gather over the bus, not a heartbeat table

The serving instance publishes a status request with a reply subject and
collects replies until a deadline. Every instance subscribes to that
subject and answers with its own status.

Chosen over a Postgres heartbeat table because the question the page
answers is "what is running *right now*", and a heartbeat answers "what
wrote a row recently" - which is the same thing only until it matters.
It also costs no migration and no periodic writes.

The cost is honest and bounded: an instance that is running but not
reachable on the bus is invisible. That is why "an incomplete picture is
reported as incomplete" is a requirement rather than a nicety - the
design's weakness is met by disclosing it in the result rather than by
pretending completeness.

*Alternative considered:* heartbeat table. Its real advantage is showing
instances that recently *stopped*, which a scatter-gather cannot. If that
turns out to matter operationally, it is an additive follow-up - a table
alongside this, not a replacement for it.

### The responder runs in every mode; the page does not

`Surface.Workers` gains a `status-responder` entry present in every
mode's row, and `Surface.Routes` gains a `status` group present wherever
the REST API is served.

Splitting them this way is the point: an instance must always be able to
*describe* itself, or it silently vanishes from the page while running.
Whether it also *serves* the page is an ordinary surface question, and
`enc` and `orchestrator` serve no REST API.

### Completeness is computed from the union of every reply's view

The transport cannot say whether everyone answered - that is a question
about how many instances exist, not about delivery - so the aggregator
has to work it out. The obvious source is the serving instance's own view
of the cluster, and it is not good enough.

NATS makes a server's routed peers visible to it (`NumRemotes`, which
counts distinct servers rather than connections, so route pooling does
not inflate it), but a *leaf* connection is visible only to the peer it
attached to. `enc` instances attach as leaves by design. So a status
request served by `web-1` cannot see a leaf attached to `web-2`, and
would report a complete picture with that instance missing - exactly the
failure the incompleteness requirement exists to prevent, on exactly the
instance type most likely to be somewhere awkward.

Each instance therefore reports its own cluster view in its reply, and
the aggregator combines them:

- **Routed peers** are fully meshed, so every routed instance sees every
  other. Taking the largest reported peer count and adding one gives the
  number of routed instances - and it still counts a routed instance that
  went silent, because the ones that replied can still see it.
- **Leaves** attach to exactly one peer each, so the sets reported by
  different peers do not overlap and are summed.

This gives a useful transitive property: an instance is accounted for
whenever *any* replying instance can see it. The remaining blind spot -
a leaf whose peer also went silent - is already covered, because that
silent peer is itself missing from the expected set and marks the result
incomplete.

When no reply carries a usable cluster view, the aggregator marks the
result incomplete rather than assuming what it holds is everything.

*Alternative considered:* trusting the serving instance's local view
alone. Half the code, and it catches every missing routed peer - but its
blind spot is the leaf, which is the case this deployment shape creates
deliberately.

### One request subject, one reply per instance

Request published on a fixed subject with a per-request reply subject
(the same shape `nodetransport.Dispatch` already uses for per-request
correlation). Replies are collected until the deadline; the count of
replies is not known in advance, so the deadline is the only terminator.

The collection window is short (on the order of a second) and fixed in
code rather than configurable: it bounds a human-facing page load, and a
knob would invite tuning it to paper over an instance that is genuinely
unwell.

Instance identity is the instance's own generated id plus its hostname,
not its address - two instances can share an address across NATs, and an
address is what you *reach* an instance at, not what it *is*.

### Dependency checks reuse the existing Checker interface

Postgres and the bus already implement `runtime.Checker`. openvoxdb and
the CA client get thin `Checker` implementations over their existing
clients, so the status endpoint and the health endpoint report the same
thing about the same dependency rather than diverging.

Each instance reports the dependencies *it* can see, because that is the
operationally interesting answer: "openvoxdb is unreachable from
`web-2`" is a different and more useful statement than "openvoxdb is
unreachable". The page presents dependencies as reported by the serving
instance, and flags any dependency another instance disagrees about.

*Alternative considered:* checking dependencies once, centrally. Simpler
to present, but it would hide exactly the partial-connectivity failures
this page exists to surface.

### Uptime is reported as a start timestamp

Each instance reports when it started, not a computed duration. The
receiving instance renders the difference. A duration computed on the
sender and rendered later is wrong by however long the reply took, and
timestamps survive being passed around.

Clock skew between instances is a real possibility, and this exposes it
rather than hiding it - an instance whose reported start time is in the
future is showing the operator something true about their fleet.

## Risks / Trade-offs

- **The page shows a healthy stack while an instance is wedged** → a
  wedged instance may not reply, and would be absent. Mitigation: the
  incompleteness requirement. A result missing replies is marked, and the
  page says so. This is the single most important behavior in the change;
  without it the page is actively misleading during exactly the incident
  it exists for.

- **Status requests become a load amplifier** → one page load asks every
  instance, and an auto-refreshing page multiplies that. Mitigation: no
  auto-refresh in this change (explicit reload only), and the reply is
  small and computed from already-held state - no database query per
  reply.

- **`status:read` is missing from existing roles after upgrade** → an
  administrator on an existing deployment sees no status link until they
  grant it. Mitigation: documented in the change and in operations.md.
  Chosen deliberately over auto-granting to every existing role, which
  would silently widen access to topology detail.

- **Instance addresses are exposed to anyone with `status:read`** → this
  is the point of the page, but it is why it is permission-gated rather
  than open to any authenticated user.

- **A slow dependency check delays a reply past the window** → an
  instance whose Postgres is hanging could miss the collection deadline
  and be reported as not replying. Mitigation: each instance's dependency
  checks carry a timeout shorter than the collection window, so a slow
  dependency makes the instance report *unhealthy* rather than making it
  report *nothing*.

## Migration Plan

Additive and inert until used. No schema change, no configuration change,
no change to any existing endpoint.

On upgrade: a freshly bootstrapped deployment's administrator holds
`status:read` automatically. An existing deployment's administrator
grants it to the roles that should have it through the existing roles
page; until then the page and its nav link are simply not shown.

Rollback is removing the route and the nav link; nothing persists.

## Open Questions

- Whether the page should auto-refresh, and at what interval. Left off
  deliberately for now (see Risks); adding it later changes the page's
  JavaScript and no requirement.
- Whether to show each instance's connected-node count for
  `orchestrator`/`all` instances. The transport registry already knows
  it and the metrics endpoint already reports it, so it is a small
  addition - but it raises whether the page should show per-mode detail
  fields generally, which is worth deciding once rather than per field.

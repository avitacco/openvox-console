## Context

Phase 0 delivered the binary/Postgres/NATS/frontend skeleton but nothing
that shows real data. `docker-compose.yml` already has an `openvox` profile
(`openvoxserver` + `openvoxdb` + their own Postgres) that stands up a real
openvoxdb to develop and test against. See `proposal.md` for motivation and
the `openvoxdb-client`/`inventory`/`reporting` specs for requirements.

## Goals / Non-Goals

**Goals:**
- Get real node/fact/report/event data flowing from a live openvoxdb into
  the console UI, proving the shell works end to end before classification,
  RBAC, or orchestration build on top of it.
- Keep the PQL client thin and purpose-built rather than a general-purpose
  PQL engine - only the query shapes the three new capabilities actually
  need.

**Non-Goals:**
- No caching or mirroring of openvoxdb data into the console's own
  Postgres. openvoxdb is already the data warehouse (see
  architecture-summary.md); duplicating it here would create a second
  source of truth for no benefit at this phase.
- No change to the existing health check endpoint. Adding openvoxdb
  reachability there would need a MODIFIED delta on `service-runtime`,
  which the proposal doesn't declare - `openvoxdb-client`'s own per-request
  error reporting is sufficient for this phase.
- No write path to openvoxdb (node deactivation, fact submission, etc.) -
  this phase is read-only.
- No pagination/virtualization for very large result sets - see Open
  Questions.

## Decisions

**Hand-rolled, thin PQL client over `net/http`, not a third-party library.**
There is no actively-maintained canonical Go PQL/PuppetDB client library,
and the actual surface needed - POST a PQL query to
`/pdb/query/v4`, parse the JSON array response - is small enough that
pulling in a dependency for it would be the opposite of the "use an
established library" principle applied elsewhere (JWT, migrations): those
libraries earn their place because the protocols are easy to get subtly
wrong; a JSON-over-HTTPS query/response pair is not. `openvoxdb-client`
wraps this as `Query(ctx, pql string) (Result, error)`, with typed result
structs for the specific entities (`nodes`, `factsets`, `reports`,
`events`) the other two capabilities need - not a general PQL AST builder.

**Mutual TLS against the OpenVox CA, cert material from configured file
paths.** Matches architecture-summary.md's existing-CA reuse principle
(the same pattern Phase 6's node transport will use). For local dev against the
`openvox` compose profile, a client cert is generated the same way the
crafty reference stack documents:
`docker exec <openvoxserver-container> puppetserver ca generate --certname console`,
then the resulting cert/key/CA files are made available to the console
binary via new config fields (paths), not hardcoded. No code changes are
needed once certs exist; this is purely a configuration/operational step.

**New internal JSON API, consumed by plain JS + voxblocks - no SPA
framework.** The console binary exposes a small JSON API
(`/api/v1/nodes`, `/api/v1/nodes/{name}`, `/api/v1/nodes/{name}/reports`,
`/api/v1/reports/{id}/events`) backed by `openvoxdb-client`. The frontend
(already plain HTML/JS/voxblocks, no bundler - see Phase 0's design.md)
fetches from it and renders with voxblocks table/card components.
Alternative considered: server-rendered HTML fragments - rejected because
list/filter/detail interactions are naturally client-side and a small
`fetch()`-based approach doesn't need a templating engine to stay simple.
Alternative considered: a full SPA framework (React/Vue) - rejected for
the same reason Phase 0 chose plain web components: it's a heavier
dependency than three list/detail/filter views need, and voxblocks is
already the established UI building block.

**Always query openvoxdb live, no caching.** Simpler, and openvoxdb's own
PQL queries are indexed and fast enough for interactive use at this scale.
Revisit only if a real deployment shows latency problems (see Non-Goals).

## Risks / Trade-offs

- **[Risk]** Hand-rolling the PQL client risks scope creep into a general
  query builder. → **Mitigation**: expose only the specific typed queries
  `inventory` and `reporting` actually call; add new query shapes only when
  a new consumer needs one, not speculatively.
- **[Risk]** Local dev requires a manual cert-generation step against the
  `openvox` compose profile before the console can reach openvoxdb. →
  **Mitigation**: document the exact `puppetserver ca generate` command
  (already proven working against the current compose setup); config
  simply points at wherever the resulting files land, so this is a one-time
  setup step, not a recurring one.
- **[Risk]** No caching means every inventory/report view is a live
  openvoxdb round trip, adding latency and load on openvoxdb per page view.
  → **Mitigation**: acceptable for a single-console, dev/small-deployment
  scale; addressed later only if it's a real problem, not preemptively.

## Migration Plan

No schema migration - this phase doesn't touch the console's own Postgres
schema. Rollout is: merge, add openvoxdb connection config (URL + cert
paths), generate a console client cert against the running `openvoxserver`
CA, restart the console, confirm the inventory and report views show real
data from the `openvox` compose profile.

## Open Questions

- Pagination/virtualization strategy for node and report lists once they
  grow large - not needed for initial dev/small-deployment use, and adding
  it later doesn't change these specs' requirements (they don't specify a
  page size) or the task breakdown.
- Exact voxblocks components used per view (e.g. `vox-card` grid vs. a
  plain table for the node list) - a presentation detail, not a behavior
  the specs constrain.

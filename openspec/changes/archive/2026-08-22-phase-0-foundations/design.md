## Context

This is the first change in the project - there is no existing code, so this
design also fixes the base architectural shape every later phase builds on
top of. See `proposal.md` for motivation; see `architecture-summary.md` at
the repo root for the full rationale behind the single-binary and embedded-
NATS decisions this design implements.

## Goals / Non-Goals

**Goals:**
- Fix the internal package layout so later phases (inventory, classifier,
  RBAC, activity, code manager, orchestrator) each land as a new package
  plus schema additions, not a restructuring.
- Get all four foundation capabilities (service-runtime, messaging,
  persistence, web-shell) working together in one binary before any feature
  work starts.

**Non-Goals:**
- No NATS clustering/federation configuration (single embedded instance
  only; see architecture-summary.md - deferred to Phase 7).
- No actual schema tables beyond whatever the migration tool needs to track
  its own applied-migrations state - feature schemas land with their owning
  phases.
- No auth on the health check endpoint or any endpoint (Phase 3 adds auth
  middleware retroactively).

## Decisions

**Single binary, internal packages communicating by direct function call.**
Rather than separate services coordinating over localhost HTTP (Puppet
Enterprise's pattern), every capability is a Go package imported into one
`main`. This is the single largest structural decision in the project (see
architecture-summary.md #1) and Phase 0 is where the package boundaries
that make it work get established: each future capability gets its own
package with an explicit, small interface, even though everything runs
in-process.

**Embed NATS via the server library, not a subprocess.** Use
`github.com/nats-io/nats-server/v2/server` to start an in-process NATS
server (unclustered) and `github.com/nats-io/nats.go` for internal
publish/subscribe, both called directly from Go rather than shelling out to
a `nats-server` binary. This keeps the single-binary property intact.
Alternative considered: a separate NATS process managed via
docker-compose/systemd - rejected because it reintroduces the
multi-process coordination problem the architecture is explicitly avoiding.

**Postgres migrations via `golang-migrate/migrate`.** Widely used, supports
plain SQL migration files (easy to review/diff), and works as a library
(no separate CLI dependency at runtime) or a standalone CLI for local dev.
Alternative considered: a hand-rolled migration runner - rejected as
unnecessary given a mature library exists and migrations are not a place to
improvise.

**Frontend assets via Go's `embed.FS`.** Standard library, no external
asset server, matches the single-binary/easy-containerization goal
directly. The placeholder UI shell in this phase is intentionally minimal -
a static page confirming the pipeline works - not real feature UI.

**Frontend UI built on `voxblocks`.** Use `@openvoxproject/voxblocks`
(the OpenVox project's Lit-based web-component design system) as the
component library for all console UI, starting with this phase's
placeholder shell. Being plain web components, they embed into the
`embed.FS`-served static bundle the same as any other frontend asset, with
no framework-specific server runtime required. Alternative considered:
a general-purpose UI kit (e.g. a React/MUI or Vue-based library) - rejected
because voxblocks is the OpenVox project's own design system, so building
on it keeps the console visually and behaviorally consistent with other
OpenVox web properties, and it's a lighter dependency (web components plus
`lit`, no separate frontend framework runtime).

**Health check reports dependency status, not just process liveness.** The
endpoint checks Postgres and NATS reachability rather than just returning
200 if the process is running, so it's useful for load balancer / orchestrator
readiness checks in Phase 8's HA work, not just a liveness probe.

## Risks / Trade-offs

- **[Risk]** Embedding NATS and Postgres migration logic into binary
  startup increases the surface that must succeed before the process is
  "up." → **Mitigation**: startup sequence treats NATS and migrations as
  independent steps with their own error messages (per the service-runtime
  spec's fail-fast requirement), so failures are diagnosable rather than a
  single opaque startup error.
- **[Risk]** Establishing package boundaries now, before real features
  exist, risks guessing wrong about where the seams should be. →
  **Mitigation**: keep Phase 0 packages narrow (runtime/config/logging,
  messaging, persistence, web) and let each feature phase introduce its own
  package rather than retrofitting into these; architecture-summary.md's
  per-capability interface pattern (e.g. the orchestrator's agent-registry
  interface) is the model to follow when a seam is needed later.
- **[Risk]** `golang-migrate` and the NATS server library are both new
  dependencies the whole project will depend on indefinitely. →
  **Mitigation**: both are widely used, actively maintained libraries
  already named in architecture-summary.md's spirit of "use established
  libraries, don't hand-roll this category of problem."

## Migration Plan

Nothing to migrate from - this is the first code in the repository. Initial
rollout is: merge, build the container image, deploy to a dev environment,
confirm the health check reports both dependencies healthy.

## Open Questions

- Exact structured logging library (e.g. `log/slog` vs `zerolog`) - either
  satisfies the structured-logging requirement; pick during implementation.
- Container base image (`distroless` vs `scratch` vs slim `alpine`) - an
  operational choice that doesn't affect any spec behavior here.

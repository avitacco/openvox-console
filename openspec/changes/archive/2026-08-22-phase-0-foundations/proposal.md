## Why

OpenVox Console has no running skeleton yet. Every later phase (inventory,
classification, RBAC, code manager, orchestration) depends on a single Go
binary that starts, talks to Postgres, exposes a health check, and has an
embedded NATS server available for internal pub/sub. Building this shared
foundation first lets every subsequent phase land as an addition rather than
a restructuring.

## What Changes

- Stand up a Go project skeleton that builds to a single binary and a
  container image.
- Embed a NATS server (unclustered mode) in the binary; wire internal
  pub/sub so later phases can publish/subscribe without adding a broker.
- Add a Postgres connection layer and migration tooling, with one shared
  schema that later phases (classifier, RBAC, activity, code manager,
  orchestrator) will add tables to.
- Embed the frontend asset pipeline (`embed.FS`) and serve a minimal
  placeholder UI shell built on the `voxblocks` OpenVox component library.
- Add config loading, structured logging, and a health check endpoint.

## Capabilities

### New Capabilities
- `service-runtime`: process lifecycle for the console binary - config
  loading, structured logging, and a health check endpoint that reports
  Postgres and NATS connectivity.
- `messaging`: embedded, unclustered NATS server running in-process,
  available as an internal pub/sub layer for later phases to publish to and
  subscribe from.
- `persistence`: Postgres connection management and migration tooling
  against one shared schema used by all later phases.
- `web-shell`: embedded frontend asset pipeline serving a minimal
  placeholder UI shell from the single binary, built on the `voxblocks`
  OpenVox component library.

### Modified Capabilities
(none - this is the first change, no existing specs to modify)

## Impact

- New Go module/project structure, container build (Dockerfile or
  equivalent).
- New dependency: embedded NATS server library.
- New dependency: Postgres driver and migration library.
- New frontend dependency: `@openvoxproject/voxblocks` (and its `lit` peer
  dependency) as the UI component library.
- No impact on external contracts (the ENC API) yet - those land in
  later phases.
- Establishes the shared Postgres schema and internal NATS subjects that
  Phase 1 (Inventory), Phase 2 (Classifier), Phase 3 (RBAC), and Phase 4
  (Activity/Audit) build on directly.

## Why

Now that the console can run as several instances in different roles, an
operator has no way to see that topology from the console itself. Which
modes are running, how many of each, where they are, and whether each one
is healthy are all answerable only by reading deployment configuration
and curling each instance's `/health` in turn - and to do that you must
already know every instance's address, which is the thing you were trying
to find out.

The same gap covers the stack the console depends on. Postgres, openvoxdb
and the openvoxserver CA are each health-checked or exercised somewhere
in the binary already, but their state is scattered across the health
endpoint, per-request error responses, and log lines.

## What Changes

- Add a **status page** showing every running console instance: its run
  mode, address, health, uptime, version, and which background workers it
  is running.
- Show the **external dependencies** alongside them - Postgres, openvoxdb,
  and the openvoxserver CA - each with its configured target and current
  reachability.
- Gather instance data **live over the internal NATS bus**: the instance
  serving the request asks every other instance and aggregates the
  replies. No new table, no heartbeat writes, and nothing stale.
- Add a `GET /api/v1/status` endpoint returning that aggregate as JSON.
- Add a **`status:read` permission** gating both the endpoint and the
  navigation link. It is added to the bootstrap admin role, so a fresh
  install works; on an existing deployment an administrator grants it to
  whichever roles should have it.
- Report honestly when the picture is incomplete: an instance that does
  not answer within the collection window is reported as such, rather
  than silently omitted as though it were not running.

Explicitly **not** included: historical status (this is a live view, not
a time series), alerting, and the infrastructure nodes the console
manages - compilers and the CA host already appear on the Nodes page, and
duplicating them here would create two places that disagree.

## Capabilities

### New Capabilities

- `stack-status`: Reporting the live state of the stack - every running
  console instance with its role, location, health and workers, plus the
  external dependencies the console relies on - as an API and a console
  page, including how that picture is gathered and how an incomplete one
  is reported.

### Modified Capabilities

- `messaging`: The bus currently supports publish, fan-out subscribe and
  queue subscribe. Aggregating instance status needs a fourth pattern -
  ask every instance a question and collect the replies within a bounded
  window - which is new capability on the bus rather than a use of an
  existing one.
- `web-shell`: The permission-gated navigation requirement is written
  specifically for the admin section. The status section is the second
  such section, so the requirement generalizes to any nav link gated on a
  permission.

## Impact

- **Code**: new `internal/stackstatus` package (collection, aggregation,
  HTTP handlers); `internal/messaging` (request/collect support);
  `internal/app` (wire the responder into every mode, register the route
  and a new route group in the mode table); `internal/rbac` (the new
  permission in the bootstrap set); `frontend/templates/pages/status.tmpl`
  and `frontend/src/status.js`; the nav in `layout.html.tmpl`.
- **Every mode responds, every mode can serve.** The responder runs in all
  five modes - an instance that could not describe itself would be
  invisible on the page. The page and endpoint are part of the web
  surface.
- **Security**: instance addresses and cluster topology become visible to
  holders of `status:read`. That is operational detail, which is why it is
  a permission rather than being available to any authenticated user.
- **Compatibility**: additive. No existing endpoint, schema or
  configuration changes. An unclustered single instance sees exactly
  itself, which is the correct answer for that deployment.
- **Dependencies**: none new.

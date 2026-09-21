## Context

See proposal.md - Why. The constraints that shape the approach:

- **The frontend has no JS bundler.** `frontend/build.sh` copies
  `voxblocks`' self-contained CDN bundle and `src/*.{js,css}`, then runs
  `frontend/gen/main.go` - a build-time-only Go program - to render
  `templates/layout.html.tmpl` plus one `templates/pages/*.tmpl` per page
  into `internal/web/dist`. There is no npm build step to extend and no
  framework to adopt; the marketing site can mirror this pattern exactly.
- **`voxblocks` is already vendored** at
  `frontend/node_modules/@openvoxproject/voxblocks`, pinned by
  `frontend/package-lock.json`, and ships `dist/cdn/voxblocks.{js,css}`.
  It carries the components a marketing site needs - `vox-hero`,
  `vox-billboard`, `vox-card`, `vox-grid`, `vox-stat`, `vox-cta`,
  `vox-cta-band`, `vox-subnav`, `vox-callout`, `vox-toc`,
  `vox-step-indicator`, `vox-link-hub`, `vox-header`, `vox-footer` - so
  "use voxblocks wherever possible" is a matter of picking the right ones,
  not of building anything.
- **The dev stack already runs the whole product.** `docker-compose.yml`
  defines a `console` service alongside openvoxserver, openvoxdb and their
  Postgres instances, and `docker-compose.override.yml` builds the console
  from source. A browser on that compose network can reach `console:8080`.
- **The write paths seeding needs mostly exist.** openvoxdb accepts facts
  and reports over `/pdb/cmd/v1` (`internal/openvoxdb`'s `command` helper);
  the console exposes HTTP writes for groups, users, roles, service tokens,
  vulnerability providers and code deploys; and for what has no HTTP write
  contract, the feature's own store type does
  (`orchestrator.Store.CreateJob`/`RecordTargetResult`/`CompleteJob`).
- **Publication target** is the project's GitHub Pages site for
  `avitacco/openvox-console`, which is served under a `/openvox-console/`
  path prefix, not a domain root.

## Goals / Non-Goals

**Goals:**

- One build pattern shared with `frontend/`, so a contributor who has
  touched one can touch the other without learning a second toolchain.
- Screenshots that cannot silently become fiction: produced from the real
  UI, regenerable in one command, and byte-stable when nothing changed.
- Zero effect on the console binary, its tests, or its image pipeline.

**Non-Goals:**

- A documentation site. `SETUP.md`, `operations.md` and
  `architecture-summary.md` keep that job; the site links to them.
- A CMS, a blog, search, analytics, or any client-side framework.
- Visual regression testing. Capture produces marketing assets; it is not
  a test harness, and a screenshot diff is not a failing build.
- Localisation.

## Decisions

### 1. `marketing/` mirrors `frontend/`'s build, rather than adopting a site generator

A static site generator (Hugo, Eleventy, Astro) would bring layouts,
partials and asset pipelines for free, and a second toolchain, a second
dependency set and a second set of conventions with it.

**Decision:** `marketing/` reproduces the `frontend/` pattern -
`marketing/build.sh`, `marketing/gen/main.go`, `marketing/templates/`
(a shared `layout.html.tmpl` plus `pages/*.tmpl`), `marketing/src/*.css` -
emitting to `marketing/dist/`. The page list is a Go slice in
`gen/main.go`, exactly as `frontend/gen/main.go` holds the console's.

*Why:* the site is roughly seven static pages with no data behind them.
That is below the threshold where a generator earns its onboarding cost,
and the pattern being identical to `frontend/`'s is worth more here than
any feature a generator would add. `marketing/dist/` is a normal build
output directory rather than `frontend/`'s unusual `internal/web/dist`
target - that indirection exists only because `go:embed` cannot reach
above its own directory, and nothing embeds the marketing site.

*Alternative considered:* extending `frontend/gen` with marketing pages.
Rejected - it would put public pages in the binary's embed tree, which
the specs explicitly forbid, and entangle two independently published
artifacts.

### 2. `voxblocks` comes from `frontend/node_modules`, not a second install

`marketing/build.sh` copies `dist/cdn/voxblocks.{js,css}` out of
`frontend/node_modules/@openvoxproject/voxblocks`, running
`frontend/build.sh`'s `npm install` first if the directory is absent.

*Why:* the spec requires site and console to resolve the same `voxblocks`
version. A second `package.json` with its own range makes that a
coincidence maintained by hand; sourcing from the one lockfile makes it
structural. A design-system bump then stays a single decision.

*Alternative considered:* an npm workspace with a shared dependency.
Rejected as more machinery than one `cp` line needs.

### 3. Page set and component mapping

| Page | Covers | Principal components |
|---|---|---|
| `index.html` | what the console is, capability summary, get started | `vox-hero`, `vox-grid` + `vox-card`, `vox-stat`, `vox-cta-band` |
| `features/nodes.html` | inventory, reports, connectivity, certificates | `vox-billboard` (alternating `reverse`), `vox-callout` |
| `features/classification.html` | node groups, match rules, ENC | `vox-billboard`, `vox-step-indicator` |
| `features/code.html` | multi-source code deployment, deploy history | `vox-billboard`, `vox-code-block` |
| `features/orchestration.html` | tasks, plans, job runs | `vox-billboard`, `vox-timeline` |
| `features/security.html` | package inventory, vulnerability tracking | `vox-billboard`, `vox-grid` + `vox-stat` |
| `features/access-control.html` | RBAC, service tokens, audit trail | `vox-billboard`, `vox-card` |

Shared across every page: `vox-header` (with the same inline OpenVox
hexagon mark and `vox-theme-toggle` the console's layout uses),
`vox-subnav` for the capability pages, `vox-footer`, and `vox-toc` on any
page long enough to need one. A screenshot sits in `vox-billboard`'s
`media` slot, which is what that component's split media/copy layout is
for.

Hand-written CSS in `marketing/src/` is limited to what no component
covers: the screenshot frame (border, radius, shadow, theme swapping) and
page-level spacing. If a rule starts reproducing a component, that is the
signal to use the component.

### 4. Theme-matched screenshots via `<picture>`, not JavaScript

Each screenshot exists as a light and a dark PNG. The page emits a
`<picture>` with a `<source media="(prefers-color-scheme: dark)">` for the
dark asset and the light one as the `<img>` fallback.

The console's own explicit theme choice (`data-vox-theme`, from
`localStorage`) needs a small script to override that media query when the
visitor has used the theme toggle - the same three-state problem the
console's `layout.html.tmpl` already solves with its inline pre-paint
script. The site reuses that script's shape and additionally swaps the
`<img src>` when an explicit choice differs from the OS preference.

*Why `<picture>` first:* it works with JavaScript disabled and without a
flash, which a purely script-driven swap does not. The script is the
refinement, not the mechanism.

### 5. Capture uses `chromedp` against a pinned headless Chrome container

`marketing/capture/` is a Go program using `chromedp`, connecting over the
DevTools protocol to a `chromedp/headless-shell` service defined in
`docker-compose.screenshots.yml` - an overlay used alongside the main
compose file, never on its own, so nothing here starts during ordinary
`make up` and `docker-compose.yml` is untouched. The same overlay carries
the retention overrides decision 6a requires; both exist only for capture,
so they belong in one file rather than two mechanisms.

*Why Go + a pinned container:* it keeps capture inside the toolchain the
repository already has (no Node dependency, no Playwright browser
download), and pinning the browser image is what makes byte-identical
reruns achievable - a floating local Chrome would rerender text
differently after any update. Running the browser on the compose network
also means it reaches `console:8080` directly, with no host-gateway
plumbing.

*Alternative considered:* Playwright. Better screenshot ergonomics and
built-in waiting, at the cost of a second language's toolchain in a
repository that has deliberately avoided one.

### 6. Determinism is engineered, not hoped for

The spec's byte-identical requirement fails by default. Each cause is
addressed at capture time:

- **Relative timestamps** ("2 minutes ago") - the browser's clock is
  overridden to a fixed instant via the DevTools time-domain override, and
  the seed's timestamps are computed as offsets from that same instant.
  Both sides therefore agree run to run.
- **Animations and transitions** - a stylesheet disabling animation,
  transition and caret blink is injected before capture, and capture waits
  for the page to settle.
- **Loading indicators** - capture waits for the view's data to have
  rendered (a content selector, not a timer) before shooting.
- **Fonts** - the site's fonts are self-hosted rather than fetched from
  Google Fonts at capture time, so a network hiccup cannot produce a
  fallback-font screenshot. (The console's own layout fetches them
  remotely; the marketing site does not inherit that.)
- **Viewport** - fixed width, height and device scale factor per entry.
- **Per-run values** - IDs, tokens and hashes come from the seed, which is
  itself deterministic (decision 8).

### 6a. openvoxdb's retention is disabled for capture runs

The fixed instant in decision 6 is months in the past, and openvoxdb's
stock retention settings (`report-ttl` 14d, `node-ttl` 7d,
`node-purge-ttl` 14d, plus PuppetDB's own `resource-events-ttl` default)
treat data that old as already expired. Confirmed live against openvoxdb
8.15 with two byte-identical report payloads differing only in timestamp:

- The report row stores either way, so run statuses appear and the
  problem is easy to miss.
- Its **resource events are discarded at ingest**, so every report detail
  page would screenshot with an empty event list.
- `node-ttl` then deactivates the whole fleet on the next GC pass, and
  openvoxdb's nodes query excludes deactivated nodes unconditionally - the
  fleet disappears from the console between seeding and capture.

**Decision:** `docker-compose.screenshots.yml` sets all four retention
settings to 36500d for the duration of a capture run. Three are templated
from the environment by the openvoxdb image; `resource-events-ttl` is not,
so it arrives as an extra file in `conf.d`, which PuppetDB merges with the
stock `database.conf`.

*Why not move the instant to the present:* anchoring the seed to
`time.Now()` sidesteps retention entirely, but then every absolute
timestamp in the UI changes with the calendar, and a refresh a week later
rewrites every screenshot that shows a date. That trades the specs'
byte-identical guarantee for a configuration convenience.

*Consequence to document:* running plain `make up` afterwards restores the
defaults, and openvoxdb then garbage-collects the demo fleet on its own
schedule. Reseeding before capture is therefore part of the refresh
command, not an optional step.

*Note on corrective change:* `latest_report_corrective_change` is always
null here, for real agent-generated reports as much as seeded ones -
corrective-change tracking is a Puppet Enterprise feature. The dashboard's
"corrected" bucket is therefore genuinely 0 against open-source openvoxdb.
That is the product's real behavior and is left alone; the seed sets the
field on events and resources anyway, so nothing has to be unpicked if a
future openvoxdb starts honoring it.

### 7. One manifest drives both capture and the site

`marketing/screenshots.go` declares a `[]Shot` - name, console path, any
pre-capture interaction (click a tab, type a filter, select a row),
viewport, and the selector that signals the view has rendered. It is a Go
file in a package both `marketing/gen` and `marketing/capture` import.

*Why a shared Go declaration rather than YAML or JSON:* it makes the
spec's "the site cannot reference an image capture does not produce" a
compile-time property. `gen` renders `<picture>` elements from the same
slice capture writes, so a typo in a screenshot name is a build failure,
not a broken image on a published page. `gen` additionally fails if a
declared PNG is missing from `marketing/assets/screenshots/`, and capture
fails on any image exceeding the size bound.

### 8. `cmd/demo-seed` writes through three tiers, in that order of preference

1. **openvoxdb's command API** - `replace facts`, `store report`,
   `replace catalog` per node, reusing `internal/openvoxdb`. This covers
   the fleet itself: dashboard, node list and detail, reports, inventory
   queries, package inventory.
2. **The console's HTTP API** - `POST /api/v1/groups`, `/users`, `/roles`,
   `/service-tokens`, `/vulnerability-providers`, `/code-deploys`,
   authenticated with the bootstrap admin via `POST /api/v1/auth/login`.
   Code deploys run for real against the existing
   `make code-sources-fixture` repos, so deploy history is genuine.
3. **The feature's own store type**, imported directly, for what has no
   HTTP write contract - orchestration job history via
   `orchestrator.Store`, and vulnerability findings via the same writer
   the provider sync uses. `cmd/demo-seed` is in this module, so these are
   ordinary function calls.

*Why not raw SQL for tier 3:* hand-written inserts keep succeeding after a
schema change while meaning something different - the failure mode the
spec names. Going through the store types means a schema change breaks the
build instead.

*Why not real agents for tier 3:* job history and connectivity would be
genuine, but the fleet would be however many agent containers the machine
can run, and the screenshots would show three nodes. The realistic-looking
fleet is the point.

**Honesty boundary:** tier-3 data is fabricated history, not a record of
work that happened. It is fine in a screenshot of the jobs page; it would
not be fine as evidence of anything. The seed's documentation says so.

### 9. The seed refuses a non-local target

Refusal checks the target's URL host resolves to a loopback address or a
private range and that an explicit `--i-know-this-is-a-demo-console` style
confirmation is present, refusing with a message naming the target
otherwise. Fabricated fleet data written into a real deployment is
corruption of that deployment's inventory.

### 10. Site links are relative; publication is a separate workflow

Every internal link and asset reference is relative (`./`, `../`), so the
site works from `/openvox-console/` and from a domain root without a
configured base URL.

`.github/workflows/marketing.yml` is new and independent: on a push to
`main` touching `marketing/`, it runs `marketing/build.sh` and publishes
`marketing/dist/` with `actions/deploy-pages`. It is not added to
`ci.yml`, so a site build failure cannot block an image publish, and the
previously published site simply remains.

Screenshot capture does **not** run in CI. It needs the full compose stack
and a seeded console; running that per-push to regenerate images that are
already committed would be slow and would defeat the byte-stability the
images are meant to have. Refresh is a deliberate local act, documented in
`marketing/README.md` and reachable as `make marketing-screenshots`.

## Risks / Trade-offs

- **Screenshots go stale after a UI change and nothing notices.** → The
  refresh is one command and documented next to the site; the seed and
  capture are kept working as part of that. Accepted residual risk: a
  contributor can still change the UI and not refresh. CI cannot close
  this without running the whole stack per push.
- **Byte-identical capture proved harder than decision 6 anticipated, and
  the requirement was relaxed.** Measured across two full refreshes, 14 of
  20 images are byte-identical. The other six move for two reasons, both
  in the product rather than the capture tool: the activity log stamps its
  entries from the real clock, and `PackageNameCounts` and the role
  listing issue grouped queries with no `ORDER BY`, so tied rows can come
  back in a different order. → Accepted rather than fixed. Both are
  addressable - a clock seam on the activity log, a total order on those
  queries - but doing so would change the console to suit its own
  marketing photographs. The screenshot-capture spec now requires capture
  to control what belongs to screenshotting (browser, clock, animation,
  fonts, viewport, waiting on content) and explicitly does not require
  byte-identical output.

  Worth noting separately: the unordered queries are visible to real users
  as a list that can reshuffle between page loads. That is a product
  observation this change surfaced, not a screenshot problem, and belongs
  in its own change if it is worth fixing.
- **Committed PNGs grow the repository.** → Two themes for roughly a dozen
  views, size-bounded per image, lossless-optimised at capture. Refreshes
  replace rather than accumulate, but git history keeps every version.
  Accepted: the alternative - generating them in CI - reintroduces the
  whole stack per push.
- **Tier-3 seeded history is fabricated.** → Bounded to orchestration and
  findings, written through the real store types, and documented as
  fabricated. It never reaches a real deployment (decision 9).
- **`marketing/build.sh` depends on `frontend/node_modules`.** → It runs
  `frontend/build.sh`'s install step when the directory is missing, so a
  clean checkout works; the coupling is deliberate (decision 2).
- **A second HTML templating surface to maintain.** → Mitigated by being
  the same pattern, not a different one. Shared boilerplate between the
  two layouts is duplicated rather than abstracted: two files that look
  alike are easier to reason about than a shared template with conditional
  branches for a console chrome the marketing site never wants.

## Migration Plan

Nothing to migrate: every artifact is new, and no existing behavior
changes. Rollback is deleting `marketing/`, `cmd/demo-seed`, the new
`Makefile` targets, the compose `screenshots` profile service, and the new
workflow - the console binary, its tests and its images are untouched
throughout.

Enabling GitHub Pages for the repository (Settings → Pages → source
"GitHub Actions") is a one-time manual step the workflow cannot do for
itself; until it is done, the workflow builds and fails to publish.

## Open Questions

- The exact fleet size and OS mix the seed generates. Roughly 25 nodes
  across a handful of distributions reads as a real deployment at the
  dashboard's scale, but the number is worth tuning once the first
  screenshots exist. Changing it affects neither the specs nor the task
  breakdown.
- Whether `features/security.html` should split into separate package
  inventory and vulnerability pages. Deferred until there is enough copy
  to judge; splitting later is adding a page to a slice and a subnav link.

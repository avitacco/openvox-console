# Marketing site

The public site describing what OpenVox Console does, built from the
console's own screenshots. Published to GitHub Pages from `main` by
`.github/workflows/marketing.yml`.

Nothing here is part of the console binary. The site is a sibling of
`frontend/`, not part of it, and neither this directory nor
`cmd/demo-seed` appears in any shipped artifact - there is a test that
asserts exactly that (`internal/demodata`'s `TestDemoDataIsNotShipped`).

## Layout

```
marketing/
  build.sh              builds the site into dist/
  gen/                  renders templates/ into HTML (build-time only)
  templates/            layout.html.tmpl + pages/*.tmpl
  src/                  site.css, theme.js
  shots/                the screenshot manifest - what gets captured
  capture/              drives a headless browser to take the screenshots
  screenshots.sh        the full refresh: demo console, seed, capture
  assets/screenshots/   the committed PNGs
```

## Building

```sh
make marketing          # -> marketing/dist/
```

`voxblocks` comes from `frontend/node_modules`, not a second
`package.json`. The site advertises the console, so the two must render
with the same component versions; taking both from one lockfile makes
that structural rather than something to remember.

The build **fails** if a screenshot a page references is missing, or if a
page asks for a name the manifest does not declare. A forgotten capture
stops the build instead of publishing a broken image.

## Refreshing the screenshots

```sh
make screenshots-up         # openvoxdb retention off, headless browser up
make marketing-screenshots  # demo console + seed + capture
make screenshots-down       # back to normal
```

That is the whole procedure. It takes a few minutes, most of it the two
browser passes.

### Why it runs its own console

Screenshots must come from a console holding nothing but the demo fleet.
A development console accumulates job rows, deploys and user accounts
from test runs and daily work, and those outrank demo data in every
"newest first" list - a capture against one publishes somebody's leftover
test fixtures. `screenshots.sh` therefore creates its own database,
starts a console against it on its own port, seeds that, captures, and
stops it. Your own console is never touched and need not be running.

### Why openvoxdb's retention has to be turned off

The demo fleet is dated to a fixed instant in the past
(`internal/demodata.Instant`), which is what makes two capture runs
produce identical images: "4 minutes ago" stays "4 minutes ago". With
openvoxdb's stock 14d/7d retention, data that old is born expired - its
resource events are discarded at ingest (so report pages screenshot
empty) and the fleet is deactivated on the next GC pass (so it vanishes
from the console entirely). `docker-compose.screenshots.yml` sets those
TTLs aside for the duration of a capture.

Running plain `make up` afterwards restores them, and openvoxdb will
garbage-collect the demo fleet on its own schedule. Reseeding is part of
the refresh command, so this is not something to remember.

### One-time setup

- `make g10k-install` - the Code page needs real deploys to show.
- `make code-sources-fixture` - creates the two control repositories the
  seed deploys from.
- Settings → Pages → Source → "GitHub Actions" on the repository, once,
  before the workflow can publish. No workflow can do this for itself;
  until it is done the site builds and fails at the deploy step.

### Determinism

Two runs against the same seeded console produce byte-identical images,
so a refresh that changes nothing produces no diff. That is not a
coincidence and it is easy to break. What holds it up:

- the browser is a pinned container image, not whatever Chrome is
  installed locally;
- the page's clock is frozen to the demo instant, so relative and
  absolute times render the same words every run;
- animations, transitions and the caret are disabled before capture;
- capture waits on a content selector and on `document.fonts.ready`,
  never on a timer;
- the viewport is fixed per shot;
- every id, hash and timestamp the seed writes is derived, never random.

If you add something to the console that varies per render - a relative
timestamp computed from the real clock, a random id, an animation that
does not respect the stillness stylesheet - the images will start
churning on every refresh. That is the signal, not a flake.

### Adding a screenshot

1. Add a `Shot` to `shots/shots.go`: its name, path, a ready selector,
   and a `MustContain` string **only the demo fleet produces**. A generic
   word like "unchanged" is also produced by leftover test data, so the
   check would pass on a contaminated console.
2. Reference it from a page template with `{{screenshot "name"}}`.
3. Run the refresh.

### Images are bounded

Each PNG must stay under `shots.MaxBytes` (1.5 MB) and capture fails on
one that does not. They are committed and replaced on every refresh, so
an oversized image grows the repository's history permanently.

## An honest note about the demo data

The fleet, its facts and its reports are fabricated but internally
consistent: they are written through openvoxdb's real command API, and
every view computes over them exactly as it would over a real fleet.

**Orchestration job history and vulnerability findings are invented.**
No task was dispatched and no node ran anything; the jobs are written
directly through the orchestrator's own store because there is no way to
record a job that already happened, and the demo fleet has no live nodes
to run one. The same goes for findings: real CVE identifiers, matched
against packages the demo fleet genuinely reports, but the pairing is
made up.

That is fine for a screenshot of the Jobs page. It is not evidence of
anything, and it must never be presented as a benchmark, a security
assessment, or a record of work performed.

Everything the seed creates is recognisably fictional - hostnames under
`example.com` (reserved by RFC 2606), addresses in RFC 1918 space, people
named after the role they play. There are tests asserting that, because a
one-off review does not survive somebody adding a node next year.

# Marketing site

The public site describing what OpenVox Console does, built from the
console's own screenshots.

**It is built here and committed, not built in CI.** `make marketing`
regenerates `docs/`, you look at the result, and you push it. GitHub
Pages serves `docs/` straight from the branch, so what is published is
something a person has actually seen - not whatever a workflow made of
the last commit.

Nothing here is part of the console binary. The site is a sibling of
`frontend/`, not part of it, and neither this directory nor
`cmd/demo-seed` appears in any shipped artifact - there is a test that
asserts exactly that (`internal/demodata`'s `TestDemoDataIsNotShipped`).

## Layout

```
marketing/              the source
  build.sh              builds the site into ../docs
  gen/                  renders templates/ into HTML (build-time only)
  templates/            layout.html.tmpl + pages/*.tmpl
  src/                  site.css, theme.js
  shots/                the screenshot manifest - what gets captured
  capture/              drives a headless browser to take the screenshots
  screenshots.sh        the full refresh: demo console, seed, capture

docs/                   the built site, committed, served by Pages
  index.html            generated - do not edit
  features/*.html       generated - do not edit
  site.css, theme.js    copied from marketing/src
  vendor/               copied from the frontend's voxblocks
  assets/screenshots/   the PNGs - content, not build output
```

`docs/assets/screenshots` is the one thing under `docs/` that the build
does not generate. Capture writes the images straight there rather than
into `marketing/` to be copied at build time, so they are committed once
instead of twice - 2MB either way, but only one copy churning per
refresh. `build.sh` removes only what it generates, so a rebuild never
takes the screenshots with it.

## Building

```sh
make marketing          # regenerate docs/
make marketing-serve    # look at it: http://localhost:8777/
git add docs && git commit && git push
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
(`internal/demodata.Instant`), so that "4 minutes ago" stays "4 minutes
ago" however long after seeding the capture runs. With
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
- Settings → Pages → Source → "Deploy from a branch", branch `main`,
  folder `/docs`. Once, on the repository. Nothing publishes until that
  is set.

### What capture pins, and what it does not

Capture controls the things that belong to screenshotting rather than to
the product:

- the browser is a pinned container image, not whatever Chrome is
  installed locally;
- the page's clock is frozen to the demo instant, so relative and
  absolute times render the same words every run;
- animations, transitions and the caret are disabled before capture;
- capture waits on a content selector and on `document.fonts.ready`,
  never on a timer;
- the viewport is fixed per shot;
- every id, hash and timestamp the seed writes is derived, never random.

Images are **not** expected to be byte-identical between runs. Measured
across two full refreshes, 14 of 20 were; the other six move because the
activity log stamps its entries from the real clock, and because a couple
of list queries impose no total order, so tied rows can come back in a
different order. Both are real product behavior, and bending the console
to suit its own marketing photographs would be the wrong trade. Expect a
refresh to produce a small diff even when nothing has changed.

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

## Translations

The site is published in English plus the locales listed in
`siteLocales` (marketing/gen/main.go) and `LOCALES` (build.sh). English
is the site root; each other locale is a directory under it
(`docs/de/index.html`). Assets - CSS, scripts, screenshots - stay at the
root and are shared, so a locale costs its pages and its captures and
nothing else.

The list leads with the world's most spoken languages - English,
Mandarin, Hindi, Spanish, French - which is the coverage this site is
meant to have. German, Japanese and Arabic follow them: they were
translated first, and between them they are what proves the pipeline
handles left-to-right, CJK and right-to-left.

Still fewer than the console's fifteen, and worth keeping in mind before
extending it: the console's strings are labels, these are argument, and
a machine-translated argument reads worse than an untranslated one.
Every locale here wants review by somebody who reads it.

### How a string becomes translatable

The same three markings the console uses, so there is one convention to
learn and one extractor to run:

```html
<h2 data-i18n>What it does</h2>
<vox-cta-band heading="Run it yourself" data-i18n-attr="heading">
<p data-i18n-html>Run <code>docker compose up</code> first.</p>
```

Page titles and meta descriptions are a Go table rather than markup, so
they are marked with `N_("...")` in gen/main.go and extracted from the
Go source. Without the marker they stay English on every translated
page, which is the one failure this convention exists to prevent.

Unlike the console, substitution happens at **build time**: gen renders
the page, then rewrites the marked nodes (translate.go) and writes
static HTML. A visitor downloads no catalogue and runs no translation
code.

### Workflow

```sh
# after editing any marked text
cd frontend && go run ./i18n \
  -source ../marketing/templates,../marketing/gen \
  -locales ../marketing/locales -pot site.pot extract

# fold the new strings into every .po, keeping existing translations
... merge          # same flags
... status         # what is still untranslated
... add <code>     # start a new locale (see frontend/i18n/languages.go)
```

`marketing/build.sh` compiles the catalogues itself, so a normal
`make marketing` needs none of the above.

### Screenshots are per locale

`make marketing-screenshots` captures every shot in every locale in
`CAPTURE_LOCALES` against one seeded console - the language is a browser
preference, so nothing about the data changes between passes. English
keeps its unsuffixed filenames (`nodes-light.png`); the rest are
suffixed (`nodes-light-de.png`).

gen refuses to build a locale whose captures are missing, for the same
reason it refuses an undeclared shot: a broken image should fail the
build, not the published page.

## Accessibility

The target is **WCAG 2.2 level AA**.

```sh
make screenshots-up    # the audit needs the headless browser
make marketing-a11y
```

`marketing/a11y` loads every page in a real browser and runs axe-core
over it in **both themes at both a desktop and a phone viewport** - 28
renders. Contrast depends on the theme and reflow depends on the width,
so a single pass would miss half of what there is to find. It also makes
three checks axe cannot: reflow at 320px (SC 1.4.10), survival of the
WCAG text-spacing overrides (SC 1.4.12), and that the skip link exists,
points at something real, and appears when focused (SC 2.4.1).

**It asserts that the rules it cares about actually ran.** `target-size`
(SC 2.5.8) ships *disabled* in axe-core, so selecting the `wcag22aa` tag
is not enough to run it - and a rule that does not run reports no
violations, which is indistinguishable from passing. The audit fails if
`target-size` or `color-contrast` is missing from the results.

### Accepted exceptions

One, recorded in `accepted` in `marketing/a11y/main.go` with its reason:
axe's best-practice `region` rule flags the skip link for sitting outside
every landmark, which is exactly where a skip link has to be. It is
suppressed for that element only; any other `region` finding still fails.

### What was found and fixed

- **Header GitHub link at 1.97:1.** A plain `<a>` slotted into
  `vox-header` gets the browser's default `#0000ee`, because the header's
  shadow styles do not reach it. Now styled from `site.css`, which can,
  because slotted elements live in the light DOM.
- **Footer bottom row at 4.37:1.** `vox-footer` styles it with
  `--vox-color-text-3` at 13px, which in the light theme is just under
  the 4.5:1 minimum. Overridden to `--vox-color-text-2` (5.48:1).
  **This is a voxblocks issue, not ours** - the console's own footer has
  it too, and it is worth reporting upstream rather than every site
  patching around it.
- **The call-to-action band sat outside every landmark**, so anyone
  navigating by landmark skipped the page's primary actions. `<main>` now
  wraps it.
- **No skip link.** A keyboard user tabbed through the header and six
  subnav links before reaching content, on every page. Added.
- **No reduced-motion handling.** Added, covering the voxblocks
  components' transitions as well as our own.

### Reviewed by hand, because no tool can check it

- **Alt text says what the console is showing**, not which page it is:
  "Node groups listed with their environment, priority and the number of
  nodes each rule currently matches", not "Groups page". The text lives
  in the shot's `Caption` in `marketing/shots`.
- **Heading structure.** One `h1` per page (rendered by `vox-hero` into
  its shadow DOM), `h2` for each section, no levels skipped.
- **Link text makes sense out of context** - "Setup guide", "Source on
  GitHub", "Container images" - since a screen reader can list links
  with no surrounding prose.
- **Focus order follows the visual order**, and the phone layout's one
  unfocusable link is the header's GitHub link, which `vox-header`
  collapses into its menu at that width. A link that is not rendered
  should not be focusable, and the same destination stays reachable from
  the footer and the closing call to action.

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

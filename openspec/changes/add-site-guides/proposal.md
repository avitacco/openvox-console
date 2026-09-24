## Why

The marketing site shows what the console does, but it can't take someone
any further. Every "how do I…" answer lives in the repository instead:
`SETUP.md` (a ~930-line install walk-through that interleaves the container
and package paths) and `operations.md` (where run modes, clustering and
runbooks sit between engineering notes such as "real-node install bugs
found via an actual VM"). A visitor convinced by the site has to leave it,
find the right Markdown file on GitHub, and read it in English, whatever
language they were browsing the site in. Scaling out has no guide at all
beyond a reference section, even though clustering now carries the node
transport as well as the bus, and it needs TLS, two secrets and a
per-mode split to get right.

This change makes the site the place the product is documented, not just
advertised.

## What Changes

- **Guides section on the site.** A guides landing page and step-by-step
  guides, grouped by what the reader is trying to do:
  - **Install:** a planning page (what you are building, what to decide
    first), *Install with containers*, and *Install from packages and
    binaries*. Each is a complete path from an empty host to a working
    console with openvoxserver, openvoxdb and Postgres, credentials,
    configuration, first start and ENC hookup.
  - **Nodes:** add nodes (the install script on Linux, macOS and Windows,
    signing, verifying the connection and the first run), manage node
    certificates (sign, revoke, clean, and what revocation does to a
    connected node), and remove a node.
  - **Scale:** run additional instances and split into modes: clustering,
    peer TLS and secrets, `web`/`orchestrator`/`worker` split, `enc`
    instances attached as leaves next to the compilers, and the migration
    path from one instance.
  - **Use:** classify nodes with groups, deploy code, run jobs, manage
    users, roles and service tokens, and set up vulnerability providers.
  - **Operate:** the runbooks currently in `operations.md`: Postgres
    failover, RBAC signing key rotation, and upgrading Postgres 17 to 18.
- **Guides are written in Markdown** in the repository and rendered by the
  site's existing generator into the same layout, design system, themes and
  accessibility floor as the feature pages. They use voxblocks components
  for sidebar navigation, an on-page table of contents, previous/next links,
  copyable code blocks, callouts and per-OS tabs.
- **Guides are translated into all eight site locales** through the
  existing extract/merge/compile pipeline. Prose is extracted block by
  block. Code blocks are never extracted, and inline code must come back
  from a translation byte-for-byte unchanged, so no locale can carry a
  command, path or setting name that differs from English. A block without
  a translation falls back to English, and a guide that isn't fully
  translated says so.
- **Guides are checked against the product at build time.** Every
  `CONSOLE_*` setting a guide names must be one the console reads, every
  internal link and anchor must resolve, and every screenshot must be
  declared and captured. A guide that has drifted from the console fails
  the build instead of misleading a reader.
- **Feature pages link to the guide that does what they describe**, and the
  site's call to action points at the install guides rather than `SETUP.md`
  on GitHub.
- **`SETUP.md` becomes a short pointer** to the published install guides,
  and **`operations.md` keeps only engineering notes**: the user-facing
  sections (run modes, multi-instance topology, node certificate revocation,
  bus monitoring, migration path, runbooks) move into guides. `README.md`'s
  links are updated to match. **BREAKING** for anyone who bookmarked
  sections of those files: the content moves, and the files link to its new
  location.

## Capabilities

### New Capabilities

- `site-guides`: step-by-step user guides published on the site: which
  guides exist and how they are organised and navigated, their Markdown
  source and rendering, their translation (including the rule that code is
  never translated), the build-time checks that keep them true to the
  console, and the site becoming the canonical home for user-facing
  documentation.

### Modified Capabilities

None. `marketing-site` is still being introduced by the in-flight
`add-marketing-site` change and is not yet in `openspec/specs/`. Nothing in
its requirements conflicts with adding guides: guides extend its navigation
and satisfy its "setup instructions" link rather than changing either. This
change should be archived after `add-marketing-site`.

## Impact

- **`marketing/`:** new `guides/` Markdown sources; a shared Markdown
  rendering package used by both the generator and the extractor; the
  generator (`marketing/gen`) gains guide pages, guide navigation and the
  drift checks; layout and CSS changes for the guides section; `build.sh`
  cleans and rebuilds `docs/guides/`.
- **`frontend/i18n`:** the extractor learns `.md` sources, using the same
  rendering as the generator so message IDs match exactly.
- **`marketing/locales/*.po`:** a large number of new strings, with
  translations for seven locales that need review by native readers before
  publishing, as the site's README already requires.
- **`marketing/shots`:** new screenshots for the Use and Nodes guides, which
  means a demo-seed and capture refresh.
- **Accessibility audit (`marketing/a11y`):** now covers the guide pages.
- **Dependencies:** adds a Go Markdown library (goldmark), build-time only.
  Nothing is added to the console binary.
- **Repository docs:** `SETUP.md` and `operations.md` restructured,
  `README.md` links updated.
- **No change** to the console binary, its API, or anything it serves.

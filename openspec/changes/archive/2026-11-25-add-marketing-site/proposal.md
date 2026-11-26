## Why

The console is now feature-complete enough to show rather than describe -
node inventory, classification, code deployment, orchestration, package and
vulnerability tracking, RBAC and audit - but the only way to find that out
is to read `architecture-summary.md` or stand the whole stack up. Anyone
evaluating OpenVox Console has no page to look at that answers "what does
this actually do, and what does it look like" in under a minute.

This change adds a public marketing site that answers that with the
product's own screenshots, and the tooling to keep those screenshots
honest: they are captured automatically from a real console running against
a real openvoxserver/openvoxdb stack, so they cannot drift into being
mockups of a UI that no longer exists.

## What Changes

- **New `marketing/` static site**, built and published independently of
  the console binary. A landing page plus one page per major capability
  (nodes and inventory, classification, code deployment, orchestration,
  security - packages and vulnerabilities, and access control and audit),
  sharing a header, subnav and footer.
- **Built from `voxblocks` throughout** - the same design system the
  console's own frontend uses, pinned to the same version, so the site and
  the product it advertises look like one thing. `vox-hero`,
  `vox-billboard`, `vox-card`, `vox-grid`, `vox-stat`, `vox-cta-band`,
  `vox-subnav`, `vox-callout`, `vox-toc`, `vox-step-indicator` and
  `vox-footer` carry the page structure; hand-written CSS is a fallback for
  what the library genuinely has no component for, not a starting point.
- **Automated screenshot capture** (`make marketing-screenshots`): a
  headless-browser script logs into a locally running console, visits a
  declared list of views, and writes deterministic PNGs - light and dark
  theme for each - into the site's asset directory, where they are
  committed. Re-running it refreshes every shot in one step.
- **A synthetic demo fleet** (`make demo-seed`): a new `cmd/demo-seed`
  populates a plausible multi-OS fleet through the same external contracts
  any other client would use - openvoxdb's commands API for facts and
  reports, the console's own HTTP API for groups, users, jobs, deploys and
  findings - so screenshots show a populated product rather than a
  one-node dev stack and a wall of empty states.
- **A GitHub Pages workflow** publishing the built site on pushes to
  `main`, separate from the existing image-building CI jobs.
- No change to the console binary, its embedded frontend, or anything it
  serves. The site is a sibling of `frontend/`, not part of it.

## Capabilities

### New Capabilities

- `marketing-site`: the public, unauthenticated static site describing the
  console's capabilities - its page set and navigation, its use of the
  shared design system, its screenshot presentation and theme behavior, its
  accessibility and responsiveness floor, and how it is built and published.
- `screenshot-capture`: automated capture of the console's own UI as
  committed image assets - which views are captured, how they are named and
  themed, determinism between runs, and the requirement that no real or
  sensitive data reaches a published image.
- `demo-data-seeding`: a repeatable synthetic dataset for a local console,
  written through the product's own external contracts rather than into its
  database, giving screenshot capture (and manual demos) a populated fleet.

### Modified Capabilities

None. The console's served frontend (`web-shell`) and every runtime
capability behave exactly as before - this change adds a separate site and
two development-time tools, and touches no served behavior.

## Impact

- **New directories**: `marketing/` (templates, page generator, CSS,
  screenshot assets, build script), `cmd/demo-seed/`,
  `marketing/capture/` for the capture script.
- **New dependencies**: a headless-browser driver for capture. Confined to
  the capture tool - it must not enter the console binary's module graph in
  a way that affects the shipped build.
- **`Makefile`**: new `marketing`, `marketing-screenshots` and `demo-seed`
  targets.
- **`.github/workflows/`**: a new Pages publishing workflow. The existing
  `ci.yml` is untouched, so image builds and tests are unaffected.
- **`frontend/`**: unchanged, but the marketing site takes its `voxblocks`
  version from the same lockfile discipline, so a design-system bump is a
  single coordinated decision rather than two drifting ones.
- **Repository size**: committed PNG screenshots, in two themes for each
  captured view. Sizes are bounded explicitly so the repo does not grow
  without limit each time shots are refreshed.

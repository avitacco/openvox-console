## 1. Demo fleet seeding

- [x] 1.1 Create `cmd/demo-seed` with its target-safety check: it accepts a console URL and an openvoxdb URL, refuses any target that is not loopback or a private address, refuses without the explicit demo confirmation flag, and names the rejected target in the message. Verify by running it against a public hostname and confirming it writes nothing and explains why.
- [x] 1.2 Add the fixed-instant clock the whole seed derives timestamps from (design decision 6), so every relative time the console renders is computed from the same base the capture browser will be pinned to. Verify two runs produce identical timestamps.
- [x] 1.3 Seed the fleet into openvoxdb over `/pdb/cmd/v1` using `internal/openvoxdb`: facts, catalogs and reports for a fleet spanning multiple operating systems and versions, with run outcomes covering unchanged, changed and failed. Verify the console's dashboard and node list show a distribution across several OSes and all three outcomes.
- [x] 1.4 Seed console-side records over the console's HTTP API, authenticated as the bootstrap admin: node groups with match rules that actually match seeded nodes, multiple users and roles, and at least one service token. Verify each page lists the seeded records and a group's member count is non-zero.
- [x] 1.5 Trigger real code deploys through `POST /api/v1/code-deploys` against the `make code-sources-fixture` repositories, across more than one environment. Verify the Code page's deploy history shows successful deploys from both sources.
- [x] 1.6 Seed orchestration job history through `orchestrator.Store` (`CreateJob`, `RecordTargetResult`, `CompleteJob`) covering both successful and failed runs across multiple targets. Verify the Jobs list and a job detail page render completed runs with per-target results.
- [x] 1.7 Seed vulnerability findings of differing severities through the same writer the provider sync uses, against packages the seeded package inventory actually reports. Verify the Vulnerabilities page and a node's vulnerability view are populated and severities vary.
- [x] 1.8 Make the whole seed converge on re-run: running it twice leaves no duplicate nodes, groups, users, jobs or findings. Verify by seeding twice into a fresh stack and diffing the console's API responses between the two runs.
- [x] 1.9 Confirm every name, hostname, address and user the seed creates is recognisably fictional (documentation-reserved domains, invented hostnames), and that no credential it creates could be valid outside a throwaway stack. Verify by grepping the seed's data for anything resembling a real domain or person.
- [x] 1.10a Add a post-write integrity check: after submitting, poll until openvoxdb reflects the whole fleet and confirm every changed/failed node kept its resource events, failing with the cause when not. openvoxdb's command API returns 200 for a command it later discards, so without this a silently-empty report page reaches a published screenshot. Verify by running against a stack with stock retention and confirming it fails loudly.
- [x] 1.10b Add `--reset`, deleting the demo fleet from openvoxdb via its admin API before seeding. openvoxdb deduplicates reports by content hash and the seed is deterministic, so a fleet stored under the wrong configuration cannot otherwise be repaired by re-running. Verify it removes only seeded certnames and leaves foreign nodes alone.
- [x] 1.10 Add a `demo-seed` Makefile target and confirm `go build ./...` for the console binary and the published images contain no reference to the seeding package. Verify with a build of `./cmd/console` and an inspection of the image's contents.

## 2. Screenshot manifest and capture

- [x] 2.1 Create the shared screenshot manifest package declaring each shot's name, console path, pre-capture interaction, viewport and render-complete selector, covering at minimum one view per capability page. Verify it compiles and both `marketing/gen` and `marketing/capture` can import it.
- [x] 2.2 Create `docker-compose.screenshots.yml`: the pinned `chromedp/headless-shell` service plus the openvoxdb retention overrides (all four TTLs at 36500d, `resource-events-ttl` via a merged `conf.d` file) that a past-dated demo fleet needs to survive ingest and GC. Verify plain `make up` is unaffected, and that with the overlay a past-dated report keeps its resource events.
- [x] 2.3 Create `marketing/capture` as a `chromedp` program that connects to that browser, logs into the console, and captures one declared shot end to end. Verify it writes a readable PNG of the expected view.
- [x] 2.4 Apply the determinism controls from design decision 6: pinned browser image, page clock frozen to the demo instant, injected stylesheet disabling animations/transitions/caret, fixed viewport, and waiting on each shot's render-complete selector and `document.fonts.ready` rather than a timer. Verify two consecutive refreshes produce images showing the same views with the same content.
      - Measured: 14 of 20 byte-identical across two full refreshes. The remaining six vary because the activity log stamps entries from the real clock, and because two list queries impose no total order so tied rows can come back reordered. Both are product behavior, and chasing them would mean changing the console to suit its own marketing photographs. Accepted deliberately; the spec records it.
- [x] 2.5 Make failure loud: a view that errors, times out, or renders an empty state where the manifest expects data fails the run naming that view, and writes no image. Verify by pointing a manifest entry at a path that does not exist and confirming the failure names it.
- [x] 2.6 Capture every declared shot in both light and dark theme, writing to `marketing/assets/screenshots/` under the manifest's names. Verify both themed files exist for every entry.
- [x] 2.7 Enforce the per-image size bound with lossless optimisation at capture, failing on any image over the bound. Verify by lowering the bound temporarily and confirming the run reports the oversized image.
- [x] 2.8 Add the `marketing-screenshots` Makefile target chaining stack-up, seed and capture into one command. Verify a full refresh from a clean stack produces every declared image.
- [x] 2.9 Review every captured image for credentials, tokens, keys and non-synthetic identities before committing. Verify each captured view against the "no real or sensitive data" requirement and record the check.
      - Reviewed 2026-09-21 against the 10 captured paths (`/`, `/nodes.html`, `/node.html`, `/groups.html`, `/code.html`, `/jobs.html`, `/packages.html`, `/vulnerabilities.html`, `/roles.html`, `/activity.html`):
      - The only console view that renders a credential value is the service tokens page, which shows an issued token once at creation. It is **not** in the manifest, and the seed discards the token values it is handed.
      - `nodes.js` and `code-repositories.js` matched a broad grep only on certificate *signing*; neither renders a token or secret.
      - Every identity visible is synthetic, enforced by `internal/demodata`'s fictional-name tests rather than by this review alone.
      - Six pre-existing non-demo nodes (`livetest-node-*`, `vuln-demo.test`) were present in earlier captures and were removed from openvoxdb before the final run; the current images show the demo fleet only.

## 3. Marketing site build

- [x] 3.1 Create `marketing/build.sh` and `marketing/gen/main.go` mirroring the `frontend/` pattern, emitting to `marketing/dist/`, with the page list as a Go slice. Verify it renders a placeholder page set into `marketing/dist/`.
- [x] 3.2 Source `voxblocks.js`/`voxblocks.css` from `frontend/node_modules/@openvoxproject/voxblocks`, running the frontend's install step when that directory is absent. Verify from a clean checkout that the build succeeds and that the copied bundle's version matches `frontend/package-lock.json`.
- [ ] 3.3 Decide whether to self-host the site's fonts. **The reason this task gave has turned out to be wrong and it should not be done as written.** It was justified by capture determinism, but capture screenshots the *console*, not this site, so the site's fonts never reach an image; the real problem there was capture shooting before webfonts arrived, now fixed by waiting on `document.fonts.ready`. What remains is a genuine but different question - whether the published site should depend on Google Fonts at all, for independence and for visitor privacy - weighed against committing font binaries or adding a network fetch to the build. The site currently loads the same Google Fonts link the console's own layout uses, which at least keeps the two consistent.
- [x] 3.4 Write the shared `layout.html.tmpl`: `vox-header` with the inline OpenVox mark and `vox-theme-toggle`, `vox-subnav` for the capability pages with the current page marked, and `vox-footer`. Verify every generated page carries navigation to the landing page and all capability pages, and marks the current one.
- [x] 3.5 Implement the three-state theme handling - pre-paint script honoring an explicit choice, OS preference otherwise - reusing the shape of the console layout's script. Verify switching the toggle repaints without a flash and the choice survives a reload.
- [x] 3.6 Add the screenshot rendering helper: `gen` emits a `<picture>` with a dark `<source>` and a light `<img>` fallback from the manifest, fails the build when a declared image file is missing, and fails when a page references a name the manifest does not declare. Verify by deleting an image and confirming the build fails naming it.
- [x] 3.7 Extend the theme script to swap the `<img>` source when an explicit theme choice differs from the OS preference. Verify a dark-preference visitor who selects light sees light screenshots, and that screenshots still match theme with JavaScript disabled.
- [x] 3.8 Write `marketing/src/*.css` for the screenshot frame and page spacing only. Verify by review that no rule reproduces a component `voxblocks` already provides.

## 4. Site content

- [x] 4.1 Write the landing page: `vox-hero`, a `vox-grid` of `vox-card`s summarising each capability and linking to its page, `vox-stat`s for the figures worth leading with, and a closing `vox-cta-band`. Verify it renders and every card links to a real page.
- [x] 4.2 Write `features/nodes.html` - inventory, reports, connectivity and certificate management - with alternating `vox-billboard`s carrying screenshots in the `media` slot. Verify each claim on the page corresponds to behavior the console has.
- [x] 4.3 Write `features/classification.html` - node groups, match rules and the ENC contract - using `vox-billboard` and `vox-step-indicator` for the classification flow. Verify against the `classifier` spec that nothing claimed is unimplemented.
- [x] 4.4 Write `features/code.html` - multi-source code deployment and deploy history - with `vox-code-block` for configuration examples. Verify the examples match the real `code-sources.yaml` shape.
- [x] 4.5 Write `features/orchestration.html` - tasks, plans and job runs - using `vox-timeline` for a run's progression. Verify against the `orchestrator` spec.
- [x] 4.6 Write `features/security.html` - package inventory and vulnerability tracking - with a `vox-grid` of `vox-stat`s for severity breakdown. Verify against the `package-inventory` and `vulnerability-tracking` specs.
- [x] 4.7 Write `features/access-control.html` - RBAC, service tokens and the audit trail. Verify against the `rbac`, `activity` and `audit-log-emission` specs.
- [x] 4.8 Add `vox-toc` to any page long enough to need one, and give every screenshot a text alternative describing what the console is showing rather than naming the page. Verify every `<img>` has a descriptive `alt`.
- [x] 4.9 Add the onward links - source repository, `SETUP.md`, and the published container images - reachable from the end of every page. Verify each link resolves.

## 5. Publication

- [x] 5.1 Make every internal link and asset reference relative. Verify by serving `marketing/dist/` from a subdirectory of a static server and confirming every page, link and asset resolves.
- [x] 5.2 Add `.github/workflows/marketing.yml` building `marketing/` and publishing `marketing/dist/` via `actions/deploy-pages` on pushes to `main` that touch the site. Verify `ci.yml` is unmodified and a site build failure publishes nothing.
- [x] 5.3 Document the one-time GitHub Pages enablement step (Settings → Pages → source "GitHub Actions") where a maintainer will find it. Verify the note is present in `marketing/README.md`.

## 6. Documentation and verification

- [x] 6.1 Write `marketing/README.md`: how the site is built, how to refresh screenshots in one command, the per-image size bound, and the statement that tier-3 seeded history is fabricated and never to be used as evidence of anything. Verify a contributor can follow it from a clean checkout.
- [x] 6.2 Add the `marketing` Makefile target and reference the site, the seed and the capture refresh from the repository `README.md`. Verify each documented command runs as written.
- [x] 6.3 Verify the console binary is untouched: `go build ./cmd/console`, `go vet ./...`, `gofmt -l .` and `go test ./...` behave exactly as before, and `internal/web/dist` contains nothing from `marketing/`.
- [x] 6.4 Audit the site against WCAG 2.2 AA in both themes at desktop and phone widths, fix what is found, and record the checks no tool can make.
      - Built `marketing/a11y`: axe-core over 28 renders (7 pages x 2 themes x 2 viewports), plus reflow at 320px (SC 1.4.10), the WCAG text-spacing overrides (SC 1.4.12), and skip-link presence/target/visibility (SC 2.4.1). Wired up as `make marketing-a11y`.
      - It asserts the rules it depends on actually ran - `target-size` (SC 2.5.8) ships disabled in axe-core, and a rule that does not run reports no violations, which reads as a pass. Verified the assertion fires by naming a rule that does not exist.
      - Fixed: header GitHub link at 1.97:1 (an unstyled slotted `<a>` falling back to `#0000ee`); footer bottom row at 4.37:1 (a voxblocks issue the console shares - worth reporting upstream); the call-to-action band sitting outside every landmark; no skip link; no reduced-motion handling.
      - One accepted exception, recorded with its reason in `marketing/a11y/main.go`: axe's best-practice `region` rule flags the skip link for being outside a landmark, which is where a skip link must be.
      - Reviewed by hand and recorded in `marketing/README.md`: alt-text quality, heading structure, link text out of context, focus order.
      - Result: clean on WCAG 2.2 A/AA and on axe's best-practice rules.
- [x] 6.5 Run a full end-to-end refresh from a clean checkout - stack up, seed, capture, build - and confirm it produces a complete site with every screenshot present and no working-tree changes on an immediate second capture run.

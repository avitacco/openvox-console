# Tasks

## 1. Guide renderer

- [x] 1.1 Add goldmark to the root module and create `marketing/guides` with front matter loading (`title`, `summary`, `order`) and section discovery from `marketing/guides/<section>/*.md`; verify with a unit test loading a fixture tree in the expected section and order
- [x] 1.2 Render CommonMark + GFM tables to HTML with every translatable block (heading, paragraph, list item, table cell, callout body) marked `data-i18n-html`, and fenced code left unmarked; verify with golden-file tests covering each block kind and a code block that is never marked
- [x] 1.3 Map fenced code to `vox-code-block`, GitHub alerts to `vox-callout` (NOTE→info, TIP→tip, WARNING→warning, CAUTION→danger), consecutive `tab="…"` blocks to one `vox-tabs`, and `shot:<name>` images to the existing screenshot `<picture>`; verify with golden-file tests, and that an unknown alert type or an undeclared shot is an error naming the file and line
- [x] 1.4 Expand `<!-- include: name.md -->` from `marketing/guides/_partials/` before parsing; verify with a test that an included partial renders in place, that a missing partial is an error, and that include cycles are refused
- [x] 1.5 Generate heading anchors from the English heading text and expose each guide's anchors and `h2`/`h3` outline; verify with a test that anchors are stable, unique within a page, and suffixed on collision
- [x] 1.6 Expose a `Messages(file)` function returning the guide's title and summary plus exactly the key (`guides.Key`) of every `data-i18n-html` block the renderer emits; verify with a test that, for every fixture, the set equals what a parse of the rendered HTML finds
- [x] 1.7 Extend `internal/demodata`'s `TestDemoDataIsNotShipped` so no shipped command links `marketing/guides` or goldmark; verify the test passes and fails when a shipped package is made to import it

## 2. Extraction

- [x] 2.1 Add a `.md` case to `frontend/i18n extract` that takes messages from `marketing/guides.Blocks`, with the guide path and block line as the reference, skipping `_partials` duplicates so a shared block is one message; verify with an extractor test over a fixture guide and partial
- [x] 2.2 Mark screenshot captions in `marketing/shots` with `N_()`, and add `data-i18n-attr="alt"` to the rendered `<img>`; verify `extract` lists every caption and a translated feature page's `alt` is in its locale
- [x] 2.3 Update the translation workflow in `marketing/README.md` (`-source` now includes `../marketing/guides`), and run it; verify `site.pot` gains the screenshot captions and the renderer's labels, that the renderer's test fixtures are *not* extracted, and `merge`/`status` run cleanly (a real guide's blocks are verified in 4.3, when the first one lands)

## 3. Guide pages in the generator

- [x] 3.1 Add `templates/guide.html.tmpl` and render every guide to `guides/<slug>.html` in every locale, inside the existing layout, with `vox-sidenav` (groups per section, current marked), a `vox-toc` from the outline, and `vox-series-nav` within the section; verify by building and checking one page per section in English and one locale, including from a path prefix
- [x] 3.2 Add `guides/index.html` listing every section and guide with its summary, plus a "Guides" header entry on every page; verify the entry appears on feature, guide and landing pages in every locale
- [x] 3.3 Add the tab behavior to `src/`: tabs with the same label switch together across the page, and the choice persists via `localStorage` in try/catch; verify by hand in a browser with and without storage available, and by keyboard alone
- [x] 3.4 Translate guide pages with the existing `translateHTML`, count English fallbacks per guide, show the translated partial-translation `vox-callout` when any exist, and print per-locale coverage at build end; verify with a test using a catalogue missing one block
- [x] 3.5 Add the inline-code check: a translated block whose `<code>` contents (as a multiset) or `href` values differ from the source fails the build naming locale, guide and source string; verify with tests for an altered, dropped and added code span and a changed link
- [x] 3.6 Add the settings drift check (every `CONSOLE_*` token must be a `getenv` name in `internal/runtime/config.go`, `_FILE` suffix allowed) and the internal link/anchor check across every generated page; verify with tests for an unknown setting, a missing page and a missing anchor, each failing with the guide and reference named
- [x] 3.7 Point the layout's `vox-cta-band` and footer "Setup guide" at `guides/index.html#install`; verify no page in any locale still links to `SETUP.md`
- [x] 3.8 Clean and regenerate `docs/guides/` (and each locale's) in `build.sh`, and add every guide page to `marketing/a11y`'s page list; verify `make marketing` builds and the audit covers guide pages in both themes and both viewports (run as `go run ./marketing/a11y` against a local headless Chromium - see marketing/README.md - since `make marketing-a11y` needs the capture container)
- [x] 3.9 Document authoring in `marketing/README.md`: file layout, front matter, the four Markdown conventions, partials, anchors, screenshots, and each build check; verify by writing the first guide (group 4) from the README alone

## 4. First guide: install with containers

- [x] 4.1 Write `install/plan.md` (what you are building, decisions to make first) from `SETUP.md` sections 0 and "Decide these before you start"; verify it builds and passes the checks
- [x] 4.2 Write partials shared by both install guides (credentials, configure, connect classification, verify) from `SETUP.md` sections 4, 5, 7 and 9, with container and package differences as tabs where they are one step; verify each renders in isolation
- [x] 4.3 Write `install/containers.md` from the container paths of `SETUP.md` sections 1–3, 6 and 8, using the partials, with a verification step after each service; verify it builds, its blocks appear in `site.pot` after extract, it passes the drift checks and the a11y audit, and it reads correctly at 320px with every command scrolling inside its block
- [ ] 4.4 Walk `install/containers.md` on a fresh VM from start to a console serving classification to openvoxserver, fix every divergence in the guide, and record the walk-through (host OS, versions, date, anything surprising) in the guide review log in `marketing/README.md`

## 5. Install from packages and binaries; SETUP.md

- [ ] 5.1 Write `install/packages.md` from the package and binary paths of `SETUP.md`, using the same partials; verify it builds and passes the checks and the a11y audit
- [ ] 5.2 Walk `install/packages.md` on a fresh VM (one supported Debian/Ubuntu and one EL release) to the same result, fix every divergence, and record the walk-through in the guide review log in `marketing/README.md`
- [x] 5.3 Replace `SETUP.md` with a short pointer to the site's install guides and to their Markdown sources, in the same commit that the last of its content lands in a guide; verify every procedure in the old `SETUP.md` is present in a guide (checked against a list made before deleting it)

## 6. Nodes guides

- [x] 6.1 Write `nodes/add.md`: enrollment with the install script as Linux/macOS/Windows tabs, building agent packages when self-built, signing, verifying the node appears connected, and its first run; verify it builds and passes the checks
- [x] 6.2 Write `nodes/certificates.md` (sign, revoke, clean, the `nodes:certs:manage` permission, what revocation does to a connected node, CRL sources) and `nodes/remove.md` (deactivate plus clean, what is and is not purged), moving the user-facing parts of the matching `operations.md` sections and leaving linked stubs; verify they build and the stubs link to the right guides
- [ ] 6.3 Declare and capture the screenshots the Nodes guides use (pending certificate request, node connectivity, delete confirmation), with demo-seed changes if the demo fleet lacks the state; verify `make marketing-screenshots` captures them in every locale and the build uses them

## 7. Scale guide

- [x] 7.1 Write `scale/run-modes.md` and `scale/cluster.md` from `operations.md`'s run modes, required configuration per mode, multi-instance topology, node certificate revocation, monitoring the bus, which modes may be multiple, and migration path sections, leaving linked stubs; cover peer TLS, the cluster and leaf secrets, `CONSOLE_CLUSTER_ADVERTISE`, and `enc` as a leaf; verify they build and pass the checks
- [ ] 7.2 Walk `scale/cluster.md` from one instance to a `web`/`orchestrator`/`worker` split plus one leaf `enc` instance, confirming token revocation and a job dispatched through `web` to a node on `orchestrator`; fix every divergence and record the walk-through in the guide review log in `marketing/README.md`

## 8. Use guides

- [x] 8.1 Write `use/classify.md` (groups, match rules, the JSON escape hatch, ENC effect) and `use/deploy-code.md` (control repositories, sources file, webhooks, deploy history); verify they build and pass the checks
- [x] 8.2 Write `use/run-jobs.md` (targeting, runs, tasks and plans as the console implements them, per-node results, report links) and `use/access.md` (users, roles, permissions, service tokens, OIDC role mapping); verify they build and pass the checks, and that neither describes behavior the console lacks
- [x] 8.3 Write `use/vulnerabilities.md` from `operations.md`'s vulnerability tracking section (providers, credentials and `CONSOLE_SECRETS_KEY_FILE`, mirrors, limits), leaving a linked stub; verify it builds and passes the checks
- [ ] 8.4 Declare and capture the screenshots the Use guides need (group editor, deploy history, job result, role editor, provider configuration); verify capture and build in every locale

## 9. Operate guides

- [x] 9.1 Write `operate/postgres-failover.md`, `operate/rotate-signing-key.md` and `operate/upgrade-postgres.md` from the matching `operations.md` runbooks, leaving linked stubs; verify they build and pass the checks, and that `operations.md` now contains only engineering notes and stubs

## 10. Cross-linking and project docs

- [x] 10.1 Add each feature page's guide links to gen's page table and render them as a "Do it" block (nodes → nodes guides, classification → classify, code → deploy-code, orchestration → run-jobs, security → vulnerabilities, access control → access); verify every feature page shows its links in every locale and the link check passes
- [x] 10.2 Update `README.md`'s documentation links to the site's guides; verify no link in `README.md` points at a removed `SETUP.md` or `operations.md` section
- [x] 10.3 Add to `openspec/config.yaml`'s context that a change altering a user-facing flow must list the guide it updates; verify `openspec instructions proposal` output includes it

## 11. Translation

- [ ] 11.1 Extract and merge, then translate every guide message into zh, hi, es, fr, de, ja and ar, leaving code untouched; verify the build passes the inline-code check in every locale and prints full coverage
- [ ] 11.2 Review by a reader of each language, one locale at a time (zh, hi, es, fr, de, ja, ar), recording who reviewed and when in `marketing/README.md`; verify each locale's review entry exists before its task is checked
- [ ] 11.3 Check the Arabic guide pages by hand for right-to-left layout: sidenav, tabs, series navigation, callouts, and code blocks that stay left-to-right; verify in both themes at both viewports

## 12. Integration

- [ ] 12.1 Run `make test`, `make marketing` and `make marketing-a11y`; verify all pass with no accepted exceptions beyond the one already recorded
- [ ] 12.2 Look at every guide page in English and one right-to-left and one CJK locale, in both themes, at desktop and phone width, and served from a path prefix; record the review in `marketing/README.md` as the site's other manual reviews are
- [ ] 12.3 Confirm `add-marketing-site` has been archived before archiving this change; verify `openspec validate add-site-guides --strict` passes

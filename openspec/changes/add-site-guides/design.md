## Context

The site is built by `marketing/build.sh`, which runs `marketing/gen` once
per locale. gen renders `templates/layout.html.tmpl` plus one
`templates/pages/**/*.tmpl` per page, then translates the rendered HTML in
place (`translate.go`): an element marked `data-i18n` has its text
replaced, `data-i18n-attr` names translatable attributes, and
`data-i18n-html` swaps an element's inner HTML. Message IDs are produced
by `frontend/i18n extract`, a regex scan of `.tmpl`, `.go` and `.js`
sources for those same markings (and `N_("...")`). Translation happens at
build time, and a missing translation falls back to the English source.

The whole repository is one Go module, so a package under `marketing/`
can be imported by both `marketing/gen` and `frontend/i18n`, as
`marketing/shots` already is by gen.

The guides' content already exists, in the wrong place and the wrong
shape. `SETUP.md` interleaves the container and package paths under each
numbered section. `operations.md` mixes reference, runbooks and
engineering history. See proposal.md for why they move, and
specs/site-guides for what the result must do.

## Goals / Non-Goals

**Goals:**

- Authors write a guide as a Markdown file and nothing else: no
  per-paragraph markup and no parallel template.
- The message IDs the extractor produces are exactly the strings gen looks
  up, by construction rather than by two implementations agreeing.
- Guides reuse the site's layout, locale handling, theme, screenshots and
  audit unchanged, extending them rather than forking them.

**Non-Goals:**

- Search. Twenty-odd guides and a sidebar don't need it, and a static
  site would need a client-side index built per locale. Revisit when the
  guide count makes the sidebar insufficient.
- Versioned documentation per console release. There is one published
  site; it documents `main`, as the feature pages already do.
- Running guide commands in CI to prove they work. The drift checks catch
  renamed settings and broken links; whether a command sequence still
  works end to end is checked by a person following it (see Migration
  Plan).
- Translating the repository's own remaining engineering notes.

## Decisions

### Markdown source, rendered by a shared package

Guides live in `marketing/guides/<section>/<slug>.md`, each with a small
front matter block (`title`, `summary`, `order`). A new package,
`marketing/guides`, owns loading, parsing and rendering them with
goldmark, and is the only Markdown implementation in the tree.

Both consumers call it:

- **gen** renders each guide to HTML, embeds it in a guide template
  (`templates/guide.html.tmpl`, inside the existing layout), then
  translates it with the existing `translateHTML`.
- **frontend/i18n extract** gets a `.md` case that calls the same package
  to produce the guide's translatable blocks.

The renderer marks each translatable block with `data-i18n-html` as it
emits it. The extractor asks the same renderer for those blocks' inner
HTML. Because one function produces both the ID and the markup that is
later looked up, whitespace, escaping and inline-markup differences
cannot make them disagree, which is the failure a second implementation
would invite.

*Alternatives considered:*
- **Guides as `.tmpl` pages with `data-i18n` markup** (how feature pages
  work). This needs no new dependency, but it means hand-marking every
  paragraph of roughly twenty long documents, escaping every command into
  HTML, and reviewing guides as markup instead of prose. The feature pages
  are short and designed. Guides are long and written, so the tool should
  follow the content.
- **Extract from gen's rendered output** rather than teaching the
  extractor Markdown. This avoids touching `frontend/i18n`, but makes
  extraction depend on a full site build (screenshots present, every
  locale), and the extractor would stop being the single entry point for
  "what needs translating".

### Translation unit: the block, with inline code frozen

The unit offered to translators is one block: a heading, paragraph, list
item, table cell, callout body, or tab label. Blocks carry their inline
markup (`<code>`, `<a>`, `<strong>`) in the message, as `data-i18n-html`
already does for feature-page prose, because a translator must be able to
move a link or an emphasised term within the sentence.

Fenced code blocks are never marked and so never extracted.

Inline code is protected by a build check rather than by trusting
translators. For each translated block, the multiset of `<code>` element
contents in the translation must equal the source's, and every `href`
must be unchanged. A mismatch fails the build with the locale, guide and
source string. The same check applies to link targets, because a
translated anchor that no longer resolves would otherwise pass as prose.

*Alternative considered:* placeholder substitution (`{0}` for each inline
code span) so translators never see the code. Translators lose the
context the code provides ("run `make up`" vs "run {0}"), and PO tooling
and reviewers handle literal `<code>` fine. The check gives the same
guarantee without the loss.

### Partial-translation notice

While translating a guide, gen counts the blocks that fell back to
English. A guide with any fallback gets a `vox-callout variant="info"`
at its top, itself translated, saying that parts of the guide are not yet
translated. English pages never show it. The count is also printed per
locale at build time, so an author sees translation coverage without
running `i18n status`.

### Markdown extensions, mapped onto voxblocks

Authors use plain CommonMark plus GFM tables, and four conventions that
the renderer maps onto voxblocks components. The mapping is fixed in one
place, and nothing outside it is accepted:

| Markdown | Rendered as |
| --- | --- |
| Fenced code block with a language | `vox-code-block language="…"` (copy button, `copy-label` etc. translated) |
| `> [!NOTE]` / `[!TIP]` / `[!WARNING]` / `[!CAUTION]` (GitHub alert syntax) | `vox-callout` with variant `info` / `tip` / `warning` / `danger` |
| Consecutive fenced blocks whose info string carries `tab="Linux"` | one `vox-tabs` with a `vox-tab` / `vox-tab-panel` per block |
| An image whose URL is `shot:<name>` | the existing theme-aware `<picture>` for that declared screenshot |

The GitHub alert syntax is chosen because it is what the same files show
when viewed on GitHub, so a guide stays readable in the repository even
though the site is canonical. Unknown alert types and a `shot:` name the
manifest does not declare are build errors, consistent with how gen
already treats undeclared screenshots.

Tabs are keyed by label across the page, so choosing "Windows" in one
step selects it in the others. The choice is remembered in
`localStorage` (wrapped in try/catch, like the locale choice), because a
reader on Windows is on Windows for the whole guide.

*Alternative considered:* a container directive (`:::tabs`). This is not
CommonMark, so it renders as literal text on GitHub and needs a custom
parser extension. The info-string convention degrades to a readable
sequence of labelled code blocks everywhere else.

### Shared steps between the two install guides

The container and package install guides share whole sections: issuing
the console's credentials, configuring it, connecting classification, and
verification. These are written once as partials
(`marketing/guides/_partials/*.md`) and included with a single-line HTML
comment, `<!-- include: credentials.md -->`, which the renderer expands
before parsing. An HTML comment is invisible on GitHub and cannot be
mistaken for content. A partial's blocks are extracted once, so a shared
paragraph is translated once, not once per guide that includes it.

### Page layout and navigation

Guides render at `guides/<slug>.html` (flat, so `rootFor` needs no new
depth rules), with `guides/index.html` as the landing page listing every
section. A guide page is:

- `vox-sidenav` with one `vox-sidenav-group` per section (Install, Nodes,
  Scale, Use, Operate), open for the current section, with the current
  guide `current`. It collapses behind its own toggle at phone width.
- the guide body, with a `vox-toc` built from its `h2`/`h3` headings
  (anchors are slugs of the **English** heading, so a link to
  `#configure` works in every locale and the anchor check needs only one
  pass);
- `vox-series-nav` for previous/next within the section.

The site header gains a "Guides" entry. The capability subnav stays
capability-only: guides are a sibling of the capability pages, not one of
them. Each feature page gets a short "Do it" block linking its guides,
listed in gen's page table rather than hard-coded in each template.

The layout's `vox-cta-band` switches from `SETUP.md` on GitHub to
`guides/index.html#install`, and the footer's "Setup guide" link follows
it.

### Drift checks

gen runs them while rendering and fails the build on the first guide with
any problem, listing every problem in that guide:

- **Settings:** every `CONSOLE_[A-Z0-9_]+` token in a guide, in prose or
  code, must appear as a `getenv("…")` name in
  `internal/runtime/config.go` (read and scanned at build time; the `_FILE`
  suffix form is accepted for any name). This is deliberately a textual
  scan of the one file where settings are declared, not an import of the
  runtime package. That keeps gen from pulling in the console's
  dependencies, and a setting read anywhere else is itself a smell the
  check would surface. Variables that `docker-compose.yml` interpolates
  (`${CONSOLE_…}`) are accepted too: they share the prefix without being
  console settings (`CONSOLE_IMAGE_TAG`, `CONSOLE_CERTS_DIR`), and the
  container guide has to name them. They are real knobs the reader sets,
  so checking them against the file that reads them keeps the check
  meaningful rather than weakening it.
- **Links:** every site-internal `href` must name a page gen is
  generating, and a `#fragment` must be an anchor that page defines.
- **Screenshots:** already enforced by the existing `screenshot` path; the
  `shot:` image form goes through it.

### Repository documents

`SETUP.md` shrinks to a short page: what OpenVox Console is, a link to the
install guides on the site, and a link to each install guide's source
Markdown for anyone reading offline. `operations.md` keeps its engineering
sections. Each moved section leaves a one-line stub under its original
heading, linking to its guide, so existing deep links still land
somewhere useful. `README.md`'s documentation links point at the site.

### Screenshot captions become translatable

Screenshot alt text is a Go string in `marketing/shots` and is currently
English on every locale. The Use and Nodes guides lean on screenshots, so
captions get the `N_()` marker and the rendered `<img>` gets
`data-i18n-attr="alt"`. This applies to the feature pages too. It is a
small fix with a large effect for a screen reader user browsing a
translated page.

## Risks / Trade-offs

- **[Translation volume]** Five sections and roughly twenty guides add
  hundreds of messages per locale, and each guide edit invalidates
  translations. → Block-level units keep an edit's blast radius to the
  blocks actually changed. The partial-translation notice and English
  fallback make a stale locale readable rather than broken. Coverage is
  printed at every build.
- **[Machine-quality translations]** Initial translations for seven
  locales will be produced without a native reader, which the site's
  README warns against. → They follow the site's existing practice:
  committed as ordinary translations (not `#, fuzzy`, which the compiler
  treats as untranslated and so would leave every locale English until
  reviewed). Review by a reader of each language is a task per locale,
  tracked to completion rather than assumed. Code can't be corrupted
  either way, because of the inline-code check.
- **[Guides drift from the product]** Commands and flows change with the
  console. → The drift checks catch renamed settings and dead links.
  Behavior changes in a flow (a new required step) are not caught
  mechanically, so each change proposal touching a user-facing flow
  should list the guide to update. This is recorded in
  `openspec/config.yaml`'s context so future proposals are prompted to.
- **[Moving `SETUP.md` breaks readers of the repository]** People and
  links that expect the full guide in the repository find a pointer. →
  The pointer links each guide's Markdown source as well as the site, and
  the operations stubs keep deep links working.
- **[New dependency]** goldmark is build-time only for the site, but it
  is added to the root module's `go.mod`. → It is imported only by
  `marketing/…` and `frontend/i18n`; the existing test that asserts
  nothing under `marketing/` is shipped is extended to assert the console
  binary does not link goldmark.
- **[Build time]** Eight locales × ~20 guides adds to `make marketing`.
  → Rendering Markdown is milliseconds per page; the existing per-locale
  `go run` compile step dominates and is unchanged.

## Migration Plan

1. Land the generator, extractor and renderer support with one guide
   (Install with containers), so the pipeline, checks, layout and audit
   are proven on real content before the rest is written.
2. Move content section by section. For each guide, the source section
   in `SETUP.md`/`operations.md` is replaced by its stub in the same
   commit, so no procedure exists twice even between commits.
3. Walk each install guide on a fresh VM (containers and packages
   separately) and the scale guide on a two-instance cluster, and record
   the walk-through in the task that closes it.
4. Translate, then review per locale.
5. Rebuild, run the accessibility audit, look at every guide in both
   themes, and commit `docs/`, as the site is always published.

Rollback is reverting the commits: the site is static and committed, and
the repository documents are restored by the same revert.

## Open Questions

- Whether the guides landing page should carry screenshots or stay a
  plain index. This is presentation only and can be settled when it is
  built.

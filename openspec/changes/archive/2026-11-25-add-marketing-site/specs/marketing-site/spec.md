## Purpose

A public, unauthenticated static site that shows what OpenVox Console does,
capability by capability, using screenshots of the console itself rather
than prose or mockups, so someone evaluating the project can understand it
without standing the stack up or reading the architecture documents.

## ADDED Requirements

### Requirement: Capability-oriented page set
The site SHALL present a landing page that summarises what the console is
and what it does, plus one page per major console capability covering, at
minimum: node inventory and reporting, node classification, code
deployment, orchestration, package and vulnerability tracking, and access
control and audit.

Each capability page SHALL explain what the capability does and show at
least one screenshot of the console performing it. A page that describes a
capability without showing it fails this requirement: the site's purpose is
to show the product, and unillustrated prose is what the repository's
existing documents already provide.

Every claim a page makes SHALL correspond to behavior the console actually
has. The site SHALL NOT describe planned, partial, or aspirational
capabilities as though they were shipped.

#### Scenario: Landing page summarises the product
- **WHEN** a visitor opens the site's landing page
- **THEN** it identifies what OpenVox Console is and links onward to each
  capability page

#### Scenario: Each capability page shows the product
- **WHEN** a visitor opens any capability page
- **THEN** the page explains that capability and displays at least one
  screenshot of the console performing it

#### Scenario: A capability the console does not have
- **WHEN** the site is reviewed against the console's current behavior
- **THEN** no page claims a capability the console does not implement

### Requirement: Navigation across the site
The site SHALL provide navigation, present on every page, that lets a
visitor reach the landing page and every capability page without typing a
URL, with the current page marked so the visitor can tell where they are.

#### Scenario: Moving between pages
- **WHEN** a visitor is on any page of the site
- **THEN** navigation to the landing page and to every capability page is
  available from that page

#### Scenario: The current page is identifiable
- **WHEN** a visitor is on a capability page
- **THEN** that page's navigation entry is marked as current

### Requirement: Built on the shared design system
The site SHALL be built on the `voxblocks` component library - the same
design system the console's own frontend uses - so the site and the product
it advertises read as one thing rather than two.

Where the library provides a component for a piece of page structure, the
site SHALL use that component rather than bespoke markup and CSS
reproducing it. Hand-written styling is permitted only for presentation the
library does not cover.

The site SHALL use the same `voxblocks` version as the console's frontend.
A site built against a different version would advertise a console that
does not look like the one a visitor would install.

Components SHALL be used according to their documented interfaces, and a
property value SHALL be one the component accepts. These components ignore
unrecognised values rather than reporting them, so a wrong value degrades
the element silently, with nothing to alert anyone.

#### Scenario: Page structure comes from the library
- **WHEN** a site page renders a hero, a feature grid, a card, a callout,
  a statistic, a secondary navigation bar, a footer, or a call to action
- **THEN** it is composed of the corresponding `voxblocks` components
  rather than bespoke markup reproducing them

#### Scenario: Design system version matches the console
- **WHEN** the site and the console frontend are built from the same commit
- **THEN** both resolve the same `voxblocks` version

#### Scenario: A component is given a value it accepts
- **WHEN** the site sets a component property that has a defined set of
  permitted values
- **THEN** the value is one of those values, so the component renders with
  its intended styling and iconography

### Requirement: Screenshots follow the visitor's theme
The site SHALL support light and dark presentation, honoring a visitor's
explicit choice where one has been made and otherwise their operating
system preference, consistent with how the console itself behaves.

Screenshots SHALL be shown in the theme the site is currently displaying.
A dark screenshot on a light page, or the reverse, reads as a screenshot of
a different product.

A screenshot SHALL carry a text alternative describing what the console is
showing, not merely naming the page.

#### Scenario: Dark presentation
- **WHEN** a visitor whose preference is dark opens a capability page
- **THEN** the page and every screenshot on it are shown in dark theme

#### Scenario: Light presentation
- **WHEN** a visitor whose preference is light opens a capability page
- **THEN** the page and every screenshot on it are shown in light theme

#### Scenario: A screenshot's text alternative
- **WHEN** a screenshot cannot be displayed or is reached by a screen
  reader
- **THEN** its text alternative describes what the console is showing in it

### Requirement: WCAG 2.2 level AA
The site SHALL meet WCAG 2.2 level AA.

Conformance SHALL be checked in both themes and at both a desktop and a
phone viewport, because contrast depends on the theme and reflow depends
on the width: a single pass cannot see either failure.

In particular the site SHALL:

- reflow to 320 CSS pixels without horizontal scrolling (SC 1.4.10);
- survive the text-spacing overrides of SC 1.4.12 without clipping text;
- meet the 4.5:1 contrast minimum for body text in both themes
  (SC 1.4.3), including elements slotted into components, whose shadow
  styles do not reach them and which therefore fall back to browser
  defaults;
- offer a way to bypass the header and navigation that repeat on every
  page (SC 2.4.1);
- keep every interactive element keyboard-reachable with a visible focus
  indicator (SC 2.1.1, 2.4.7);
- meet the 24-by-24 minimum target size (SC 2.5.8);
- contain all content within landmarks, so nothing is skipped by someone
  navigating by landmark.

Automated checking SHALL verify that the rules it relies on actually ran.
A rule that does not run reports no violations, which is indistinguishable
from passing, and at least one relevant rule is disabled by default in the
tooling.

Checks a tool cannot make - whether alt text describes the right thing,
whether link text means anything out of context, whether focus order
matches the visual order - SHALL be reviewed by a person, and that review
SHALL be recorded rather than merely performed.

#### Scenario: Reflow at the narrowest supported width
- **WHEN** a page is rendered at 320 CSS pixels wide
- **THEN** its content reflows and the page does not scroll horizontally

#### Scenario: Contrast in both themes
- **WHEN** a page is checked for contrast in the light theme and again in
  the dark theme
- **THEN** body text meets 4.5:1 in both

#### Scenario: Bypassing the repeated navigation
- **WHEN** a keyboard user begins tabbing through any page
- **THEN** the first focusable element offers to skip past the header and
  navigation to the content, and is visible once focused

#### Scenario: A rule that did not run
- **WHEN** the audit's tooling silently skips a rule the site relies on
- **THEN** the audit fails rather than reporting no violations

#### Scenario: Screenshots at phone width
- **WHEN** a page carrying a console screenshot is viewed at phone width
- **THEN** the screenshot is shown whole rather than cropped to an
  uninformative corner of itself

### Requirement: Self-contained static output
The site's build SHALL produce static files requiring no application
server, no backend, and no runtime data source. The site SHALL NOT call the
console's API or depend on a running console.

The output SHALL work when served from a subdirectory of a host, not only
from a domain root, because it is published to a project page whose URL
carries a path prefix. Internal links and asset references SHALL resolve
correctly in that case.

The site SHALL NOT be embedded in, or served by, the console binary: it is
public marketing material, and the console serves an authenticated product.

#### Scenario: Serving the built output
- **WHEN** the built site is served by any plain static file server
- **THEN** every page renders and every internal link and asset resolves

#### Scenario: Serving from a path prefix
- **WHEN** the built site is served from a subdirectory rather than a
  domain root
- **THEN** every internal link and asset still resolves

#### Scenario: The console binary is unaffected
- **WHEN** the console binary is built
- **THEN** it embeds and serves exactly what it did before this capability
  existed

### Requirement: Built locally, published by committing
The site SHALL be built by a person on their own machine and committed,
and the hosting SHALL serve those committed files directly. It SHALL NOT
be rebuilt by CI on push.

This is deliberate. A site built by a workflow is published without anyone
having seen it; a broken layout, a caption describing the wrong thing, or
a screenshot that did not render all reach visitors before they reach the
author. Building locally makes looking at the result a step you cannot
skip, because you are the one publishing it.

A build SHALL NOT overwrite the committed screenshots, which are content
rather than build output and cannot be regenerated without a seeded
console.

The build SHALL fail rather than emit a site with a missing or
undeclared screenshot, so a forgotten capture is caught before anything
is committed.

#### Scenario: Publishing a change to the site
- **WHEN** an author rebuilds the site and commits the result
- **THEN** the published site serves those committed files, with no build
  step between the commit and what a visitor sees

#### Scenario: A push that does not rebuild the site
- **WHEN** a commit lands that changes the site's source but not its built
  output
- **THEN** the published site is unchanged, because nothing rebuilds it
  automatically

#### Scenario: Rebuilding does not destroy the screenshots
- **WHEN** the site is rebuilt
- **THEN** the committed screenshots are still present and unmodified

#### Scenario: A declared screenshot is missing
- **WHEN** the site is built and a screenshot a page references is absent
- **THEN** the build fails naming it, and no site is produced

### Requirement: Routes visitors to the real thing
The site SHALL link a visitor to the material they need next: the source
repository, the setup instructions for a real deployment, and the published
container images.

#### Scenario: Getting started
- **WHEN** a visitor finishes reading a page
- **THEN** a path onward to the repository, the setup instructions, and the
  container images is available

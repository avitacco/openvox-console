## Purpose

Step-by-step guides on the public site that take a reader from nothing
to a working, scaled, day-to-day OpenVox Console. The site is the one
canonical home for user-facing documentation, in every language the site
is published in, and is kept true to the console by build-time checks.

## ADDED Requirements

### Requirement: Guide set
The site SHALL publish a guides section containing, at minimum, guides
for:

- **Install:** planning a deployment; installing with containers;
  installing from packages and binaries.
- **Nodes:** adding nodes on Linux, macOS and Windows; managing node
  certificates (sign, revoke, clean); removing a node.
- **Scale:** running additional instances, clustering them, and splitting
  them into run modes, including `enc` instances attached as leaves.
- **Use:** classifying nodes with groups; deploying code; running jobs;
  managing users, roles and service tokens; configuring vulnerability
  providers.
- **Operate:** Postgres failover; rotating the RBAC signing key; upgrading
  the console's Postgres across a major version.

Each install guide SHALL be a complete path on its own. A reader following
one SHALL NOT need the other install guide, or any document outside the
site, to reach a working console with openvoxserver, openvoxdb, Postgres
and classification connected.

A guide SHALL describe only behavior the console actually has. Steps for
planned or partial behavior SHALL NOT appear as though they work.

#### Scenario: Installing with containers alone
- **WHEN** a reader follows the container install guide from start to
  finish on a fresh host
- **THEN** they reach a running console serving classification to
  openvoxserver without consulting the package guide or the repository

#### Scenario: Installing from packages alone
- **WHEN** a reader follows the packages-and-binaries install guide from
  start to finish
- **THEN** they reach the same working result without consulting the
  container guide or the repository

#### Scenario: Scaling out
- **WHEN** a reader with one working instance follows the scale guide
- **THEN** it takes them through adding a clustered peer, splitting roles
  into modes, and attaching `enc` instances as leaves, including the peer
  TLS and the two separate secrets that clustering requires

### Requirement: Guides are step-by-step
A guide SHALL present its procedure as ordered steps. Each step SHALL say
what to do and, where the outcome can be checked, how to confirm the step
worked before moving on.

Where a step differs by platform (for example, the node install script on
Linux, macOS and Windows), the variants SHALL be presented side by side
under one step, so a reader sees the variant for their platform without
reading the others.

Commands and configuration a reader is expected to run or paste SHALL be
presented as copyable code, distinct from prose.

Notes, tips and warnings SHALL be visually distinct from the procedure,
and a warning SHALL precede the step it concerns, never follow it.

#### Scenario: Confirming a step
- **WHEN** a guide step has an observable result, such as a service
  answering its health check or a node appearing as connected
- **THEN** the guide states how to check that result before the next step

#### Scenario: A platform-specific step
- **WHEN** a reader reaches the step that installs the node agent
- **THEN** the Linux, macOS and Windows variants are offered as
  alternatives within that one step

#### Scenario: Copying a command
- **WHEN** a reader wants to run a command shown in a guide
- **THEN** they can copy it exactly as shown, with nothing but the command
  itself copied

### Requirement: Guide navigation
Every guide page SHALL offer navigation to every other guide, grouped by
the sections of the guide set, with the current guide marked. It SHALL
also offer an in-page table of contents of the guide's own sections, and
links to the previous and next guide within its group.

The guides section SHALL be reachable from every page of the site, and
from the site's call to action in place of the repository's setup file.

A feature page SHALL link to the guide or guides that carry out what it
describes.

#### Scenario: Finding another guide
- **WHEN** a reader is on any guide page
- **THEN** every guide is reachable from that page, and the current one is
  marked

#### Scenario: From a feature to doing it
- **WHEN** a reader on the orchestration feature page wants to run a job
- **THEN** the page links to the guide for running jobs

#### Scenario: Getting started from any page
- **WHEN** a reader on any page follows the site's primary call to action
  to install the console
- **THEN** it leads to the install guides on the site, not to a file in
  the repository

### Requirement: Guides share the site's presentation and accessibility
Guide pages SHALL use the same layout, design system, theme behavior and
path-prefix-safe linking as the rest of the site, and SHALL meet the same
WCAG 2.2 level AA requirement, checked the same way, in both themes and at
both viewports.

In particular, code blocks SHALL reflow or scroll within themselves at 320
CSS pixels without making the page scroll horizontally, and the platform
variants of a step SHALL be operable by keyboard.

#### Scenario: A long command at phone width
- **WHEN** a guide containing a long command is viewed at 320 CSS pixels
- **THEN** the command scrolls within its own block and the page itself
  does not scroll horizontally

#### Scenario: Switching platform by keyboard
- **WHEN** a keyboard user reaches a step with platform variants
- **THEN** they can move between the variants and read each one without a
  pointer

#### Scenario: Guides are audited
- **WHEN** the site's accessibility audit runs
- **THEN** every guide page is audited in both themes at both viewports

### Requirement: Guides are translated, code is not
Every guide SHALL be published in every locale the site is published in,
at the same path under that locale as the rest of the site.

A guide's prose - headings, paragraphs, list items, table cells, notes
and platform labels - SHALL be translatable through the site's existing
translation workflow.

Code SHALL NOT be translated. Code blocks SHALL NOT be offered for
translation at all, and inline code within translated prose SHALL appear
in the translation exactly as in the source. A translation that alters,
drops or adds inline code SHALL fail the build, naming the locale and the
string, so a command, path or setting name can never differ between
languages.

Where a piece of a guide has no translation in a locale, that piece SHALL
appear in English rather than being omitted. A guide that is not fully
translated in a locale SHALL say so at its top, so a reader knows that the
English passages are not a mistake.

#### Scenario: Reading a guide in German
- **WHEN** a reader opens a guide in the German locale
- **THEN** its prose is in German and every command, path and setting name
  is identical to the English guide

#### Scenario: A translation that changes a command
- **WHEN** a catalogue's translation of a guide passage renders an inline
  command differently from the source
- **THEN** the build fails naming the locale and the passage

#### Scenario: A partially translated guide
- **WHEN** a guide has passages without a translation in a locale
- **THEN** those passages appear in English, and the page states that the
  guide is not yet fully translated

### Requirement: Guides stay true to the console
The site's build SHALL fail, and produce no site, when a guide:

- names a `CONSOLE_*` setting the console does not read;
- links to a page, guide or in-page anchor within the site that does not
  exist;
- asks for a screenshot the screenshot manifest does not declare, or whose
  capture is missing.

The failure SHALL name the guide and the offending reference.

#### Scenario: A renamed setting
- **WHEN** a console setting is renamed or removed and a guide still names
  the old one
- **THEN** the next site build fails naming the guide and the setting

#### Scenario: A broken internal link
- **WHEN** a guide links to an anchor that no longer exists in its target
  guide
- **THEN** the build fails naming the guide and the link

### Requirement: The site is the canonical user documentation
User-facing documentation - how to install, configure, scale, use and
operate the console - SHALL live in the guides and nowhere else in the
repository.

The repository's setup file SHALL be reduced to a pointer to the install
guides. The repository's operations document SHALL retain only
engineering notes, and each user-facing section removed from it SHALL be
replaced by a link to the guide that now carries it. No procedure SHALL
exist both in a guide and in a repository document.

#### Scenario: Looking for setup instructions in the repository
- **WHEN** a reader opens the repository's setup file
- **THEN** it directs them to the install guides on the site

#### Scenario: A moved runbook
- **WHEN** a reader follows an old reference to the Postgres failover
  runbook in the operations document
- **THEN** they find a link to the guide that now contains it

## Purpose

Captures the console's own interface as committed image assets, driven from
a declared list of views, so that published screenshots are produced from a
running console rather than drawn by hand and can be refreshed wholesale
when the UI changes.

## ADDED Requirements

### Requirement: A declared list of captured views
The system SHALL maintain a single declared list of the views to capture.
Each entry SHALL identify the view, where within the console it is found,
any interaction needed to reach the state worth showing (a tab, a filter, a
selected record), and the name of the image it produces.

That list SHALL be the only place a view is declared. The site SHALL
reference captured images by the names that list defines, so an image the
site expects and an image capture produces cannot silently diverge.

#### Scenario: Capturing the declared views
- **WHEN** capture runs
- **THEN** it captures exactly the views the list declares, and writes each
  to the image name that entry defines

#### Scenario: The site references a name that is not declared
- **WHEN** the site references a screenshot no list entry produces
- **THEN** the mismatch is reported rather than the site publishing a
  missing image

### Requirement: Both themes for every view
Every declared view SHALL be captured in both light and dark theme, so the
site can show a screenshot matching the visitor's own theme.

#### Scenario: A view is captured
- **WHEN** capture runs for any declared view
- **THEN** both a light-theme and a dark-theme image are produced for it

### Requirement: Captures are of the real console
Capture SHALL drive a real console instance through a browser, exercising
the same interface a user would. It SHALL NOT assemble images from mocked
responses, static fixtures of rendered HTML, or hand-edited images.

A captured image SHALL NOT be retouched after capture. A screenshot that
has been edited no longer evidences that the console does what the page
claims.

#### Scenario: Capture source
- **WHEN** capture produces an image
- **THEN** that image is a screenshot of a running console rendering the
  declared view

### Requirement: Capture controls what it can control
Capture SHALL eliminate the sources of run-to-run variation that belong to
the act of screenshotting rather than to the product: the browser build,
the page's clock, in-flight animations and transitions, the caret, loading
indicators, viewport size, and fonts that have not finished loading.

A capture run SHALL NOT wait on a timer for a view to be ready. It waits
on the view's own rendered content, so a slow response changes how long
the run takes rather than what the image shows.

Identifiers, hashes and timestamps written by the demo seed SHALL be
derived from fixed inputs rather than generated per run, so that reseeding
does not by itself change what a screenshot shows.

Images are NOT required to be byte-identical between runs. Some of what
the console displays genuinely moves - the activity log stamps entries
from the real clock, and some list queries impose no total order, so tied
rows may come back in a different order. Chasing those into the product
would change the console to suit its own marketing photographs, which is
the wrong way round. Screenshots are expected to differ slightly between
refreshes, and that is accepted.

#### Scenario: Re-running capture with no UI change
- **WHEN** capture is run twice against the same seeded console
- **THEN** the images show the same views with the same content, though
  they need not be byte-identical

#### Scenario: A view showing relative time
- **WHEN** a captured view displays a timestamp or an elapsed duration
  derived from the page's clock
- **THEN** the captured image shows the value that clock yields, fixed to
  the demo instant rather than to whenever the capture ran

#### Scenario: A view that is still loading
- **WHEN** a view's data has not finished rendering
- **THEN** capture waits for the content itself rather than a fixed
  delay, and no partially rendered image is written

### Requirement: A failed capture is loud
Capture SHALL fail, with the failing view named, rather than write an image
when a view does not reach the state it declares - it fails to load, shows
an error, shows an empty state where data was expected, or times out.

A broken or empty screenshot that reaches the published site is worse than
no screenshot: it advertises a product that appears not to work.

#### Scenario: A view fails to load
- **WHEN** a declared view errors, times out, or renders empty where data
  was expected
- **THEN** capture fails and names that view, and no image is written for it

#### Scenario: A partial run
- **WHEN** capture fails partway through
- **THEN** previously committed images are not left in a mixed state that
  would be committed as though the run had succeeded

### Requirement: No real or sensitive data in an image
A captured image SHALL contain no credential, token, key, session
identifier, or real personal or host data. Everything visible SHALL come
from the synthetic demo dataset.

Capture SHALL NOT be run against, and its images SHALL NOT be produced
from, any console holding real data.

#### Scenario: A view that can display a secret
- **WHEN** a declared view could display a token, key, or credential
- **THEN** the captured image does not show its value

#### Scenario: Identities shown in a capture
- **WHEN** a captured image shows users, node names, or addresses
- **THEN** all of them are synthetic

### Requirement: Refreshing every screenshot is one command
Refreshing the full set of screenshots SHALL be a single documented
command, starting from a checkout with the dependency services available.
The procedure SHALL be recorded where a contributor will find it.

Committed images SHALL be bounded in size, with the bound stated, so that
repeated refreshes do not grow the repository without limit.

#### Scenario: Refreshing after a UI change
- **WHEN** a contributor changes the console's UI and refreshes the
  screenshots
- **THEN** one documented command reseeds, recaptures, and rewrites every
  declared image

#### Scenario: An oversized image
- **WHEN** a captured image exceeds the stated size bound
- **THEN** that is reported rather than silently committed

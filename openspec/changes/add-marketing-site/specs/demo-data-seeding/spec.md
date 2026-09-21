## Purpose

Populates a local console with a repeatable, obviously synthetic fleet -
nodes, reports, groups, users, jobs, deployments and findings - so that
screenshot capture and hands-on demonstrations show a console in use rather
than a stack of empty states.

## ADDED Requirements

### Requirement: Seeded data is indistinguishable from real operation
Seeded data SHALL be written through the paths the console itself uses to
record that kind of data - its published interfaces where one exists for
writing it, and otherwise the same internal writer the feature uses - never
by statements composed against its tables by hand.

Data written by hand against a schema drifts silently when that schema
changes: it keeps inserting successfully while meaning something different,
and the first sign of trouble is a screenshot that is quietly wrong.

The console SHALL NOT need to know it is looking at seeded data, and no
view SHALL require special handling to display it.

#### Scenario: A seeded node in the console
- **WHEN** a seeded node is viewed in the console
- **THEN** it renders identically to a node that reported for real,
  including its facts, its reports, and its derived status

#### Scenario: Derived views over seeded data
- **WHEN** a view computes over fleet data - a dashboard summary, a group's
  matched members, an inventory query
- **THEN** it computes over seeded data the same way it would over real
  data, with no seeding-specific path

#### Scenario: The schema behind a seeded feature changes
- **WHEN** the storage behind a feature the seed populates changes shape
- **THEN** the seed fails to build or fails loudly, rather than continuing
  to write data that no longer means what it did

### Requirement: A fleet with enough variety to be representative
The seeded dataset SHALL be large and varied enough that fleet-wide views
show meaningful distributions rather than a single row. It SHALL include,
at minimum:

- nodes across multiple operating systems and versions
- run outcomes covering unchanged, changed, and failed
- nodes in differing connectivity and certificate states
- a node group structure with match rules that actually match members
- more than one user and role, and at least one service token
- job history containing both successful and failed runs
- code deployment history across more than one environment
- package inventory and vulnerability findings of differing severities

Every view the site captures a screenshot of SHALL be populated by the
seed. A declared view left empty by seeding is a gap in the dataset, not an
acceptable screenshot.

#### Scenario: A fleet-wide summary
- **WHEN** a fleet-wide view is opened against the seeded console
- **THEN** it shows a distribution across multiple values rather than a
  single node or a single status

#### Scenario: Every captured view is populated
- **WHEN** the seed has run and a declared screenshot view is opened
- **THEN** that view shows data rather than an empty state

### Requirement: Seeding is repeatable
Running the seed against an already-seeded console SHALL converge on the
same state rather than duplicating records or failing. Two runs SHALL leave
the console indistinguishable from one run.

Values that would otherwise vary between runs SHALL be fixed, so that the
same seed produces the same console state and therefore the same
screenshots.

#### Scenario: Seeding twice
- **WHEN** the seed is run a second time against the same console
- **THEN** it succeeds, and the resulting state matches the state after the
  first run, with no duplicated nodes, groups, users, or jobs

#### Scenario: Seeding two separate consoles
- **WHEN** the seed is run against two freshly initialised consoles
- **THEN** both reach the same state

### Requirement: The dataset is obviously synthetic
Every name, hostname, address, user, and identifier the seed creates SHALL
be recognisably fictional, using names and domains reserved for
documentation and examples. Nothing SHALL resemble a real organisation's
infrastructure or a real person.

No credential the seed creates SHALL be one that could be valid anywhere
but a throwaway local stack.

#### Scenario: Names in seeded data
- **WHEN** seeded nodes, users, groups, and addresses are inspected
- **THEN** each is recognisably fictional rather than resembling real
  infrastructure or a real person

### Requirement: Seeding refuses a console that is not a local demo
The seed SHALL be usable only against a local development or demonstration
console, and SHALL refuse to run when its target is not one. Seeding writes
fabricated fleet data; against a real deployment that is corruption.

Refusal SHALL state why, so the operator is not left guessing.

#### Scenario: Pointed at a non-local console
- **WHEN** the seed is run against a target that is not a local development
  or demonstration console
- **THEN** it refuses, writes nothing, and states why

### Requirement: Seeding is not part of the shipped product
The seed SHALL be a development-time tool only. It SHALL NOT be part of the
console binary, SHALL NOT be reachable at runtime, and SHALL NOT be
included in the published container images.

#### Scenario: The shipped artifacts
- **WHEN** the console binary and the published images are built
- **THEN** neither contains the seeding tool nor any way to invoke it

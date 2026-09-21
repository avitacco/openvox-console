## ADDED Requirements

### Requirement: Control repo source configuration
The system SHALL support any number of named control repo sources, each
declaring at minimum a git remote, and optionally an environment name
prefix, a deploy key, and a webhook secret of its own. A source's name
SHALL be usable as a Puppet environment name component, and two sources
SHALL NOT share a name.

Sources SHALL be declarable either as a single control repo (the
pre-existing single-remote configuration) or as a list, but not both at
once: when both forms are present the system SHALL refuse to start
rather than silently preferring one. A single control repo configured
the pre-existing way SHALL behave as one source named `control` with no
prefix.

Configuring no source at all SHALL leave code deployment inactive - the
deploy endpoints erroring per request - rather than preventing the
console from starting.

#### Scenario: Multiple sources are configured
- **WHEN** the configuration declares two sources with distinct names and
  remotes
- **THEN** both are available to deploy from, and each is listed as a
  deploy target

#### Scenario: A single control repo keeps working unchanged
- **WHEN** only the pre-existing single control repo setting is
  configured
- **THEN** deploys run against it as a source named `control` with no
  prefix, producing the same environment directory names as before

#### Scenario: Both configuration forms present
- **WHEN** both a single control repo and a source list are configured
- **THEN** the console refuses to start and reports that the two are
  mutually exclusive

#### Scenario: Two sources share a name
- **WHEN** the configuration declares two sources with the same name
- **THEN** the console refuses to start and names the duplicate

#### Scenario: No source configured
- **WHEN** no control repo source is configured at all
- **THEN** the console starts normally and deploy triggers report that
  code deployment is not configured

### Requirement: Environment name prefixing
The system SHALL derive a deployed environment's name from its source's
prefix setting and the branch name, so that two sources carrying the
same branch name produce distinct Puppet environments that can be live
simultaneously. A source with prefixing disabled SHALL produce the
branch name unchanged; a source with prefixing enabled SHALL produce a
name incorporating the source's name or its configured prefix.

The system SHALL reject a configuration whose sources could produce
colliding environment names for the same branch, rather than allowing
one source's deploy to overwrite another's live code.

#### Scenario: Two sources both deploy a production branch
- **WHEN** two sources are configured, one unprefixed and one prefixed,
  and each has a `production` branch deployed
- **THEN** each has its own live environment directory under a distinct
  name, and neither deploy replaced the other's code

#### Scenario: A prefixed source's environment name
- **WHEN** a source configured with prefixing enabled deploys its
  `production` branch
- **THEN** the resulting environment name is distinct from the
  unprefixed `production` and identifies the source it came from

#### Scenario: A configuration that would collide is rejected
- **WHEN** two configured sources would produce the same environment name
  for the same branch name
- **THEN** the console refuses to start and reports which sources collide

## MODIFIED Requirements

### Requirement: Control-repo deploy execution
The system SHALL, on a deploy trigger naming a source and a branch, fetch
that source's current state and run a code deployment (module resolution
per its Puppetfile) into the live environment directory for the
environment name that source and branch produce, replacing whatever was
there for that environment.

A deploy SHALL affect only the environment it names. Deploying a branch
from one source SHALL NOT deploy, replace, or remove the code of a
same-named branch belonging to any other source.

#### Scenario: A deploy resolves the control repo's Puppetfile
- **WHEN** a deploy runs against a control repo containing a Puppetfile
- **THEN** the modules that Puppetfile declares are present in the
  deployed environment directory afterward

#### Scenario: A deploy failure leaves the previous environment intact
- **WHEN** a deploy fails partway (e.g. an unreachable module source)
- **THEN** the live environment directory still serves the last
  successfully deployed code, not a partial deploy

#### Scenario: Deploying one source leaves other sources untouched
- **WHEN** two sources each have a deployed `production` branch and a
  deploy is triggered for one of them
- **THEN** only that source's environment is replaced, and the other
  source's live code is byte-for-byte unchanged

#### Scenario: A deploy naming an unknown source
- **WHEN** a deploy is triggered naming a source that is not configured
- **THEN** the system rejects the request and runs no deploy

### Requirement: Deploy triggers
The system SHALL support triggering a deploy two ways: a webhook
endpoint accepting a signed push notification from a control repo's git
host, and a manual trigger through the console UI/API. Both SHALL
identify which configured source the deploy is for.

Each source SHALL have its own webhook endpoint, verified against that
source's own secret, so that a secret belonging to one source cannot
trigger a deploy of another. A manual trigger that names no source SHALL
target the default source.

#### Scenario: A webhook push triggers a deploy
- **WHEN** a control repo's git host sends a validly signed webhook
  notification for a push to that source's endpoint
- **THEN** the system runs a deploy for that source

#### Scenario: An invalidly signed webhook is rejected
- **WHEN** a webhook request's signature does not match the secret
  configured for the source it addresses
- **THEN** the system rejects the request and does not run a deploy

#### Scenario: One source's secret cannot trigger another's deploy
- **WHEN** a webhook request addressed to one source is signed with a
  different source's secret
- **THEN** the system rejects the request and does not run a deploy

#### Scenario: A webhook addressed to an unknown source
- **WHEN** a webhook request addresses a source that is not configured
- **THEN** the system rejects the request and does not run a deploy

#### Scenario: A manual trigger runs a deploy
- **WHEN** an administrator triggers a deploy for a named source through
  the console
- **THEN** the system runs a deploy the same way a webhook-triggered one
  for that source would

### Requirement: Deploy status and history
The system SHALL record every deploy attempt (source, trigger source,
control repo ref, status, start/finish time, and error detail on
failure) and expose it via an API and UI, restricted to users with
`code:read`. History SHALL be filterable by source. Triggering a deploy
SHALL require `code:deploy`.

#### Scenario: Viewing deploy history
- **WHEN** a user with `code:read` opens the deploy history page
- **THEN** the system displays every recorded deploy attempt, most
  recent first, with its status and the source it deployed

#### Scenario: Filtering history by source
- **WHEN** a user with `code:read` filters deploy history by one source
- **THEN** only that source's deploy attempts are returned

#### Scenario: Attempts from different sources are distinguishable
- **WHEN** two sources have each deployed a branch of the same name
- **THEN** their recorded attempts are distinguishable by source rather
  than appearing as repeated deploys of one thing

#### Scenario: Triggering a deploy requires the write permission
- **WHEN** a request to manually trigger a deploy presents a token
  carrying `code:read` but not `code:deploy`
- **THEN** the system rejects the request

### Requirement: Deployment-ready event
The system SHALL publish an event on successful deploy completion,
naming the source and the deployed environment and control repo ref, for
other components to subscribe to - the contract other in-cluster
consumers (e.g. a future compiler distribution mechanism) rely on,
independent of how many subscribers exist at any given time.

#### Scenario: A successful deploy publishes an event
- **WHEN** a deploy completes successfully
- **THEN** an event naming the source, environment and deployed ref is
  published, observable by any subscriber without the console needing to
  know who's listening

#### Scenario: A failed deploy does not publish a deployment-ready event
- **WHEN** a deploy fails
- **THEN** no deployment-ready event is published for that attempt

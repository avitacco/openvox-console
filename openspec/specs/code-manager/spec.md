# code-manager Specification

## Purpose

Deploys Puppet code from a control repo (Puppetfile plus manifests) into
the live environment directory `openvoxserver` reads from, on a webhook
push or a manual trigger, and tracks the result - so every other
capability's classification and catalog compilation work against code
the console itself manages, not code that was hand-placed on disk.

## Requirements

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

### Requirement: Atomic deploy activation
The system SHALL make a successful deploy's code live as a single atomic
operation, such that no request being served concurrently can observe a
partially-deployed environment.

#### Scenario: A concurrent request during deploy sees one consistent version
- **WHEN** a deploy completes while requests against the live
  environment are in flight
- **THEN** each request is served entirely from either the pre-deploy or
  the post-deploy code, never a mix of both

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
control repo ref, status, start/finish time, the deployed tree's size,
and error detail on failure) and expose it via an API and UI, restricted
to users with `code:read`. History SHALL be filterable by source.
Triggering a deploy SHALL require `code:deploy`.

Size SHALL be measured when the deploy runs rather than when it is
displayed, so that reporting it never requires walking a deployed
module tree. An attempt whose size could not be determined, including
every attempt recorded before sizes were measured, SHALL report its size
as absent rather than as zero.

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

#### Scenario: A successful deploy records its size
- **WHEN** a deploy completes successfully
- **THEN** the size of the deployed tree is recorded against that
  attempt

#### Scenario: An attempt recorded before sizes were measured
- **WHEN** deploy history includes an attempt with no recorded size
- **THEN** its size is reported as absent, not as zero

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

### Requirement: Code repository overview
The system SHALL expose, to users with `code:read`, a view of every
configured control repo source reporting at minimum: its name, its git
remote, the environment name prefix it applies, the environments it has
deployed, how many nodes use it, its deployed size, and when it last
deployed successfully.

Every configured source SHALL appear, including one that has never been
deployed from - a repository that has never worked is the one most worth
seeing - with the fields it has no data for reported as absent rather
than as zero or an error.

#### Scenario: Viewing configured repositories
- **WHEN** a user with `code:read` opens the code repositories view
- **THEN** every configured source is listed with its name, remote,
  prefix, node counts, size and last successful deploy

#### Scenario: A configured repository that has never been deployed
- **WHEN** a source is configured but no deploy has ever succeeded for it
- **THEN** it still appears in the list, with its last deploy, size and
  environments reported as absent rather than as zero

#### Scenario: The overview requires the read permission
- **WHEN** a request for the repository overview presents a token
  without `code:read`
- **THEN** the system rejects the request

### Requirement: Credentials are redacted from displayed remotes
The system SHALL NOT disclose credentials embedded in a source's git
remote when reporting that remote. A remote carrying userinfo (for
example an access token in an HTTPS URL) SHALL be reported with that
userinfo replaced, while remaining recognisable as the same repository -
host and path SHALL be preserved.

#### Scenario: A remote carrying an access token
- **WHEN** a source's remote embeds credentials, such as
  `https://x-access-token:SECRET@github.com/org/repo.git`
- **THEN** the reported remote identifies the same host and path but
  does not contain `SECRET`

#### Scenario: A remote carrying no credentials
- **WHEN** a source's remote has no embedded userinfo, such as an SSH
  remote
- **THEN** it is reported unchanged

### Requirement: Node usage counts per repository
The system SHALL report two distinct per-repository node counts: the
number of nodes the classifier resolves into one of that repository's
environments, and the number of nodes whose most recent report ran in
one of them. Both SHALL be reported separately rather than combined,
because their divergence is what identifies a repository nothing uses
and nodes running code no group assigns.

A node's assigned environment SHALL be its effective classification
result, so that a node matching several groups is counted against the
one environment it would actually be sent to, not against every group
that matches it.

Where the counts cannot be determined - openvoxdb unreachable, for
instance - the system SHALL report them as unavailable and still report
the rest of the repository's detail, rather than failing the view.

#### Scenario: A deployed repository no node uses yet
- **WHEN** a prefixed source has deployed successfully but no node is
  classified into, or reports from, its environments
- **THEN** it is listed with both counts at zero, distinguishing it from
  a repository that has not deployed at all

#### Scenario: A node matching several groups counts once
- **WHEN** a node matches multiple groups that name different
  environments
- **THEN** it is counted only against the environment its effective
  classification resolves to

#### Scenario: Assigned and reporting counts disagree
- **WHEN** nodes report from a repository's environment but no group
  assigns any node to it
- **THEN** the view reports a non-zero reporting count alongside a zero
  assigned count, rather than reconciling them into one number

#### Scenario: Node data is unavailable
- **WHEN** the node data needed for the counts cannot be retrieved
- **THEN** the counts are reported as unavailable and the repository's
  other detail is still returned

### Requirement: Deployment artifacts provide the code deployment toolchain
A deployment artifact of the console that is not installed through a
package manager - a container image, for instance - SHALL provide
everything a code deploy needs to run, including the code deployment
tool itself and the version control tooling and transports that tool
invokes.

Such an artifact SHALL identify the provided tool to the console by
default, so that configuring a control repo is the only step an operator
must take. Providing the toolchain SHALL NOT by itself enable code
deployment: it remains inactive until a control repo is configured.

The provided tool SHALL report its own version, so an operator can
establish what is running when a deploy misbehaves.

#### Scenario: Deploying from a container image
- **WHEN** the console runs from a published image with a control repo
  configured
- **THEN** a deploy succeeds without the operator installing anything
  into the image

#### Scenario: Version control transports are present
- **WHEN** a configured control repo is reached over HTTPS or SSH
- **THEN** the image provides the tooling and trust material those
  transports need

#### Scenario: The toolchain alone does not enable deployment
- **WHEN** the console runs from that image with no control repo
  configured
- **THEN** code deployment reports itself as not configured, and the
  console starts normally

#### Scenario: Identifying the bundled tool
- **WHEN** an operator asks the bundled deployment tool for its version
- **THEN** it reports a version identifying the build, rather than an
  empty value

## ADDED Requirements

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

## MODIFIED Requirements

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

# Spec Delta

## Purpose
Lets an operator register hosts the console reaches over SSH instead of a node
agent, with their login credentials sealed, their host keys pinned and their
connectivity checkable, and shows them alongside agent nodes.

## ADDED Requirements

### Requirement: An operator registers an agentless target
The system SHALL let a user with `agentless:manage` register a target with a
unique name, an address, a port (default 22), and a login: a user name, a
private key (optionally with a passphrase) or a password, and an optional
user to run as with `sudo`. A name that is already a node's name, or another
target's, SHALL be refused.

#### Scenario: Registering a target
- **WHEN** an operator registers `db-legacy` at `10.0.0.5` with a user and a key
- **THEN** the target exists, is marked agentless, and its host key is not yet trusted

#### Scenario: A name already in use
- **WHEN** the name is the certname of a node the console knows
- **THEN** the registration is refused naming the clash

### Requirement: Credentials are sealed and never shown
The system SHALL store a private key, passphrase or password only sealed with
the console's secrets key, bound to the row that owns it, and SHALL NOT return
it from any endpoint, write it to a log or audit entry, or place it in a job.
A read SHALL say only that a secret is set, and for a key its public
fingerprint. Replacing a secret SHALL be possible without reading the old one.

#### Scenario: Reading a target
- **WHEN** a target with a password is read
- **THEN** the response says a secret is set and contains no part of it

#### Scenario: Without the secrets key
- **WHEN** the console has no secrets key configured
- **THEN** a target with credentials cannot be registered and the refusal says why

### Requirement: A credential may be shared
The system SHALL let several targets use one named shared credential, and
SHALL let a target instead hold its own. A target's own login SHALL win over a
shared credential. A shared credential still in use SHALL NOT be deleted.

#### Scenario: Several targets, one key
- **WHEN** three targets reference one shared credential and its key is replaced
- **THEN** all three use the new key on their next connection

#### Scenario: Deleting a credential in use
- **WHEN** a shared credential is referenced by a target and deletion is requested
- **THEN** the deletion is refused naming the targets

### Requirement: Host keys are pinned
The system SHALL record the host key a target presents at its first check as
proposed, SHALL trust it only once a user with `agentless:manage` accepts it
after being shown its fingerprint, and SHALL refuse to connect to a target
whose key is not trusted. A different key from a pinned one SHALL be refused
and reported as changed, and SHALL be replaceable only by an explicit action.

#### Scenario: First connection
- **WHEN** a target is checked for the first time
- **THEN** its fingerprint is reported for acceptance and nothing is run on it

#### Scenario: A changed key
- **WHEN** a pinned target presents a different host key
- **THEN** the connection is refused as a changed key and no credential is sent

### Requirement: A connectivity check proves a target
The system SHALL let a user with `orchestrator:run`, for a target in their
scope, check it: connect, verify the host key, authenticate, run a trivial
command as the configured user, and report success or the stage and reason it
failed, recording the result on the target.

#### Scenario: A working target
- **WHEN** a target with a trusted key is checked
- **THEN** the result says it connected and shows the user it ran as

#### Scenario: A failure is specific
- **WHEN** the password is wrong
- **THEN** the result says authentication failed, without the password

### Requirement: Agentless targets are visible and scoped like nodes
The system SHALL list agentless targets with the nodes, marked agentless, and
offer them in target choosers and node groups, subject to the caller's node
scope. A node group SHALL include a target by pin; a group's rule, which
matches on facts, SHALL NOT match one. A caller limited to some nodes SHALL
manage only targets inside their scope, and a target outside it SHALL read as
unknown.

#### Scenario: Pinning a target into a group
- **WHEN** a target is pinned into a node group
- **THEN** users scoped to that group see it and may run tasks on it

#### Scenario: An out-of-scope target
- **WHEN** a scoped user reads a target outside their groups
- **THEN** the answer is the same as for a target that does not exist

### Requirement: Target changes are audited
The system SHALL write an audit entry when a target, credential or host-key
pin is created, changed, deleted, accepted or replaced, naming the actor and
the object, and never a secret.

#### Scenario: A key is replaced
- **WHEN** a user replaces a target's pinned host key
- **THEN** an audit entry records who and which target, with both fingerprints

### Requirement: Console UI for agentless targets
The console SHALL let a user with `agentless:manage` add, edit, check and
delete targets and shared credentials and accept host keys, SHALL mark
agentless entries on the Nodes page and in choosers, and SHALL be translated
in every supported language.

#### Scenario: Accepting a key
- **WHEN** a check reports an untrusted key and the user accepts the fingerprint shown
- **THEN** the target becomes usable for jobs

#### Scenario: Without the permission
- **WHEN** a user lacks `agentless:manage`
- **THEN** the pages offer no way to change targets or credentials

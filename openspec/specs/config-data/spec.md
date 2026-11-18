# config-data Specification

## Purpose

Lets an operator attach configuration data (Hiera-style key/value pairs) to
node groups in the console, and delivers a node's merged data to openvox-server
as a global-layer Hiera backend, so console-managed data takes precedence over
control-repo Hiera while keys it does not set fall through.

## Requirements

### Requirement: A node group carries configuration data
The system SHALL let a node group hold configuration data: a map of keys
to values, each value any JSON value. A key SHALL be 1 to 255 characters
from letters, digits, `_`, `:`, `.` and `-`, and SHALL NOT be
`lookup_options`. A group SHALL hold at most 1000 keys and a value SHALL
be at most 64 KiB encoded. An invalid group is refused with `400` naming
the `configData` field and the key.

#### Scenario: Setting data
- **WHEN** an administrator saves a group with configData
  `{"ntp::servers": ["a.example", "b.example"], "dns::timeout": 5}`
- **THEN** reading the group returns the same data, with the list and the
  number as typed

#### Scenario: A bad key
- **WHEN** a group is saved with the key `lookup_options` or `has space`
- **THEN** it is refused with `400` and nothing is saved

#### Scenario: Saving without sending data
- **WHEN** a client that does not send `configData` saves a group that has
  some
- **THEN** the group keeps its data

#### Scenario: Clearing data
- **WHEN** a group is saved with `configData: {}`
- **THEN** its data is removed

### Requirement: A node's data is its groups' data merged
The system SHALL compute a node's configuration data from the groups that
match it by the same order that merges class parameters: a descendant
overrides its ancestors, and otherwise the higher priority wins. A key set
by a winning group SHALL replace the same key from any other group whole;
maps SHALL NOT be merged key by key. Keys no group sets SHALL be absent.

#### Scenario: A child overrides its parent
- **WHEN** a parent sets `dns::timeout` to 5 and its child sets it to 2
  and a node matches both
- **THEN** the node's data has 2

#### Scenario: Different keys combine
- **WHEN** two groups a node matches set different keys
- **THEN** the node's data has both

#### Scenario: A map is replaced, not merged
- **WHEN** a lower group sets `app::opts` to `{"a": 1, "b": 2}` and a
  higher one sets it to `{"a": 9}`
- **THEN** the node's value is `{"a": 9}`

### Requirement: Console data wins over control-repo Hiera
The system SHALL make openvoxserver consult a node's configuration data
before its environment's Hiera data, through a global-layer `data_hash`
function named `console_config_data`, so a key set in both takes the
console's value and a key set only in the control repo is still found
there.

#### Scenario: Console wins
- **WHEN** a key is set in a group the node matches and in the control
  repo's `common.yaml`
- **THEN** `lookup()` in the node's catalog returns the console's value

#### Scenario: Fall through
- **WHEN** a key is set only in the control repo
- **THEN** `lookup()` returns the control repo's value

#### Scenario: Group priority
- **WHEN** two groups the node matches set the same key
- **THEN** the catalog holds the value of the group that wins by the merge
  order


### Requirement: A console that cannot answer fails the compile
The `console_config_data` function SHALL fetch the node's data from the
console with the same service token and address the ENC uses, and a
console that cannot answer SHALL fail the catalog compile rather than
return an empty answer.

#### Scenario: The console is down
- **WHEN** the console cannot answer a node's data request during a compile
- **THEN** the compile fails with an error naming the console, and no
  catalog is built from a partial answer

### Requirement: The image wires itself up without overriding an operator
The openvoxserver image SHALL, on every start, install the
`console_config_data` function where the server loads system functions
and write a global `hiera.yaml` containing the console entry when none
exists. When a global `hiera.yaml` exists that the image did not write and
that does not already name `console_config_data`, the image SHALL leave it
untouched and log the entry to add. Running it again SHALL change nothing.

#### Scenario: First start
- **WHEN** the image starts with no global `hiera.yaml`
- **THEN** one is written with the console entry first, and the function is
  installed

#### Scenario: An operator's hiera.yaml
- **WHEN** the image starts with a global `hiera.yaml` it did not write
- **THEN** the file is unchanged and the log names the entry to add

#### Scenario: Restart
- **WHEN** the container restarts
- **THEN** the function is installed again (the server's gem directory is
  not preserved) and the written `hiera.yaml` is unchanged

### Requirement: Console UI for configuration data
The node group page SHALL show a Configuration data editor, a JSON object
with the group's data, that saves with the group, shows the server's
reason on the field when refused, and is opened when the group has data.
The page SHALL say that the data is visible to everyone who can see node
groups and is not for secrets. Every new string SHALL be translated in all
15 languages.

#### Scenario: Editing
- **WHEN** an administrator types a JSON object and saves
- **THEN** the data is saved with the group and shown when the page is
  opened again

#### Scenario: Not JSON
- **WHEN** the text is not a JSON object
- **THEN** the field says so and nothing is saved

### Requirement: Configuration data changes are audited without values
The system SHALL record, for each change to a group's configuration data,
the keys added, changed and removed, and SHALL NOT record any value.

#### Scenario: A key is changed
- **WHEN** an administrator changes the value of `dns::timeout`
- **THEN** the audit entry lists `dns::timeout` as changed and contains
  neither the old nor the new value

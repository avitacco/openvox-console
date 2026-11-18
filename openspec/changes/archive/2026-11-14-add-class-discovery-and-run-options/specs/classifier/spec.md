## ADDED Requirements

### Requirement: Classes are chosen from the group's environment
The system SHALL let a user edit a group's classes by choosing classes
and their declared parameters from the code of the group's environment,
with value inputs suited to each parameter's type, while keeping the
underlying JSON viewable and editable. The environment used SHALL be
the group's own, else its nearest ancestor's, else `production`.

#### Scenario: Adding a class from the picker
- **WHEN** a user adds a class by choosing it from the list for the
  group's environment and saves
- **THEN** the group's classes include that class, exactly as if it had
  been entered as JSON

#### Scenario: Adding a declared parameter
- **WHEN** a user adds a parameter the class declares and enters a value
- **THEN** saving stores that parameter and value on the class, with a
  Boolean, Integer or Float parameter stored as that JSON type rather
  than as text

#### Scenario: A parameter left unset
- **WHEN** a user adds a class and sets none of its parameters
- **THEN** the class is saved with no parameters, so the code's own
  defaults and Hiera apply

#### Scenario: A group that inherits its environment
- **WHEN** a group sets no environment and its parent sets `staging`
- **THEN** the picker offers the classes of `staging`

#### Scenario: Switching to the JSON view
- **WHEN** a user switches the classes editor to its JSON view
- **THEN** the current classes are shown as JSON, and an edit made there
  is reflected in the structured editor when switched back

#### Scenario: A class or parameter not in the list
- **WHEN** a user enters a class name or parameter name the environment
  does not define
- **THEN** the editor accepts it and flags it, rather than refusing it

#### Scenario: Discovery is unavailable
- **WHEN** the group page is opened while class discovery is unavailable
- **THEN** classes and parameters are entered by name, the page says
  that the list of classes is unavailable and why, and saving works

#### Scenario: Choosing the group's environment
- **WHEN** a user edits the group's environment field
- **THEN** the server's environments are offered, and a name not among
  them is still accepted

#### Scenario: Refreshing the list
- **WHEN** a user with `classifier:write` asks the editor to refresh
  classes
- **THEN** the list is read again from the server and the page shows
  when it was read

### Requirement: Groups flag classes that do not match their environment's code
The system SHALL report, on the group page and as a mark in the groups
list, each class, parameter and value of a group that does not match
the code of its environment. These reports SHALL be advisory: they
SHALL NOT block a save or alter what is stored, and SHALL NOT be made
when discovery is unavailable or stale.

#### Scenario: A class removed from the code
- **WHEN** a group names a class and a later code deploy removes it from
  the group's environment
- **THEN** the group page flags the class as not defined in that
  environment, the groups list marks the group, and the group's stored
  classes are unchanged

#### Scenario: An undeclared parameter
- **WHEN** a group sets a parameter its class does not declare
- **THEN** the group page flags that parameter

#### Scenario: A value of the wrong type
- **WHEN** a group sets a text value on a parameter declared `Integer`
- **THEN** the group page flags the value as not fitting the type

#### Scenario: A type the system cannot check
- **WHEN** a parameter is declared with a type the system does not
  evaluate
- **THEN** no value for it is flagged

#### Scenario: Saving with flags present
- **WHEN** a user saves a group that has flagged classes or parameters
- **THEN** the save succeeds

#### Scenario: Discovery is unavailable
- **WHEN** discovery is unavailable or stale for the group's environment
- **THEN** no class or parameter of the group is flagged

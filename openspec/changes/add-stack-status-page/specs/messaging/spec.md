## ADDED Requirements

### Requirement: Ask every instance and collect replies
The system SHALL provide a mechanism for an internal component to ask a
question of every instance on the bus and collect their replies within a
caller-specified bounded window, returning the replies received and
indicating whether the window elapsed before every instance had answered.

This is distinct from the bus's existing patterns and cannot be built
from them: a fan-out subscription delivers a message to everyone but
carries no reply path, and a queue subscription deliberately reaches only
one member. Aggregating a fleet-wide answer needs both the fan-out and
the replies.

The window SHALL bound the wait, so a caller aggregating a reply per
instance completes in a predictable time however many instances are
running and whether or not any of them is unresponsive.

#### Scenario: Collecting replies from several instances
- **WHEN** a component asks a question on a subject that several
  instances answer
- **THEN** it receives the replies from every instance that answered
  within the window

#### Scenario: An instance does not answer
- **WHEN** one instance does not answer within the window
- **THEN** the caller still receives the other instances' replies
- **AND** the caller can tell that the window elapsed rather than every
  instance having answered

#### Scenario: Nobody answers
- **WHEN** no instance answers the question
- **THEN** the caller receives no replies and an indication that none
  arrived, rather than waiting indefinitely

#### Scenario: A single unclustered instance answers itself
- **WHEN** a component on an unclustered instance asks a question its own
  instance answers
- **THEN** it receives that instance's reply, with no peer required

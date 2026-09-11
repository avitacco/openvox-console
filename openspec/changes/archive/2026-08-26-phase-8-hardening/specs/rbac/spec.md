## ADDED Requirements

### Requirement: JWT signing key rotation
The system SHALL support more than one currently-valid token verification key at once, each identified by a key ID (`kid`) carried in a token's header, while signing every newly issued token with exactly one designated active key. Rotating keys - introducing a new active signing key while a prior key remains valid for verification only - SHALL NOT invalidate tokens already issued under the prior key before its own validity ends.

#### Scenario: A token signed by a previous key still verifies
- **WHEN** a key rotation introduces a new active signing key while the previous key remains in the configured verification set
- **THEN** a token issued and signed under the previous key still verifies successfully until that key is removed from the verification set

#### Scenario: New tokens are signed by the current active key
- **WHEN** a token is issued after a key rotation
- **THEN** it is signed with the newly active key and carries that key's `kid`

#### Scenario: A key removed from the verification set no longer verifies
- **WHEN** a previously-valid key is removed from the configured verification set
- **THEN** a token whose `kid` names that key is rejected

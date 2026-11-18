## ADDED Requirements

### Requirement: Password rules for local accounts
The system SHALL refuse to set a local account's password that fails the
account policy's minimum length or lacks a character kind the policy
requires, that is longer than 72 bytes, or that equals the account's
username ignoring case. This SHALL apply when an administrator creates a
user or sets a user's password and when a user changes their own. A
refusal SHALL name every rule the password fails.

#### Scenario: Too short
- **WHEN** an administrator creates a user with a password of 8
  characters and the policy's minimum length is 12
- **THEN** the request is refused, saying the password needs at least 12
  characters, and no user is created

#### Scenario: Missing required kinds
- **WHEN** the policy requires a digit and a symbol and a user changes
  their password to one with neither
- **THEN** the request is refused, naming both rules, and the password
  is unchanged

#### Scenario: The username as the password
- **WHEN** a password equal to the account's username in a different
  letter case is set
- **THEN** the request is refused

#### Scenario: A password that meets the rules
- **WHEN** a password meeting every rule is set
- **THEN** it is accepted and the user can log in with it

### Requirement: Existing passwords are not rechecked
The system SHALL NOT refuse a login because the account's password,
set before a rule existed or was tightened, would now fail the policy.

#### Scenario: A stricter policy
- **WHEN** an administrator raises the minimum length above the length
  of an existing user's password
- **THEN** that user still logs in with it, and the new rule applies the
  next time their password is set

### Requirement: The first administrator's password meets the rules
The system SHALL NOT create the first-start administrator when the
configured password fails the default password rules, and SHALL log
which rules it fails without logging the password.

#### Scenario: A short first-start password
- **WHEN** the console starts with no users and a configured
  administrator password of 6 characters
- **THEN** no administrator is created and the log names the minimum
  length

#### Scenario: An installation that already has users
- **WHEN** the console starts with users already present
- **THEN** the configured first-start password is not examined

### Requirement: Login lockout
The system SHALL lock a local account for the policy's lockout period
when the number of consecutive failed password logins for it reaches the
policy's threshold. While an account is locked, a password login for it
SHALL be refused whatever password is given. A successful password
login SHALL reset the count. The lock SHALL end by itself when the
period has passed, and the count SHALL start again from zero. A
threshold of zero SHALL turn lockout off.

#### Scenario: Reaching the threshold
- **WHEN** the threshold is 10 and a tenth consecutive login with a
  wrong password is made for an account
- **THEN** the account is locked for the lockout period

#### Scenario: The right password while locked
- **WHEN** a login with the correct password is made for a locked
  account
- **THEN** it is refused and no tokens are issued

#### Scenario: The lock ends
- **WHEN** the lockout period has passed
- **THEN** a login with the correct password succeeds

#### Scenario: A success in between
- **WHEN** an account has nine failed logins, then a successful one,
  then one failed login
- **THEN** the account is not locked

#### Scenario: Attempts while locked do not extend the lock
- **WHEN** further failed logins are made for an account that is locked
- **THEN** the time the lock ends does not change

#### Scenario: Several instances
- **WHEN** failed logins for one account are spread across the instances
  of a cluster
- **THEN** they are counted together

#### Scenario: Lockout turned off
- **WHEN** the threshold is zero
- **THEN** no number of failed logins locks an account

### Requirement: A locked account is indistinguishable from a wrong password
The system SHALL answer a password login for a locked account with the
same status and body as a login with a wrong password or an unknown
username.

#### Scenario: Comparing the answers
- **WHEN** a login is made for a locked account, for an existing account
  with a wrong password, and for a username that does not exist
- **THEN** the three responses have the same status and the same body

### Requirement: Lockout covers password login only
The system SHALL NOT refuse an identity-provider login, a token refresh,
a request made with an existing access token, or a service token because
the account is locked. The system SHALL count a wrong current password
given when a user changes their own password as a failed login, and
SHALL refuse that change while the account is locked.

#### Scenario: A session that is already open
- **WHEN** an account is locked while its user has an open session
- **THEN** that session continues and can be refreshed

#### Scenario: Identity-provider login
- **WHEN** a user whose account is locked signs in through the identity
  provider
- **THEN** the login succeeds

#### Scenario: Guessing the current password from an open session
- **WHEN** wrong current passwords are given to the change-password
  request until the threshold is reached
- **THEN** the account is locked, and a change with the correct current
  password is refused until the lock ends

### Requirement: An administrator can unlock an account
The system SHALL let a caller with `rbac:admin` unlock a locked account,
which ends the lock and resets the count. An administrator setting a
user's password SHALL unlock that account as well. User records read by
an administrator SHALL say whether the account is locked and until when.

#### Scenario: Unlocking
- **WHEN** an administrator unlocks a locked account
- **THEN** the user can log in at once with the correct password

#### Scenario: Setting a new password
- **WHEN** an administrator sets a new password for a locked account
- **THEN** the account is unlocked

#### Scenario: Without the permission
- **WHEN** a caller without `rbac:admin` asks to unlock an account
- **THEN** the request is rejected

### Requirement: The account policy is stored and changed in the console
The system SHALL keep one account policy for the whole installation:
minimum password length, which character kinds are required, the lockout
threshold and the lockout period. Any signed-in caller SHALL be able to
read it. Changing it SHALL require `rbac:admin`, SHALL refuse a value
out of range, and SHALL take effect on every instance without a restart.

#### Scenario: Before anything is changed
- **WHEN** the policy is read on a new installation
- **THEN** it is the defaults: length 12, no required kinds, threshold
  10, period 15 minutes

#### Scenario: The allowed ranges
- **WHEN** an administrator changes the policy
- **THEN** the minimum length may be 8 to 72, the threshold 0 or 3 to
  100, and the period 1 to 1,440 minutes

#### Scenario: Changing the policy
- **WHEN** an administrator sets the minimum length to 16
- **THEN** the next password set on any instance must have 16 characters

#### Scenario: A value out of range
- **WHEN** an administrator sets the minimum length to 4, or the
  threshold to 2
- **THEN** the request is refused, naming the field and its range

#### Scenario: A caller who is not an administrator
- **WHEN** a signed-in caller without `rbac:admin` reads the policy and
  then tries to change it
- **THEN** the read succeeds and the change is rejected

### Requirement: Account policy events are recorded
The system SHALL write to the audit log: an account being locked, an
account being unlocked and by whom, a login refused because the account
is locked, a password refused by the rules (naming the account and the
rules, never the password), and a change to the policy with the previous
and new values. The system SHALL write an account being locked, an
account being unlocked and a policy change to the activity log.

#### Scenario: A lockout
- **WHEN** an account is locked
- **THEN** an audit entry and an activity entry name the account

#### Scenario: A refused password
- **WHEN** a password is refused by the rules
- **THEN** an audit entry names the account, the actor and the rules
  failed, and contains no part of the password

#### Scenario: A policy change
- **WHEN** an administrator changes the lockout threshold from 10 to 5
- **THEN** the audit entry shows 10 and 5 and names the administrator

### Requirement: Console UI for the account policy
The system SHALL provide an Account policy page in the administration
area, restricted to users with `rbac:admin`, showing the policy and
letting it be changed. Every form in which a password is set SHALL show
the current rules beside the field and, on a refusal, the rules the
password failed. The login page's message for a failed login SHALL
mention that an account is locked for a time after repeated failures,
without saying whether this account is.

#### Scenario: Changing the policy on the page
- **WHEN** an administrator changes the lockout period and saves
- **THEN** the page shows the saved value and the change is in effect

#### Scenario: Rules beside the password field
- **WHEN** a user opens the change-password form and the policy requires
  14 characters and a digit
- **THEN** both rules are shown beside the new-password field

#### Scenario: A refused password in a form
- **WHEN** the form is submitted with a password failing two rules
- **THEN** both are shown on the field and what else was entered is kept

#### Scenario: A failed login
- **WHEN** a login fails for any reason
- **THEN** the page says the username or password is wrong, and that
  accounts are locked for a time after repeated failed attempts

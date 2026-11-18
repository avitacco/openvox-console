-- Account policy: password rules and login lockout
-- (add-plans-ui-and-account-policy).
--
-- One policy for the whole installation, in the database so that every
-- instance applies the same one without a restart. No row means the
-- defaults (internal/rbac.DefaultAccountPolicy), so nothing is written
-- until an administrator changes something.
CREATE TABLE account_policy (
    id                INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    min_length        INT NOT NULL,
    require_lowercase BOOLEAN NOT NULL,
    require_uppercase BOOLEAN NOT NULL,
    require_digit     BOOLEAN NOT NULL,
    require_symbol    BOOLEAN NOT NULL,
    lockout_threshold INT NOT NULL,
    lockout_minutes   INT NOT NULL,
    updated_by        TEXT NOT NULL,
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- failed_logins counts consecutive failed password logins. locked_until
-- is set when the count reaches the policy's threshold; a lock that has
-- passed is simply ignored, and the next failure starts the count again.
ALTER TABLE users ADD COLUMN failed_logins INT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN locked_until TIMESTAMPTZ;

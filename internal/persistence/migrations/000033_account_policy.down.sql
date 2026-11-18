-- Drops the policy and every account's lock state: afterwards no
-- account is locked and any password is accepted again.
ALTER TABLE users DROP COLUMN locked_until;
ALTER TABLE users DROP COLUMN failed_logins;
DROP TABLE account_policy;

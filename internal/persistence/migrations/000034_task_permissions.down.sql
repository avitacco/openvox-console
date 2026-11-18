-- Drops every task allowlist: afterwards every role and token may run
-- every task its permissions allow, as before the migration.
ALTER TABLE service_tokens DROP COLUMN task_patterns;
ALTER TABLE roles DROP COLUMN task_patterns;

-- The tasks a role or a service token may run, as patterns (a task name,
-- "module::*", or "*"). An empty list means every task, which is what
-- every row existing before this migration has, so nothing changes for
-- them. Validation lives in the application (internal/taskscope).
ALTER TABLE roles ADD COLUMN task_patterns TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE service_tokens ADD COLUMN task_patterns TEXT[] NOT NULL DEFAULT '{}';

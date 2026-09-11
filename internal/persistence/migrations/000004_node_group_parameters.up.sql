CREATE TABLE node_group_parameters (
    id          BIGSERIAL PRIMARY KEY,
    group_id    BIGINT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    param_name  TEXT NOT NULL,
    param_value JSONB NOT NULL,
    UNIQUE (group_id, param_name)
);

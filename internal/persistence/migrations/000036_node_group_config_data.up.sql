-- Configuration data on a node group: key/value pairs that openvoxserver
-- reads through Hiera (see openvoxserver-init). A value is any JSON value.
-- Validation of keys and sizes lives in the application (internal/classifier).
CREATE TABLE node_group_config_data (
    group_id BIGINT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    key      TEXT NOT NULL,
    value    JSONB NOT NULL,
    PRIMARY KEY (group_id, key)
);

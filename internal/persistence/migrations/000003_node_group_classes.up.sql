CREATE TABLE node_group_classes (
    id         BIGSERIAL PRIMARY KEY,
    group_id   BIGINT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    class_name TEXT NOT NULL,
    UNIQUE (group_id, class_name)
);

CREATE TABLE node_group_class_parameters (
    id          BIGSERIAL PRIMARY KEY,
    class_id    BIGINT NOT NULL REFERENCES node_group_classes(id) ON DELETE CASCADE,
    param_name  TEXT NOT NULL,
    param_value JSONB NOT NULL,
    UNIQUE (class_id, param_name)
);

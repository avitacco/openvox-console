CREATE TABLE node_group_rule_conditions (
    id        BIGSERIAL PRIMARY KEY,
    group_id  BIGINT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    fact_path TEXT NOT NULL,
    operator  TEXT NOT NULL,
    value     TEXT NOT NULL
);

CREATE TABLE node_group_pins (
    id       BIGSERIAL PRIMARY KEY,
    group_id BIGINT NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    certname TEXT NOT NULL,
    UNIQUE (group_id, certname)
);

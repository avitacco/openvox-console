CREATE TABLE roles (
    id   BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE role_permissions (
    id         BIGSERIAL PRIMARY KEY,
    role_id    BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission TEXT NOT NULL,
    UNIQUE (role_id, permission)
);

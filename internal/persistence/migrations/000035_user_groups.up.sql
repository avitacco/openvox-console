-- User groups: named sets of users that roles can be granted to. A user's
-- effective roles are the roles assigned to them directly (user_roles)
-- plus those granted to each group they belong to; the view below is the
-- one place that says so, and every query about what a user may do reads
-- it rather than user_roles.
CREATE TABLE user_groups (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX user_groups_name_key ON user_groups (lower(name));

CREATE TABLE user_group_members (
    group_id BIGINT NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX user_group_members_user_idx ON user_group_members (user_id);

CREATE TABLE user_group_roles (
    group_id BIGINT NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    role_id  BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, role_id)
);
CREATE INDEX user_group_roles_role_idx ON user_group_roles (role_id);

-- UNION (not UNION ALL) so a role held twice is one row.
CREATE VIEW user_effective_roles AS
    SELECT user_id, role_id FROM user_roles
    UNION
    SELECT m.user_id, gr.role_id
    FROM user_group_members m
    JOIN user_group_roles gr ON gr.group_id = m.group_id;

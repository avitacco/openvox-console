-- Roles are granted on node groups (add-group-scoped-node-access).
--
-- A role's permissions apply to the nodes in the groups named here and,
-- by inheritance, in every descendant of those groups. A role on the
-- root group is fleet-wide; a role with no rows covers no nodes.
--
-- role_id   cascades: deleting a role removes its grants.
-- group_id  restricts: a group cannot be deleted out from under a role
--           that is granted on it, whatever the application does.
CREATE TABLE role_node_groups (
    role_id  BIGINT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES node_groups(id) ON DELETE RESTRICT,
    PRIMARY KEY (role_id, group_id)
);

CREATE INDEX role_node_groups_group_idx ON role_node_groups (group_id);

-- Every role that exists today is fleet-wide, so it is granted on the
-- root: nobody sees more or less after the upgrade than before it.
INSERT INTO role_node_groups (role_id, group_id)
SELECT r.id, g.id FROM roles r CROSS JOIN node_groups g WHERE g.is_root;

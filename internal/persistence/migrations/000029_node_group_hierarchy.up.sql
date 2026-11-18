-- Node groups become a tree (add-group-hierarchy).
--
-- parent_id   every group but the root has one. ON DELETE RESTRICT so a
--             group with children cannot be removed out from under them,
--             whatever the application does.
-- is_root     marks the single built-in "All Nodes" group, which has no
--             parent and matches every node.
-- rule_match  whether a group's rule conditions must all hold ('all',
--             what every existing group does) or any one of them ('any').
--
-- priority becomes nullable for the root only. The root ranks below
-- every other group, and a real number there would both take a slot in
-- the unique range and, since a group ranks no lower than its parent,
-- lift every group in the fleet to at least that number.
ALTER TABLE node_groups
    ADD COLUMN parent_id  BIGINT REFERENCES node_groups(id) ON DELETE RESTRICT,
    ADD COLUMN is_root    BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN rule_match TEXT NOT NULL DEFAULT 'all'
        CONSTRAINT node_groups_rule_match_check CHECK (rule_match IN ('all', 'any')),
    ALTER COLUMN priority DROP NOT NULL;

-- An existing group already called "All Nodes" is an ordinary group with
-- its own rule, classes and priority. It is never adopted as the root:
-- the root matches every node, so adopting it would apply its classes to
-- nodes its rule did not match and change their classification. It is
-- renamed out of the way and keeps behaving exactly as it did.
UPDATE node_groups
   SET name = CASE
           WHEN EXISTS (SELECT 1 FROM node_groups WHERE name = 'All Nodes (previous)')
               THEN 'All Nodes (previous ' || id || ')'
           ELSE 'All Nodes (previous)'
       END
 WHERE name = 'All Nodes';

INSERT INTO node_groups (name, priority, is_root) VALUES ('All Nodes', NULL, true);

-- Every existing group becomes a direct child of the root. The root sets
-- nothing and narrows nothing, so classification is unchanged.
UPDATE node_groups
   SET parent_id = (SELECT id FROM node_groups WHERE is_root)
 WHERE NOT is_root;

CREATE UNIQUE INDEX node_groups_one_root_idx ON node_groups (is_root) WHERE is_root;
CREATE INDEX node_groups_parent_idx ON node_groups (parent_id);

ALTER TABLE node_groups
    ADD CONSTRAINT node_groups_root_has_no_parent CHECK (is_root = (parent_id IS NULL)),
    ADD CONSTRAINT node_groups_priority_required  CHECK (is_root OR priority IS NOT NULL),
    ADD CONSTRAINT node_groups_not_own_parent     CHECK (parent_id <> id);

-- Re-flattens the groups. The root is deleted along with anything set on
-- it, and every other group keeps its own rule, classes and priority, so
-- classification goes back to priority alone. A group renamed
-- "All Nodes (previous)" by the up migration keeps that name.
ALTER TABLE node_groups
    DROP CONSTRAINT node_groups_root_has_no_parent,
    DROP CONSTRAINT node_groups_priority_required,
    DROP CONSTRAINT node_groups_not_own_parent;

DROP INDEX node_groups_one_root_idx;
DROP INDEX node_groups_parent_idx;

UPDATE node_groups SET parent_id = NULL;
DELETE FROM node_groups WHERE is_root;

ALTER TABLE node_groups
    DROP COLUMN parent_id,
    DROP COLUMN is_root,
    DROP COLUMN rule_match,
    ALTER COLUMN priority SET NOT NULL;

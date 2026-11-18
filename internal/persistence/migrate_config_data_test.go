package persistence

import "testing"

func TestMigrate_ConfigData_UpDownUp(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 35)
	// A group from before the upgrade must survive it both ways and have
	// no data.
	execSQL(t, dsn, `INSERT INTO node_groups (name, priority, parent_id) SELECT 'before', 5, id FROM node_groups WHERE is_root`)

	migrateTo(t, dsn, 36)
	execSQL(t, dsn, `DO $$ BEGIN
	   IF (SELECT count(*) FROM node_group_config_data) <> 0 THEN
	     RAISE EXCEPTION 'an existing group started with config data';
	   END IF;
	 END $$`,
		`INSERT INTO node_group_config_data (group_id, key, value) SELECT id, 'a::b', '{"x": [1, 2]}' FROM node_groups WHERE name = 'before'`,
	)
	// One value per key per group.
	if err := tryExecSQL(dsn, `INSERT INTO node_group_config_data (group_id, key, value) SELECT id, 'a::b', '1' FROM node_groups WHERE name = 'before'`); err == nil {
		t.Fatal("a second value for the same key was accepted")
	}
	// Deleting the group removes its data.
	execSQL(t, dsn, `DELETE FROM node_groups WHERE name = 'before'`, `DO $$ BEGIN
	   IF (SELECT count(*) FROM node_group_config_data) <> 0 THEN RAISE EXCEPTION 'data outlived its group'; END IF;
	 END $$`)

	migrateTo(t, dsn, 35)
	migrateTo(t, dsn, 36)
}

package persistence

import "testing"

func TestMigrate_PatchInventory_UpDownUp(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 36)
	migrateTo(t, dsn, 37)

	// The settings row exists with its defaults.
	execSQL(t, dsn, `DO $$ BEGIN
	   IF (SELECT interval_minutes FROM patch_scan_settings) <> 1440 OR NOT (SELECT enabled FROM patch_scan_settings) THEN
	     RAISE EXCEPTION 'wrong default settings';
	   END IF;
	 END $$`)
	// A second settings row is refused, and so is an interval out of range.
	if err := tryExecSQL(dsn, `INSERT INTO patch_scan_settings (id) VALUES (FALSE)`); err == nil {
		t.Fatal("a second settings row was accepted")
	}
	if err := tryExecSQL(dsn, `UPDATE patch_scan_settings SET interval_minutes = 5`); err == nil {
		t.Fatal("an interval under 15 minutes was accepted")
	}

	execSQL(t, dsn,
		`INSERT INTO node_patch_scans (certname, attempted_at) VALUES ('web01', now())`,
		`INSERT INTO node_patch_updates (certname, name, available) VALUES ('web01', 'curl', '1')`,
	)
	if err := tryExecSQL(dsn, `INSERT INTO node_patch_updates (certname, name, available) VALUES ('web01', 'curl', '2')`); err == nil {
		t.Fatal("a package listed twice for one node was accepted")
	}
	execSQL(t, dsn, `DELETE FROM node_patch_scans WHERE certname = 'web01'`, `DO $$ BEGIN
	   IF (SELECT count(*) FROM node_patch_updates) <> 0 THEN RAISE EXCEPTION 'updates outlived their scan'; END IF;
	 END $$`)

	migrateTo(t, dsn, 36)
	migrateTo(t, dsn, 37)
}

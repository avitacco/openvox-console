package persistence

import "testing"

func TestMigrate_AgentlessTargets_UpDownUp(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 38)
	migrateTo(t, dsn, 39)

	// A target needs a login of its own or a credential's.
	if err := tryExecSQL(dsn, `INSERT INTO agentless_targets (name, address, created_by, updated_by) VALUES ('a', '10.0.0.1', 'x', 'x')`); err == nil {
		t.Fatal("a target without any login was accepted")
	}
	if err := tryExecSQL(dsn, `INSERT INTO agentless_credentials (name, username, created_by, updated_by) VALUES ('c', 'u', 'x', 'x')`); err == nil {
		t.Fatal("a credential without a secret was accepted")
	}
	execSQL(t, dsn,
		`INSERT INTO agentless_credentials (name, username, sealed_password, created_by, updated_by) VALUES ('shared', 'ops', '\x01', 'x', 'x')`,
		`INSERT INTO agentless_targets (name, address, credential_id, created_by, updated_by) SELECT 'a', '10.0.0.1', id, 'x', 'x' FROM agentless_credentials`,
	)
	if err := tryExecSQL(dsn, `INSERT INTO agentless_targets (name, address, credential_id, created_by, updated_by) SELECT 'a', '10.0.0.2', id, 'x', 'x' FROM agentless_credentials`); err == nil {
		t.Fatal("a duplicate target name was accepted")
	}
	if err := tryExecSQL(dsn, `INSERT INTO agentless_targets (name, address, port, credential_id, created_by, updated_by) SELECT 'b', '10.0.0.2', 70000, id, 'x', 'x' FROM agentless_credentials`); err == nil {
		t.Fatal("a port out of range was accepted")
	}
	// A credential in use cannot be deleted out from under its targets.
	if err := tryExecSQL(dsn, `DELETE FROM agentless_credentials`); err == nil {
		t.Fatal("a credential in use was deleted")
	}

	migrateTo(t, dsn, 38)
	migrateTo(t, dsn, 39)
}

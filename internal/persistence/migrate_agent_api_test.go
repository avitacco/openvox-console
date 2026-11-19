package persistence

import "testing"

func TestMigrate_AgentAPI_UpDownUp(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 40)
	execSQL(t, dsn, `INSERT INTO jobs (kind, status, triggered_by, started_at) VALUES ('run', 'running', 'a', now())`)

	migrateTo(t, dsn, 41)
	execSQL(t, dsn,
		`INSERT INTO agent_tokens (name, jti, owner, permissions, expires_at) VALUES ('bot', 'j1', 'admin', '{nodes:read}', now() + interval '1 day')`,
		`INSERT INTO agent_requests (token_id, token_name, kind, summary, body, expires_at)
		   SELECT id, name, 'task', 'run x::y', '{}', now() + interval '1 hour' FROM agent_tokens`,
		`UPDATE jobs SET agent_request_id = 1, agent_token = 'bot', approved_by = 'admin'`,
	)
	// A request's kind and status are bounded.
	if err := tryExecSQL(dsn, `INSERT INTO agent_requests (token_id, token_name, kind, summary, body, expires_at)
	   SELECT id, name, 'shell', 'x', '{}', now() FROM agent_tokens`); err == nil {
		t.Fatal("an unknown request kind was accepted")
	}
	if err := tryExecSQL(dsn, `UPDATE agent_requests SET status = 'maybe'`); err == nil {
		t.Fatal("an unknown request status was accepted")
	}
	// A request outlives its token, which it no longer points at.
	execSQL(t, dsn, `DELETE FROM agent_tokens`, `DO $$ BEGIN
	   IF (SELECT count(*) FROM agent_requests WHERE token_id IS NULL AND token_name = 'bot') <> 1 THEN RAISE EXCEPTION 'the request did not outlive its token'; END IF;
	 END $$`)

	migrateTo(t, dsn, 40)
	if err := tryExecSQL(dsn, `SELECT agent_token FROM jobs`); err == nil {
		t.Fatal("down left jobs.agent_token")
	}
	execSQL(t, dsn, `DO $$ BEGIN
	   IF (SELECT count(*) FROM jobs) <> 1 THEN RAISE EXCEPTION 'down lost a job'; END IF;
	 END $$`)
	migrateTo(t, dsn, 41)
}

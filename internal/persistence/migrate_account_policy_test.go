package persistence

import "testing"

func TestMigrate_AccountPolicy_UpDownUp(t *testing.T) {
	dsn := scratchDatabase(t, testDSN(t))
	migrateTo(t, dsn, 32)
	// A user from before the upgrade must survive it both ways.
	execSQL(t, dsn, `INSERT INTO users (username, password_hash) VALUES ('before', 'x')`)

	migrateTo(t, dsn, 33)
	if !tableExists(t, dsn, "account_policy") {
		t.Fatal("up: account_policy missing")
	}
	execSQL(t, dsn,
		// The existing user starts unlocked, with no failures counted.
		`DO $$ BEGIN
		   IF (SELECT count(*) FROM users WHERE failed_logins = 0 AND locked_until IS NULL) <> 1 THEN
		     RAISE EXCEPTION 'an existing user did not start unlocked';
		   END IF;
		 END $$`,
		`INSERT INTO account_policy (min_length, require_lowercase, require_uppercase, require_digit, require_symbol, lockout_threshold, lockout_minutes, updated_by)
		 VALUES (14, false, false, true, false, 5, 30, 'admin')`,
		// There is one policy: a second row is refused.
		`DO $$ BEGIN
		   BEGIN
		     INSERT INTO account_policy (id, min_length, require_lowercase, require_uppercase, require_digit, require_symbol, lockout_threshold, lockout_minutes, updated_by)
		     VALUES (2, 8, false, false, false, false, 0, 1, 'admin');
		     RAISE EXCEPTION 'a second policy row was accepted';
		   EXCEPTION WHEN check_violation THEN NULL;
		   END;
		 END $$`,
		`UPDATE users SET failed_logins = 3, locked_until = now() + interval '5 minutes'`,
	)

	migrateTo(t, dsn, 32)
	if tableExists(t, dsn, "account_policy") {
		t.Fatal("down: account_policy still present")
	}
	execSQL(t, dsn, `DO $$ BEGIN
	   IF (SELECT count(*) FROM users WHERE username = 'before') <> 1 THEN RAISE EXCEPTION 'the user was lost'; END IF;
	 END $$`)
	migrateTo(t, dsn, 33)
	if !tableExists(t, dsn, "account_policy") {
		t.Fatal("up again: account_policy missing")
	}
}

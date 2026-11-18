package app_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/voxpupuli/enterprise-console/internal/runtime"
	"github.com/voxpupuli/enterprise-console/internal/testapp"
)

// lockedBuffer is a log sink an instance can go on writing to after the
// test body has returned.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func userCount(t *testing.T, dsn string) int {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	return n
}

func loginStatus(t *testing.T, inst *testapp.Instance, username, password string) int {
	t.Helper()
	resp := inst.PostJSON(t, "/api/v1/auth/login", "", map[string]string{"username": username, "password": password})
	defer resp.Body.Close()
	return resp.StatusCode
}

// The first administrator's password meets the account policy like any
// other. One that does not creates nobody, and the log says which rule
// - without the password.
func TestBootstrap_AShortFirstStartPasswordCreatesNoAdministrator(t *testing.T) {
	const short = "sixchr"
	cluster := testapp.NewCluster(t)
	logs := &lockedBuffer{}
	inst := cluster.Start(testapp.Config{Log: logs, Env: map[string]string{"CONSOLE_BOOTSTRAP_ADMIN_PASSWORD": short}})

	if n := userCount(t, cluster.DSN()); n != 0 {
		t.Fatalf("%d users exist after starting with a 6-character first-start password, want none", n)
	}
	if status := loginStatus(t, inst, testapp.BootstrapAdminUser, short); status != http.StatusUnauthorized {
		t.Errorf("login with the refused password = %d, want 401", status)
	}
	out := logs.String()
	if !strings.Contains(out, "CONSOLE_BOOTSTRAP_ADMIN_PASSWORD does not meet the account policy") || !strings.Contains(out, "at least 12 characters") || !strings.Contains(out, "min_length") {
		t.Errorf("the log does not name the rule:\n%s", out)
	}
	if strings.Contains(out, short) {
		t.Errorf("the log contains the password:\n%s", out)
	}

	// A second instance with a password that meets the rules, on the
	// same still-empty database, creates the administrator.
	second := cluster.Start(testapp.Config{})
	if status := loginStatus(t, second, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword); status != http.StatusOK {
		t.Fatalf("login after starting with a good first-start password = %d, want 200", status)
	}

	// Once users exist the configured password is not examined: an
	// instance started with the short one again says nothing about it.
	more := &lockedBuffer{}
	cluster.Start(testapp.Config{Log: more, Env: map[string]string{"CONSOLE_BOOTSTRAP_ADMIN_PASSWORD": short}})
	if strings.Contains(more.String(), "does not meet the account policy") {
		t.Errorf("the first-start password was examined with users already present:\n%s", more.String())
	}
	if n := userCount(t, cluster.DSN()); n != 1 {
		t.Errorf("%d users, want the one administrator", n)
	}
}

// Failed logins are counted in the database, so spreading them over
// the instances of a cluster locks the account at the threshold all the
// same - and a policy changed through one instance is the policy on the
// other.
func TestAccountPolicy_SharedAcrossInstances(t *testing.T) {
	cluster := testapp.NewCluster(t)
	instances := clusterOf(t, cluster, runtime.ModeAll, 2)
	a, b := sweepClient{t: t, inst: instances[0]}, sweepClient{t: t, inst: instances[1]}
	admin := instances[0].Login(t, testapp.BootstrapAdminUser, testapp.BootstrapAdminPassword)

	// Changed on the first instance: lock after four failures.
	if status, body := a.do(http.MethodPut, "/api/v1/account-policy", admin, map[string]any{
		"minLength": 14, "requireDigit": true, "lockoutThreshold": 4, "lockoutMinutes": 5,
	}); status != http.StatusOK {
		t.Fatalf("change policy = %d %s", status, body)
	}
	// In force on the second, with no restart.
	if status, body := b.get("/api/v1/account-policy", admin); status != http.StatusOK || !strings.Contains(body, `"minLength":14`) || !strings.Contains(body, `"lockoutThreshold":4`) {
		t.Fatalf("policy as the second instance reads it = %d %s", status, body)
	}
	if status, body := b.do(http.MethodPost, "/api/v1/users", admin, map[string]string{"username": "too-weak", "password": "thirteen-char"}); status != http.StatusBadRequest || !strings.Contains(body, "min_length") || !strings.Contains(body, "digit") {
		t.Errorf("a password the new policy refuses, set through the second instance = %d %s", status, body)
	}

	var user struct {
		ID int64 `json:"id"`
	}
	const password = "spread-over-2-instances"
	a.send(http.MethodPost, "/api/v1/users", admin, map[string]string{"username": "guessed", "password": password}, &user)

	// Four wrong passwords, alternating between the two instances.
	for i := 0; i < 4; i++ {
		if status := loginStatus(t, instances[i%2], "guessed", "not-the-password"); status != http.StatusUnauthorized {
			t.Fatalf("failed login %d = %d", i+1, status)
		}
	}
	for i, inst := range instances {
		if status := loginStatus(t, inst, "guessed", password); status != http.StatusUnauthorized {
			t.Errorf("login with the right password on instance %d after four failures across both = %d, want 401: the account should be locked", i+1, status)
		}
	}
	path := fmt.Sprintf("/api/v1/users/%d", user.ID)
	if status, body := b.get(path, admin); status != http.StatusOK || !strings.Contains(body, `"lockedUntil":"`) {
		t.Errorf("the user as the second instance reads it = %d %s, want it locked", status, body)
	}

	// Unlocked through one instance, the user logs in on the other.
	if status, body := b.do(http.MethodPost, path+"/unlock", admin, nil); status != http.StatusOK {
		t.Fatalf("unlock = %d %s", status, body)
	}
	if status := loginStatus(t, instances[0], "guessed", password); status != http.StatusOK {
		t.Errorf("login after the unlock = %d, want 200", status)
	}

	// The lock and the unlock are in the activity log.
	if status, body := a.get("/api/v1/audit-log?pageSize=50", admin); status != http.StatusOK ||
		!strings.Contains(body, "user.locked") || !strings.Contains(body, "user.unlocked") || !strings.Contains(body, "account_policy.updated") {
		t.Errorf("activity log = %d %s; want the lock, the unlock and the policy change", status, body)
	}
}

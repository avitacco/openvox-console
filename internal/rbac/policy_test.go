package rbac

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
	"github.com/voxpupuli/enterprise-console/internal/messaging"
)

func TestCheckPassword(t *testing.T) {
	strict := AccountPolicy{MinLength: 12, RequireLowercase: true, RequireUppercase: true, RequireDigit: true, RequireSymbol: true}
	plain := AccountPolicy{MinLength: 12}
	cases := []struct {
		name     string
		policy   AccountPolicy
		username string
		password string
		want     []string
	}{
		{"meets every rule", strict, "alice", "Correct-horse-9", nil},
		{"too short", plain, "alice", "elevenchars", []string{RuleMinLength}},
		{"exactly the minimum", plain, "alice", "twelve-chars", nil},
		{"no required kinds, all lowercase", plain, "alice", "correct horse battery", nil},
		{"missing a digit and a symbol", strict, "alice", "CorrectHorseBattery", []string{RuleDigit, RuleSymbol}},
		{"missing everything", strict, "alice", "            ", []string{RuleLowercase, RuleUppercase, RuleDigit, RuleSymbol}},
		{"a space is not a symbol", strict, "alice", "Correct horse 9", []string{RuleSymbol}},
		{"the username", plain, "alice-the-admin", "alice-the-admin", []string{RuleUsername}},
		{"the username in another case", plain, "alice-the-admin", "ALICE-The-Admin", []string{RuleUsername}},
		// Twelve characters of two bytes each: counted as a person counts.
		{"multi-byte characters count once each for the minimum", plain, "alice", strings.Repeat("é", 12), nil},
		{"eleven multi-byte characters", plain, "alice", strings.Repeat("é", 11), []string{RuleMinLength}},
		// bcrypt's limit is in bytes.
		{"72 bytes", plain, "alice", strings.Repeat("a", 72), nil},
		{"73 bytes", plain, "alice", strings.Repeat("a", 73), []string{RuleMaxLength}},
		{"36 two-byte characters are 72 bytes", plain, "alice", strings.Repeat("é", 36), nil},
		{"37 two-byte characters are 74 bytes", plain, "alice", strings.Repeat("é", 37), []string{RuleMaxLength}},
		{"non-ASCII letters and digits count", strict, "alice", "Ünïcödé-pässwörd-٣", nil},
	}
	for _, tc := range cases {
		if got := CheckPassword(tc.policy, tc.username, tc.password); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: failed rules = %v, want %v", tc.name, got, tc.want)
		}
	}

	// A refusal names every rule in words, and none of the password.
	err := checkPassword(strict, "alice", "short")
	for _, want := range []string{"at least 12 characters", "an uppercase letter", "a digit", "a symbol"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q lacks %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "short") {
		t.Errorf("refusal contains the password: %q", err)
	}
}

func TestAccountPolicy_Validate(t *testing.T) {
	ok := DefaultAccountPolicy()
	if ok.MinLength != 12 || ok.RequireLowercase || ok.RequireUppercase || ok.RequireDigit || ok.RequireSymbol || ok.LockoutThreshold != 10 || ok.LockoutMinutes != 15 {
		t.Errorf("defaults = %+v", ok)
	}
	change := func(f func(*AccountPolicy)) AccountPolicy { p := ok; f(&p); return p }
	cases := []struct {
		name   string
		policy AccountPolicy
		field  string
	}{
		{"the defaults", ok, ""},
		{"length 8", change(func(p *AccountPolicy) { p.MinLength = 8 }), ""},
		{"length 72", change(func(p *AccountPolicy) { p.MinLength = 72 }), ""},
		{"length 4", change(func(p *AccountPolicy) { p.MinLength = 4 }), "minLength"},
		{"length 73", change(func(p *AccountPolicy) { p.MinLength = 73 }), "minLength"},
		{"lockout off", change(func(p *AccountPolicy) { p.LockoutThreshold = 0 }), ""},
		{"threshold 3", change(func(p *AccountPolicy) { p.LockoutThreshold = 3 }), ""},
		{"threshold 2", change(func(p *AccountPolicy) { p.LockoutThreshold = 2 }), "lockoutThreshold"},
		{"threshold 101", change(func(p *AccountPolicy) { p.LockoutThreshold = 101 }), "lockoutThreshold"},
		{"a negative threshold", change(func(p *AccountPolicy) { p.LockoutThreshold = -1 }), "lockoutThreshold"},
		{"a day", change(func(p *AccountPolicy) { p.LockoutMinutes = 1440 }), ""},
		{"no period", change(func(p *AccountPolicy) { p.LockoutMinutes = 0 }), "lockoutMinutes"},
		{"over a day", change(func(p *AccountPolicy) { p.LockoutMinutes = 1441 }), "lockoutMinutes"},
	}
	for _, tc := range cases {
		err := tc.policy.Validate()
		invalid, _ := err.(*InvalidPolicyError)
		switch {
		case tc.field == "" && err != nil:
			t.Errorf("%s: refused: %v", tc.name, err)
		case tc.field != "" && (invalid == nil || invalid.Field != tc.field):
			t.Errorf("%s: err = %v, want it refused on %s", tc.name, err, tc.field)
		}
	}
}

// policyEnv is the rbac routes over a real store, with every audit and
// activity entry captured. The account policy is one row shared by the
// whole test database, so it is put back when the test ends.
type policyEnv struct {
	t        *testing.T
	store    *Store
	mux      *http.ServeMux
	issuer   *Issuer
	verifier *Verifier
	tag      string

	mu       sync.Mutex
	audit    []auditlog.Event
	activity []string
}

func newPolicyEnv(t *testing.T) *policyEnv {
	t.Helper()
	e := &policyEnv{t: t, store: testStore(t), mux: http.NewServeMux(), tag: strconv.FormatInt(time.Now().UnixNano(), 36)}
	ctx := context.Background()

	var had bool
	if err := e.store.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM account_policy)`).Scan(&had); err != nil {
		t.Fatal(err)
	}
	before, err := e.store.AccountPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.pool.Exec(ctx, `DELETE FROM account_policy`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.store.pool.Exec(ctx, `DELETE FROM account_policy`)
		if had {
			_, _ = e.store.SetAccountPolicy(ctx, before, before.UpdatedBy)
		}
		_, _ = e.store.pool.Exec(ctx, `DELETE FROM users WHERE username LIKE '%' || $1`, e.tag)
	})

	key := testKey(t)
	bus, err := messaging.Start()
	if err != nil {
		t.Fatalf("messaging.Start() error: %v", err)
	}
	t.Cleanup(bus.Close)
	revoker := NewRevoker(bus, newFakeRevokedTokenStore())
	if err := revoker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	e.issuer = NewIssuer(key, testKid)
	e.verifier = singleKeyVerifier(testKid, key, revoker)

	record := func(event auditlog.Event) {
		e.mu.Lock()
		e.audit = append(e.audit, event)
		e.mu.Unlock()
	}
	fromRequest := func(r *http.Request, event auditlog.Event) {
		if claims, ok := ClaimsFromContext(r.Context()); ok {
			event.Actor = claims.Subject
		}
		record(event)
	}
	auth := NewAuthService(e.store, e.issuer, e.verifier, revoker, record)
	auth.SetActivityRecorder(func(action, actor, summary string) {
		e.mu.Lock()
		e.activity = append(e.activity, action+" "+summary)
		e.mu.Unlock()
	})
	oidc := NewOIDCService(ctx, discardLogger(), e.store, e.issuer, OIDCConfig{}, noopRecordSystemActivity, noopRecordAudit)
	NewHandlers(auth, e.store, e.verifier, revoker, e.issuer, oidc,
		func(r *http.Request, action, summary string) {
			e.mu.Lock()
			e.activity = append(e.activity, action+" "+summary)
			e.mu.Unlock()
		}, fromRequest, fromRequest, func(*http.Request, auditlog.Event) {},
	).Register(e.mux)
	return e
}

// name makes a username unique to this test.
func (e *policyEnv) name(s string) string { return s + "-" + e.tag }

const goodPassword = "correct horse battery staple"

func (e *policyEnv) user(name string) User {
	e.t.Helper()
	u, err := e.store.CreateUser(context.Background(), e.name(name), goodPassword)
	if err != nil {
		e.t.Fatalf("CreateUser() error: %v", err)
	}
	return u
}

func (e *policyEnv) read(u User) User {
	e.t.Helper()
	got, err := e.store.GetUser(context.Background(), u.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	return got
}

func (e *policyEnv) setPolicy(change func(*AccountPolicy)) {
	e.t.Helper()
	p := DefaultAccountPolicy()
	change(&p)
	if _, err := e.store.SetAccountPolicy(context.Background(), p, "test"); err != nil {
		e.t.Fatalf("SetAccountPolicy() error: %v", err)
	}
}

// token is an access token for subject with permissions.
func (e *policyEnv) token(subject string, permissions ...string) string {
	e.t.Helper()
	token, _, err := e.issuer.IssueUserToken(subject, TokenTypeAccess, permissions, nil, time.Hour)
	if err != nil {
		e.t.Fatal(err)
	}
	return token
}

func (e *policyEnv) do(method, path, token string, body any) (int, map[string]any, string) {
	var payload bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&payload).Encode(body)
	}
	req := httptest.NewRequest(method, path, &payload)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	var decoded map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	return rec.Code, decoded, rec.Body.String()
}

func (e *policyEnv) login(username, password string) (int, map[string]any, string) {
	return e.do("POST", "/api/v1/auth/login", "", map[string]string{"username": username, "password": password})
}

// fail makes n failed logins for u.
func (e *policyEnv) fail(u User, n int) {
	e.t.Helper()
	for i := 0; i < n; i++ {
		if code, _, _ := e.login(u.Username, "wrong-password"); code != http.StatusUnauthorized {
			e.t.Fatalf("failed login %d = %d, want 401", i+1, code)
		}
	}
}

// expireLock moves u's lock into the past, as the passing of the
// lockout period would.
func (e *policyEnv) expireLock(u User) {
	e.t.Helper()
	if _, err := e.store.pool.Exec(context.Background(), `UPDATE users SET locked_until = now() - interval '1 second' WHERE id = $1`, u.ID); err != nil {
		e.t.Fatal(err)
	}
}

func (e *policyEnv) audited(action string) []auditlog.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	var found []auditlog.Event
	for _, event := range e.audit {
		if event.Action == action {
			found = append(found, event)
		}
	}
	return found
}

func (e *policyEnv) activityContaining(s string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var found []string
	for _, a := range e.activity {
		if strings.Contains(a, s) {
			found = append(found, a)
		}
	}
	return found
}

func TestAccountPolicy_StoredAndChanged(t *testing.T) {
	e := newPolicyEnv(t)
	admin, reader := e.token("policy-admin", "rbac:admin"), e.token("policy-reader", "nodes:read")

	// Before anything is changed: the defaults, readable by anyone
	// signed in and by nobody who is not.
	code, policy, raw := e.do("GET", "/api/v1/account-policy", reader, nil)
	if code != http.StatusOK || policy["minLength"] != float64(12) || policy["lockoutThreshold"] != float64(10) || policy["lockoutMinutes"] != float64(15) ||
		policy["requireDigit"] != false || policy["requireSymbol"] != false || policy["requireLowercase"] != false || policy["requireUppercase"] != false {
		t.Fatalf("default policy = %d %s", code, raw)
	}
	if code, _, _ := e.do("GET", "/api/v1/account-policy", "", nil); code != http.StatusUnauthorized {
		t.Errorf("unauthenticated read = %d, want 401", code)
	}

	// Someone who is not an administrator cannot change it.
	change := map[string]any{"minLength": 16, "requireLowercase": false, "requireUppercase": false, "requireDigit": true, "requireSymbol": false, "lockoutThreshold": 5, "lockoutMinutes": 30}
	if code, _, raw := e.do("PUT", "/api/v1/account-policy", reader, change); code != http.StatusForbidden {
		t.Errorf("change without rbac:admin = %d %s, want 403", code, raw)
	}

	// Out of range: refused naming the field, and nothing changes.
	for field, value := range map[string]int{"minLength": 4, "lockoutThreshold": 2, "lockoutMinutes": 0} {
		bad := map[string]any{}
		for k, v := range change {
			bad[k] = v
		}
		bad[field] = value
		code, body, raw := e.do("PUT", "/api/v1/account-policy", admin, bad)
		if code != http.StatusBadRequest || body["field"] != field || !strings.Contains(raw, "between") {
			t.Errorf("%s = %d: %d %s, want 400 naming the field and its range", field, value, code, raw)
		}
	}
	if p, _ := e.store.AccountPolicy(context.Background()); p.MinLength != 12 {
		t.Errorf("a refused change was stored: %+v", p)
	}

	code, saved, raw := e.do("PUT", "/api/v1/account-policy", admin, change)
	if code != http.StatusOK || saved["minLength"] != float64(16) || saved["requireDigit"] != true || saved["updatedBy"] != "policy-admin" {
		t.Fatalf("change = %d %s", code, raw)
	}
	// It is in force for the next password set - read from the
	// database, which is what makes it apply on every instance.
	code, body, raw := e.do("POST", "/api/v1/users", admin, map[string]string{"username": e.name("after-change"), "password": "fifteen-chars-9"})
	if code != http.StatusBadRequest || !reflect.DeepEqual(body["rules"], []any{RuleMinLength}) {
		t.Errorf("a 15-character password under a minimum of 16 = %d %s", code, raw)
	}

	events := e.audited("account_policy.updated")
	if len(events) != 1 || events[0].Actor != "policy-admin" {
		t.Fatalf("audit = %+v", events)
	}
	before, after := events[0].Before.(map[string]any), events[0].After.(map[string]any)
	if before["lockoutThreshold"] != 10 || after["lockoutThreshold"] != 5 || before["minLength"] != 12 || after["minLength"] != 16 {
		t.Errorf("audit before %v after %v", before, after)
	}
	if found := e.activityContaining("account_policy.updated"); len(found) != 1 || !strings.Contains(found[0], "at least 16 characters") || !strings.Contains(found[0], "after 5 failed logins") {
		t.Errorf("activity = %v", found)
	}
}

func TestPasswordRules_AppliedWhereverAPasswordIsSet(t *testing.T) {
	e := newPolicyEnv(t)
	e.setPolicy(func(p *AccountPolicy) { p.RequireDigit, p.RequireSymbol = true, true })
	admin := e.token("policy-admin", "rbac:admin")
	const weak = "nodigitsorsymbols"
	ctx := context.Background()

	// Creating a user.
	code, body, raw := e.do("POST", "/api/v1/users", admin, map[string]string{"username": e.name("new"), "password": "short"})
	if code != http.StatusBadRequest || !reflect.DeepEqual(body["rules"], []any{RuleMinLength, RuleDigit, RuleSymbol}) || !strings.Contains(raw, "at least 12 characters") {
		t.Errorf("create with a short password = %d %s", code, raw)
	}
	if fields, _ := body["fields"].(map[string]any); fields["password"] == nil {
		t.Errorf("fields = %v, want password", body["fields"])
	}
	if _, err := e.store.GetUserByUsername(ctx, e.name("new")); err != ErrNotFound {
		t.Errorf("a user was created with a refused password: %v", err)
	}
	if code, _, raw := e.do("POST", "/api/v1/users", admin, map[string]string{"username": e.name("new"), "password": e.name("new")}); code != http.StatusBadRequest || !strings.Contains(raw, RuleUsername) {
		t.Errorf("the username as the password = %d %s", code, raw)
	}
	if code, _, raw := e.do("POST", "/api/v1/users", admin, map[string]string{"username": e.name("new"), "password": "Good-enough-9-password"}); code != http.StatusCreated {
		t.Fatalf("create with a good password = %d %s", code, raw)
	}
	if code, _, raw := e.login(e.name("new"), "Good-enough-9-password"); code != http.StatusOK {
		t.Errorf("login with the accepted password = %d %s", code, raw)
	}

	// An administrator setting a password.
	u := e.user("existing")
	path := "/api/v1/users/" + strconv.FormatInt(u.ID, 10)
	code, body, raw = e.do("PUT", path, admin, map[string]string{"password": weak})
	if code != http.StatusBadRequest || !reflect.DeepEqual(body["rules"], []any{RuleDigit, RuleSymbol}) {
		t.Errorf("admin setting a weak password = %d %s", code, raw)
	}
	// Refused whole: a rename sent with it is not applied either.
	e.do("PUT", path, admin, map[string]string{"username": e.name("renamed"), "password": weak})
	if got := e.read(u); got.Username != u.Username {
		t.Errorf("username = %q after a refused change", got.Username)
	}
	if code, _, _ := e.login(u.Username, goodPassword); code != http.StatusOK {
		t.Errorf("the old password after refused changes = %d, want it still to work", code)
	}

	// A user changing their own.
	own := e.token(u.Username)
	code, body, raw = e.do("PUT", "/api/v1/me", own, map[string]string{"currentPassword": goodPassword, "newPassword": weak})
	if code != http.StatusBadRequest || !reflect.DeepEqual(body["rules"], []any{RuleDigit, RuleSymbol}) {
		t.Errorf("changing to a weak password = %d %s", code, raw)
	}
	if code, _, raw := e.do("PUT", "/api/v1/me", own, map[string]string{"currentPassword": goodPassword, "newPassword": "A-better-password-42"}); code != http.StatusNoContent {
		t.Errorf("changing to a good password = %d %s", code, raw)
	}
	if code, _, _ := e.login(u.Username, "A-better-password-42"); code != http.StatusOK {
		t.Errorf("login with the changed password = %d", code)
	}

	// Each refusal is audited with the account and the rules, and no
	// part of any password.
	refused := e.audited("user.password.rejected")
	if len(refused) != 5 {
		t.Fatalf("%d refusals audited, want 5: %+v", len(refused), refused)
	}
	if refused[0].Actor != "policy-admin" || refused[0].ResourceID != e.name("new") || !reflect.DeepEqual(refused[0].After.(map[string]any)["rules"], []string{RuleMinLength, RuleDigit, RuleSymbol}) {
		t.Errorf("first refusal = %+v", refused[0])
	}
	e.mu.Lock()
	everything, _ := json.Marshal(e.audit)
	e.mu.Unlock()
	for _, secret := range []string{weak, "short", "Good-enough-9-password", "A-better-password-42", goodPassword} {
		if strings.Contains(string(everything), secret) {
			t.Errorf("the audit log contains a password (%q)", secret)
		}
	}
}

func TestPasswordRules_ExistingPasswordsAreNotRechecked(t *testing.T) {
	e := newPolicyEnv(t)
	u := e.user("old-password")
	e.setPolicy(func(p *AccountPolicy) { p.MinLength, p.RequireSymbol, p.RequireDigit = 40, true, true })
	if code, _, raw := e.login(u.Username, goodPassword); code != http.StatusOK {
		t.Errorf("login with a password the stricter policy would refuse = %d %s", code, raw)
	}
	if code, _, _ := e.do("PUT", "/api/v1/me", e.token(u.Username), map[string]string{"currentPassword": goodPassword, "newPassword": goodPassword + "x"}); code != http.StatusBadRequest {
		t.Errorf("setting a password under the stricter policy = %d, want 400", code)
	}
}

func TestLockout_ReachingTheThresholdAndBack(t *testing.T) {
	e := newPolicyEnv(t)
	e.setPolicy(func(p *AccountPolicy) { p.LockoutThreshold, p.LockoutMinutes = 10, 15 })
	u := e.user("lockme")

	e.fail(u, 9)
	if got := e.read(u); got.Locked() || got.FailedLogins != 9 {
		t.Fatalf("after nine failures: locked %v, count %d", got.Locked(), got.FailedLogins)
	}
	e.fail(u, 1)
	locked := e.read(u)
	if !locked.Locked() {
		t.Fatal("the tenth failure did not lock the account")
	}
	if d := time.Until(*locked.LockedUntil); d < 14*time.Minute || d > 16*time.Minute {
		t.Errorf("locked for %s, want 15 minutes", d)
	}

	// The right password is refused, exactly as a wrong one and as an
	// unknown username are.
	rightCode, _, right := e.login(u.Username, goodPassword)
	wrongCode, _, wrong := e.login(e.user("bystander").Username, "wrong-password")
	unknownCode, _, unknown := e.login(e.name("nobody"), "wrong-password")
	if rightCode != http.StatusUnauthorized || rightCode != wrongCode || rightCode != unknownCode || right != wrong || right != unknown {
		t.Errorf("locked: %d %q; wrong password: %d %q; unknown user: %d %q; want all three identical", rightCode, right, wrongCode, wrong, unknownCode, unknown)
	}
	if strings.Contains(right, "lock") {
		t.Errorf("the response to a locked account says so: %s", right)
	}

	// More attempts do not move the end of the lock.
	e.fail(u, 5)
	if again := e.read(u); !again.LockedUntil.Equal(*locked.LockedUntil) || again.FailedLogins != 10 {
		t.Errorf("lock end moved from %v to %v (count %d)", locked.LockedUntil, again.LockedUntil, again.FailedLogins)
	}

	// The lock ends by itself, and the count starts again.
	e.expireLock(u)
	e.fail(u, 1)
	if got := e.read(u); got.Locked() || got.FailedLogins != 1 {
		t.Errorf("first failure after the lock ended: locked %v, count %d; want unlocked and 1", got.Locked(), got.FailedLogins)
	}
	if code, _, raw := e.login(u.Username, goodPassword); code != http.StatusOK {
		t.Errorf("login after the lock ended = %d %s", code, raw)
	}
	if got := e.read(u); got.FailedLogins != 0 {
		t.Errorf("count after a successful login = %d, want 0", got.FailedLogins)
	}

	// Recorded: the lock once, in both logs, and each refusal while
	// locked.
	if events := e.audited("user.locked"); len(events) != 1 || events[0].ResourceID != u.Username {
		t.Errorf("user.locked audit = %+v", events)
	}
	if found := e.activityContaining("user.locked"); len(found) != 1 || !strings.Contains(found[0], u.Username) || !strings.Contains(found[0], "15 minutes") {
		t.Errorf("activity = %v", found)
	}
	if events := e.audited("user.login.locked"); len(events) != 6 {
		t.Errorf("%d logins refused while locked were audited, want 6", len(events))
	}
}

func TestLockout_ASuccessInBetweenResetsTheCount(t *testing.T) {
	e := newPolicyEnv(t)
	u := e.user("almost")
	e.fail(u, 9)
	if code, _, _ := e.login(u.Username, goodPassword); code != http.StatusOK {
		t.Fatalf("login = %d", code)
	}
	e.fail(u, 1)
	if got := e.read(u); got.Locked() || got.FailedLogins != 1 {
		t.Errorf("locked %v, count %d; want unlocked with one failure", got.Locked(), got.FailedLogins)
	}
}

func TestLockout_TurnedOff(t *testing.T) {
	e := newPolicyEnv(t)
	e.setPolicy(func(p *AccountPolicy) { p.LockoutThreshold = 0 })
	u := e.user("never")
	e.fail(u, 25)
	if got := e.read(u); got.Locked() {
		t.Error("an account was locked with lockout turned off")
	}
	if code, _, _ := e.login(u.Username, goodPassword); code != http.StatusOK {
		t.Errorf("login = %d", code)
	}
}

// Failures arriving at once - as they do from several instances - are
// each counted, and the account is locked once.
func TestLockout_ConcurrentFailuresAreCountedTogether(t *testing.T) {
	e := newPolicyEnv(t)
	u := e.user("contended")
	ctx := context.Background()

	var wg sync.WaitGroup
	var mu sync.Mutex
	var lockTimes []time.Time
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			locked, until, err := e.store.RecordFailedLogin(ctx, u.ID, 10, 15*time.Minute)
			if err != nil {
				t.Errorf("RecordFailedLogin() error: %v", err)
			}
			if locked {
				mu.Lock()
				lockTimes = append(lockTimes, *until)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(lockTimes) != 1 {
		t.Errorf("the account was locked %d times, want once", len(lockTimes))
	}
	if got := e.read(u); !got.Locked() || got.FailedLogins != 10 {
		t.Errorf("locked %v, count %d; want locked at exactly the threshold", got.Locked(), got.FailedLogins)
	}

	// Below the threshold nothing is lost either.
	v := e.user("counted")
	for i := 0; i < 7; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = e.store.RecordFailedLogin(ctx, v.ID, 10, time.Minute)
		}()
	}
	wg.Wait()
	if got := e.read(v); got.FailedLogins != 7 {
		t.Errorf("count = %d after seven concurrent failures, want 7", got.FailedLogins)
	}
}

func TestLockout_CoversPasswordLoginOnly(t *testing.T) {
	e := newPolicyEnv(t)
	e.setPolicy(func(p *AccountPolicy) { p.LockoutThreshold = 3 })
	u := e.user("session")

	// A session opened before the lock.
	code, pair, raw := e.login(u.Username, goodPassword)
	if code != http.StatusOK {
		t.Fatalf("login = %d %s", code, raw)
	}
	access, refresh := pair["accessToken"].(string), pair["refreshToken"].(string)
	service, _, err := e.issuer.IssueToken("ci-bot", TokenTypeService, []string{"nodes:read"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	e.fail(u, 3)
	if !e.read(u).Locked() {
		t.Fatal("not locked")
	}

	// The open session goes on, and can be refreshed.
	if code, _, raw := e.do("GET", "/api/v1/me", access, nil); code != http.StatusOK {
		t.Errorf("a request with the existing access token = %d %s", code, raw)
	}
	code, refreshed, raw := e.do("POST", "/api/v1/auth/refresh", "", map[string]string{"refreshToken": refresh})
	if code != http.StatusOK || refreshed["accessToken"] == nil {
		t.Errorf("refresh while locked = %d %s", code, raw)
	}
	// A service token is no account's, and is untouched.
	if _, err := e.verifier.Verify(service); err != nil {
		t.Errorf("service token while an account is locked: %v", err)
	}
	if code, _, raw := e.do("GET", "/api/v1/account-policy", service, nil); code != http.StatusOK {
		t.Errorf("a request with a service token = %d %s", code, raw)
	}
}

func TestLockout_GuessingTheCurrentPasswordFromAnOpenSession(t *testing.T) {
	e := newPolicyEnv(t)
	e.setPolicy(func(p *AccountPolicy) { p.LockoutThreshold = 3 })
	u := e.user("stolen-session")
	own := e.token(u.Username)

	for i := 0; i < 3; i++ {
		code, _, raw := e.do("PUT", "/api/v1/me", own, map[string]string{"firstName": "Guess " + strconv.Itoa(i), "currentPassword": "a-wrong-guess", "newPassword": "A-new-password-42"})
		if code != http.StatusUnauthorized || !strings.Contains(raw, "incorrect") {
			t.Fatalf("guess %d = %d %s", i+1, code, raw)
		}
	}
	got := e.read(u)
	if !got.Locked() {
		t.Fatal("three wrong current passwords did not lock the account")
	}
	// The profile part of each request was still applied.
	if got.FirstName == nil || *got.FirstName != "Guess 2" {
		t.Errorf("first name = %v, want the profile change applied", got.FirstName)
	}

	// The correct current password is refused until the lock ends.
	code, _, raw := e.do("PUT", "/api/v1/me", own, map[string]string{"currentPassword": goodPassword, "newPassword": "A-new-password-42"})
	if code != http.StatusUnauthorized || !strings.Contains(raw, "locked") {
		t.Errorf("change with the right password while locked = %d %s", code, raw)
	}
	if code, _, _ := e.login(u.Username, "A-new-password-42"); code == http.StatusOK {
		t.Error("the password was changed while the account was locked")
	}
	e.expireLock(u)
	if code, _, raw := e.do("PUT", "/api/v1/me", own, map[string]string{"currentPassword": goodPassword, "newPassword": "A-new-password-42"}); code != http.StatusNoContent {
		t.Errorf("change after the lock ended = %d %s", code, raw)
	}
	if len(e.audited("user.locked")) != 1 {
		t.Errorf("user.locked audit = %+v", e.audited("user.locked"))
	}
}

func TestLockout_AnAdministratorUnlocks(t *testing.T) {
	e := newPolicyEnv(t)
	e.setPolicy(func(p *AccountPolicy) { p.LockoutThreshold = 3 })
	admin := e.token("policy-admin", "rbac:admin")
	u := e.user("locked-out")
	path := "/api/v1/users/" + strconv.FormatInt(u.ID, 10)
	e.fail(u, 3)

	// The administrator's view says the account is locked, and until
	// when.
	code, view, raw := e.do("GET", path, admin, nil)
	until, _ := view["lockedUntil"].(string)
	if code != http.StatusOK || until == "" {
		t.Fatalf("user as read = %d %s, want lockedUntil", code, raw)
	}
	if _, _, list := e.do("GET", "/api/v1/users", admin, nil); !strings.Contains(list, until) {
		t.Errorf("the user list does not mark the locked account")
	}

	// Without the permission.
	if code, _, _ := e.do("POST", path+"/unlock", e.token("someone", "nodes:read"), nil); code != http.StatusForbidden {
		t.Errorf("unlock without rbac:admin = %d, want 403", code)
	}
	if !e.read(u).Locked() {
		t.Fatal("a refused unlock unlocked the account")
	}

	code, view, raw = e.do("POST", path+"/unlock", admin, nil)
	if code != http.StatusOK || view["lockedUntil"] != nil {
		t.Errorf("unlock = %d %s", code, raw)
	}
	if code, _, raw := e.login(u.Username, goodPassword); code != http.StatusOK {
		t.Errorf("login straight after an unlock = %d %s", code, raw)
	}
	if code, _, _ := e.do("POST", "/api/v1/users/999999999/unlock", admin, nil); code != http.StatusNotFound {
		t.Errorf("unlocking a user that does not exist = %d, want 404", code)
	}

	// Setting a new password unlocks as well.
	e.fail(u, 3)
	if code, _, raw := e.do("PUT", path, admin, map[string]string{"password": "A-reset-password-77"}); code != http.StatusNoContent {
		t.Fatalf("admin password set = %d %s", code, raw)
	}
	if code, _, raw := e.login(u.Username, "A-reset-password-77"); code != http.StatusOK {
		t.Errorf("login after an administrator set the password = %d %s", code, raw)
	}

	// Unlocking an account that is not locked records nothing.
	e.do("POST", path+"/unlock", admin, nil)
	events := e.audited("user.unlocked")
	if len(events) != 2 || events[0].Actor != "policy-admin" || events[0].ResourceID != u.Username {
		t.Errorf("user.unlocked audit = %+v, want two, by the administrator", events)
	}
	if found := e.activityContaining("user.unlocked"); len(found) != 2 {
		t.Errorf("activity = %v", found)
	}
}

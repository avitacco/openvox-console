package rbac

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// roleCall sends body to the role create (id 0) or update handler and
// returns the recorder.
func roleCall(t *testing.T, h *Handlers, id int64, body string) *httptest.ResponseRecorder {
	t.Helper()
	method, path := http.MethodPost, "/api/v1/roles"
	if id != 0 {
		method, path = http.MethodPut, "/api/v1/roles/"+strconv.FormatInt(id, 10)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(requestAs("admin").Context())
	rec := httptest.NewRecorder()
	if id == 0 {
		h.createRole(rec, req)
	} else {
		req.SetPathValue("id", strconv.FormatInt(id, 10))
		h.updateRole(rec, req)
	}
	return rec
}

func decodeRole(t *testing.T, rec *httptest.ResponseRecorder) Role {
	t.Helper()
	var role Role
	if err := json.Unmarshal(rec.Body.Bytes(), &role); err != nil {
		t.Fatalf("decode role: %v: %s", err, rec.Body.String())
	}
	return role
}

func TestRole_TaskPatterns_SavedAndRead(t *testing.T) {
	h, store, _ := newTestHandlers(t)

	rec := roleCall(t, h, 0, `{"name":"tp-patch","permissions":["orchestrator:run"],"taskPatterns":["patch::*","patch::*","package::install"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	created := decodeRole(t, rec)
	t.Cleanup(func() { _ = store.DeleteRole(context.Background(), created.ID) })
	want := []string{"patch::*", "package::install"} // a repeat is dropped
	if !reflect.DeepEqual(created.TaskPatterns, want) {
		t.Errorf("created patterns = %v, want %v", created.TaskPatterns, want)
	}

	got, err := store.GetRole(context.Background(), created.ID)
	if err != nil || !reflect.DeepEqual(got.TaskPatterns, want) {
		t.Errorf("stored patterns = %v (%v), want %v", got.TaskPatterns, err, want)
	}

	// An update that does not send the field keeps the list.
	rec = roleCall(t, h, created.ID, `{"name":"tp-patch","permissions":["orchestrator:run","orchestrator:read"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", rec.Code, rec.Body.String())
	}
	if got := decodeRole(t, rec).TaskPatterns; !reflect.DeepEqual(got, want) {
		t.Errorf("an update that sent no list changed it: %v", got)
	}
	if stored, _ := store.GetRole(context.Background(), created.ID); !reflect.DeepEqual(stored.TaskPatterns, want) {
		t.Errorf("stored after a partial update = %v", stored.TaskPatterns)
	}

	// Sent empty, it allows every task again.
	rec = roleCall(t, h, created.ID, `{"name":"tp-patch","permissions":["orchestrator:run"],"taskPatterns":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update to empty = %d: %s", rec.Code, rec.Body.String())
	}
	if stored, _ := store.GetRole(context.Background(), created.ID); len(stored.TaskPatterns) != 0 || stored.TaskPatterns == nil {
		t.Errorf("stored after clearing = %#v, want an empty list", stored.TaskPatterns)
	}
}

func TestRole_NoTaskPatterns_ReadsAsAnEmptyList(t *testing.T) {
	h, store, _ := newTestHandlers(t)
	rec := roleCall(t, h, 0, `{"name":"tp-none","permissions":["nodes:read"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	created := decodeRole(t, rec)
	t.Cleanup(func() { _ = store.DeleteRole(context.Background(), created.ID) })
	if !strings.Contains(rec.Body.String(), `"taskPatterns":[]`) {
		t.Errorf("a role with no allowlist serializes %s, want taskPatterns as an empty list", rec.Body.String())
	}
}

func TestRole_BadTaskPattern_IsRefusedNamingTheField(t *testing.T) {
	h, store, _ := newTestHandlers(t)
	rec := roleCall(t, h, 0, `{"name":"tp-keep","permissions":["nodes:read"],"taskPatterns":["patch::*"]}`)
	created := decodeRole(t, rec)
	t.Cleanup(func() { _ = store.DeleteRole(context.Background(), created.ID) })

	for _, bad := range []string{`"pa*ch"`, `"::*"`, `""`, `"Patch::apply"`} {
		rec := roleCall(t, h, 0, `{"name":"tp-bad","permissions":["nodes:read"],"taskPatterns":[`+bad+`]}`)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"taskPatterns"`) {
			t.Errorf("create with %s = %d %s, want 400 naming taskPatterns", bad, rec.Code, rec.Body.String())
		}
		if _, err := store.GetRoleByName(context.Background(), "tp-bad"); err == nil {
			t.Errorf("a role was created with %s", bad)
		}

		rec = roleCall(t, h, created.ID, `{"name":"tp-keep","permissions":["nodes:read"],"taskPatterns":[`+bad+`]}`)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"taskPatterns"`) {
			t.Errorf("update with %s = %d %s, want 400 naming taskPatterns", bad, rec.Code, rec.Body.String())
		}
	}
	// The role is unchanged by the refused updates.
	if stored, _ := store.GetRole(context.Background(), created.ID); !reflect.DeepEqual(stored.TaskPatterns, []string{"patch::*"}) {
		t.Errorf("a refused update changed the list: %v", stored.TaskPatterns)
	}

	tooMany := make([]string, 21)
	for i := range tooMany {
		tooMany[i] = `"a::t` + strconv.Itoa(i) + `"`
	}
	rec = roleCall(t, h, 0, `{"name":"tp-many","permissions":["nodes:read"],"taskPatterns":[`+strings.Join(tooMany, ",")+`]}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("21 patterns = %d, want 400", rec.Code)
	}
}

func TestRole_TaskPatternChange_IsAudited(t *testing.T) {
	h, store, captured := newTestHandlers(t)
	rec := roleCall(t, h, 0, `{"name":"tp-audit","permissions":["orchestrator:run"]}`)
	created := decodeRole(t, rec)
	t.Cleanup(func() { _ = store.DeleteRole(context.Background(), created.ID) })
	captured.write = nil

	rec = roleCall(t, h, created.ID, `{"name":"tp-audit","permissions":["orchestrator:run"],"taskPatterns":["patch::*"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d: %s", rec.Code, rec.Body.String())
	}
	if len(captured.write) != 1 {
		t.Fatalf("write events = %v, want 1", captured.write)
	}
	before, _ := captured.write[0].Before.(roleAudit)
	after, _ := captured.write[0].After.(roleAudit)
	if len(before.TaskPatterns) != 0 || !reflect.DeepEqual(after.TaskPatterns, []string{"patch::*"}) {
		t.Errorf("audit before %v after %v, want empty then patch::*", before.TaskPatterns, after.TaskPatterns)
	}
}

func serviceTokenCall(t *testing.T, h *Handlers, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/service-tokens", strings.NewReader(body)).WithContext(requestAs("admin").Context())
	rec := httptest.NewRecorder()
	h.createServiceToken(rec, req)
	return rec
}

func TestServiceToken_TaskPatterns(t *testing.T) {
	h, store, captured := newTestHandlers(t)
	t.Cleanup(func() {
		tokens, _ := store.ListServiceTokens(context.Background())
		for _, st := range tokens {
			if strings.HasPrefix(st.Name, "tp-") {
				_ = store.DeleteServiceToken(context.Background(), st.ID)
			}
		}
	})

	rec := serviceTokenCall(t, h, `{"name":"tp-deploy","permissions":["orchestrator:run"],"taskPatterns":["deploy::*"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body.String())
	}
	var resp serviceTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || !reflect.DeepEqual(resp.TaskPatterns, []string{"deploy::*"}) {
		t.Errorf("created token patterns = %v (%v)", resp.TaskPatterns, err)
	}
	if len(captured.write) == 0 || !strings.Contains(jsonOf(t, captured.write[len(captured.write)-1].After), "deploy::*") {
		t.Errorf("the creation audit entry does not carry the allowlist: %+v", captured.write)
	}

	// Without a list, a token reads as allowing every task.
	rec = serviceTokenCall(t, h, `{"name":"tp-plain","permissions":["orchestrator:run"]}`)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"taskPatterns":[]`) {
		t.Errorf("create without a list = %d %s", rec.Code, rec.Body.String())
	}
	tokens, _ := store.ListServiceTokens(context.Background())
	found := 0
	for _, st := range tokens {
		switch st.Name {
		case "tp-deploy":
			found++
			if !reflect.DeepEqual(st.TaskPatterns, []string{"deploy::*"}) {
				t.Errorf("listed patterns = %v", st.TaskPatterns)
			}
		case "tp-plain":
			found++
			if st.TaskPatterns == nil || len(st.TaskPatterns) != 0 {
				t.Errorf("a token with no list lists %#v", st.TaskPatterns)
			}
		}
	}
	if found != 2 {
		t.Errorf("found %d of the two tokens", found)
	}

	rec = serviceTokenCall(t, h, `{"name":"tp-bad","permissions":["orchestrator:run"],"taskPatterns":["pa*ch"]}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"taskPatterns"`) {
		t.Errorf("a bad pattern = %d %s, want 400 naming taskPatterns", rec.Code, rec.Body.String())
	}
	tokens, _ = store.ListServiceTokens(context.Background())
	for _, st := range tokens {
		if st.Name == "tp-bad" {
			t.Error("a token was created with a bad pattern")
		}
	}
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

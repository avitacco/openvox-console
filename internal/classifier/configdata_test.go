package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/internal/auditlog"
)

func TestValidateConfigData(t *testing.T) {
	big := strings.Repeat("x", MaxConfigDataValueBytes)
	tooMany := map[string]any{}
	for i := 0; i <= MaxConfigDataKeys; i++ {
		tooMany["k"+strings.Repeat("a", i%5)+string(rune('a'+i%26))+strings.Repeat("b", i/26)] = 1
	}
	tests := []struct {
		name    string
		data    map[string]any
		badKey  string
		wantErr bool
	}{
		{"nil", nil, "", false},
		{"typed values", map[string]any{"ntp::servers": []any{"a", "b"}, "dns::timeout": 5.0, "x.y-z_1": true, "n": nil, "m": map[string]any{"a": 1.0}}, "", false},
		{"lookup_options is reserved", map[string]any{"lookup_options": map[string]any{}}, "lookup_options", true},
		{"a space", map[string]any{"has space": 1}, "has space", true},
		{"an empty key", map[string]any{"": 1}, "", true},
		{"a slash", map[string]any{"a/b": 1}, "a/b", true},
		{"a long key", map[string]any{strings.Repeat("k", MaxConfigDataKeyLength+1): 1}, "", true},
		{"a value over the limit", map[string]any{"big": big}, "big", true},
		{"too many keys", tooMany, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfigData(tt.data)
			var cfg *ConfigDataError
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !errors.As(err, &cfg) {
				t.Fatalf("err = %T, want *ConfigDataError", err)
			}
			if tt.wantErr && tt.badKey != "" && cfg.Key != tt.badKey {
				t.Errorf("key = %q, want %q", cfg.Key, tt.badKey)
			}
		})
	}
}

func TestStore_ConfigData(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	data := map[string]any{
		"ntp::servers": []any{"a.example", "b.example"},
		"dns::timeout": 5.0,
		"app::opts":    map[string]any{"a": 1.0, "b": []any{true, nil}},
	}
	created, err := s.CreateGroup(ctx, Group{Name: "config-data-store-test", Priority: 107001, ConfigData: data})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	cleanupGroup(t, s, created.ID)

	t.Run("round trips with its types", func(t *testing.T) {
		got, err := s.GetGroup(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.ConfigData, data) {
			t.Errorf("ConfigData = %#v, want %#v", got.ConfigData, data)
		}
	})

	t.Run("an update that does not send it keeps it", func(t *testing.T) {
		got, _ := s.GetGroup(ctx, created.ID)
		got.ConfigData = nil
		got.Name = "config-data-store-test-renamed"
		if err := s.UpdateGroup(ctx, got); err != nil {
			t.Fatal(err)
		}
		after, _ := s.GetGroup(ctx, created.ID)
		if !reflect.DeepEqual(after.ConfigData, data) {
			t.Errorf("ConfigData after an update without it = %#v, want it kept", after.ConfigData)
		}
	})

	t.Run("an update that sends it replaces it", func(t *testing.T) {
		got, _ := s.GetGroup(ctx, created.ID)
		got.ConfigData = map[string]any{"only": "this"}
		if err := s.UpdateGroup(ctx, got); err != nil {
			t.Fatal(err)
		}
		after, _ := s.GetGroup(ctx, created.ID)
		if !reflect.DeepEqual(after.ConfigData, map[string]any{"only": "this"}) {
			t.Errorf("ConfigData = %#v, want only the new key", after.ConfigData)
		}
	})

	t.Run("an invalid update changes nothing", func(t *testing.T) {
		got, _ := s.GetGroup(ctx, created.ID)
		got.ConfigData = map[string]any{"lookup_options": 1}
		var cfg *ConfigDataError
		if err := s.UpdateGroup(ctx, got); !errors.As(err, &cfg) {
			t.Fatalf("err = %v, want *ConfigDataError", err)
		}
		after, _ := s.GetGroup(ctx, created.ID)
		if !reflect.DeepEqual(after.ConfigData, map[string]any{"only": "this"}) {
			t.Errorf("a refused update changed the data: %#v", after.ConfigData)
		}
	})

	t.Run("an empty map clears it, and it reads as an empty object", func(t *testing.T) {
		got, _ := s.GetGroup(ctx, created.ID)
		got.ConfigData = map[string]any{}
		if err := s.UpdateGroup(ctx, got); err != nil {
			t.Fatal(err)
		}
		after, _ := s.GetGroup(ctx, created.ID)
		if after.ConfigData == nil || len(after.ConfigData) != 0 {
			t.Errorf("ConfigData = %#v, want an empty non-nil map", after.ConfigData)
		}
	})

	t.Run("a new group with none reads as an empty object", func(t *testing.T) {
		g, err := s.CreateGroup(ctx, Group{Name: "config-data-store-test-none", Priority: 107002})
		if err != nil {
			t.Fatal(err)
		}
		cleanupGroup(t, s, g.ID)
		if g.ConfigData == nil {
			t.Error("CreateGroup returned nil ConfigData")
		}
		got, _ := s.GetGroup(ctx, g.ID)
		if got.ConfigData == nil {
			t.Error("GetGroup returned nil ConfigData")
		}
	})
}

func TestClassify_ConfigData(t *testing.T) {
	root := rootGroup()
	root.ConfigData = map[string]any{"dns::timeout": 5.0, "root::only": "r"}
	parent := Group{ID: 2, Name: "parent", ParentID: int64Ptr(1), Priority: 10, Pins: []string{"n1"},
		ConfigData: map[string]any{"dns::timeout": 3.0, "app::opts": map[string]any{"a": 1.0, "b": 2.0}}}
	child := Group{ID: 3, Name: "child", ParentID: int64Ptr(2), Priority: 5, Pins: []string{"n1"},
		ConfigData: map[string]any{"dns::timeout": 2.0}}
	sibling := Group{ID: 4, Name: "sibling", ParentID: int64Ptr(1), Priority: 20, Pins: []string{"n1"},
		ConfigData: map[string]any{"app::opts": map[string]any{"a": 9.0}, "sibling::only": true}}
	other := Group{ID: 5, Name: "other", ParentID: int64Ptr(1), Priority: 30, Pins: []string{"someone-else"},
		ConfigData: map[string]any{"never": "seen"}}
	groups := []Group{root, parent, child, sibling, other}

	got := NewTree(groups).Classify("n1", nil).ConfigData
	want := map[string]any{
		"dns::timeout":  2.0,                      // the child beats its parent and the root
		"root::only":    "r",                      // keys combine across groups
		"app::opts":     map[string]any{"a": 9.0}, // the higher priority wins whole: no deep merge
		"sibling::only": true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ConfigData = %#v\nwant %#v", got, want)
	}

	if none := NewTree([]Group{rootGroup()}).Classify("n1", nil).ConfigData; none == nil || len(none) != 0 {
		t.Errorf("a node with no data got %#v, want an empty non-nil map", none)
	}
}

func TestDiffConfigData(t *testing.T) {
	got := DiffConfigData(
		map[string]any{"same": 1.0, "changed": "a", "removed": true, "deep": map[string]any{"x": 1.0}},
		map[string]any{"same": 1.0, "changed": "b", "added": []any{1.0}, "deep": map[string]any{"x": 1.0}},
	)
	want := ConfigDataChange{Added: []string{"added"}, Changed: []string{"changed"}, Removed: []string{"removed"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("diff = %+v, want %+v", got, want)
	}
	if !DiffConfigData(nil, nil).Empty() {
		t.Error("two empty maps differ")
	}
}

// The handlers on a real store: a refusal names the field, an update that
// omits the data keeps it, and the audit log holds key names and no value.
func TestHandlers_ConfigData(t *testing.T) {
	s := testStore(t)
	var events []auditlog.Event
	h := NewHandlers(s, noopRecordActivity,
		func(_ *http.Request, e auditlog.Event) { events = append(events, e) }, noopRecordAudit)

	post := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.createGroup(rec, httptest.NewRequest(http.MethodPost, "/api/v1/groups", strings.NewReader(body)))
		return rec
	}
	put := func(id int64, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/v1/groups/x", strings.NewReader(body))
		req.SetPathValue("id", jsonInt(id))
		h.updateGroup(rec, req)
		return rec
	}

	if rec := post(`{"name":"cfg-h-bad","priority":107011,"configData":{"has space":1}}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), `"configData"`) || !strings.Contains(rec.Body.String(), "has space") {
		t.Fatalf("a bad key = %d %s, want 400 naming the field and the key", rec.Code, rec.Body)
	}

	const secretA, secretB = "sentinel-secret-AAA", "sentinel-secret-BBB"
	rec := post(`{"name":"cfg-h","priority":107012,"configData":{"db::password":"` + secretA + `","dns::timeout":5}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	var created struct {
		ID         int64          `json:"id"`
		ConfigData map[string]any `json:"configData"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	cleanupGroup(t, s, created.ID)
	if created.ConfigData["dns::timeout"] != 5.0 {
		t.Errorf("created configData = %v", created.ConfigData)
	}

	// An update that leaves configData out keeps it.
	rec = put(created.ID, `{"name":"cfg-h-renamed","priority":107012}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d %s", rec.Code, rec.Body)
	}
	var kept struct {
		ConfigData map[string]any `json:"configData"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &kept)
	if len(kept.ConfigData) != 2 {
		t.Errorf("configData after an update without it = %v, want it kept", kept.ConfigData)
	}

	// Changing one value, and removing one key.
	events = nil
	rec = put(created.ID, `{"name":"cfg-h-renamed","priority":107012,"configData":{"db::password":"`+secretB+`","added::key":true}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update = %d %s", rec.Code, rec.Body)
	}
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	raw, _ := json.Marshal(events[0])
	for _, secret := range []string{secretA, secretB} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("the audit entry contains a configuration value: %s", raw)
		}
	}
	after, _ := events[0].After.(auditedGroup)
	if after.ConfigDataChange == nil ||
		!reflect.DeepEqual(after.ConfigDataChange.Added, []string{"added::key"}) ||
		!reflect.DeepEqual(after.ConfigDataChange.Changed, []string{"db::password"}) ||
		!reflect.DeepEqual(after.ConfigDataChange.Removed, []string{"dns::timeout"}) {
		t.Errorf("the audit entry's change = %+v, want added/changed/removed by key", after.ConfigDataChange)
	}

	// An empty object clears it.
	rec = put(created.ID, `{"name":"cfg-h-renamed","priority":107012,"configData":{}}`)
	var cleared struct {
		ConfigData map[string]any `json:"configData"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &cleared)
	if rec.Code != http.StatusOK || cleared.ConfigData == nil || len(cleared.ConfigData) != 0 {
		t.Errorf("clearing = %d %v", rec.Code, cleared.ConfigData)
	}
}

func jsonInt(n int64) string { b, _ := json.Marshal(n); return string(b) }

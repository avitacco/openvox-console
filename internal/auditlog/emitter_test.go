package auditlog

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func newTestEmitter(cfg Config) (*Emitter, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	return NewEmitter(cfg, logger), &buf
}

func TestEmitter_EmittedEventHasFixedKeySet(t *testing.T) {
	emitter, buf := newTestEmitter(Config{CategoryRBAC: LevelWrites})
	emitter.Write(CategoryRBAC, Event{Action: "user.created", Actor: "admin", ResourceType: "user", ResourceID: "alice"})

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("emitted line is not valid JSON: %v (%s)", err, buf.String())
	}

	wantKeys := []string{"time", "level", "msg", "action", "actor", "category", "resourceType", "resourceId"}
	for _, k := range wantKeys {
		if _, ok := decoded[k]; !ok {
			t.Errorf("emitted event missing key %q: %v", k, decoded)
		}
	}
	// before/after are omitted (not just null) when not set.
	if _, ok := decoded["before"]; ok {
		t.Errorf("emitted event has unexpected key \"before\" when Event.Before was nil: %v", decoded)
	}
	if _, ok := decoded["after"]; ok {
		t.Errorf("emitted event has unexpected key \"after\" when Event.After was nil: %v", decoded)
	}

	if decoded["category"] != "rbac" {
		t.Errorf("category = %v, want rbac", decoded["category"])
	}
}

func TestEmitter_BeforeAfterIncludedWhenSet(t *testing.T) {
	emitter, buf := newTestEmitter(Config{CategoryRBAC: LevelWrites})
	emitter.Write(CategoryRBAC, Event{
		Action: "role.updated", Actor: "admin", ResourceType: "role", ResourceID: "admin",
		Before: []string{"nodes:read"}, After: []string{"nodes:read", "rbac:admin"},
	})

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("emitted line is not valid JSON: %v", err)
	}
	if _, ok := decoded["before"]; !ok {
		t.Error("emitted event missing \"before\" when Event.Before was set")
	}
	if _, ok := decoded["after"]; !ok {
		t.Error("emitted event missing \"after\" when Event.After was set")
	}
}

func TestEmitter_WriteAndReadRespectConfiguredLevel(t *testing.T) {
	cases := []struct {
		level     Level
		wantWrite bool
		wantRead  bool
	}{
		{LevelOff, false, false},
		{LevelWrites, true, false},
		{LevelFull, true, true},
	}

	for _, c := range cases {
		emitter, buf := newTestEmitter(Config{CategoryNodes: c.level})
		emitter.Write(CategoryNodes, Event{Action: "node.deleted"})
		gotWrite := buf.Len() > 0
		if gotWrite != c.wantWrite {
			t.Errorf("level=%v: Write() emitted = %v, want %v", c.level, gotWrite, c.wantWrite)
		}

		emitter, buf = newTestEmitter(Config{CategoryNodes: c.level})
		emitter.Read(CategoryNodes, Event{Action: "node.viewed"})
		gotRead := buf.Len() > 0
		if gotRead != c.wantRead {
			t.Errorf("level=%v: Read() emitted = %v, want %v", c.level, gotRead, c.wantRead)
		}
	}
}

func TestEmitter_CategoriesAreIndependent(t *testing.T) {
	emitter, buf := newTestEmitter(Config{CategoryNodes: LevelFull, CategoryRBAC: LevelOff})

	emitter.Read(CategoryNodes, Event{Action: "node.viewed"})
	if buf.Len() == 0 {
		t.Error("nodes category at LevelFull should emit on Read")
	}
	buf.Reset()

	emitter.Write(CategoryRBAC, Event{Action: "user.created"})
	if buf.Len() != 0 {
		t.Errorf("rbac category at LevelOff should not emit, got %q", buf.String())
	}
}

func TestEmitter_MissingCategoryDefaultsToOff(t *testing.T) {
	emitter, buf := newTestEmitter(Config{})
	emitter.Write(CategoryCode, Event{Action: "deploy.triggered"})
	if buf.Len() != 0 {
		t.Errorf("a category absent from Config should default to off, got %q", buf.String())
	}
}

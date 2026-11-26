package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A translation that reads the same as its source is still a
// translation, and reaches the compiled catalogue; an empty or fuzzy one
// does not.
func TestWriteJSON_KeepsIdenticalTranslations(t *testing.T) {
	cat := newCatalog("de")
	cat.add(&entry{ID: "Linux", Str: "Linux"})
	cat.add(&entry{ID: "Groups", Str: "Gruppen"})
	cat.add(&entry{ID: "Untranslated", Str: ""})
	cat.add(&entry{ID: "Guessed", Str: "Geraten", Fuzzy: true})

	path := filepath.Join(t.TempDir(), "de.json")
	if err := writeJSON(path, cat); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}

	for id, want := range map[string]string{"Linux": "Linux", "Groups": "Gruppen"} {
		if got[id] != want {
			t.Errorf("%q: got %v, want %q", id, got[id], want)
		}
	}
	for _, id := range []string{"Untranslated", "Guessed"} {
		if _, ok := got[id]; ok {
			t.Errorf("%q should not be in the compiled catalogue", id)
		}
	}
}

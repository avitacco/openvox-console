package main

import (
	"strings"
	"testing"
)

// Guides are extracted through the guide renderer: prose and labels in,
// code out, and a partial's paragraph once however many guides include
// it, referenced at the partial's own line.
func TestExtract_Guides(t *testing.T) {
	msgs, err := extract([]string{"../../marketing/guides/testdata/tree"})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	byID := map[string]message{}
	for _, m := range msgs {
		byID[m.ID] = m
	}

	for _, want := range []string{
		"Install with containers", // title
		"From an empty host to a working console, with Docker Compose.", // summary
		"Don't sign <code>&lt;certname&gt;</code> requests you did not expect.",
		"Linux", // a tab label
		"Check it answers:",
	} {
		if _, ok := byID[want]; !ok {
			t.Errorf("missing message %q", want)
		}
	}

	for id := range byID {
		if strings.Contains(id, "docker compose up") || strings.Contains(id, "Invoke-WebRequest") {
			t.Errorf("code was extracted: %q", id)
		}
	}

	refs := byID["Check it answers:"].References
	if len(refs) != 1 || !strings.HasSuffix(refs[0], "_partials/verify.md:3") {
		t.Errorf("partial message references = %v, want exactly _partials/verify.md:3", refs)
	}
}

package auditlog

import "testing"

func TestLevel_Ordering(t *testing.T) {
	if !(LevelFull >= LevelWrites) {
		t.Errorf("LevelFull (%d) should be >= LevelWrites (%d)", LevelFull, LevelWrites)
	}
	if !(LevelWrites >= LevelOff) {
		t.Errorf("LevelWrites (%d) should be >= LevelOff (%d)", LevelWrites, LevelOff)
	}
	if !(LevelFull >= LevelOff) {
		t.Errorf("LevelFull (%d) should be >= LevelOff (%d)", LevelFull, LevelOff)
	}
}

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in      string
		want    Level
		wantErr bool
	}{
		{"off", LevelOff, false},
		{"writes", LevelWrites, false},
		{"full", LevelFull, false},
		{"bogus", 0, true},
		{"", 0, true},
	}
	for _, c := range cases {
		got, err := ParseLevel(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseLevel(%q) error = nil, want error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLevel(%q) error = %v, want nil", c.in, err)
		}
		if got != c.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

package demodata

import (
	"testing"
	"time"
)

// The whole point of the package is that these values do not move. A test
// that recomputed them from time.Now() would pass while proving nothing,
// so the expectations are written out literally.
func TestInstantIsFixed(t *testing.T) {
	want := "2026-06-16T09:30:00Z"
	if got := Instant.Format(time.RFC3339); got != want {
		t.Errorf("Instant = %s, want %s", got, want)
	}
	if got := Instant.Weekday(); got != time.Tuesday {
		t.Errorf("Instant falls on %s, want Tuesday", got)
	}
	if got := Instant.Location(); got != time.UTC {
		t.Errorf("Instant is in %v, want UTC", got)
	}
}

func TestAgoIsStable(t *testing.T) {
	first := Ago(4 * time.Minute)
	second := Ago(4 * time.Minute)
	if !first.Equal(second) {
		t.Errorf("Ago is not stable: %s then %s", first, second)
	}
	if want := "2026-06-16T09:26:00Z"; first.Format(time.RFC3339) != want {
		t.Errorf("Ago(4m) = %s, want %s", first.Format(time.RFC3339), want)
	}
}

func TestInstantMillisMatchesInstant(t *testing.T) {
	if got, want := InstantMillis(), Instant.UnixMilli(); got != want {
		t.Errorf("InstantMillis() = %d, want %d", got, want)
	}
}

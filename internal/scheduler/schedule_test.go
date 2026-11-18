package scheduler

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return parsed
}

func TestNextStart(t *testing.T) {
	sunday := at("2026-11-15T03:00:00Z")
	cases := []struct {
		name    string
		startAt time.Time
		repeat  *Repeat
		now     time.Time
		want    string // empty: nothing further due
	}{
		{"one-off still ahead", sunday, nil, at("2026-11-14T00:00:00Z"), "2026-11-15T03:00:00Z"},
		{"one-off passed", sunday, nil, at("2026-11-15T03:00:01Z"), ""},
		{"one-off exactly now", sunday, nil, sunday, ""},
		{"repeating, start ahead", sunday, &Repeat{1, UnitWeek}, at("2026-11-10T00:00:00Z"), "2026-11-15T03:00:00Z"},
		{"weekly, just fired", sunday, &Repeat{1, UnitWeek}, sunday, "2026-11-22T03:00:00Z"},
		{"weekly, mid-week", sunday, &Repeat{1, UnitWeek}, at("2026-11-18T12:00:00Z"), "2026-11-22T03:00:00Z"},
		{"daily, started three days ago", at("2026-11-12T02:00:00Z"), &Repeat{1, UnitDay}, at("2026-11-15T09:30:00Z"), "2026-11-16T02:00:00Z"},
		{"hourly, four and a half hours on", at("2026-11-15T00:00:00Z"), &Repeat{1, UnitHour}, at("2026-11-15T04:30:00Z"), "2026-11-15T05:00:00Z"},
		{"every 15 minutes", at("2026-11-15T00:00:00Z"), &Repeat{15, UnitMinute}, at("2026-11-15T00:31:00Z"), "2026-11-15T00:45:00Z"},
		{"every 2 weeks", sunday, &Repeat{2, UnitWeek}, at("2026-11-23T00:00:00Z"), "2026-11-29T03:00:00Z"},
		// A day is 24 hours: across the end of daylight saving in
		// Europe (2026-10-25) the UTC time of day does not move, which
		// is an hour's shift on a local clock.
		{"daily across a daylight-saving change", at("2026-10-24T01:00:00Z"), &Repeat{1, UnitDay}, at("2026-10-25T12:00:00Z"), "2026-10-26T01:00:00Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := nextStart(c.startAt, c.repeat, c.now)
			switch {
			case c.want == "" && got != nil:
				t.Errorf("next = %s, want nothing further due", got)
			case c.want != "" && (got == nil || !got.Equal(at(c.want))):
				t.Errorf("next = %v, want %s", got, c.want)
			}
		})
	}
}

func TestCheckTiming(t *testing.T) {
	now := at("2026-11-15T12:00:00Z")
	ahead, past := now.Add(time.Hour), now.Add(-72*time.Hour)

	ok := []struct {
		startAt time.Time
		repeat  *Repeat
	}{
		{ahead, nil},
		{ahead, &Repeat{1, UnitWeek}},
		{past, &Repeat{1, UnitDay}},
		{ahead, &Repeat{5, UnitMinute}},
	}
	for _, c := range ok {
		if err := checkTiming("nightly", c.startAt, c.repeat, now); err != nil {
			t.Errorf("checkTiming(%s, %+v) = %v, want nil", c.startAt, c.repeat, err)
		}
	}

	bad := []struct {
		name    string
		sched   string
		startAt time.Time
		repeat  *Repeat
		field   string
		mention string
	}{
		{"no name", "  ", ahead, nil, "name", "required"},
		{"long name", string(make([]byte, MaxNameLength+1)), ahead, nil, "name", "longer"},
		{"no start", "n", time.Time{}, nil, "startAt", "required"},
		{"one-off in the past", "n", past, nil, "startAt", "passed"},
		{"every two minutes", "n", ahead, &Repeat{2, UnitMinute}, "repeat", "5 minutes"},
		{"every zero", "n", ahead, &Repeat{0, UnitHour}, "repeat", "at least 1"},
		{"unknown unit", "n", ahead, &Repeat{1, "month"}, "repeat", "minute, hour, day or week"},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			wantField(t, checkTiming(c.sched, c.startAt, c.repeat, now), c.field, c.mention)
		})
	}
}

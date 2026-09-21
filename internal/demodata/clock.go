// Package demodata holds the fixed reference points the demo seed and the
// screenshot capture tool must agree on.
//
// It exists because those two tools are separate programs that have to
// share one notion of "now". The seed writes a fleet whose reports are
// "four minutes old"; capture pins the browser's clock so the console
// renders them as four minutes old rather than as however old they
// actually are by the time a screenshot is taken. Both read that instant
// from here, so they cannot drift apart.
//
// Nothing in the console binary imports this package - it is
// development-time data, not product behavior.
package demodata

import "time"

// Instant is the moment the demo fleet is pinned to: a Tuesday
// mid-morning in UTC, chosen so that a weekly activity chart looks like a
// working week in progress rather than a dead weekend.
//
// It is deliberately a constant rather than time.Now(). Every timestamp
// the seed writes is an offset from it, and capture overrides the
// browser's clock to it, which is what makes two capture runs produce
// byte-identical images: relative times ("4 minutes ago", "yesterday")
// resolve to the same words every time.
//
// Changing it re-dates the entire demo fleet, and every screenshot showing
// a timestamp will differ on the next capture. That is a deliberate act,
// not a maintenance chore.
var Instant = time.Date(2026, time.June, 16, 9, 30, 0, 0, time.UTC)

// Ago returns the instant d before the demo instant. It is the seed's main
// way of placing an event: Ago(4*time.Minute) is "four minutes before the
// console was screenshotted".
func Ago(d time.Duration) time.Time {
	return Instant.Add(-d)
}

// InstantMillis is the demo instant as milliseconds since the Unix epoch,
// which is the unit the browser's clock override takes.
func InstantMillis() int64 {
	return Instant.UnixMilli()
}

// Package dateutil holds the day/week boundary helpers shared by the CLI
// commands, TUI, and MCP server so they agree on what "today" and "this
// week" mean.
package dateutil

import "time"

// StartOfDay returns t truncated to 00:00:00 in t's location.
func StartOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// EndOfDay returns t set to 23:59:59 in t's location.
func EndOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 23, 59, 59, 0, t.Location())
}

// WeekRange returns the Monday 00:00:00–Sunday 23:59:59 bounds (local time)
// of the week containing now.
func WeekRange(now time.Time) (time.Time, time.Time) {
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	mon := now.AddDate(0, 0, -(wd - 1))
	mon = time.Date(mon.Year(), mon.Month(), mon.Day(), 0, 0, 0, 0, time.Local)
	sun := mon.AddDate(0, 0, 6)
	sun = time.Date(sun.Year(), sun.Month(), sun.Day(), 23, 59, 59, 0, time.Local)
	return mon, sun
}

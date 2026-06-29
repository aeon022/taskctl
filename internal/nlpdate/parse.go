package nlpdate

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Parse parses a date string. Understands standard YYYY-MM-DD as well as
// German/English natural language: "morgen", "nächsten montag", "in 3 tagen".
// Returns nil, nil if the string is empty.
func Parse(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	lower := strings.ToLower(s)

	// exact keywords
	switch lower {
	case "heute", "today":
		return &today, nil
	case "morgen", "tomorrow":
		d := today.AddDate(0, 0, 1)
		return &d, nil
	case "übermorgen", "day after tomorrow":
		d := today.AddDate(0, 0, 2)
		return &d, nil
	}

	// "nächsten/nächste/next WEEKDAY"
	for _, prefix := range []string{"nächsten ", "nächste ", "nächster ", "next "} {
		if strings.HasPrefix(lower, prefix) {
			rest := lower[len(prefix):]
			if wd, ok := weekdayName(rest); ok {
				d := nextWeekday(today, wd)
				return &d, nil
			}
		}
	}

	// bare weekday name → next occurrence
	if wd, ok := weekdayName(lower); ok {
		d := nextWeekday(today, wd)
		return &d, nil
	}

	// "in N tagen / days"
	if d, ok := parseInDays(lower, today); ok {
		return &d, nil
	}

	// "in N wochen / weeks"
	if d, ok := parseInWeeks(lower, today); ok {
		return &d, nil
	}

	// standard YYYY-MM-DD
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return &t, nil
	}

	return nil, fmt.Errorf("cannot parse date %q — use YYYY-MM-DD, 'morgen', 'nächsten montag', 'in 3 tagen'", s)
}

func weekdayName(s string) (time.Weekday, bool) {
	names := map[string]time.Weekday{
		"montag": time.Monday, "monday": time.Monday, "mo": time.Monday,
		"dienstag": time.Tuesday, "tuesday": time.Tuesday, "di": time.Tuesday,
		"mittwoch": time.Wednesday, "wednesday": time.Wednesday, "mi": time.Wednesday,
		"donnerstag": time.Thursday, "thursday": time.Thursday, "do": time.Thursday,
		"freitag": time.Friday, "friday": time.Friday, "fr": time.Friday,
		"samstag": time.Saturday, "saturday": time.Saturday, "sa": time.Saturday,
		"sonntag": time.Sunday, "sunday": time.Sunday, "so": time.Sunday,
	}
	wd, ok := names[s]
	return wd, ok
}

func nextWeekday(from time.Time, wd time.Weekday) time.Time {
	d := from.AddDate(0, 0, 1)
	for d.Weekday() != wd {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

func parseInDays(s string, today time.Time) (time.Time, bool) {
	for _, suffix := range []string{" tagen", " tage", " days", " day"} {
		if strings.HasSuffix(s, suffix) {
			prefix := strings.TrimSuffix(s, suffix)
			prefix = strings.TrimPrefix(prefix, "in ")
			if n, err := strconv.Atoi(strings.TrimSpace(prefix)); err == nil {
				return today.AddDate(0, 0, n), true
			}
		}
	}
	return time.Time{}, false
}

func parseInWeeks(s string, today time.Time) (time.Time, bool) {
	for _, suffix := range []string{" wochen", " woche", " weeks", " week"} {
		if strings.HasSuffix(s, suffix) {
			prefix := strings.TrimSuffix(s, suffix)
			prefix = strings.TrimPrefix(prefix, "in ")
			if n, err := strconv.Atoi(strings.TrimSpace(prefix)); err == nil {
				return today.AddDate(0, 0, n*7), true
			}
		}
	}
	return time.Time{}, false
}

package nlpdate

import (
	"testing"
	"time"
)

func today() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
}

func TestParseEmpty(t *testing.T) {
	got, err := Parse("")
	if err != nil || got != nil {
		t.Fatalf("Parse(\"\") = %v, %v — want nil, nil", got, err)
	}
	got, err = Parse("   ")
	if err != nil || got != nil {
		t.Fatalf("Parse(whitespace) = %v, %v — want nil, nil", got, err)
	}
}

func TestParseKeywords(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
	}{
		{"heute", today()},
		{"today", today()},
		{"HEUTE", today()},
		{"morgen", today().AddDate(0, 0, 1)},
		{"tomorrow", today().AddDate(0, 0, 1)},
		{"übermorgen", today().AddDate(0, 0, 2)},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("Parse(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseISO(t *testing.T) {
	got, err := Parse("2026-12-24")
	if err != nil {
		t.Fatal(err)
	}
	if got.Format("2006-01-02") != "2026-12-24" {
		t.Errorf("got %v", got)
	}
}

func TestParseInDays(t *testing.T) {
	cases := map[string]int{
		"in 3 tagen": 3,
		"in 1 tage":  1,
		"in 10 days": 10,
		"in 1 day":   1,
	}
	for in, days := range cases {
		got, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		want := today().AddDate(0, 0, days)
		if !got.Equal(want) {
			t.Errorf("Parse(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseInWeeks(t *testing.T) {
	got, err := Parse("in 2 wochen")
	if err != nil {
		t.Fatal(err)
	}
	want := today().AddDate(0, 0, 14)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseNextWeekday(t *testing.T) {
	for _, in := range []string{"nächsten montag", "next monday", "montag", "mo"} {
		got, err := Parse(in)
		if err != nil {
			t.Errorf("Parse(%q): %v", in, err)
			continue
		}
		if got.Weekday() != time.Monday {
			t.Errorf("Parse(%q) landed on %v, want Monday", in, got.Weekday())
		}
		if !got.After(today()) {
			t.Errorf("Parse(%q) = %v — must be in the future", in, got)
		}
		if got.Sub(today()) > 7*24*time.Hour {
			t.Errorf("Parse(%q) = %v — more than a week away", in, got)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	for _, in := range []string{"gibberish", "32.13.2026", "in tagen"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q): want error, got nil", in)
		}
	}
}

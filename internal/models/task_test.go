package models

import (
	"testing"
	"time"
)

func TestDone(t *testing.T) {
	if (&Task{Status: "completed"}).Done() != true || (&Task{Status: "needsAction"}).Done() {
		t.Error("Done must be true only for completed")
	}
}

func TestSpawnDate(t *testing.T) {
	base := time.Date(2026, 1, 31, 9, 0, 0, 0, time.UTC)
	for rec, want := range map[string]time.Time{
		"daily":   base.AddDate(0, 0, 1),
		"weekly":  base.AddDate(0, 0, 7),
		"monthly": base.AddDate(0, 1, 0),
		"":        base.AddDate(0, 0, 1), // unknown → daily
		"bogus":   base.AddDate(0, 0, 1),
	} {
		if got := (&Task{DueDate: &base, Recurrence: rec}).SpawnDate(); !got.Equal(want) {
			t.Errorf("%q: %v, want %v", rec, got, want)
		}
	}
	// no due date: counted from now
	if got := (&Task{Recurrence: "weekly"}).SpawnDate(); got.Before(time.Now().AddDate(0, 0, 6)) {
		t.Errorf("without a due date the base is now, got %v", got)
	}
}

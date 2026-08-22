package cmd

import (
	"testing"
	"time"

	"github.com/aeon022/taskctl/internal/models"
)

func TestFilterWeek(t *testing.T) {
	mon := time.Date(2026, 8, 17, 0, 0, 0, 0, time.Local)
	sun := time.Date(2026, 8, 23, 23, 59, 59, 0, time.Local)

	inWeek := time.Date(2026, 8, 20, 0, 0, 0, 0, time.Local)
	outOfWeek := time.Date(2026, 8, 25, 0, 0, 0, 0, time.Local)

	all := []models.Task{
		{Title: "in range, pending", DueDate: &inWeek, Status: "needsAction"},
		{Title: "in range, completed", DueDate: &inWeek, Status: "completed"},
		{Title: "out of range", DueDate: &outOfWeek, Status: "needsAction"},
		{Title: "no due date", Status: "needsAction"},
	}

	got := filterWeek(all, mon, sun)
	if len(got) != 2 {
		t.Fatalf("got %d tasks, want 2 (pending+completed within range): %+v", len(got), got)
	}
}

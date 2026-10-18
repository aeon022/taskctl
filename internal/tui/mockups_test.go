package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/taskctl/internal/models"
)

// TestRenderLandingMockups writes an ANSI screenshot of the real TUI (made-up demo data) for the landing
// page: TASKCTL_MOCKUP_DIR=/dir go test ./internal/tui -run TestRenderLandingMockups
func TestRenderLandingMockups(t *testing.T) {
	dir := os.Getenv("TASKCTL_MOCKUP_DIR")
	if dir == "" {
		t.Skip("set TASKCTL_MOCKUP_DIR to render landing mockups")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TASKCTL_DATA_DIR", "")

	now := time.Now()
	at := func(days, h, m int) *time.Time {
		d := time.Date(now.Year(), now.Month(), now.Day()+days, h, m, 0, 0, now.Location())
		return &d
	}
	mk := func(id, title, list string, due *time.Time, prio int, subs ...models.Subtask) models.Task {
		return models.Task{ID: id, Title: title, List: list, Status: "needsAction", DueDate: due, Priority: prio, Subtasks: subs, Source: "apple", CreatedAt: now, UpdatedAt: now}
	}
	tasks := []models.Task{
		mk("1", "Review Q3 budget proposal", "Work", at(0, 9, 0), 1),
		mk("2", "Reply to Alice re: design review", "Work", at(0, 14, 0), 5),
		mk("3", "Update ROADMAP.md", "Work", at(0, 17, 0), 5, models.Subtask{Title: "Move shipped items", Done: true}, models.Subtask{Title: "Add Q4 themes"}, models.Subtask{Title: "Share with team"}),
		mk("4", "Book team offsite venue", "Work", at(-1, 8, 0), 1),
		mk("5", "Weekly team sync prep", "Work", at(1, 10, 0), 0),
		mk("8", "Order new monitor arm", "Work", at(2, 12, 0), 9),
		mk("6", "Renew passport", "Personal", at(4, 0, 0), 9),
		mk("7", "Plan weekend hike", "Personal", nil, 0),
	}
	m := newModel("")
	m, _ = send(m, tea.WindowSizeMsg{Width: 118, Height: 22}, tasksLoadedMsg{tasks: tasks, doneTotal: 12})
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "taskctl.ansi"), []byte(m.View().Content), 0o644); err != nil {
		t.Fatal(err)
	}
}

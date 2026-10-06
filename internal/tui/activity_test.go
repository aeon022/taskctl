package tui

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	"github.com/aeon022/missionctl-core/activity"
	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/aeon022/taskctl/internal/reminders"
)

// actSandbox isolates HOME, the suite data dir and the taskctl DB, and swaps
// the Reminders writes for stubs. The returned wait blocks until n background
// provider calls (the TUI fires them in goroutines) have happened, so no
// goroutine can outlive the stubs.
func actSandbox(t *testing.T) (wait func(n int)) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MISSIONCTL_DATA_DIR", t.TempDir())
	t.Setenv("MISSIONCTL_ACTIVITY", "")
	config.DBPathOverride = filepath.Join(t.TempDir(), "taskctl.db")
	t.Cleanup(func() { config.DBPathOverride = "" })

	var wg sync.WaitGroup
	oc, oco, od := reminders.CreateTask, reminders.CompleteTask, reminders.DeleteTask
	reminders.CreateTask = func(*models.Task) error { wg.Done(); return nil }
	reminders.CompleteTask = func(*models.Task) error { wg.Done(); return nil }
	reminders.DeleteTask = func(*models.Task) error { wg.Done(); return nil }
	t.Cleanup(func() {
		wg.Wait()
		reminders.CreateTask, reminders.CompleteTask, reminders.DeleteTask = oc, oco, od
	})
	return func(n int) { wg.Add(n) }
}

func eventsToday(t *testing.T) []activity.Event {
	t.Helper()
	from, to := activity.Day(time.Now())
	evs, err := activity.Read(from, to)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func summary(evs []activity.Event) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.Tool+"/"+e.Action+"/"+e.Title)
	}
	return out
}

func eq(t *testing.T, got []activity.Event, want ...string) {
	t.Helper()
	g := summary(got)
	if len(g) != len(want) {
		t.Fatalf("events = %v, want %v", g, want)
	}
	for i := range want {
		if g[i] != want[i] {
			t.Errorf("event %d = %s, want %s", i, g[i], want[i])
		}
	}
}

func TestTUIAddCompleteDeleteLoggedOnce(t *testing.T) {
	expect := actSandbox(t)

	// add through the real form path
	var inputs [fCount]textinput.Model
	for i := range inputs {
		inputs[i] = textinput.New()
	}
	inputs[fTitle].SetValue("Steuer abgeben")
	inputs[fList].SetValue("Arbeit")
	expect(1) // background CreateTask
	saveTaskCmd(inputs, nil)()
	eq(t, eventsToday(t), "taskctl/added/Steuer abgeben")

	// completing logs "completed" (un-completing is a different path and logs nothing)
	task := &models.Task{ID: "t1", Title: "Milch", List: "Einkauf", Status: "completed", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	expect(1)
	toggleDoneCmd(task)()

	// delete
	expect(1)
	deleteTaskCmd(task)()

	got := summary(eventsToday(t))
	want := []string{"taskctl/added/Steuer abgeben", "taskctl/completed/Milch", "taskctl/deleted/Milch"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("events = %v, want %v", got, want)
	}
}

func TestTUIEditIsNotAnAdd(t *testing.T) {
	expect := actSandbox(t)
	var inputs [fCount]textinput.Model
	for i := range inputs {
		inputs[i] = textinput.New()
	}
	inputs[fTitle].SetValue("Neuer Titel")
	inputs[fList].SetValue("Arbeit")
	orig := &models.Task{ID: "old", Title: "Alter Titel", List: "Arbeit", CreatedAt: time.Now()}
	expect(2) // delete of the old row + create of the replacement
	saveTaskCmd(inputs, orig)()
	if evs := eventsToday(t); len(evs) != 0 {
		t.Errorf("an edit re-creates the row but is not a new task: %v", summary(evs))
	}
}

func TestTUIBatchActionsLogEachTask(t *testing.T) {
	expect := actSandbox(t)
	mk := func(id, title string) *models.Task {
		return &models.Task{ID: id, Title: title, List: "L", Status: "needsAction", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	}
	a, b := mk("a", "Eins"), mk("b", "Zwei")
	expect(2)
	batchCompleteCmd([]*models.Task{a, b})()
	expect(2)
	batchDeleteCmd([]*models.Task{a, b})()
	eq(t, eventsToday(t),
		"taskctl/completed/Eins", "taskctl/completed/Zwei", "taskctl/deleted/Eins", "taskctl/deleted/Zwei")
}

func TestTUIActivityOffStillActs(t *testing.T) {
	expect := actSandbox(t)
	t.Setenv("MISSIONCTL_ACTIVITY", "off")
	task := &models.Task{ID: "x", Title: "Ruhig", List: "L", Status: "needsAction", CreatedAt: time.Now()}
	expect(1)
	if msg := deleteTaskCmd(task)(); msg == nil {
		t.Error("delete must still return its message with logging off")
	}
	if evs := eventsToday(t); len(evs) != 0 {
		t.Errorf("logged while off: %v", summary(evs))
	}
}

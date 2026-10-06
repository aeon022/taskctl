package tasks

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/aeon022/missionctl-core/activity"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/aeon022/taskctl/internal/reminders"
	"github.com/aeon022/taskctl/internal/store"
)

// stubProvider replaces the Reminders writes for the test; they fail on demand.
func stubProvider(t *testing.T, fail error) {
	t.Helper()
	oc, oco, od := reminders.CreateTask, reminders.CompleteTask, reminders.DeleteTask
	reminders.CreateTask = func(*models.Task) error { return fail }
	reminders.CompleteTask = func(*models.Task) error { return fail }
	reminders.DeleteTask = func(*models.Task) error { return fail }
	t.Cleanup(func() { reminders.CreateTask, reminders.CompleteTask, reminders.DeleteTask = oc, oco, od })
}

func sandbox(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MISSIONCTL_DATA_DIR", t.TempDir())
	t.Setenv("MISSIONCTL_ACTIVITY", "")
	s, err := store.New(filepath.Join(t.TempDir(), "taskctl.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func logged(t *testing.T) []activity.Event {
	t.Helper()
	from, to := activity.Day(time.Now())
	evs, err := activity.Read(from, to)
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func TestCreateCompleteDeleteAreLoggedOnce(t *testing.T) {
	s := sandbox(t)
	stubProvider(t, nil)

	if _, err := Create(s, "Steuer abgeben", "Arbeit", "geheime Notiz", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := Complete(s, "Steuer abgeben", "Arbeit"); err != nil {
		t.Fatal(err)
	}
	if err := Delete(s, "Steuer abgeben", "Arbeit"); err != nil {
		t.Fatal(err)
	}
	evs := logged(t)
	want := []string{"added", "completed", "deleted"}
	if len(evs) != 3 {
		t.Fatalf("got %d events: %+v", len(evs), evs)
	}
	for i, w := range want {
		if evs[i].Tool != "taskctl" || evs[i].Action != w || evs[i].Title != "Steuer abgeben" {
			t.Errorf("event %d = %+v, want taskctl/%s/Steuer abgeben", i, evs[i], w)
		}
	}
}

func TestProviderFailureLogsNothing(t *testing.T) {
	s := sandbox(t)
	stubProvider(t, errBoom{})
	_, _ = Create(s, "x", "L", "", "", nil)
	_ = Complete(s, "x", "L")
	_ = Delete(s, "x", "L")
	if evs := logged(t); len(evs) != 0 {
		t.Errorf("failed actions must not be logged: %+v", evs)
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }

func TestActivityOffStillPerformsAction(t *testing.T) {
	s := sandbox(t)
	stubProvider(t, nil)
	t.Setenv("MISSIONCTL_ACTIVITY", "off")
	if _, err := Create(s, "ruhig", "L", "", "", nil); err != nil {
		t.Fatalf("action must succeed with logging off: %v", err)
	}
	if evs := logged(t); len(evs) != 0 {
		t.Errorf("MISSIONCTL_ACTIVITY=off logged %+v", evs)
	}
	got, _ := s.ListTasks(context.Background(), store.ListFilter{})
	if len(got) != 1 {
		t.Errorf("task not created locally: %d", len(got))
	}
}

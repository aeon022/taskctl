package tui

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/taskctl/internal/models"
)

func browsingModel() Model {
	m := newModel("")
	m.width, m.height = 100, 30
	m.loading = false
	m.tasks = []models.Task{
		{ID: "1", Title: "a", List: "Work", Status: "needsAction"},
		{ID: "2", Title: "b", List: "Work", Status: "needsAction"},
	}
	m.rows = buildRows(m.tasks, "", filterNone)
	m.cursor = firstTaskRow(m.rows)
	m.lastLoad = time.Now().Add(-time.Minute) // stale
	return m
}

func TestFocusReloadsWhenBrowsingAndStale(t *testing.T) {
	m := browsingModel()
	mi, cmd := m.Update(tea.FocusMsg{})
	if cmd == nil || !mi.(Model).focusLoading {
		t.Errorf("stale browse state must reload on focus: cmd=%v focusLoading=%v", cmd != nil, mi.(Model).focusLoading)
	}
}

func TestFocusDoesNotReloadWhenFreshOrBusy(t *testing.T) {
	cases := map[string]func(*Model){
		"fresh":        func(m *Model) { m.lastLoad = time.Now() },
		"form":         func(m *Model) { m.view = viewCreate },
		"search":       func(m *Model) { m.searching = true },
		"palette":      func(m *Model) { m.inPalette = true },
		"delete":       func(m *Model) { m.deleteTarget = &m.tasks[0] },
		"batch select": func(m *Model) { m.selecting = true },
		"subtask add":  func(m *Model) { m.addingSubtask = true },
		"syncing":      func(m *Model) { m.syncing = true },
		"already busy": func(m *Model) { m.focusLoading = true },
		"first load":   func(m *Model) { m.loading = true },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			m := browsingModel()
			setup(&m)
			if _, cmd := m.Update(tea.FocusMsg{}); cmd != nil {
				t.Errorf("%s: focus must not trigger a reload", name)
			}
		})
	}
}

func TestFocusReloadKeepsCursorOnSameTask(t *testing.T) {
	m := browsingModel()
	m.cursor = len(m.rows) - 1 // second task
	want := cursorTask(m).ID
	mi, _ := m.Update(tea.FocusMsg{})
	// the reloaded list arrives with the same tasks
	mi, _ = mi.Update(tasksLoadedMsg{tasks: m.tasks})
	m = mi.(Model)
	if got := cursorTask(m); got == nil || got.ID != want {
		t.Errorf("cursor moved off task %s after focus reload: %+v", want, got)
	}
	if m.focusLoading || time.Since(m.lastLoad) > time.Second {
		t.Errorf("load bookkeeping not reset: focusLoading=%v lastLoad=%v", m.focusLoading, m.lastLoad)
	}
}

func TestViewReportsFocus(t *testing.T) {
	if !browsingModel().View().ReportFocus {
		t.Error("View must set ReportFocus so FocusMsg is delivered")
	}
}

func TestCopyUsesOSC52AndPbcopy(t *testing.T) {
	msg := copyToClipboardCmd("hello")()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("want a 2-command batch (OSC 52 + pbcopy), got %T %v", msg, msg)
	}
	var osc bool
	for _, c := range batch {
		if strings.Contains(runtime.FuncForPC(reflect.ValueOf(c).Pointer()).Name(), "SetClipboard") {
			osc = true // identified by name — executing the pbcopy half would touch the real clipboard
		}
	}
	if !osc {
		t.Error("batch must contain tea.SetClipboard so copying works over SSH/tmux")
	}
}

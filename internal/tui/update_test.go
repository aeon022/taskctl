package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/taskctl/internal/models"
)

// These tests drive Model.Update with synthetic messages and assert state.
// Returned tea.Cmds are never executed: they would write to the DB and call
// Reminders.app. HOME is a temp dir so ui_state/last_synced never touch real data.

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "ctrl+s":
		return tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func send(m Model, msgs ...tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, msg := range msgs {
		var mi tea.Model
		mi, cmd = m.Update(msg)
		m = mi.(Model)
	}
	return m, cmd
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m, _ = send(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func day(offset int) *time.Time {
	d := time.Now().AddDate(0, 0, offset)
	return &d
}

// loaded returns a model with tasks in lists "Home" (a, b) and "Work" (c, d),
// the cursor on the first task row.
func loaded(t *testing.T) Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	m := newModel("")
	tasks := []models.Task{
		{ID: "a", Title: "buy milk", List: "Home", Status: "needsAction", DueDate: day(-2)},
		{ID: "b", Title: "call mom", List: "Home", Status: "needsAction", Notes: "birthday gift", DueDate: day(0)},
		{ID: "c", Title: "write report", List: "Work", Status: "needsAction", DueDate: day(5)},
		{ID: "d", Title: "plan sprint", List: "Work", Status: "needsAction"},
	}
	m, _ = send(m, tea.WindowSizeMsg{Width: 100, Height: 40}, tasksLoadedMsg{tasks: tasks})
	return m
}

func cursorID(m Model) string {
	if t := cursorTask(m); t != nil {
		return t.ID
	}
	return ""
}

func taskRows(m Model) (ids []string) {
	for _, r := range m.rows {
		if !r.isHeader {
			ids = append(ids, r.task.ID)
		}
	}
	return
}

func TestWindowSizeClamp(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.width != 80 || m.height != 23 {
		t.Errorf("size = %dx%d, want 80x23 (one row of slack)", m.width, m.height)
	}
	m, _ = send(m, tea.WindowSizeMsg{Width: 10, Height: 0})
	if m.height != 1 {
		t.Errorf("height = %d, want clamp to 1", m.height)
	}
}

func TestTasksLoadedBuildsRowsAndPlacesCursor(t *testing.T) {
	m := loaded(t)
	if m.loading {
		t.Error("still loading after tasksLoadedMsg")
	}
	if len(m.rows) != 6 || !m.rows[0].isHeader || m.rows[0].label != "Home" {
		t.Fatalf("rows = %+v, want header+2 tasks per list", m.rows)
	}
	if m.cursor != 1 || cursorID(m) != "a" {
		t.Errorf("cursor = %d (%s), want first task row", m.cursor, cursorID(m))
	}
	if len(m.listEntries) != 2 || m.listEntries[0].Name != "Home" {
		t.Errorf("listEntries = %+v, want sorted unique lists from tasks", m.listEntries)
	}
}

func TestOpenTaskIDOpensDetailOnceAfterLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newModel("c")
	m, _ = send(m, tasksLoadedMsg{tasks: []models.Task{
		{ID: "b", Title: "x", List: "L"}, {ID: "c", Title: "y", List: "L"},
	}})
	if m.view != viewDetail || m.detailTarget == nil || m.detailTarget.ID != "c" {
		t.Fatalf("view=%v target=%+v, want detail popup of task c", m.view, m.detailTarget)
	}
	if m.openTaskID != "" {
		t.Error("openTaskID must clear so a later reload doesn't re-open the popup")
	}
	m, _ = send(m, key("esc"), tasksLoadedMsg{tasks: m.tasks})
	if m.view != viewList {
		t.Errorf("view = %v after esc + reload, want list", m.view)
	}
}

func TestNavigationSkipsHeaders(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, key("j"))
	if cursorID(m) != "b" {
		t.Fatalf("j: cursor on %q, want b", cursorID(m))
	}
	m, _ = send(m, key("j"))
	if cursorID(m) != "c" {
		t.Errorf("j across the list boundary: cursor on %q, want c (header skipped)", cursorID(m))
	}
	m, _ = send(m, key("k"))
	if cursorID(m) != "b" {
		t.Errorf("k back across the boundary: cursor on %q, want b", cursorID(m))
	}
	for i := 0; i < 10; i++ {
		m, _ = send(m, key("down"))
	}
	if cursorID(m) != "d" {
		t.Errorf("down past the end: cursor on %q, want last task d", cursorID(m))
	}
	for i := 0; i < 10; i++ {
		m, _ = send(m, key("up"))
	}
	if m.cursor > 1 {
		t.Errorf("up past the start: cursor = %d", m.cursor)
	}
}

func TestNumberKeysJumpToNthVisibleTask(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, key("3"))
	if cursorID(m) != "c" {
		t.Errorf("'3' → %q, want third task (c); headers must not count", cursorID(m))
	}
	m, _ = send(m, key("9"))
	if cursorID(m) != "c" {
		t.Errorf("'9' with only 4 tasks moved the cursor to %q", cursorID(m))
	}
}

func TestMouseWheelMovesCursor(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, tea.MouseWheelMsg{Button: tea.MouseWheelDown}, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	if cursorID(m) != "c" {
		t.Errorf("two wheel-downs: cursor on %q, want c", cursorID(m))
	}
	m, _ = send(m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if cursorID(m) != "b" {
		t.Errorf("wheel-up: cursor on %q, want b", cursorID(m))
	}
}

// y for m.rows[i] on screen: appPadV + the rowHitTest row for it.
func clickY(t *testing.T, m Model, wantID string) int {
	t.Helper()
	for y := 0; y < 60; y++ {
		if i := m.rowHitTest(y); i >= 0 && m.rows[i].task != nil && m.rows[i].task.ID == wantID {
			return y + appPadV
		}
	}
	t.Fatalf("no screen row maps to task %s", wantID)
	return 0
}

func TestMouseClickSelectsAndDoubleClickOpensDetail(t *testing.T) {
	m := loaded(t)
	y := clickY(t, m, "c")
	click := tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: y}

	m, _ = send(m, click)
	if cursorID(m) != "c" || m.view != viewList {
		t.Fatalf("single click: cursor %q view %v, want c / list", cursorID(m), m.view)
	}
	m, _ = send(m, click)
	if m.view != viewDetail || m.detailTarget == nil || m.detailTarget.ID != "c" {
		t.Errorf("double click: view=%v target=%+v, want detail of c", m.view, m.detailTarget)
	}
}

func TestMouseSlowSecondClickIsNotDoubleClick(t *testing.T) {
	m := loaded(t)
	click := tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: clickY(t, m, "b")}
	m, _ = send(m, click)
	m.lastClickAt = time.Now().Add(-2 * doubleClickWindow)
	m, _ = send(m, click)
	if m.view != viewList {
		t.Errorf("view = %v, a click after the window must not open detail", m.view)
	}
}

func TestMouseRightClickTogglesClickedRowNotCursor(t *testing.T) {
	m := loaded(t)
	m, cmd := send(m, tea.MouseClickMsg{Button: tea.MouseRight, X: 5, Y: clickY(t, m, "d")})
	var d *models.Task
	for i := range m.tasks {
		if m.tasks[i].ID == "d" {
			d = &m.tasks[i]
		}
	}
	if d == nil || !d.Done() || d.CompletedAt == nil {
		t.Fatalf("right-click did not complete the clicked task: %+v", d)
	}
	if m.tasks[0].Done() {
		t.Error("right-click completed the cursor task instead of the clicked one")
	}
	if cmd == nil {
		t.Error("expected a persist command")
	}
}

func TestMouseMotionSetsHoverOnlyInList(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, tea.MouseMotionMsg{X: 5, Y: clickY(t, m, "b")})
	if m.hoverRow < 0 || m.rows[m.hoverRow].task.ID != "b" {
		t.Errorf("hoverRow = %d, want the row of b", m.hoverRow)
	}
	m.view = viewHelp
	m.hoverRow = -1
	m, _ = send(m, tea.MouseMotionMsg{X: 5, Y: 6})
	if m.hoverRow != -1 {
		t.Error("hover must only track in the list view")
	}
}

func TestFilterKeysToggleAndNarrow(t *testing.T) {
	m := loaded(t)

	m, _ = send(m, key("t")) // focus = due today or overdue
	if m.filter != filterFocus {
		t.Fatalf("filter = %v, want focus", m.filter)
	}
	if got := strings.Join(taskRows(m), ","); got != "a,b" {
		t.Errorf("focus rows = %s, want a,b (overdue + today)", got)
	}
	m, _ = send(m, key("t"))
	if m.filter != filterNone || len(taskRows(m)) != 4 {
		t.Errorf("second t must clear the filter, got filter=%v rows=%v", m.filter, taskRows(m))
	}

	m, _ = send(m, key("O")) // overdue only
	if got := strings.Join(taskRows(m), ","); got != "a" {
		t.Errorf("overdue rows = %s, want a", got)
	}
	m, _ = send(m, key("t")) // switching modes replaces, not stacks
	if m.filter != filterFocus {
		t.Errorf("t after O: filter = %v, want focus", m.filter)
	}
}

func TestFilterPersistsAcrossModels(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, key("O"))
	again := newModel("") // same HOME → reads ui_state
	if again.filter != filterOverdue {
		t.Errorf("restored filter = %v, want overdue", again.filter)
	}
}

func TestSearchFiltersLiveAndEnterKeepsEscKeeps(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, key("/"))
	if !m.searching {
		t.Fatal("/ must start search")
	}
	m = typeText(m, "mom")
	if got := strings.Join(taskRows(m), ","); got != "b" {
		t.Errorf("rows for %q = %s, want b", "mom", got)
	}
	if cursorID(m) != "b" {
		t.Errorf("cursor must land on the first match, got %q", cursorID(m))
	}
	m, _ = send(m, key("enter"))
	if m.searching || len(taskRows(m)) != 1 {
		t.Errorf("enter: searching=%v rows=%v, want search closed with filter kept", m.searching, taskRows(m))
	}
}

func TestSearchMatchesNotes(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, key("/"))
	m = typeText(m, "gift")
	if got := strings.Join(taskRows(m), ","); got != "b" {
		t.Errorf("notes search = %s, want b", got)
	}
}

func TestDeleteConfirmFlow(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, key("d"))
	if m.deleteTarget == nil || m.deleteTarget.ID != "a" {
		t.Fatalf("d must ask for confirmation on the cursor task, got %+v", m.deleteTarget)
	}
	m, cmd := send(m, key("n"))
	if m.deleteTarget != nil || cmd != nil {
		t.Errorf("any other key cancels: target=%+v cmd=%v", m.deleteTarget, cmd != nil)
	}
	m, _ = send(m, key("d"))
	m, cmd = send(m, key("y"))
	if m.deleteTarget != nil {
		t.Error("y must clear the confirmation")
	}
	if cmd == nil {
		t.Error("y must return the delete command")
	}
}

func TestUndoDeleteLifecycle(t *testing.T) {
	m := loaded(t)
	gone := &models.Task{ID: "x", Title: "gone", List: "Home"}

	m, _ = send(m, taskDeletedMsg{task: gone})
	if m.lastDeleted != gone {
		t.Fatal("deleted task must be remembered for undo")
	}
	m, _ = send(m, clearDeletedToastMsg{id: "other"})
	if m.lastDeleted == nil {
		t.Error("a stale toast timer must not clear a newer deletion")
	}
	m, cmd := send(m, key("u"))
	if m.lastDeleted != nil || cmd == nil {
		t.Errorf("u: lastDeleted=%v cmd=%v, want cleared + restore command", m.lastDeleted, cmd != nil)
	}
	if _, cmd = send(m, key("u")); cmd != nil {
		t.Error("u with nothing to undo must be a no-op")
	}

	m, _ = send(m, taskDeletedMsg{task: gone}, clearDeletedToastMsg{id: "x"})
	if m.lastDeleted != nil {
		t.Error("the matching toast timer must clear the undo slot")
	}
}

func TestTaskDeletedErrorIsSurfacedButDeleteStillApplies(t *testing.T) {
	m := loaded(t)
	m.deleteTarget = &m.tasks[0]
	m, _ = send(m, taskDeletedMsg{task: &models.Task{ID: "a"}, err: errors.New("not iCloud")})
	if m.err == nil || !strings.Contains(m.err.Error(), "removed locally") {
		t.Errorf("err = %v, want 'removed locally (Reminders: …)'", m.err)
	}
	if m.deleteTarget != nil {
		t.Error("deleteTarget must clear")
	}
}

func TestBatchSelectCompleteAndCancel(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, key("v"))
	if !m.selecting || !m.selected["a"] {
		t.Fatalf("v must start selecting with the cursor task selected: %v", m.selected)
	}
	m, _ = send(m, key("j"), key("space"))
	if !m.selected["b"] || len(m.selected) != 2 {
		t.Errorf("selected = %v, want a,b", m.selected)
	}
	m, _ = send(m, key("space"))
	if m.selected["b"] {
		t.Error("space again must deselect")
	}
	m, _ = send(m, key("A"))
	if len(m.selected) != 4 {
		t.Errorf("A selects every task row, got %d", len(m.selected))
	}
	if got := len(m.selectedTasks()); got != 4 {
		t.Errorf("selectedTasks = %d, want 4", got)
	}

	m, cmd := send(m, key("enter"))
	if m.selecting || m.selected != nil || cmd == nil {
		t.Fatalf("enter: selecting=%v selected=%v cmd=%v", m.selecting, m.selected, cmd != nil)
	}
	for _, task := range m.tasks {
		if !task.Done() || task.CompletedAt == nil {
			t.Errorf("task %s not flipped to completed immediately", task.ID)
		}
	}

	m = loaded(t)
	m, _ = send(m, key("v"), key("esc"))
	if m.selecting || m.selected != nil {
		t.Error("esc must leave batch mode and drop the selection")
	}
}

func TestBatchDeleteNeedsSelection(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, key("v"), key("v"))
	m.selected = map[string]bool{}
	if _, cmd := send(m, key("d")); cmd != nil {
		t.Error("d with an empty selection must do nothing")
	}
	m.selected["a"] = true
	if _, cmd := send(m, key("d")); cmd == nil {
		t.Error("d with a selection must return the batch delete command")
	}
}

func TestBatchEnterWithoutSelectionKeepsMode(t *testing.T) {
	m := loaded(t)
	m.selecting, m.selected = true, map[string]bool{}
	m, cmd := send(m, key("enter"))
	if !m.selecting || cmd != nil {
		t.Errorf("selecting=%v cmd=%v, want still selecting and no command", m.selecting, cmd != nil)
	}
}

func TestPostponeSetsTomorrow(t *testing.T) {
	m := loaded(t)
	m, cmd := send(m, key("S"))
	due := m.tasks[0].DueDate
	if due == nil || due.Before(time.Now()) || due.After(time.Now().AddDate(0, 0, 2)) {
		t.Errorf("due = %v, want about tomorrow", due)
	}
	if cmd == nil {
		t.Error("expected a postpone command")
	}
}

func TestSimpleListKeys(t *testing.T) {
	m := loaded(t)

	m2, cmd := send(m, key("c"))
	if !m2.showDone || cmd == nil {
		t.Errorf("c: showDone=%v cmd=%v, want toggled + reload", m2.showDone, cmd != nil)
	}
	m2, cmd = send(m, key("s"))
	if !m2.syncing || cmd == nil {
		t.Errorf("s: syncing=%v cmd=%v", m2.syncing, cmd != nil)
	}
	if _, cmd = send(m2, key("s")); cmd != nil {
		t.Error("s while already syncing must be ignored")
	}
	m2, cmd = send(m, key("y"))
	if m2.flash != "Copied to clipboard" || cmd == nil {
		t.Errorf("y: flash=%q", m2.flash)
	}
	m2, _ = send(m2, clearFlashMsg{text: "something else"})
	if m2.flash == "" {
		t.Error("a stale flash timer must not clear a newer flash")
	}
	m2, _ = send(m2, clearFlashMsg{text: "Copied to clipboard"})
	if m2.flash != "" {
		t.Error("matching flash timer must clear it")
	}
	m2, cmd = send(m, key("i"))
	if m2.view != viewStats || cmd == nil {
		t.Errorf("i: view=%v", m2.view)
	}
	m2, _ = send(m2, key("x")) // any key leaves stats
	if m2.view != viewList {
		t.Errorf("view = %v after a key in stats, want list", m2.view)
	}
	if _, cmd = send(m, key("q")); cmd == nil {
		t.Error("q must quit")
	}
}

func TestKeysWithNoTaskUnderCursorAreSafe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m, _ := send(newModel(""), tasksLoadedMsg{})
	for _, k := range []string{"space", "d", "e", "y", "S", "p", "enter", "o", "v", "u", "j", "k", "1"} {
		m, _ = send(m, key(k))
		m.selecting, m.selected, m.view = false, nil, viewList
	}
	if m.deleteTarget != nil || m.detailTarget != nil {
		t.Error("actions on an empty list must not set targets")
	}
}

func TestDetailPopupSubtasks(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, key("enter"))
	if m.view != viewDetail || m.detailTarget.ID != "a" {
		t.Fatalf("enter: view=%v", m.view)
	}

	m, _ = send(m, key("a"))
	if !m.addingSubtask {
		t.Fatal("a must start subtask entry")
	}
	m = typeText(m, "first")
	m, cmd := send(m, key("enter"))
	if m.addingSubtask || cmd == nil {
		t.Fatalf("enter must finish adding and persist: adding=%v cmd=%v", m.addingSubtask, cmd != nil)
	}
	got := m.tasks[0].Subtasks
	if len(got) != 1 || got[0].Title != "first" || got[0].Done {
		t.Fatalf("subtasks = %+v", got)
	}

	m, _ = send(m, key("a"), key("enter")) // empty title is ignored
	if len(m.tasks[0].Subtasks) != 1 {
		t.Error("blank subtask title must not be added")
	}
	m, _ = send(m, key("a"))
	m = typeText(m, "  second  ")
	m, _ = send(m, key("enter"))
	if len(m.tasks[0].Subtasks) != 2 || m.tasks[0].Subtasks[1].Title != "second" || m.subtaskCursor != 1 {
		t.Errorf("subtasks = %+v cursor=%d, want trimmed title and cursor on the new one", m.tasks[0].Subtasks, m.subtaskCursor)
	}

	m, _ = send(m, key("a"), key("esc"))
	if m.addingSubtask || len(m.tasks[0].Subtasks) != 2 {
		t.Error("esc must abort subtask entry without adding")
	}

	m, _ = send(m, key("space"))
	if !m.tasks[0].Subtasks[1].Done {
		t.Error("space must toggle the subtask under the cursor")
	}
	m, _ = send(m, key("k"), key("space"), key("space"))
	if m.tasks[0].Subtasks[0].Done {
		t.Error("double toggle must restore the state")
	}

	m, _ = send(m, key("j"), key("x"))
	if len(m.tasks[0].Subtasks) != 1 || m.subtaskCursor != 0 {
		t.Errorf("x on the last subtask: %+v cursor=%d, want cursor clamped to 0", m.tasks[0].Subtasks, m.subtaskCursor)
	}
	m, _ = send(m, key("x"))
	m, _ = send(m, key("x")) // nothing left: must not panic
	if len(m.tasks[0].Subtasks) != 0 || m.subtaskCursor != 0 {
		t.Errorf("after deleting all: %+v cursor=%d", m.tasks[0].Subtasks, m.subtaskCursor)
	}

	m, _ = send(m, key("esc"))
	if m.view != viewList || m.detailTarget != nil {
		t.Errorf("esc: view=%v target=%v", m.view, m.detailTarget)
	}
}

func TestDetailPopupActions(t *testing.T) {
	m := loaded(t)
	open := func() Model { m2, _ := send(m, key("enter")); return m2 }

	m2, _ := send(open(), key("d"))
	if m2.view != viewList || m2.deleteTarget == nil || m2.deleteTarget.ID != "a" {
		t.Errorf("d in detail: view=%v target=%v, want confirm prompt on the list", m2.view, m2.deleteTarget)
	}
	m2, _ = send(open(), key("e"))
	if m2.view != viewCreate || m2.editTarget == nil || m2.inputs[fTitle].Value() != "buy milk" {
		t.Errorf("e in detail: view=%v title=%q", m2.view, m2.inputs[fTitle].Value())
	}
	m2, cmd := send(open(), key("p"))
	if m2.view != viewPomodoro || !m2.pomRunning || m2.pomTask == nil || cmd == nil {
		t.Errorf("p in detail: view=%v running=%v", m2.view, m2.pomRunning)
	}
	if _, cmd = send(open(), key("o")); cmd != nil {
		t.Error("o without any URL must do nothing")
	}
	m.tasks[0].Notes = "see https://example.com/x for details"
	if _, cmd = send(open(), key("o")); cmd == nil {
		t.Error("o must open the URL found in the notes")
	}
}

func TestPomodoroLifecycle(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, key("p"))
	if m.view != viewPomodoro || !m.pomRunning {
		t.Fatalf("view=%v running=%v", m.view, m.pomRunning)
	}
	_, cmd := send(m, tickMsg(time.Now()))
	if cmd == nil {
		t.Error("tick while running must schedule the next tick")
	}
	m, _ = send(m, key("x")) // unrelated keys are ignored
	if m.view != viewPomodoro {
		t.Error("only esc/q leave the pomodoro")
	}
	m, _ = send(m, key("esc"))
	if m.view != viewList || m.pomRunning {
		t.Errorf("esc: view=%v running=%v", m.view, m.pomRunning)
	}
	if _, cmd = send(m, tickMsg(time.Now())); cmd != nil {
		t.Error("tick after stopping must not reschedule")
	}
}

func TestCreateFormFlow(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, key("n"))
	if m.view != viewCreate || m.editTarget != nil || m.inputIdx != fTitle {
		t.Fatalf("n: view=%v idx=%d", m.view, m.inputIdx)
	}

	m, cmd := send(m, key("ctrl+s"))
	if m.err == nil || m.err.Error() != "title is required" || cmd != nil || m.submitting {
		t.Errorf("empty submit: err=%v submitting=%v", m.err, m.submitting)
	}

	m = typeText(m, "ship it")
	m, _ = send(m, key("tab"))
	if m.inputIdx != fList {
		t.Errorf("tab: idx=%d, want list field", m.inputIdx)
	}
	m, _ = send(m, key("shift+tab"), key("shift+tab"))
	if m.inputIdx != fCount-1 {
		t.Errorf("shift+tab wraps from the first field to the last, got %d", m.inputIdx)
	}
	m, _ = send(m, key("tab"))
	if m.inputIdx != fTitle {
		t.Errorf("tab wraps to the first field, got %d", m.inputIdx)
	}

	m, _ = send(m, key("enter")) // advances, doesn't submit
	if m.inputIdx != fList || m.submitting {
		t.Errorf("enter on a middle field: idx=%d submitting=%v", m.inputIdx, m.submitting)
	}

	m, cmd = send(m, key("ctrl+s"))
	if !m.submitting || m.err != nil || cmd == nil {
		t.Errorf("valid submit: submitting=%v err=%v cmd=%v", m.submitting, m.err, cmd != nil)
	}
}

func TestCreateFormEnterOnLastFieldSubmits(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, key("n"))
	m = typeText(m, "t")
	m.inputs[fTitle].Blur()
	m.inputIdx = fCount - 1
	m, cmd := send(m, key("enter"))
	if !m.submitting || cmd == nil {
		t.Errorf("enter on the last field must submit: submitting=%v", m.submitting)
	}
}

func TestCreateFormListPicker(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, key("n"), key("tab"))
	if m.inputIdx != fList {
		t.Fatalf("idx=%d", m.inputIdx)
	}
	m, _ = send(m, key("down"))
	if m.inputs[fList].Value() != "Work" || m.listPickerIdx != 1 {
		t.Errorf("down: list=%q idx=%d, want Work", m.inputs[fList].Value(), m.listPickerIdx)
	}
	m, _ = send(m, key("down"))
	if m.listPickerIdx != 1 {
		t.Errorf("down past the last entry moved the picker to %d", m.listPickerIdx)
	}
	m, _ = send(m, key("up"), key("up"))
	if m.inputs[fList].Value() != "Home" || m.listPickerIdx != 0 {
		t.Errorf("up: list=%q idx=%d, want Home", m.inputs[fList].Value(), m.listPickerIdx)
	}
	m, _ = send(m, key("esc"))
	if m.view != viewList {
		t.Errorf("esc: view=%v", m.view)
	}
}

func TestEditFormIsPrefilled(t *testing.T) {
	m := loaded(t)
	m.tasks[1].URL = "https://x.test"
	m.tasks[1].Recurrence = "weekly"
	m, _ = send(m, key("j"), key("e"))
	if m.view != viewCreate || m.editTarget == nil || m.editTarget.ID != "b" {
		t.Fatalf("e: view=%v target=%v", m.view, m.editTarget)
	}
	want := [fCount]string{"call mom", "Home", time.Now().Format("2006-01-02"), "birthday gift", "https://x.test", "weekly"}
	for i, w := range want {
		if got := m.inputs[i].Value(); got != w {
			t.Errorf("field %d = %q, want %q", i, got, w)
		}
	}
	m, _ = send(m, key("esc"))
	if m.editTarget != nil {
		t.Error("esc must drop the edit target")
	}
}

func TestSavedAndSyncMessages(t *testing.T) {
	m := loaded(t)
	m.view, m.submitting, m.editTarget = viewCreate, true, &m.tasks[0]

	m, cmd := send(m, taskSavedMsg{err: errors.New("db locked")})
	if m.err == nil || m.submitting || m.view != viewCreate || cmd != nil {
		t.Errorf("failed save: err=%v submitting=%v view=%v (form must stay open)", m.err, m.submitting, m.view)
	}
	m.submitting = true
	m, cmd = send(m, taskSavedMsg{})
	if m.err != nil || m.view != viewList || m.editTarget != nil || cmd == nil {
		t.Errorf("save ok: err=%v view=%v target=%v cmd=%v", m.err, m.view, m.editTarget, cmd != nil)
	}

	m.syncing = true
	m, _ = send(m, syncDoneMsg{err: errors.New("offline")})
	if m.syncing || m.err == nil || len(m.tasks) != 4 {
		t.Errorf("failed sync must keep tasks and show the error: syncing=%v err=%v tasks=%d", m.syncing, m.err, len(m.tasks))
	}
	m.syncing = true
	m, _ = send(m, syncDoneMsg{tasks: []models.Task{{ID: "z", Title: "only", List: "L"}}})
	if m.syncing || m.err != nil || len(m.tasks) != 1 || m.lastSynced.IsZero() {
		t.Errorf("sync ok: syncing=%v err=%v tasks=%d lastSynced=%v", m.syncing, m.err, len(m.tasks), m.lastSynced)
	}
	m, _ = send(m, batchDeletedMsg{count: 1})
	if m.selecting || m.selected != nil {
		t.Error("batch delete must leave selection mode")
	}
	m, _ = send(m, toggleDonedMsg{err: errors.New("x")})
	if m.err == nil {
		t.Error("toggle error must be shown")
	}
	m, _ = send(m, postponeMsg{err: errors.New("y")}, batchDoneMsg{err: errors.New("z")})
	if m.err == nil || m.err.Error() != "z" {
		t.Errorf("err = %v", m.err)
	}
	m, _ = send(m, statsMsg{today: 1, week: 2, total: 3})
	if m.statsData == nil || m.statsData.total != 3 {
		t.Error("statsMsg must be stored")
	}
}

func TestListNamesMsg(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, listNamesMsg{entries: []models.ListEntry{
		{Name: "Z", Account: "iCloud"}, {Name: "A", Account: "Gmail"}, {Name: "A", Account: "iCloud"},
	}})
	if len(m.listEntries) != 3 || m.listEntries[0].Account != "Gmail" || m.listEntries[2].Name != "Z" {
		t.Errorf("entries = %+v, want sorted by name then account, replacing the task-derived ones", m.listEntries)
	}

	m = loaded(t) // has task-derived entries → an error must stay silent
	m, _ = send(m, listNamesMsg{err: errors.New("no Reminders")})
	if m.err != nil {
		t.Errorf("err = %v, must not clobber the form when entries already exist", m.err)
	}
	m.listEntries = nil
	m, _ = send(m, listNamesMsg{err: errors.New("no Reminders")})
	if m.err == nil || !strings.Contains(m.err.Error(), "couldn't load Reminders lists") {
		t.Errorf("err = %v, want surfaced when the picker would stay empty", m.err)
	}
}

func TestViewsRenderWithoutPanic(t *testing.T) {
	m := loaded(t)
	m.tasks[0].Subtasks = []models.Subtask{{Title: "step one"}, {Title: "step two", Done: true}}
	m.tasks[0].URL = "https://example.com"
	m.statsData = &statsMsg{today: 2, week: 7, total: 40, daily: []int{0, 1, 3, 2, 5, 0, 4}}

	steps := []struct {
		name string
		keys []string
		want string
	}{
		{"list", nil, "buy milk"},
		{"detail", []string{"enter"}, "step one"},
		{"form", []string{"esc", "n"}, "Title"},
		{"help", []string{"esc", "?"}, "taskctl"},
		{"stats", []string{"esc", "i"}, "taskctl"},
		{"pomodoro", []string{"esc", "p"}, "buy milk"},
		{"palette", []string{"esc", ":"}, "new"},
		{"search", []string{"esc", "/"}, "search"},
	}
	for _, s := range steps {
		m, _ = send(m, func() []tea.Msg {
			var out []tea.Msg
			for _, k := range s.keys {
				out = append(out, key(k))
			}
			return out
		}()...)
		out := m.viewContent()
		if !strings.Contains(out, s.want) {
			t.Errorf("%s view lacks %q:\n%s", s.name, s.want, out)
		}
		if v := m.View(); !v.AltScreen {
			t.Errorf("%s: View must request the alt screen", s.name)
		}
	}
	m.loading = true
	if !strings.Contains(m.viewContent(), "Loading") {
		t.Error("loading view must say so")
	}
}

func TestPureHelpers(t *testing.T) {
	for _, c := range []struct {
		in    string
		title string
		prio  int
	}{
		{"!! urgent", "urgent", 1},
		{"! soon", "soon", 5},
		{"!!no space", "!!no space", 0},
		{"plain", "plain", 0},
		{"a ! b", "a ! b", 0},
	} {
		if title, p := parsePriority(c.in); title != c.title || p != c.prio {
			t.Errorf("parsePriority(%q) = %q,%d want %q,%d", c.in, title, p, c.title, c.prio)
		}
	}

	for _, c := range []struct{ in, want string }{
		{"no link", ""},
		{"see https://a.b/c now", "https://a.b/c"},
		{"HTTPS://UPPER.example/x ok", "HTTPS://UPPER.example/x"},
		{"(http://paren.example/p)", "http://paren.example/p"},
		{`<a href="https://q.example/z">`, "https://q.example/z"},
		{"http://first.example then https://second.example", "http://first.example"},
		{"İstanbul ok https://tr.example/ist", "https://tr.example/ist"}, // lowercasing İ changes byte length
		{"https://end.example", "https://end.example"},
	} {
		if got := firstURL(c.in); got != c.want {
			t.Errorf("firstURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	if got := effectiveURL(&models.Task{URL: "https://own", Notes: "https://notes"}); got != "https://own" {
		t.Errorf("explicit URL must win, got %q", got)
	}
	if got := effectiveURL(&models.Task{Notes: "x https://notes.example y"}); got != "https://notes.example" {
		t.Errorf("notes fallback = %q", got)
	}

	entries := uniqueListEntries([]models.Task{{List: "b"}, {List: ""}, {List: "a"}, {List: "b"}})
	if len(entries) != 2 || entries[0].Name != "a" || entries[1].Name != "b" {
		t.Errorf("uniqueListEntries = %+v, want sorted, deduped, no empty", entries)
	}

	if len(sparkline(nil)) != 0 || len([]rune(sparkline([]int{0, 5, 10}))) != 3 {
		t.Errorf("sparkline lengths off: %q / %q", sparkline(nil), sparkline([]int{0, 5, 10}))
	}
}

func TestTaskFromForm(t *testing.T) {
	inputs := newFormInputs("")
	set := func(i int, v string) { inputs[i].SetValue(v) }

	if _, err := taskFromForm(inputs, nil); err == nil {
		t.Error("an empty title must be rejected")
	}

	set(fTitle, "!! file taxes ")
	set(fList, " Home ")
	set(fDue, "2026-07-05")
	set(fNotes, " bring receipts ")
	set(fURL, " https://tax.example ")
	set(fRecurrence, " WEEKLY ")
	got, err := taskFromForm(inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "file taxes" || got.Priority != 1 || got.List != "Home" || got.Notes != "bring receipts" ||
		got.URL != "https://tax.example" || got.Recurrence != "weekly" || got.Source != "taskctl" || got.Status != "needsAction" {
		t.Errorf("task = %+v", got)
	}
	if got.DueDate == nil || got.DueDate.Format("2006-01-02") != "2026-07-05" {
		t.Errorf("due = %v", got.DueDate)
	}
	if !strings.HasPrefix(got.ID, "taskctl-") {
		t.Errorf("id = %q", got.ID)
	}

	set(fDue, "someday maybe")
	if _, err := taskFromForm(inputs, nil); err == nil || !strings.Contains(err.Error(), "datum") {
		t.Errorf("unparseable due date must be an error, got %v", err)
	}

	// Editing replaces the row under a new ID — the local-only subtasks must come along.
	set(fDue, "")
	orig := &models.Task{ID: "old", Subtasks: []models.Subtask{{Title: "keep me", Done: true}}}
	edited, err := taskFromForm(inputs, orig)
	if err != nil {
		t.Fatal(err)
	}
	if len(edited.Subtasks) != 1 || edited.Subtasks[0].Title != "keep me" || !edited.Subtasks[0].Done {
		t.Errorf("edit lost the subtasks: %+v", edited.Subtasks)
	}
	if edited.ID == "old" {
		t.Error("edit must get a fresh ID")
	}
}

func TestPrefillFormRoundTripsThroughTaskFromForm(t *testing.T) {
	src := &models.Task{Title: "plan", List: "Work", Notes: "n", URL: "https://u.example", Recurrence: "daily", DueDate: day(3)}
	got, err := taskFromForm(prefillForm(src), src)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "plan" || got.List != "Work" || got.Notes != "n" || got.URL != "https://u.example" || got.Recurrence != "daily" {
		t.Errorf("round trip = %+v", got)
	}
	if got.DueDate == nil || got.DueDate.Format("2006-01-02") != src.DueDate.Format("2006-01-02") {
		t.Errorf("due = %v, want %v", got.DueDate, src.DueDate)
	}
}

func TestMotionThrottleDropsRapidMotionOnly(t *testing.T) {
	f := motionThrottleFilter()
	if f(nil, tea.MouseMotionMsg{}) == nil {
		t.Fatal("first motion must pass")
	}
	if f(nil, tea.MouseMotionMsg{}) != nil {
		t.Error("an immediate second motion must be dropped")
	}
	if f(nil, tea.MouseClickMsg{}) == nil {
		t.Error("non-motion messages must always pass")
	}
}

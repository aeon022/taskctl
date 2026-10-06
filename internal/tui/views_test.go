package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/tuitest"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/charmbracelet/x/ansi"
)

// testNow is Tue 06 Oct 2026, 15:00 local.
var testNow = time.Date(2026, 10, 6, 15, 0, 0, 0, time.Local)

func at(days, hour int) *time.Time {
	d := time.Date(2026, 10, 6+days, hour, 0, 0, 0, time.Local)
	return &d
}

// viewsModel has two lists with one task per tab: Home (a overdue P1, b
// today), Work (c in 3 days P1, d in 20 days, e undated, f completed).
func viewsModel(t *testing.T, w, h int) Model {
	t.Helper()
	fixedNow(t, testNow)
	t.Setenv("HOME", t.TempDir())
	tasks := []models.Task{
		{ID: "a", Title: "overdue home", List: "Home", Status: "needsAction", DueDate: at(-2, 9), Priority: 1},
		{ID: "b", Title: "today home", List: "Home", Status: "needsAction", DueDate: at(0, 9)},
		{ID: "c", Title: "soon work", List: "Work", Status: "needsAction", DueDate: at(3, 9), Priority: 1},
		{ID: "d", Title: "later work", List: "Work", Status: "needsAction", DueDate: at(20, 9)},
		{ID: "e", Title: "undated work", List: "Work", Status: "needsAction"},
		{ID: "f", Title: "finished work", List: "Work", Status: "completed", DueDate: at(40, 9)},
	}
	m := newModel("")
	m, _ = send(m, tea.WindowSizeMsg{Width: w, Height: h}, tasksLoadedMsg{tasks: tasks})
	return m
}

func ids(m Model) string { return strings.Join(taskRows(m), ",") }

// ── pure tab logic ────────────────────────────────────────────────────────────

func TestFilterViewBoundaries(t *testing.T) {
	now := testNow // Tue 15:00
	d := func(y, mo, day, h, mi, s, ns int) *time.Time {
		x := time.Date(y, time.Month(mo), day, h, mi, s, ns, time.Local)
		return &x
	}
	tasks := []models.Task{
		{ID: "overdue-last-second", Status: "needsAction", DueDate: d(2026, 10, 5, 23, 59, 59, 0)},
		{ID: "today-midnight", Status: "needsAction", DueDate: d(2026, 10, 6, 0, 0, 0, 0)},
		{ID: "today-last-second", Status: "needsAction", DueDate: d(2026, 10, 6, 23, 59, 59, 0)},
		{ID: "tomorrow-midnight", Status: "needsAction", DueDate: d(2026, 10, 7, 0, 0, 0, 0)},
		{ID: "plus7-evening", Status: "needsAction", DueDate: d(2026, 10, 13, 23, 59, 0, 0)},
		{ID: "plus8-midnight", Status: "needsAction", DueDate: d(2026, 10, 14, 0, 0, 0, 0)},
		{ID: "undated", Status: "needsAction"},
		{ID: "done-overdue", Status: "completed", DueDate: d(2026, 10, 1, 9, 0, 0, 0)},
		{ID: "done-today", Status: "completed", DueDate: d(2026, 10, 6, 9, 0, 0, 0)},
		{ID: "recurring-tomorrow", Status: "needsAction", Recurrence: "weekly", DueDate: d(2026, 10, 7, 9, 0, 0, 0)},
	}
	name := func(idx []int) string {
		var out []string
		for _, i := range idx {
			out = append(out, tasks[i].ID)
		}
		return strings.Join(out, ",")
	}
	for tab, want := range map[listTab]string{
		tabAll:     "overdue-last-second,today-midnight,today-last-second,tomorrow-midnight,plus7-evening,plus8-midnight,undated,recurring-tomorrow",
		tabToday:   "today-midnight,today-last-second",
		tabOverdue: "overdue-last-second",
		tabNext7:   "tomorrow-midnight,plus7-evening,recurring-tomorrow",
		tabNoDate:  "undated",
		tabDone:    "done-overdue,done-today",
	} {
		if got := name(filterView(tasks, tab, now)); got != want {
			t.Errorf("%s: %s\nwant %s", tabLabels[tab], got, want)
		}
	}
}

func TestTabCondKeepsJustCompletedTasksVisible(t *testing.T) {
	// the list uses tabCond (status-blind for open tabs) so a task completed
	// right now stays on screen greyed out; counting uses inTab and drops it
	task := models.Task{Status: "completed", DueDate: at(0, 9)}
	if !tabCond(&task, tabToday, testNow) {
		t.Error("tabCond(Today) must still match a task completed today")
	}
	if inTab(&task, tabToday, testNow) {
		t.Error("inTab(Today) must not count a completed task")
	}
}

// ── tabs: counts, keys, clicks ────────────────────────────────────────────────

func TestTabCountsAndFilteringByTab(t *testing.T) {
	m := viewsModel(t, 100, 34)
	if got := m.tabCounts(); got[tabAll] != 5 || got[tabToday] != 1 || got[tabOverdue] != 1 ||
		got[tabNext7] != 1 || got[tabNoDate] != 1 || got[tabDone] != 1 {
		t.Errorf("counts = %v, want All 5 · Today 1 · Overdue 1 · Next7 1 · NoDate 1 · Done 1", got)
	}
	line := ansi.Strip(m.tabsLine())
	for _, want := range []string{"All 5", "Today 1", "Overdue 1", "Next 7 days 1", "No date 1", "Done 1"} {
		if !strings.Contains(line, want) {
			t.Errorf("tabs row missing %q: %q", want, line)
		}
	}
	m, _ = m.setTab(tabNext7)
	if ids(m) != "c" {
		t.Errorf("Next 7 days rows = %s, want c", ids(m))
	}
	m, _ = m.setTab(tabNoDate)
	if ids(m) != "e" {
		t.Errorf("No date rows = %s", ids(m))
	}
}

func TestTabCountsReflectOtherFiltersButNotTheTab(t *testing.T) {
	m := viewsModel(t, 100, 34)
	m.listFilter = "Work"
	if got := m.tabCounts(); got[tabAll] != 3 || got[tabToday] != 0 || got[tabOverdue] != 0 || got[tabNext7] != 1 || got[tabNoDate] != 1 || got[tabDone] != 1 {
		t.Errorf("list=Work counts = %v", got)
	}
	m.listFilter, m.prioFilter = "", 1
	if got := m.tabCounts(); got[tabAll] != 2 || got[tabOverdue] != 1 || got[tabNext7] != 1 || got[tabToday] != 0 {
		t.Errorf("prio=P1 counts = %v", got)
	}
	m.prioFilter = 0
	m, _ = m.setTab(tabOverdue)
	if got := m.tabCounts(); got[tabAll] != 5 {
		t.Errorf("the active tab must not narrow the other counts: %v", got)
	}
	m, _ = send(m, key("/"))
	m = typeText(m, "work")
	m, _ = send(m, key("enter"))
	if got := m.tabCounts(); got[tabAll] != 3 {
		t.Errorf("search narrows the counts too: %v", got)
	}
}

func TestTabKeysCycleAndCMapsToDone(t *testing.T) {
	m := viewsModel(t, 100, 34)
	m, _ = send(m, key("tab"))
	if m.tab != tabToday || ids(m) != "b" {
		t.Errorf("tab → %v rows %s, want Today / b", m.tab, ids(m))
	}
	m, _ = send(m, key("]"))
	if m.tab != tabOverdue || ids(m) != "a" {
		t.Errorf("] → %v rows %s", m.tab, ids(m))
	}
	m, _ = send(m, key("["), key("shift+tab"))
	if m.tab != tabAll {
		t.Errorf("[ and shift+tab step back to All, got %v", m.tab)
	}
	m, cmd := send(m, key("shift+tab")) // wraps to Done
	if m.tab != tabDone || !m.showDone || cmd == nil {
		t.Errorf("wrap to Done: tab=%v showDone=%v reload=%v", m.tab, m.showDone, cmd != nil)
	}
	if ids(m) != "f" {
		t.Errorf("Done rows = %s, want only the completed task", ids(m))
	}
	m, cmd = send(m, key("tab")) // Done → All
	if m.tab != tabAll || m.showDone || cmd == nil {
		t.Errorf("leaving Done: tab=%v showDone=%v reload=%v", m.tab, m.showDone, cmd != nil)
	}

	m, cmd = send(m, key("c"))
	if m.tab != tabDone || !m.showDone || cmd == nil {
		t.Errorf("c → Done tab: tab=%v showDone=%v reload=%v", m.tab, m.showDone, cmd != nil)
	}
	m, _ = send(m, key("c"))
	if m.tab != tabAll || m.showDone {
		t.Errorf("c again → All: tab=%v showDone=%v", m.tab, m.showDone)
	}
}

func TestTabsRowClickSelectsTab(t *testing.T) {
	m := viewsModel(t, 100, 34)
	for want, label := range map[listTab]string{tabToday: "Today", tabOverdue: "Overdue", tabNoDate: "No date"} {
		plain := ansi.Strip(m.tabsLine())
		x := lipgloss.Width(plain[:strings.Index(plain, label)]) + 1
		n, _ := send(m, tea.MouseClickMsg{Button: tea.MouseLeft, X: x + appPadH, Y: 2 + appPadV})
		if n.tab != want {
			t.Errorf("click on %q (x=%d) → tab %v, want %v", label, x, n.tab, want)
		}
	}
	// a click on the tabs row far to the right of the last tab does nothing
	n, _ := send(m, tea.MouseClickMsg{Button: tea.MouseLeft, X: 95, Y: 2 + appPadV})
	if n.tab != tabAll {
		t.Errorf("click past the tabs changed the tab to %v", n.tab)
	}
}

func TestEmptyStateNamesTheView(t *testing.T) {
	m := viewsModel(t, 100, 34)
	m.tasks = m.tasks[:1] // only the overdue task
	m, _ = m.setTab(tabNoDate)
	if out := tuitest.Text(m); !strings.Contains(out, "Nothing in No date") || !strings.Contains(out, "press tab for another view") {
		t.Errorf("empty tab:\n%s", out)
	}
	m, _ = m.setTab(tabAll)
	m.prioFilter = 9
	m.rows = m.rebuildRows()
	if out := tuitest.Text(m); !strings.Contains(out, "No tasks match these filters") || !strings.Contains(out, "press x to clear them") {
		t.Errorf("empty filter:\n%s", out)
	}
}

// ── "f" filter menu and chips ─────────────────────────────────────────────────

func TestFilterMenuListPriorityClearAndCancel(t *testing.T) {
	m := viewsModel(t, 100, 34)

	m, _ = send(m, key("f"))
	if m.filterMenu != menuMain || !strings.Contains(tuitest.Text(m), "l list · p priority · x clear all") {
		t.Fatalf("f opens the menu: %v\n%s", m.filterMenu, tuitest.Text(m))
	}
	m, _ = send(m, key("l"))
	if out := tuitest.Text(m); m.filterMenu != menuList || !strings.Contains(out, "1 Home") || !strings.Contains(out, "2 Work") {
		t.Fatalf("l shows the list picker:\n%s", out)
	}
	m, _ = send(m, key("9")) // there is no 9th list: stays in the picker
	if m.filterMenu != menuList || m.listFilter != "" {
		t.Error("an out-of-range digit must be ignored")
	}
	m, _ = send(m, key("2"))
	if m.filterMenu != menuNone || m.listFilter != "Work" || ids(m) != "c,d,e,f" {
		t.Errorf("2 → list Work: menu=%v filter=%q rows=%s", m.filterMenu, m.listFilter, ids(m))
	}

	m, _ = send(m, key("f"), key("p"), key("1"))
	if m.prioFilter != 1 || ids(m) != "c" {
		t.Errorf("f p 1 → P1 within Work: prio=%d rows=%s", m.prioFilter, ids(m))
	}
	if line := ansi.Strip(m.chipsLine()); !strings.Contains(line, "Work ×") || !strings.Contains(line, "P1 ×") {
		t.Errorf("chips = %q", line)
	}

	m, _ = send(m, key("f"), key("esc")) // cancel keeps everything
	if m.filterMenu != menuNone || m.listFilter != "Work" || m.prioFilter != 1 {
		t.Error("esc in the menu must change nothing")
	}
	m, _ = send(m, key("f"), key("p"), key("0"))
	if m.prioFilter != 0 {
		t.Error("p 0 clears the priority filter")
	}

	m, _ = m.setTab(tabToday)
	m, _ = send(m, key("f"), key("x"))
	if m.listFilter != "" || m.prioFilter != 0 || m.tab != tabAll || len(m.chips()) != 0 {
		t.Errorf("f x clears everything: list=%q prio=%d tab=%v", m.listFilter, m.prioFilter, m.tab)
	}
}

func TestXAndEscClearFiltersOnlyWhenSomethingIsActive(t *testing.T) {
	m := viewsModel(t, 100, 34)
	if n, cmd := send(m, key("x")); cmd != nil || n.tab != tabAll || ids(n) != ids(m) {
		t.Error("x with nothing active is a no-op")
	}
	m, _ = send(m, key("O")) // legacy overdue filter + a list chip
	m.listFilter = "Home"
	m.rows = m.rebuildRows()
	m, _ = send(m, key("esc"))
	if m.filter != filterNone || m.listFilter != "" || ids(m) != "a,b,c,d,e,f" {
		t.Errorf("esc clears chips and the focus filter: filter=%v list=%q rows=%s", m.filter, m.listFilter, ids(m))
	}
}

func TestChipClickRemovesTheChip(t *testing.T) {
	m := viewsModel(t, 100, 34)
	m.listFilter, m.prioFilter = "Work", 1
	m.rows = m.rebuildRows()
	plain := ansi.Strip(m.chipsLine())
	if got := strings.Count(plain, "×"); got != 2 {
		t.Fatalf("chips = %q", plain)
	}
	px := lipgloss.Width(plain[:strings.Index(plain, "P1")]) + 1
	n, _ := send(m, tea.MouseClickMsg{Button: tea.MouseLeft, X: px + appPadH, Y: 3 + appPadV})
	if n.prioFilter != 0 || n.listFilter != "Work" {
		t.Errorf("click on P1: prio=%d list=%q (only P1 should go)", n.prioFilter, n.listFilter)
	}
	wx := lipgloss.Width(plain[:strings.Index(plain, "Work")]) + 1
	n, _ = send(m, tea.MouseClickMsg{Button: tea.MouseLeft, X: wx + appPadH, Y: 3 + appPadV})
	if n.listFilter != "" || n.prioFilter != 1 {
		t.Errorf("click on Work: list=%q prio=%d", n.listFilter, n.prioFilter)
	}
}

func TestSearchChipAppearsWhenTheBarIsClosed(t *testing.T) {
	m := viewsModel(t, 100, 34)
	m, _ = send(m, key("/"))
	m = typeText(m, "soon")
	if len(m.chips()) != 0 {
		t.Error("while typing the search bar itself is the indicator")
	}
	m, _ = send(m, key("enter"))
	if line := ansi.Strip(m.chipsLine()); !strings.Contains(line, "/soon ×") {
		t.Errorf("chips after enter = %q", line)
	}
	n, _ := send(m, tea.MouseClickMsg{Button: tea.MouseLeft, X: 4 + appPadH, Y: 3 + appPadV})
	if n.searchQuery() != "" {
		t.Error("clicking the search chip clears the query")
	}
}

// ── geometry: every new row must be in the hit-test math ──────────────────────

func assertRowsHit(t *testing.T, name string, m Model) {
	t.Helper()
	lines := strings.Split(tuitest.Text(m), "\n")
	checked := 0
	for y, line := range lines {
		line = ansi.Truncate(line, m.sideWidth()+m.listWidth()+appPadH, "")
		for i, r := range m.rows {
			if r.isHeader || !strings.Contains(line, r.task.Title) {
				continue
			}
			checked++
			if got := m.rowHitTest(y - appPadV); got != i {
				t.Errorf("%s: screen line %d shows %q (row %d) but rowHitTest(%d) = %d", name, y, r.task.Title, i, y-appPadV, got)
			}
			x := m.sideWidth() + appPadH + 6
			n, _ := send(m, tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
			if cursorID(n) != r.task.ID {
				t.Errorf("%s: clicking line %d selected %q, want %q", name, y, cursorID(n), r.task.ID)
			}
		}
	}
	if checked < 2 {
		t.Fatalf("%s: only %d task rows on screen:\n%s", name, checked, strings.Join(lines, "\n"))
	}
}

func TestClickMappingWithTabsAndChipsAtEveryWidth(t *testing.T) {
	for _, size := range [][2]int{{80, 30}, {100, 30}, {130, 34}, {150, 34}} {
		for _, state := range []string{"tabs only", "priority chip (2 groups)", "search chip", "chip + menu open", "chip + search bar", "chip + palette", "Next 7 days tab"} {
			m := viewsModel(t, size[0], size[1])
			switch state {
			case "priority chip (2 groups)":
				m.prioFilter = 1
			case "search chip":
				m, _ = send(m, key("/"))
				m = typeText(m, "work")
				m, _ = send(m, key("enter"))
			case "chip + menu open":
				m.prioFilter = 1
				m, _ = send(m, key("f"))
			case "chip + search bar":
				m.prioFilter = 1
				m, _ = send(m, key("/"))
			case "chip + palette":
				m.prioFilter = 1
				m, _ = send(m, key(":"))
			case "Next 7 days tab":
				m, _ = m.setTab(tabAll)
			}
			m.rows = m.rebuildRows()
			m.cursor = firstTaskRow(m.rows)
			assertRowsHit(t, state+" "+strings.Join([]string{string(rune('0' + size[0]/100)), "w"}, ""), m)
		}
	}
}

func TestViewsNeverOverflowOrChangeHeight(t *testing.T) {
	for _, size := range [][2]int{{40, 14}, {60, 15}, {80, 24}, {100, 30}, {139, 32}, {140, 32}, {150, 34}} {
		for _, state := range []string{"plain", "chips", "menu main", "menu list", "menu prio", "sidebar focus", "overdue tab"} {
			m := viewsModel(t, size[0], size[1])
			switch state {
			case "chips":
				m.listFilter, m.prioFilter = "Home", 1
				m.rows = m.rebuildRows()
			case "menu main":
				m, _ = send(m, key("f"))
			case "menu list":
				m, _ = send(m, key("f"), key("l"))
			case "menu prio":
				m, _ = send(m, key("f"), key("p"))
			case "sidebar focus":
				m, _ = send(m, key("h"))
			case "overdue tab":
				m, _ = m.setTab(tabOverdue)
			}
			lines := strings.Split(tuitest.Text(m), "\n")
			if len(lines) != size[1]-1 {
				t.Errorf("%dx%d %s: %d lines, want %d", size[0], size[1], state, len(lines), size[1]-1)
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > size[0] {
					t.Errorf("%dx%d %s: line %d is %d cells wide: %q", size[0], size[1], state, i, w, l)
				}
			}
			if line := ansi.Strip(m.tabsLine()); !strings.Contains(line, tabLabels[m.tab]) {
				t.Errorf("%dx%d %s: the active tab %q must stay visible: %q", size[0], size[1], state, tabLabels[m.tab], line)
			}
		}
	}
}

// ── Lists sidebar ─────────────────────────────────────────────────────────────

func TestSidebarOnlyFrom140Columns(t *testing.T) {
	for w, want := range map[int]bool{100: false, 119: false, 120: false, 139: false, 140: true, 150: true} {
		m := viewsModel(t, w, 34)
		out := tuitest.Text(m)
		if has := strings.Contains(out, "─ Lists"); has != want {
			t.Errorf("width %d: sidebar shown = %v, want %v", w, has, want)
		}
		if w >= wideMin && !strings.Contains(out, "─ Tasks") {
			t.Errorf("width %d: the Tasks panel is always there from %d columns", w, wideMin)
		}
	}
}

func TestSidebarCountsOverdueDotAndClickFilter(t *testing.T) {
	m := viewsModel(t, 150, 34)
	stats := m.sideEntries()
	if len(stats) != 3 || stats[0].name != "All lists" || stats[0].open != 5 || stats[0].overdue != 1 ||
		stats[1].name != "Home" || stats[1].open != 2 || stats[1].overdue != 1 ||
		stats[2].name != "Work" || stats[2].open != 3 || stats[2].overdue != 0 {
		t.Fatalf("entries = %+v", stats)
	}
	out := tuitest.Text(m)
	if !strings.Contains(out, "All lists") || !strings.Contains(out, "● 2") {
		t.Errorf("the overdue list carries a red dot before its count:\n%s", out)
	}

	click := func(m Model, i int) Model {
		x, y := appPadH+3, appPadV+m.chromeAbove()+1+i
		n, _ := send(m, tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
		return n
	}
	m = click(m, 2) // Work
	if m.listFilter != "Work" || ids(m) != "c,d,e,f" {
		t.Errorf("click on Work: filter=%q rows=%s", m.listFilter, ids(m))
	}
	if !strings.Contains(ansi.Strip(m.chipsLine()), "Work ×") {
		t.Error("the sidebar filter shows up as a chip too")
	}
	m = click(m, 2) // again → off
	if m.listFilter != "" {
		t.Errorf("clicking the active list again clears it: %q", m.listFilter)
	}
	m = click(click(m, 1), 0) // Home, then All lists
	if m.listFilter != "" {
		t.Errorf("All lists clears the filter: %q", m.listFilter)
	}
}

func TestSidebarKeyboard(t *testing.T) {
	m := viewsModel(t, 150, 34)
	m, _ = send(m, key("h"))
	if !m.sideFocus || m.sideCursor != 0 {
		t.Fatalf("h focuses the sidebar on All lists: focus=%v cursor=%d", m.sideFocus, m.sideCursor)
	}
	if m.browsing() {
		t.Error("no focus-reload while the sidebar has focus")
	}
	m, _ = send(m, key("j"), key("j"), key("j"), key("k")) // clamps at the last entry
	if m.sideCursor != 1 {
		t.Errorf("cursor = %d, want 1 (j j j k clamps at 2 then back)", m.sideCursor)
	}
	m, _ = send(m, key("j"), key("enter"))
	if m.sideFocus || m.listFilter != "Work" || ids(m) != "c,d,e,f" {
		t.Errorf("enter applies: focus=%v filter=%q rows=%s", m.sideFocus, m.listFilter, ids(m))
	}
	m, _ = send(m, key("h"))
	if m.sideCursor != 2 {
		t.Errorf("the cursor starts on the active list, got %d", m.sideCursor)
	}
	m, _ = send(m, key("esc"))
	if m.sideFocus || m.listFilter != "Work" {
		t.Error("esc leaves the sidebar without changing the filter")
	}
	m, _ = send(m, key("h"), key("l"))
	if m.sideFocus {
		t.Error("l leaves the sidebar")
	}

	narrow := viewsModel(t, 130, 34)
	narrow, _ = send(narrow, key("h"))
	if narrow.sideFocus {
		t.Error("h does nothing below 140 columns")
	}
}

// ── overview in the Details panel ─────────────────────────────────────────────

func TestOverviewNumbersSparkAndTopLists(t *testing.T) {
	m := viewsModel(t, 150, 34)
	lines := m.overviewLines()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = ansi.Strip(l)
	}
	if plain[0] != "This week" || plain[1] != "overdue 1  ·  today 1  ·  next 7 days 1" {
		t.Errorf("summary lines = %q", plain[:2])
	}
	// open tasks due per day from today: b today, c in 3 days; the overdue and the 20-day ones don't count
	if plain[2] != "█▁▁█▁▁▁" || plain[3] != "TWTFSSM" {
		t.Errorf("spark/initials = %q / %q (Tue Oct 6 → T W T F S S M)", plain[2], plain[3])
	}
	if plain[4] != "most overdue  Home 1" {
		t.Errorf("top lists = %q", plain[4])
	}
	m.tasks = m.tasks[2:] // nothing overdue → no 'most overdue' line
	if got := m.overviewLines(); len(got) != 4 {
		t.Errorf("without overdue tasks the overview has 4 lines, got %d", len(got))
	}
}

func TestOverviewPlacement(t *testing.T) {
	m := viewsModel(t, 150, 34)
	if got := ansi.Strip(m.detailText(60, 30)); !strings.Contains(got, "overdue home") || !strings.Contains(got, "This week") {
		t.Errorf("room to spare → task details, then the overview:\n%s", got)
	}
	if got := ansi.Strip(m.detailText(60, 6)); !strings.Contains(got, "overdue home") || strings.Contains(got, "This week") {
		t.Errorf("no room → details only, never a cut-off overview:\n%s", got)
	}
	empty := m
	empty.tasks, empty.rows = nil, nil
	if got := ansi.Strip(empty.detailText(60, 30)); !strings.HasPrefix(got, "This week") {
		t.Errorf("nothing selected → the overview alone:\n%s", got)
	}
	if out := tuitest.Text(m); !strings.Contains(out, "This week") {
		t.Errorf("a 150x34 screen has room for the overview next to the selected task:\n%s", out)
	}
}

// ── footer ────────────────────────────────────────────────────────────────────

func TestFooterListsFilterAndViewKeysButKeepsEssentials(t *testing.T) {
	m := viewsModel(t, 140, 30)
	foot := ansi.Strip(m.renderStatusBar())
	for _, want := range []string{"tab view", "f filter", "? help", "q quit"} {
		if !strings.Contains(foot, want) {
			t.Errorf("footer at 140 cols missing %q: %q", want, foot)
		}
	}
	narrow := ansi.Strip(viewsModel(t, 50, 30).renderStatusBar())
	if !strings.Contains(narrow, "? help") || !strings.Contains(narrow, "q quit") || strings.Contains(narrow, "f filter") {
		t.Errorf("narrow footer drops filter before help/quit: %q", narrow)
	}
	if lipgloss.Width(narrow) > 50 {
		t.Errorf("footer overflows: %d", lipgloss.Width(narrow))
	}
}

package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/tuitest"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/charmbracelet/x/ansi"
)

func fixedNow(t *testing.T, now time.Time) {
	t.Helper()
	orig := nowFn
	nowFn = func() time.Time { return now }
	t.Cleanup(func() { nowFn = orig })
}

// manyTasks builds n tasks "task 00".. across two lists, enough to scroll.
func manyTasks(n int) []models.Task {
	var ts []models.Task
	for i := 0; i < n; i++ {
		list := "Home"
		if i >= n/2 {
			list = "Work"
		}
		ts = append(ts, models.Task{ID: fmt.Sprintf("t%02d", i), Title: fmt.Sprintf("task %02d", i), List: list, Status: "needsAction"})
	}
	return ts
}

func sized(m Model, w, h int) Model {
	m, _ = send(m, tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// The mouse must land on the row that is actually drawn there: for every task
// row on screen, the rendered line index (minus the app padding) has to map
// back to that row via rowHitTest — in the plain, wide, search and palette
// layouts, and after the list has scrolled.
func TestHitTestMatchesRenderedRows(t *testing.T) {
	cases := []struct {
		name string
		prep func(Model) Model
		w, h int
	}{
		{"plain", func(m Model) Model { return m }, 100, 30},
		{"wide", func(m Model) Model { return m }, 140, 30},
		{"search open", func(m Model) Model { m, _ = send(m, key("/")); return m }, 100, 30},
		{"palette open", func(m Model) Model { m, _ = send(m, key(":")); return m }, 100, 30},
		{"filter line", func(m Model) Model { m, _ = send(m, key("t")); return m }, 100, 30},
		{"scrolled", func(m Model) Model {
			for i := 0; i < 25; i++ {
				m, _ = send(m, key("j"))
			}
			return m
		}, 100, 14},
		{"scrolled wide", func(m Model) Model {
			for i := 0; i < 25; i++ {
				m, _ = send(m, key("j"))
			}
			return m
		}, 140, 14},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			m := newModel("")
			m, _ = send(m, tea.WindowSizeMsg{Width: c.w, Height: c.h}, tasksLoadedMsg{tasks: manyTasks(30)})
			m = c.prep(m)
			if c.name == "filter line" {
				// focus filter hides undated tasks; give two tasks due dates so rows exist
				for i := range m.tasks[:2] {
					m.tasks[i].DueDate = day(0)
				}
				m, _ = send(m, tasksLoadedMsg{tasks: m.tasks})
			}
			lines := strings.Split(tuitest.Text(m), "\n")
			checked := 0
			for y, line := range lines {
				// only the list area: the wide Details panel repeats the title
				line = ansi.Truncate(line, m.listWidth()+appPadH, "")
				for i, r := range m.rows {
					if r.isHeader || !strings.Contains(line, r.task.Title+" ") && !strings.HasSuffix(strings.TrimRight(line, " │"), r.task.Title) {
						continue
					}
					checked++
					if got := m.rowHitTest(y - appPadV); got != i {
						t.Errorf("screen line %d shows %q (row %d) but rowHitTest(%d) = %d", y, r.task.Title, i, y-appPadV, got)
					}
				}
			}
			if checked < 2 {
				t.Fatalf("only %d task rows found on screen:\n%s", checked, strings.Join(lines, "\n"))
			}
		})
	}
}

func TestViewIsConstantHeightAndNeverWider(t *testing.T) {
	for _, size := range [][2]int{{40, 14}, {60, 15}, {80, 24}, {100, 30}, {119, 30}, {120, 30}, {140, 32}} {
		for _, name := range []string{"plain", "search", "palette", "toast", "delete-confirm"} {
			m := loaded(t)
			m = sized(m, size[0], size[1])
			switch name {
			case "search":
				m, _ = send(m, key("/"))
			case "palette":
				m, _ = send(m, key(":"))
			case "toast":
				m.flash = "Copied to clipboard"
			case "delete-confirm":
				m, _ = send(m, key("d"))
			}
			lines := strings.Split(tuitest.Text(m), "\n")
			// WindowSizeMsg keeps one row of slack (height-1, see Update), so the
			// frame is always exactly terminal height - 1, whatever is open.
			if len(lines) != size[1]-1 {
				t.Errorf("%dx%d %s: %d lines, want exactly %d", size[0], size[1], name, len(lines), size[1]-1)
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > size[0] {
					t.Errorf("%dx%d %s: line %d is %d cells wide: %q", size[0], size[1], name, i, w, l)
				}
			}
		}
	}
}

func TestHeaderShowsCountsAndDateAndFitsNarrow(t *testing.T) {
	now := time.Date(2026, 10, 6, 10, 0, 0, 0, time.Local)
	fixedNow(t, now)
	m := loaded(t)
	done := now.Add(-time.Hour)
	other := now.AddDate(0, 0, -3)
	m.tasks = append(m.tasks,
		models.Task{ID: "x", Title: "done today", List: "Home", Status: "completed", CompletedAt: &done},
		models.Task{ID: "y", Title: "done earlier", List: "Home", Status: "completed", CompletedAt: &other})
	if got := m.summary(); got != "4 open · 1 done today" {
		t.Errorf("summary = %q", got)
	}
	m = sized(m, 100, 30)
	hdr := ansi.Strip(m.renderHeader(m.summary()))
	if !strings.Contains(hdr, "taskctl") || !strings.Contains(hdr, "4 open") || !strings.HasSuffix(strings.TrimSpace(hdr), "Tue 06 Oct") {
		t.Errorf("header = %q", hdr)
	}
	for _, w := range []int{40, 60, 80, 100, 140} {
		n := sized(m, w, 30)
		if lw := lipgloss.Width(n.renderHeader(n.summary())); lw > w {
			t.Errorf("width %d: header is %d cells wide", w, lw)
		}
	}
}

func TestDuePillStates(t *testing.T) {
	now := time.Date(2026, 10, 6, 15, 0, 0, 0, time.Local)
	at := func(days int, h int) time.Time { return time.Date(2026, 10, 6+days, h, 0, 0, 0, time.Local) }
	for want, due := range map[string]time.Time{
		" overdue  Oct 04": at(-2, 9),
		" overdue  Oct 05": at(-1, 23),
		" today ":          at(0, 8),
		"tomorrow":         at(1, 9),
		"in 3d":            at(3, 9),
		"Oct 20":           at(14, 9),
		"2027-01-02":       time.Date(2027, 1, 2, 9, 0, 0, 0, time.Local),
	} {
		if got := ansi.Strip(duePill(due, now)); got != want {
			t.Errorf("duePill(%v) = %q, want %q", due.Format("01-02 15h"), got, want)
		}
	}
	// only overdue and today carry a filled pill (background); later dates are plain dimmed text
	for _, d := range []time.Time{at(1, 9), at(3, 9), at(14, 9)} {
		if strings.Contains(duePill(d, now), "\x1b[48;") {
			t.Errorf("a later date must not be a pill: %q", duePill(d, now))
		}
	}
	// the date after "overdue" is NOT inside the pill, so consecutive overdue rows don't form a solid block
	if p := duePill(at(-2, 9), now); strings.Count(p, "\x1b[m")+strings.Count(p, "\x1b[0m") < 2 || !strings.Contains(ansi.Strip(p), "Oct 04") {
		t.Errorf("overdue date should be a separate dimmed segment: %q", p)
	}
	// no due date → no meta at all
	task := &models.Task{ID: "n", Title: "no due"}
	if got := ansi.Strip(taskMeta(task, now, 0)); got != "" {
		t.Errorf("task without due/subtasks/repeat has no meta, got %q", got)
	}
	d := at(-2, 9)
	task = &models.Task{ID: "o", Title: "late", DueDate: &d, Recurrence: "daily", Subtasks: []models.Subtask{{Done: true}, {}}}
	got := ansi.Strip(taskMeta(task, now, 0))
	for _, want := range []string{"overdue", "Oct 04", "▰▰▱▱ 1/2", "↻ daily"} {
		if !strings.Contains(got, want) {
			t.Errorf("meta %q missing %q", got, want)
		}
	}
	// markers come first, the due label last (so the due column stays at the right edge)
	if !(strings.Index(got, "↻ daily") < strings.Index(got, "overdue")) {
		t.Errorf("markers must precede the due label: %q", got)
	}
}

func TestPriorityDotAndSubtaskBar(t *testing.T) {
	if ansi.Strip(priorityDot(0)) != "  " || ansi.Strip(priorityDot(3)) != "  " {
		t.Error("no priority = two blanks (titles stay aligned)")
	}
	seen := map[string]bool{}
	for _, p := range []int{1, 5, 9} {
		d := priorityDot(p)
		if ansi.Strip(d) != "● " {
			t.Errorf("priority %d dot = %q", p, ansi.Strip(d))
		}
		seen[d] = true
	}
	if len(seen) != 3 {
		t.Error("high / medium / low must be three different colors")
	}
	for done, want := range map[int]string{0: "▱▱▱▱ 0/3", 1: "▰▱▱▱ 1/3", 2: "▰▰▰▱ 2/3", 3: "▰▰▰▰ 3/3"} {
		subs := []models.Subtask{{Done: done > 0}, {Done: done > 1}, {Done: done > 2}}
		if got := ansi.Strip(subtaskProgress(subs)); got != want {
			t.Errorf("%d/3 done: %q, want %q", done, got, want)
		}
	}
}

func TestListRowsShowPillsDotsAndGroupCounts(t *testing.T) {
	m := loaded(t)
	m.tasks[0].Priority = 1
	m, _ = send(m, tasksLoadedMsg{tasks: m.tasks})
	m = sized(m, 100, 30)
	out := tuitest.Text(m)
	for _, want := range []string{"Home", "Work", " 2 ", "overdue", "today", "● buy milk", "  write report"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}
	// group headers take one line (a divider), not the old label+rule pair
	if strings.Count(out, "── Home") != 1 {
		t.Errorf("one divider line per group:\n%s", out)
	}
}

func TestSelectedRowKeepsAccentBar(t *testing.T) {
	m := sized(loaded(t), 100, 30)
	out := tuitest.Text(m)
	var bars int
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "▌") {
			bars++
			if !strings.Contains(l, "buy milk") {
				t.Errorf("accent bar on the wrong row: %q", l)
			}
		}
	}
	if bars != 1 {
		t.Errorf("exactly one selected row carries ▌, got %d:\n%s", bars, out)
	}
}

func TestOneLineFooterWithSyncStatus(t *testing.T) {
	m := sized(loaded(t), 100, 30)
	m.lastSynced = time.Now().Add(-5 * time.Minute)
	footer := ansi.Strip(m.renderStatusBar())
	if strings.Contains(footer, "\n") {
		t.Errorf("footer must be one line: %q", footer)
	}
	if !strings.Contains(footer, "synced 5m ago") || !strings.HasSuffix(strings.TrimRight(footer, " "), "synced 5m ago") {
		t.Errorf("sync status sits right: %q", footer)
	}
	if !strings.Contains(footer, "? help") || !strings.Contains(footer, "q quit") {
		t.Errorf("? and q are among the last hints to drop: %q", footer)
	}
	narrow := ansi.Strip(sized(m, 44, 30).renderStatusBar())
	if !strings.Contains(narrow, "? help") || strings.Contains(narrow, "p pomo") {
		t.Errorf("narrow footer keeps essentials, drops the rest: %q", narrow)
	}
}

func TestPanelsOnlyFromWideMin(t *testing.T) {
	for w, wide := range map[int]bool{100: false, 119: false, 120: true, 160: true} {
		out := tuitest.Text(sized(loaded(t), w, 30))
		if got := strings.Contains(out, "╭─ Tasks") && strings.Contains(out, "Details"); got != wide {
			t.Errorf("width %d: panels = %v, want %v", w, got, wide)
		}
	}
}

func TestDetailsPanelShowsSelectedTask(t *testing.T) {
	m := loaded(t)
	m.tasks[1].Subtasks = []models.Subtask{{Title: "gift", Done: true}, {Title: "card"}}
	m.tasks[1].Priority = 5
	m, _ = send(m, tasksLoadedMsg{tasks: m.tasks})
	m = sized(m, 140, 32)
	m, _ = send(m, key("j")) // call mom
	out := tuitest.Text(m)
	for _, want := range []string{"call mom", "birthday gift", "Home", "medium", "[x] gift", "[ ] card", "1/2", "today"} {
		if !strings.Contains(out, want) {
			t.Errorf("details panel missing %q:\n%s", want, out)
		}
	}
	m, _ = send(m, key("j")) // moves to the next group's task; details follow the cursor
	if out := tuitest.Text(m); !strings.Contains(out, "write report") || strings.Contains(out, "birthday gift") {
		t.Errorf("details must follow the cursor:\n%s", out)
	}
}

func TestClickInDetailsPanelIsIgnored(t *testing.T) {
	m := sized(loaded(t), 140, 32)
	before := cursorID(m)
	y := clickY(t, m, "c")
	m, _ = send(m, tea.MouseClickMsg{Button: tea.MouseLeft, X: m.listWidth() + appPadH + 5, Y: y})
	if cursorID(m) != before {
		t.Errorf("a click over the Details panel moved the cursor to %q", cursorID(m))
	}
	m, _ = send(m, tea.MouseClickMsg{Button: tea.MouseLeft, X: 8, Y: y})
	if cursorID(m) != "c" {
		t.Errorf("a click over the list selects: cursor %q", cursorID(m))
	}
	m, _ = send(m, tea.MouseMotionMsg{X: m.listWidth() + appPadH + 5, Y: y})
	if m.hoverRow != -1 {
		t.Errorf("no hover over the Details panel, got %d", m.hoverRow)
	}
}

func TestEmptyStateInBothLayouts(t *testing.T) {
	for _, w := range []int{100, 140} {
		t.Setenv("HOME", t.TempDir())
		m := newModel("")
		m, _ = send(m, tea.WindowSizeMsg{Width: w, Height: 30}, tasksLoadedMsg{})
		out := tuitest.Text(m)
		if !strings.Contains(out, "No tasks yet") || !strings.Contains(out, "0 open") {
			t.Errorf("width %d empty state / header:\n%s", w, out)
		}
		if w >= wideMin && !strings.Contains(out, "No task selected") {
			t.Errorf("wide empty: the Details panel says so:\n%s", out)
		}
	}
}

func TestToastLineReplacesNothingBelowAndKeepsHeight(t *testing.T) {
	m := sized(loaded(t), 100, 30)
	plain := m.listHeight()
	m.flash = "Saved"
	if m.listHeight() != plain-1 {
		t.Errorf("a toast takes one line from the list: %d → %d", plain, m.listHeight())
	}
	m.err = fmt.Errorf("boom")
	if out := tuitest.Text(m); !strings.Contains(out, "boom") || strings.Contains(out, "Saved") {
		t.Errorf("error wins over flash:\n%s", out)
	}
	m.err, m.flash = nil, ""
	d := models.Task{Title: "gone"}
	m.lastDeleted = &d
	if out := tuitest.Text(m); !strings.Contains(out, `Deleted "gone" — press u to undo`) {
		t.Errorf("undo hint:\n%s", out)
	}
}

func plainLines(m Model) []string {
	var out []string
	for _, l := range strings.Split(tuitest.Text(m), "\n") {
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

// Groups get a blank line above their header — except the very first one.
func TestBlankLineBetweenGroupsButNotBeforeTheFirst(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newModel("")
	m, _ = send(m, tea.WindowSizeMsg{Width: 100, Height: 40}, tasksLoadedMsg{tasks: manyTasks(6)})
	lines := plainLines(m)
	var headers []int
	for i, l := range lines {
		if strings.Contains(l, "── ") && (strings.Contains(l, "Home") || strings.Contains(l, "Work")) {
			headers = append(headers, i)
		}
	}
	if len(headers) != 2 {
		t.Fatalf("want 2 group headers, got %v:\n%s", headers, strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[headers[0]-1]) == "" {
		t.Errorf("the first group header must not be preceded by a blank line (it follows the divider):\n%s", strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[headers[1]-1]) != "" {
		t.Errorf("the second group header needs a blank line above it:\n%s", strings.Join(lines, "\n"))
	}
}

// The mouse lands on the right task on both sides of a group gap, and a
// header or the gap itself is not a task.
func TestClickMappingAcrossGroupGaps(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newModel("")
	m, _ = send(m, tea.WindowSizeMsg{Width: 100, Height: 40}, tasksLoadedMsg{tasks: manyTasks(6)})
	lines := plainLines(m)
	for i, r := range m.rows {
		if r.isHeader {
			continue
		}
		y := -1
		for ly, l := range lines {
			if strings.Contains(l, r.task.Title) {
				y = ly
			}
		}
		if y < 0 {
			t.Fatalf("%q not on screen", r.task.Title)
		}
		if got := m.rowHitTest(y - appPadV); got != i {
			t.Errorf("click on %q (line %d) hit row %d, want %d", r.task.Title, y, got, i)
		}
	}
	// the blank gap line and both headers resolve to no task
	for ly, l := range lines {
		isGap := strings.TrimSpace(l) == "" && ly > 3 && ly+1 < len(lines) && strings.Contains(lines[ly+1], "Work")
		isHeader := strings.Contains(l, "── ") && (strings.Contains(l, "Home") || strings.Contains(l, "Work"))
		if (isGap || isHeader) && m.rowHitTest(ly-appPadV) != -1 {
			t.Errorf("line %d (%q) must not select a task", ly, l)
		}
	}
}

// A scrolled list with several groups never draws more lines than it has
// room for, keeps the cursor row on screen, and the clicks still match.
func TestScrollWindowWithGapsFitsAndKeepsCursorVisible(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var ts []models.Task
	for g := 0; g < 6; g++ {
		for i := 0; i < 4; i++ {
			ts = append(ts, models.Task{ID: fmt.Sprintf("g%d-%d", g, i), Title: fmt.Sprintf("item-%d-%d", g, i), List: fmt.Sprintf("List%d", g), Status: "needsAction"})
		}
	}
	for _, h := range []int{9, 12, 16} {
		m := newModel("")
		m, _ = send(m, tea.WindowSizeMsg{Width: 100, Height: h + 4}, tasksLoadedMsg{tasks: ts})
		for step := 0; step < 40; step++ {
			m, _ = send(m, key("j"))
			room := m.listHeight()
			visible, start := m.visibleRowsWithStart(room)
			lines := 0
			for j := range visible {
				lines += m.rowLines(start + j)
			}
			if lines > room {
				t.Fatalf("h=%d step %d: window needs %d lines, room %d", h, step, lines, room)
			}
			if m.cursor < start || m.cursor >= start+len(visible) {
				t.Fatalf("h=%d step %d: cursor %d outside window [%d,%d)", h, step, m.cursor, start, start+len(visible))
			}
			if got := len(m.listLines(100, room)); got > room {
				t.Fatalf("h=%d step %d: rendered %d lines for room %d", h, step, got, room)
			}
			if r := m.rows[m.cursor]; !r.isHeader {
				screen := plainLines(m)
				y := -1
				for ly, l := range screen {
					if strings.Contains(l, r.task.Title) {
						y = ly
					}
				}
				if y < 0 || m.rowHitTest(y-appPadV) != m.cursor {
					t.Fatalf("h=%d step %d: cursor task %q: line %d, hit %d", h, step, r.task.Title, y, m.rowHitTest(y-appPadV))
				}
			}
		}
	}
}

// The due labels share one right-aligned column: every dated row ends at the
// same screen column, and undated rows don't push anything around.
func TestDueColumnIsRightAlignedAndStraight(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 10, 6, 15, 0, 0, 0, time.Local)
	fixedNow(t, now)
	dd := func(days int) *time.Time { d := time.Date(2026, 10, 6+days, 9, 0, 0, 0, time.Local); return &d }
	ts := []models.Task{
		{ID: "a", Title: "alpha", List: "Home", Status: "needsAction", DueDate: dd(-3)},
		{ID: "b", Title: "beta", List: "Home", Status: "needsAction", DueDate: dd(-2)},
		{ID: "c", Title: "gamma", List: "Home", Status: "needsAction", DueDate: dd(0)},
		{ID: "d", Title: "delta", List: "Home", Status: "needsAction", DueDate: dd(2)},
		{ID: "e", Title: "epsilon", List: "Home", Status: "needsAction", DueDate: dd(120)},
		{ID: "f", Title: "zeta undated", List: "Home", Status: "needsAction"},
	}
	m := newModel("")
	m, _ = send(m, tea.WindowSizeMsg{Width: 100, Height: 24}, tasksLoadedMsg{tasks: ts})
	col := m.dueColWidth()
	if col == 0 {
		t.Fatal("no due column width")
	}
	want := map[string]string{
		"alpha": ansi.Strip(duePill(*dd(-3), now)), "beta": ansi.Strip(duePill(*dd(-2), now)),
		"gamma": ansi.Strip(duePill(*dd(0), now)), "delta": ansi.Strip(duePill(*dd(2), now)),
		"epsilon": ansi.Strip(duePill(*dd(120), now)),
	}
	seen := 0
	for _, l := range strings.Split(tuitest.Text(m), "\n") {
		for name, label := range want {
			if !strings.Contains(l, name) {
				continue
			}
			seen++
			w := lipgloss.Width(l)
			cell := ansi.Cut(l, w-appPadH-col, w-appPadH) // the fixed-width cell at the right edge
			if got := strings.TrimSpace(cell); got != strings.TrimSpace(label) {
				t.Errorf("row %q: right-edge cell is %q, want the label %q:\n%s", name, cell, label, l)
			}
			if lipgloss.Width(cell) != col {
				t.Errorf("row %q: cell width %d, want the fixed column width %d", name, lipgloss.Width(cell), col)
			}
		}
	}
	if seen != len(want) {
		t.Fatalf("found %d of %d dated rows on screen", seen, len(want))
	}
	text := tuitest.Text(m)
	for _, want := range []string{"overdue", "Oct 03", "today", "in 2d"} {
		if !strings.Contains(text, want) {
			t.Errorf("screen is missing %q:\n%s", want, text)
		}
	}
}

func TestSyncAgeTurnsAmberAfterADay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 10, 6, 15, 0, 0, 0, time.Local)
	fixedNow(t, now)
	m := newModel("")
	m.lastSynced = now.Add(-2 * time.Hour)
	fresh := m.syncStatus()
	m.lastSynced = now.Add(-48 * time.Hour)
	stale := m.syncStatus()
	if !strings.Contains(ansi.Strip(stale), "synced") || !strings.Contains(ansi.Strip(fresh), "synced") {
		t.Fatalf("status lost its text: %q / %q", ansi.Strip(fresh), ansi.Strip(stale))
	}
	amber := lipgloss.NewStyle().Foreground(colorAmber).Render("x")
	amberSeq := amber[:strings.Index(amber, "x")]
	if !strings.HasPrefix(stale, amberSeq) {
		t.Errorf("older than 24h must be amber: %q", stale)
	}
	if strings.HasPrefix(fresh, amberSeq) {
		t.Errorf("a fresh sync must not be amber: %q", fresh)
	}
}

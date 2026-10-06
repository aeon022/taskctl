package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/dateutil"
	"github.com/aeon022/missionctl-core/emptystate"
	"github.com/aeon022/missionctl-core/humanize"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/missionctl-core/statusbar"
	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/missionctl-core/ui"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/charmbracelet/x/ansi"
)

// The list view: header, divider, optional filter/search/palette lines, then
// the task rows (one physical line each — group headers included), a one-line
// toast and a one-line footer. From wideMin terminal columns the rows sit in a
// "Tasks" panel next to a "Details" panel for the selected task.
//
// Every geometry question (how tall is the list, which row is at screen row y)
// is answered from the same helpers below — chromeAbove/chromeBelow/dims — so
// rendering and mouse hit-testing cannot drift apart.

// wideMin is the terminal width from which the two-panel layout is used.
const wideMin = 120

// nowFn is the clock for due dates and the header counts (a variable for tests).
var nowFn = time.Now

// dims is the size of the content area (terminal minus the app padding),
// whether m holds the raw terminal size (Update) or already the content size
// (the render copy made in viewContent).
func (m Model) dims() (w, h int) {
	if m.contentSized {
		return m.width, m.height
	}
	return m.width - 2*appPadH, m.height - 2*appPadV
}

// wide reports whether the terminal is wide enough for the two-panel layout.
func (m Model) wide() bool {
	w, _ := m.dims()
	return w+2*appPadH >= wideMin
}

func (m Model) listWidth() int {
	w, _ := m.dims()
	if m.wide() {
		return w * 3 / 5
	}
	return w
}

// inListArea reports whether content column x is over the task list (always
// true in the single-panel layout; the right panel is not clickable).
func (m Model) inListArea(x int) bool { return !m.wide() || x < m.listWidth() }

// renderHeader is the one header shared by every view: app name left, a
// middle label (the counts on the list, the section elsewhere), the date right.
func (m Model) renderHeader(mid string) string {
	w, _ := m.dims()
	return "  " + ui.Header(max(w-2, 0), styleHeader.Render("taskctl"), styleSubhead.Render(mid),
		styleSubhead.Render(nowFn().Format("Mon 02 Jan")))
}

// renderDivider draws the rule under the header, full content width.
func (m Model) renderDivider() string {
	w, _ := m.dims()
	return ui.Divider(max(w, 0), "")
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.In(a.Location()).Date()
	return ay == by && am == bm && ad == bd
}

// summary is the header's middle label: open tasks and today's completions.
func (m Model) summary() string {
	open, done, now := 0, 0, nowFn()
	for i := range m.tasks {
		t := &m.tasks[i]
		switch {
		case !t.Done():
			open++
		case t.CompletedAt != nil && sameDay(*t.CompletedAt, now):
			done++
		}
	}
	return fmt.Sprintf("%d open · %d done today", open, done)
}

// groupCounts and visibleRowsWithStart live in view.go.

// extraLine is the optional line under the divider: active filter and select
// mode. Empty (and then not drawn at all) otherwise.
func (m Model) extraLine() string {
	var s string
	switch m.filter {
	case filterFocus:
		s += "  " + styleFocusBadge.Render("focus: today & overdue")
	case filterOverdue:
		s += "  " + styleFocusBadge.Render("overdue only")
	}
	if m.selecting {
		s += "  " + theme.Selected.Render(fmt.Sprintf("select: %d", len(m.selected))) +
			styleSubhead.Render("  space toggle  A all  enter done  d delete  esc cancel")
	}
	if s == "" {
		return ""
	}
	return "  " + strings.TrimLeft(s, " ")
}

func (m Model) paletteMatches() []palette.Command {
	matches := palette.Match(paletteCommands, m.paletteInput.Value())
	if len(matches) > 6 {
		matches = matches[:6]
	}
	return matches
}

// paletteHeight is how many lines the ":" palette block draws: input, its
// matches (or the "no matching command" line) and a trailing blank.
func (m Model) paletteHeight() int {
	if !m.inPalette {
		return 0
	}
	return 1 + max(1, len(m.paletteMatches())) + 1
}

// toastLine is the single transient message line above the footer, in
// priority order: error, confirmation, undo hint.
func (m Model) toastLine() string {
	w, _ := m.dims()
	var s string
	switch {
	case m.err != nil:
		s = ui.Toast(ui.Err, m.err.Error())
	case m.flash != "":
		s = ui.Toast(ui.OK, m.flash)
	case m.lastDeleted != nil:
		s = ui.Toast(ui.Info, fmt.Sprintf("Deleted %q — press u to undo", m.lastDeleted.Title))
	default:
		return ""
	}
	return ansi.Truncate("  "+s, max(w, 0), "…")
}

// chromeAbove is the number of lines above the body: header + divider, the
// optional filter line, the 2-line search bar, the palette block.
func (m Model) chromeAbove() int {
	n := 2
	if m.extraLine() != "" {
		n++
	}
	if m.searching {
		n += 2
	}
	return n + m.paletteHeight()
}

// chromeBelow is the toast line (when there is one) plus the one-line footer.
func (m Model) chromeBelow() int {
	if m.toastLine() != "" {
		return 2
	}
	return 1
}

func (m Model) bodyHeight() int {
	_, h := m.dims()
	return max(h-m.chromeAbove()-m.chromeBelow(), 1)
}

// listHeight is the number of task rows that fit: the body, minus the panel
// borders in the wide layout.
func (m Model) listHeight() int {
	h := m.bodyHeight()
	if m.wide() {
		h -= 2
	}
	return max(h, 1)
}

// rowHitTest returns the m.rows index at content row y (screen row minus the
// app padding), or -1 when the click missed (header, blank, outside the list).
// Rows and group headers take exactly one line each, so this walks the same
// scroll window the renderer draws.
func (m Model) rowHitTest(y int) int {
	row := m.chromeAbove()
	if m.wide() {
		row++ // the panel's top border
	}
	visible, start := m.visibleRowsWithStart(m.listHeight())
	for localI, r := range visible {
		if r.isHeader && localI > 0 {
			row++ // the blank line between groups
		}
		if y == row {
			if r.isHeader {
				return -1
			}
			return start + localI
		}
		row++
	}
	return -1
}

// ── Rows ──────────────────────────────────────────────────────────────────────

// duePill is the due-date badge. Only what needs attention is a pill: "overdue"
// (red, the date after it dimmed so consecutive overdue rows don't form one
// solid block) and "today" (amber). Anything later — tomorrow, in 3d, Oct 20,
// 2027-01-02 — is plain dimmed text.
func duePill(due, now time.Time) string {
	switch {
	case due.Before(dateutil.StartOfDay(now)):
		return ui.Pill("overdue", ui.Err) + " " + styleSubhead.Render(due.Format("Jan 02"))
	case !due.After(dateutil.EndOfDay(now)):
		return ui.Pill("today", ui.Warn)
	}
	return styleSubhead.Render(ui.RelTime(due, now))
}

// dueColWidth is the width of the right-aligned due-date column: the widest
// label among the current rows, so the column's left edge is straight. 0 when
// no row has a due date.
func (m Model) dueColWidth() int {
	now, w := nowFn(), 0
	for _, r := range m.rows {
		if !r.isHeader && r.task != nil && r.task.DueDate != nil {
			w = max(w, lipgloss.Width(duePill(*r.task.DueDate, now)))
		}
	}
	return w
}

// priorityDot is the colored dot for high/medium/low (two spaces for none, so
// titles stay aligned).
func priorityDot(p int) string {
	switch p {
	case 1:
		return ui.Dot(ui.Err) + " "
	case 5:
		return ui.Dot(ui.Warn) + " "
	case 9:
		return ui.Dot(ui.Muted) + " "
	}
	return "  "
}

// subtaskProgress is a 4-cell ▰▱ bar plus "done/total", dimmed.
func subtaskProgress(subs []models.Subtask) string {
	done := 0
	for _, s := range subs {
		if s.Done {
			done++
		}
	}
	filled := (done*4 + len(subs)/2) / len(subs)
	bar := lipgloss.NewStyle().Foreground(colorGreen).Render(strings.Repeat("▰", filled)) +
		lipgloss.NewStyle().Foreground(colorSubtle).Render(strings.Repeat("▱", 4-filled))
	return bar + " " + styleSubhead.Render(fmt.Sprintf("%d/%d", done, len(subs)))
}

// taskMeta is the right-hand side of a row. With dueCol > 0 the due label is
// right-aligned in a fixed-width column at the very end (markers before it);
// with 0 it follows the markers at its natural width.
func taskMeta(t *models.Task, now time.Time, dueCol int) string {
	var parts []string
	if len(t.Subtasks) > 0 {
		parts = append(parts, subtaskProgress(t.Subtasks))
	}
	if t.Recurrence != "" {
		parts = append(parts, styleRecur.Render("↻ "+t.Recurrence))
	}
	if effectiveURL(t) != "" {
		parts = append(parts, styleRecur.Render("↗"))
	}
	due := ""
	if t.DueDate != nil {
		due = duePill(*t.DueDate, now)
	}
	if dueCol > 0 {
		due = strings.Repeat(" ", max(dueCol-lipgloss.Width(due), 0)) + due
	}
	if due != "" {
		parts = append(parts, due)
	}
	return strings.Join(parts, " ")
}

// groupHeader is the one-line list divider: "── ● Home ② ───".
func groupHeader(width int, label string, count int, color string) string {
	bullet := ""
	if color != "" {
		bullet = lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("● ")
	}
	return ui.Divider(width, bullet+label+" "+ui.Pill(strconv.Itoa(count), ui.Muted))
}

// taskRow renders one task row of the given width: mark, priority dot, title
// on the left; due pill, subtask progress, repeat and link markers on the right.
func (m Model) taskRow(t *models.Task, selected, hovered bool, query, accent string, width, dueCol int) string {
	var mark string
	switch {
	case m.selecting && m.selected[t.ID]:
		mark = styleSelected.Render("[x]")
	case m.selecting:
		mark = styleSubhead.Render("[ ]")
	case t.Done():
		mark = "✓"
	default:
		mark = "○"
	}
	var title string
	switch {
	case t.Done() && !m.selecting:
		title = styleDone.Render(t.Title)
	case selected:
		title = t.Title // ui.Row restyles the whole selected row
	default:
		title = highlightMatches(t.Title, fuzzyMatchIndexes(query, t.Title), styleTitle)
	}
	left := mark + " " + priorityDot(t.Priority) + title
	text := left
	if right := taskMeta(t, nowFn(), dueCol); right != "" {
		avail := max(width-2-lipgloss.Width(right)-1, 1)
		left = ansi.Truncate(left, avail, "…")
		text = left + strings.Repeat(" ", max(avail-lipgloss.Width(left), 0)) + " " + right
	}
	row := ui.Row(width, selected, text)
	switch {
	case selected && accent != "":
		// keep the list-color accent bar the cursor row always had
		blue := lipgloss.NewStyle().Foreground(theme.BlueV2).Render("▌")
		row = strings.Replace(row, blue, lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Render("▌"), 1)
	case hovered && !selected:
		row = theme.HoverV2.Render(ansi.Strip(row))
	}
	return row
}

// listLines draws the task rows into exactly height lines of width cells.
func (m Model) listLines(width, height int) []string {
	if len(m.rows) == 0 {
		var title, hint string
		switch {
		case m.searchQuery() != "":
			title = "No tasks match your search"
		case m.filter == filterFocus:
			title, hint = "No tasks due today or overdue", "press t to show all tasks"
		case m.filter == filterOverdue:
			title, hint = "No overdue tasks", "press O to show all tasks"
		case len(m.tasks) == 0:
			title, hint = "No tasks yet", "press n to add one, or s to sync with Apple Reminders"
		default:
			title = "No tasks found"
		}
		return strings.Split(emptystate.Render(width, max(height, 1), "", title, hint), "\n")
	}

	query := m.searchQuery()
	counts := m.groupCounts()
	colors := make(map[string]string, len(m.listEntries))
	for _, e := range m.listEntries {
		if e.Color != "" {
			colors[e.Name] = e.Color
		}
	}

	dueCol := m.dueColWidth()
	if dueCol > width/3 {
		dueCol = 0 // too narrow for a column: labels follow the markers instead
	}
	var lines []string
	visible, start := m.visibleRowsWithStart(height)
	for localI, r := range visible {
		i := start + localI
		if r.isHeader {
			if localI > 0 {
				lines = append(lines, "") // breathing room between groups
			}
			lines = append(lines, groupHeader(width, r.label, counts[r.label], colors[r.label]))
			continue
		}
		lines = append(lines, m.taskRow(r.task, i == m.cursor, i == m.hoverRow, query, colors[r.task.List], width, dueCol))
	}
	return lines
}

// detailText is the right-hand panel: every field of the selected task.
func (m Model) detailText(width int) string {
	t := cursorTask(m)
	if t == nil {
		return styleSubhead.Render("No task selected")
	}
	label := func(s string) string { return styleSubhead.Render(fmt.Sprintf("%-9s", s)) }
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(t.Title) + "\n\n")
	b.WriteString(label("List") + t.List + "\n")
	status := "open"
	if t.Done() {
		status = "done"
	}
	b.WriteString(label("Status") + status + "\n")
	if t.DueDate != nil {
		b.WriteString(label("Due") + duePill(*t.DueDate, nowFn()) + " " + styleSubhead.Render(t.DueDate.Format("Mon, Jan 02 2006 15:04")) + "\n")
	}
	switch t.Priority {
	case 1, 5, 9:
		word := map[int]string{1: "high", 5: "medium", 9: "low"}[t.Priority]
		b.WriteString(label("Priority") + priorityDot(t.Priority) + word + "\n")
	}
	if t.Recurrence != "" {
		b.WriteString(label("Repeat") + t.Recurrence + "\n")
	}
	if url := effectiveURL(t); url != "" {
		b.WriteString(label("URL") + styleDue.Render(url) + "\n")
	}
	if n := len(t.Subtasks); n > 0 {
		done := 0
		for _, s := range t.Subtasks {
			if s.Done {
				done++
			}
		}
		b.WriteString("\n" + label("Subtasks") + ui.Bar(max(min(width-16, 20), 4), float64(done)/float64(n), false) +
			styleSubhead.Render(fmt.Sprintf(" %d/%d", done, n)) + "\n")
		for _, s := range t.Subtasks {
			box, text := styleSubhead.Render("[ ]"), s.Title
			if s.Done {
				box, text = styleSelected.Render("[x]"), styleSubhead.Render(s.Title)
			}
			b.WriteString("  " + box + " " + text + "\n")
		}
	}
	if t.Notes != "" {
		b.WriteString("\n" + label("Notes") + "\n" + lipgloss.NewStyle().Width(max(width, 1)).Render(t.Notes) + "\n")
	}
	return b.String()
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) renderList() string {
	w, h := m.dims()

	var top []string
	top = append(top, m.renderHeader(m.summary()), m.renderDivider())
	if x := m.extraLine(); x != "" {
		top = append(top, x)
	}
	// The inputs are 40 cells wide by default; shrink them (and drop the hint)
	// so none of these lines can outgrow a narrow terminal.
	fit := func(line string) string { return ansi.Truncate(line, max(w, 0), "…") }
	inputW := max(min(40, w-8), 6)
	if m.searching {
		si := m.searchInput
		si.SetWidth(inputW)
		line := "  " + styleKey.Render("/") + " " + si.View()
		if w >= 70 {
			line += "  (enter/esc to close)"
		}
		top = append(top, fit(line), "")
	}
	if m.inPalette {
		pi := m.paletteInput
		pi.SetWidth(inputW)
		top = append(top, fit("  "+styleKey.Render(":")+" "+pi.View()))
		matches := m.paletteMatches()
		if len(matches) == 0 {
			top = append(top, fit("    "+styleSubhead.Render("no matching command")))
		}
		for i, c := range matches {
			row := fmt.Sprintf("%-11s %s", c.Name, c.Desc)
			if i == m.paletteCursor {
				top = append(top, fit("    "+styleSelected.Render("▶ "+row)))
			} else {
				top = append(top, fit("      "+styleSubhead.Render(row)))
			}
		}
		top = append(top, "")
	}

	var body string
	if m.wide() {
		lw, bh := m.listWidth(), m.bodyHeight()
		left := ui.Panel(lw, bh, "Tasks", strings.Join(m.listLines(lw-4, bh-2), "\n"), true)
		right := ui.Panel(w-lw, bh, "Details", m.detailText(w-lw-4), false)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	} else {
		body = strings.Join(m.listLines(w, m.listHeight()), "\n")
	}

	var bottom []string
	if t := m.toastLine(); t != "" {
		bottom = append(bottom, t)
	}
	bottom = append(bottom, m.renderStatusBar())
	return ui.Frame(h, strings.Join(top, "\n"), body, strings.Join(bottom, "\n"))
}

// syncStatus is the footer's right-hand side.
func (m Model) syncStatus() string {
	switch {
	case m.syncing:
		return m.sp.View() + styleSubhead.Render(" syncing…")
	case !m.lastSynced.IsZero():
		st := styleSubhead
		if nowFn().Sub(m.lastSynced) > 24*time.Hour {
			st = lipgloss.NewStyle().Foreground(theme.AmberV2) // stale data should stand out
		}
		return st.Render("synced " + humanize.TimeAgo(m.lastSynced))
	}
	return ""
}

// renderStatusBar is the one-line footer: key hints in priority order (the
// last ones drop first when the terminal is narrow, `?` and `q` last of all
// the essentials) with the sync status on the right.
func (m Model) renderStatusBar() string {
	w, _ := m.dims()
	inner := max(w-2, 0) // 2-column indent
	if m.deleteTarget != nil {
		return ansi.Truncate(fmt.Sprintf("  Delete %q?  %sconfirm  any cancel", m.deleteTarget.Title, styleKey.Render("y")+":"), max(w, 0), "…")
	}
	doneLabel := "show done"
	if m.showDone {
		doneLabel = "hide done"
	}
	pairs := [][2]string{
		{"↑/↓", "nav"}, {"space", "done"}, {"?", "help"}, {"q", "quit"},
		{"n", "new"}, {"enter", "details"}, {"/", "search"}, {"e", "edit"},
		{"d", "delete"}, {"u", "undo"}, {"p", "pomo"}, {"v", "select"},
		{"t", "focus"}, {"s", "sync"}, {"i", "stats"}, {"c", doneLabel},
		{"o", "open url"}, {"S", "postpone"},
	}
	// The sync status only gets a place when the four essential hints still
	// fit next to it; on a narrow terminal the hints win.
	right := m.syncStatus()
	if right != "" && inner-lipgloss.Width(right)-2 < lipgloss.Width(statusbar.Hints(0, pairs[:4]...)) {
		right = ""
	}
	room := inner
	if right != "" {
		room = inner - lipgloss.Width(right) - 2
	}
	hints := statusbar.Hints(room, pairs...)
	return "  " + statusbar.Line(inner, hints, right)
}

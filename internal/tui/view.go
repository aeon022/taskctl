package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/emptystate"
	"github.com/aeon022/missionctl-core/keymap"
	"github.com/aeon022/missionctl-core/overlay"
)

func (m Model) View() tea.View {
	v := tea.NewView(m.viewContent())
	// v1's tea.WithAltScreen()/WithMouseAllMotion() Program options are gone
	// in v2 — they are per-View fields now.
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	v.ReportFocus = true // FocusMsg → reload stale tasks when the window regains focus
	return v
}

func (m Model) viewContent() string {
	if m.loading {
		return emptystate.Loading(m.width, m.height, m.sp.View(), "Loading tasks…")
	}
	m.width -= appPadH * 2
	m.height -= appPadV * 2
	m.contentSized = true

	var content string
	switch m.view {
	case viewCreate:
		content = m.renderForm()
	case viewPomodoro:
		content = overlay.Center(m.renderList(), m.renderPomodoro(), m.width, m.height, 0)
	case viewStats:
		content = m.renderStats()
	case viewHelp:
		// "?" is only reachable from the main list, so the list is always
		// the correct background to keep visible behind the popup. No
		// enclosing border on the list view, so inset 0 is safe.
		content = overlay.CenterDim(m.renderList(), m.renderHelpPopup(), m.width, m.height, 0)
	case viewDetail:
		content = overlay.Center(m.renderList(), m.renderDetailPopup(), m.width, m.height, 0)
	default:
		content = m.renderList()
	}
	return lipgloss.NewStyle().Padding(appPadV, appPadH).Render(content)
}

// ── Render ────────────────────────────────────────────────────────────────────

// groupCounts tallies how many task rows fall under each list-section
// header in m.rows, for the "(n)" badge next to each list name.
func (m Model) groupCounts() map[string]int {
	counts := make(map[string]int)
	label := ""
	for _, r := range m.rows {
		if r.isHeader {
			label = r.label
			continue
		}
		counts[label]++
	}
	return counts
}

// rowLines is how many screen lines row i can take: group headers after the
// first row come with a blank line above them.
func (m Model) rowLines(i int) int {
	if i > 0 && m.rows[i].isHeader {
		return 2
	}
	return 1
}

// visibleRowsWithStart returns the scroll-windowed slice of m.rows that keeps
// m.cursor in view within height screen lines, plus its start index into
// m.rows. The window grows around the cursor (below first, then above) so the
// cursor sits roughly mid-screen; headers are budgeted at two lines (the gap),
// so the real height never exceeds the budget (a header that ends up first in
// the window skips its gap, leaving at most one spare line).
// renderList, rowHitTest and the 1-9 jump all use this one window.
func (m Model) visibleRowsWithStart(height int) ([]row, int) {
	n := len(m.rows)
	if n == 0 {
		return nil, 0
	}
	height = max(height, 1)
	total := 0
	for i := range m.rows {
		total += m.rowLines(i)
	}
	if total <= height {
		return m.rows, 0
	}
	cur := min(max(m.cursor, 0), n-1)
	start, end, used := cur, cur+1, m.rowLines(cur)
	for {
		grew := false
		if end < n && used+m.rowLines(end) <= height {
			used += m.rowLines(end)
			end++
			grew = true
		}
		if start > 0 && used+m.rowLines(start-1) <= height {
			start--
			used += m.rowLines(start)
			grew = true
		}
		if !grew {
			break
		}
	}
	return m.rows[start:end], start
}

func (m Model) helpContent() string {
	return keymap.New("taskctl", "tasks from the terminal").
		Section("Navigation").
		Row("j / ↓", "move down").
		Row("k / ↑", "move up").
		Row("1-9", "jump to the nth visible task").
		Row("/", "search tasks (esc clears)").
		Row(":", "command palette — type an action by name").
		Row("t", "focus mode — today & overdue only").
		Row("O", "filter — overdue only").
		Row("c", "Done view — show / hide completed tasks").
		Row("tab / ] / [", "next / previous view: All · Today · Overdue · Next 7 days · No date · Done").
		Row("f", "filter by list (l) or priority (p); x clears everything").
		Row("x / esc", "clear view and filters").
		Row("h / ←", "Lists sidebar (≥ 140 columns): j/k move, enter filter, esc back").
		Section("Tasks").
		Row("space", "toggle done").
		Row("enter", "task details (a subtask, space toggle, x remove)").
		Row("n", "new task").
		Row("e", "edit task").
		Row("d", "delete task (asks to confirm)").
		Row("o", "open task URL in browser").
		Row("S", "postpone to tomorrow").
		Row("y", "copy title to clipboard").
		Row("u", "undo last action").
		Section("Batch & Extras").
		Row("v", "select mode (space toggle, A all, enter done, d delete)").
		Row("p", "pomodoro timer for selected task").
		Row("i", "stats").
		Row("s", "sync with Apple Reminders").
		Section("Other").
		Row("?", "toggle this help").
		Row("q", "quit").
		String()
}

// openHelp sizes and populates the transient help popup (see
// renderHelpPopup/overlay.Center) from the ACTUAL rendered background
// height, not the terminal size.
func (m Model) openHelp() Model {
	bgLines := strings.Split(m.renderList(), "\n")

	safeH := max(6, len(bgLines))
	popH := min(safeH, 22)
	popW := min(70, m.width)
	if popW < 40 {
		popW = 40
	}

	vp := viewport.New(viewport.WithWidth(popW-6), viewport.WithHeight(popH-6)) // border 1+1, padding(1,2) → 2 rows/4 cols; -1 row for title bar, -1 for footer
	vp.SetContent(m.helpContent())

	m.helpVP = vp
	m.helpPopW = popW
	m.helpPopH = popH
	m.view = viewHelp
	return m
}

// renderHelpPopup renders the help viewport in a bordered box, meant to be
// composited over the list view via overlay.Center rather than replacing
// the whole screen — the list stays visible around it.
func (m Model) renderHelpPopup() string {
	footer := "esc / ?  close"
	if m.helpVP.TotalLineCount() > m.helpVP.Height() {
		footer = fmt.Sprintf("j/k scroll (%d%%)  ·  %s", int(m.helpVP.ScrollPercent()*100), footer)
	}
	titleBar := styleTitleBar.Width(max(0, m.helpPopW-6)).Render(" Help")
	body := titleBar + "\n" + m.helpVP.View() + "\n" + styleSubhead.Render(footer)
	return stylePopupBorder.Width(m.helpPopW).Render(body)
}

// renderDetailPopup shows every field of the task under the cursor — the
// compact list row only has room for title/due/recurrence/link, so this is
// the one place notes and the full URL are actually readable.
func (m Model) renderDetailPopup() string {
	t := m.detailTarget
	if t == nil {
		return ""
	}
	label := func(s string) string { return styleSubhead.Render(fmt.Sprintf("%-10s", s)) }

	popW := min(70, m.width)
	if popW < 40 {
		popW = 40
	}
	// inner content width: popW minus the border box's own padding+border
	innerW := max(0, popW-6)
	rule := styleSep.Render(strings.Repeat("─", innerW))

	var b strings.Builder
	b.WriteString(styleTitleBar.Width(innerW).Render(" "+t.Title) + "\n" + rule + "\n")
	b.WriteString(label("List") + t.List + "\n")
	status := "open"
	if t.Done() {
		status = "done"
	}
	b.WriteString(label("Status") + status + "\n")
	if t.DueDate != nil {
		b.WriteString(label("Due") + t.DueDate.Format("Mon, Jan 02 2006 15:04") + "\n")
	}
	if t.Recurrence != "" {
		b.WriteString(label("Repeat") + t.Recurrence + "\n")
	}
	switch t.Priority {
	case 1:
		b.WriteString(label("Priority") + "high\n")
	case 5:
		b.WriteString(label("Priority") + "medium\n")
	case 9:
		b.WriteString(label("Priority") + "low\n")
	}
	url := effectiveURL(t)
	if t.URL != "" {
		b.WriteString(label("URL") + styleDue.Render(t.URL) + "\n")
	} else if url != "" {
		b.WriteString(label("URL") + styleDue.Render(url) + styleSubhead.Render(" (from notes)") + "\n")
	}
	if len(t.Subtasks) > 0 || m.addingSubtask {
		b.WriteString(rule + "\n" + label("Subtasks") + "\n")
		for i, st := range t.Subtasks {
			box := styleSubhead.Render("[ ]")
			text := st.Title
			if st.Done {
				box = styleSelected.Render("[x]")
				text = styleSubhead.Render(st.Title)
			}
			cursor := "  "
			if i == m.subtaskCursor && !m.addingSubtask {
				cursor = styleSelected.Render("› ")
			}
			b.WriteString(cursor + box + " " + text + "\n")
		}
		if m.addingSubtask {
			b.WriteString("  " + styleKey.Render("+") + " " + m.subtaskInput.View() + "\n")
		}
	}

	if t.Notes != "" {
		b.WriteString(rule + "\n" + label("Notes") + "\n" + t.Notes + "\n")
	}

	footer := "e edit  d delete  p pomodoro"
	if url != "" {
		footer = "o open url  " + footer
	}
	if m.addingSubtask {
		footer = "enter add  esc cancel"
	} else {
		footer = "a subtask  space toggle  x remove  " + footer + "  esc/enter close"
	}
	b.WriteString("\n" + styleSubhead.Render(footer))

	return stylePopupBorder.Width(popW).Render(b.String())
}

func (m Model) renderForm() string {
	heading := "New Task"
	if m.editTarget != nil {
		heading = "Edit Task"
	}
	var inner strings.Builder
	for i, inp := range m.inputs {
		inner.WriteString(styleLabel.Render(formLabels[i]) + "  " + inp.View() + "\n")
		// show list picker below the List field when focused
		if i == fList && m.inputIdx == fList && len(m.listEntries) > 0 {
			const pickerHeight = 6
			start := m.listPickerIdx - 2
			if start < 0 {
				start = 0
			}
			end := start + pickerHeight
			if end > len(m.listEntries) {
				end = len(m.listEntries)
				start = end - pickerHeight
				if start < 0 {
					start = 0
				}
			}
			for j := start; j < end; j++ {
				e := m.listEntries[j]
				label := e.Name
				if e.Account != "" {
					label += styleSubhead.Render(" (" + e.Account + ")")
				}
				if j == m.listPickerIdx {
					inner.WriteString(strings.Repeat(" ", formLabelWidth+2) + styleKey.Render("▶ ") + styleKey.Render(e.Name) + styleSubhead.Render(func() string {
						if e.Account != "" {
							return " (" + e.Account + ")"
						}
						return ""
					}()) + "\n")
				} else {
					inner.WriteString(strings.Repeat(" ", formLabelWidth+2) + styleSubhead.Render("  "+label) + "\n")
				}
			}
		}
	}
	if m.err != nil {
		inner.WriteString("\n" + styleErr.Render(m.err.Error()))
	}
	if m.submitting {
		inner.WriteString("\n" + styleSubhead.Render("Saving…"))
	}

	key := func(k string) string { return styleKey.Render(k) }
	bodyLines := strings.Split(inner.String(), "\n")
	innerW := 0
	for _, l := range bodyLines {
		if w := lipgloss.Width(l); w > innerW {
			innerW = w
		}
	}
	titleBar := styleTitleBar.Width(innerW).Render(" " + heading)

	var b strings.Builder
	b.WriteString(m.renderHeader(heading) + "\n" + m.renderDivider() + "\n\n")
	b.WriteString(stylePopupBorder.Render(titleBar + "\n\n" + inner.String()))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("  %s next  %s next/save  %s save  %s cancel\n",
		key("tab"), key("enter"), key("ctrl+s"), key("esc")))
	return b.String()
}

func (m Model) renderPomodoro() string {
	elapsed := time.Since(m.pomStart)
	if !m.pomRunning {
		elapsed = pomodoroDuration
	}
	remaining := pomodoroDuration - elapsed
	if remaining < 0 {
		remaining = 0
	}

	mins := int(remaining.Minutes())
	secs := int(remaining.Seconds()) % 60

	title := "Pomodoro"
	if m.pomTask != nil {
		title = m.pomTask.Title
	}

	done := elapsed >= pomodoroDuration
	timerStr := fmt.Sprintf("%02d:%02d", mins, secs)
	if done {
		timerStr = "Done! 🍅"
	}

	// progress bar (40 chars wide)
	width := 40
	filled := int(float64(width) * elapsed.Seconds() / pomodoroDuration.Seconds())
	if filled > width {
		filled = width
	}
	bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"

	popW := min(56, m.width)
	if popW < 40 {
		popW = 40
	}
	titleBar := styleTitleBar.Width(max(0, popW-6)).Render(" " + title)

	var b strings.Builder
	b.WriteString(titleBar + "\n\n")
	b.WriteString(stylePomo.Render(timerStr) + "\n\n")
	b.WriteString(styleSubhead.Render(bar) + "\n\n")
	if done {
		b.WriteString(styleHeader.Render("Time's up! Take a break.") + "\n\n")
	} else {
		b.WriteString(styleSubhead.Render(fmt.Sprintf("%d min focus session", int(pomodoroDuration.Minutes()))) + "\n\n")
	}
	b.WriteString(styleKey.Render("esc") + " / " + styleKey.Render("q") + styleSubhead.Render("  cancel"))

	return stylePopupBorder.Width(popW).Render(b.String())
}

func (m Model) renderStats() string {
	var b strings.Builder
	b.WriteString(m.renderHeader("Stats") + "\n" + m.renderDivider() + "\n\n")
	b.WriteString("  " + styleHeader.Render("Productivity") + "\n\n")

	if m.statsData == nil {
		b.WriteString("  Loading…\n")
		return b.String()
	}

	st := m.statsData
	b.WriteString(fmt.Sprintf("  %-14s %s\n", "Today", styleCountBadge.Render(fmt.Sprintf("%d ✓", st.today))))
	b.WriteString(fmt.Sprintf("  %-14s %s\n", "This week", styleCountBadge.Render(fmt.Sprintf("%d ✓", st.week))))
	b.WriteString(fmt.Sprintf("  %-14s %s\n", "Total", styleCountBadge.Render(fmt.Sprintf("%d ✓", st.total))))

	if len(st.daily) > 0 {
		b.WriteString("\n  " + styleSubhead.Render("Last 10 days") + "\n")
		b.WriteString("  " + styleStats.Render(sparkline(st.daily)) + "\n")
		// date range label
		from := time.Now().AddDate(0, 0, -(len(st.daily) - 1)).Format("Jan 02")
		to := time.Now().Format("Jan 02")
		b.WriteString("  " + styleSubhead.Render(from+" – "+to) + "\n")
	}

	b.WriteString("\n  " + styleSubhead.Render("any key to close") + "\n")
	return b.String()
}

func sparkline(counts []int) string {
	blocks := []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	max := 1
	for _, c := range counts {
		if c > max {
			max = c
		}
	}
	var b strings.Builder
	for _, c := range counts {
		idx := (c * (len(blocks) - 1)) / max
		b.WriteRune(blocks[idx])
	}
	return b.String()
}

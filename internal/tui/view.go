package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/dateutil"
	"github.com/aeon022/missionctl-core/humanize"
	"github.com/aeon022/missionctl-core/keymap"
	"github.com/aeon022/missionctl-core/lastsync"
	"github.com/aeon022/missionctl-core/overlay"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/models"
)

// ── Init / Update / View ──────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadTasks(m.showDone), loadCachedListEntriesCmd(), loadAllListNamesCmd(), m.sp.Tick, loadLastSyncedCmd())
}

type lastSyncedLoadedMsg struct{ t time.Time }

func loadLastSyncedCmd() tea.Cmd {
	return func() tea.Msg {
		t, _ := lastsync.Load(config.LastSyncedPath())
		return lastSyncedLoadedMsg{t: t}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		// -1: reserves a row of slack so View() never emits exactly as many
		// lines as the terminal height (charmbracelet/bubbletea#304 — that
		// combined with no trailing newline can fail to fully redraw).
		m.height = msg.Height - 1
		if m.height < 1 {
			m.height = 1
		}

	case tasksLoadedMsg:
		m.tasks = msg.tasks
		m.rows = buildRows(m.tasks, m.searchQuery(), m.filter)
		m.loading = false
		m.cursor = firstTaskRow(m.rows)
		// pre-populate list entries from loaded tasks so picker works immediately
		if len(m.listEntries) == 0 {
			m.listEntries = uniqueListEntries(m.tasks)
		}
		if m.openTaskID != "" {
			for i, r := range m.rows {
				if !r.isHeader && r.task != nil && r.task.ID == m.openTaskID {
					m.cursor = i
					m.detailTarget = r.task
					m.subtaskCursor = 0
					m.view = viewDetail
					break
				}
			}
			m.openTaskID = ""
		}

	case lastSyncedLoadedMsg:
		m.lastSynced = msg.t

	case syncDoneMsg:
		m.syncing = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.tasks = msg.tasks
			m.rows = buildRows(m.tasks, m.searchQuery(), m.filter)
			m.cursor = firstTaskRow(m.rows)
			m.err = nil
			m.lastSynced = time.Now()
			_ = lastsync.Save(config.LastSyncedPath(), m.lastSynced)
		}

	case taskSavedMsg:
		m.submitting = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
			m.view = viewList
			m.editTarget = nil
			return m, loadTasks(m.showDone)
		}

	case toggleDonedMsg:
		// don't reload — task stays visible as greyed-out until next sync or restart
		if msg.err != nil {
			m.err = msg.err
		}

	case taskDeletedMsg:
		m.deleteTarget = nil
		var clearCmd tea.Cmd
		if msg.task != nil {
			m.lastDeleted = msg.task
			clearCmd = clearDeletedToastCmd(msg.task.ID)
		}
		if msg.err != nil {
			// Reminders delete failed (e.g. non-iCloud list) — show warning
			// but still reload since we removed from local cache
			m.err = fmt.Errorf("removed locally (Reminders: %v)", msg.err)
		} else {
			m.err = nil
		}
		return m, tea.Batch(loadTasks(m.showDone), clearCmd)

	case clearDeletedToastMsg:
		if m.lastDeleted != nil && m.lastDeleted.ID == msg.id {
			m.lastDeleted = nil
		}

	case clearFlashMsg:
		if m.flash == msg.text {
			m.flash = ""
		}

	case postponeMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
			return m, loadTasks(m.showDone)
		}

	case batchDoneMsg:
		// selection already cleared + tasks already greyed out from the key handler
		if msg.err != nil {
			m.err = msg.err
		}

	case batchDeletedMsg:
		m.selecting = false
		m.selected = nil
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
			return m, loadTasks(m.showDone)
		}

	case listNamesMsg:
		if len(msg.entries) > 0 {
			// replace entirely — async load has account info and empty lists;
			// merging would show "Erinnerungen" + "Erinnerungen (iCloud)" as duplicates
			m.listEntries = msg.entries
			sort.Slice(m.listEntries, func(i, j int) bool {
				if m.listEntries[i].Name != m.listEntries[j].Name {
					return m.listEntries[i].Name < m.listEntries[j].Name
				}
				return m.listEntries[i].Account < m.listEntries[j].Account
			})
		} else if msg.err != nil && len(m.listEntries) == 0 {
			// Only surface this when the picker would otherwise stay silently
			// empty (e.g. a fresh install with no cached tasks, so
			// uniqueListEntries(m.tasks) also had nothing to fall back on) —
			// don't clobber the form with an error if we already have entries
			// from the task cache.
			m.err = fmt.Errorf("couldn't load Reminders lists: %v", msg.err)
		}

	case statsMsg:
		m.statsData = &msg

	case tickMsg:
		if m.pomRunning && m.view == viewPomodoro {
			elapsed := time.Since(m.pomStart)
			if elapsed >= pomodoroDuration {
				m.pomRunning = false
				notifyPomodoro(m.pomTask)
				return m, nil
			}
			return m, tick()
		}

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < len(m.rows) && m.rows[m.cursor].isHeader && m.cursor > 0 {
					m.cursor--
				}
			}
		case tea.MouseWheelDown:
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				if m.cursor < len(m.rows) && m.rows[m.cursor].isHeader && m.cursor < len(m.rows)-1 {
					m.cursor++
				}
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		if m.view != viewList {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseLeft:
			if i := m.rowHitTest(msg.Y - appPadV); i >= 0 {
				now := time.Now()
				if i == m.lastClickRow && now.Sub(m.lastClickAt) < doubleClickWindow {
					m.cursor = i
					m.lastClickRow = -1 // consumed, so a third click starts fresh
					if t := cursorTask(m); t != nil {
						m.detailTarget = t
						m.subtaskCursor = 0
						m.view = viewDetail
					}
					return m, nil
				}
				m.cursor = i
				m.lastClickRow = i
				m.lastClickAt = now
			}
		case tea.MouseRight:
			// Toggle done on whatever row was clicked, not the cursor row —
			// a quick-action shouldn't require selecting first.
			if i := m.rowHitTest(msg.Y - appPadV); i >= 0 {
				if t := taskAtRow(m, i); t != nil {
					if t.Done() {
						t.Status = "needsAction"
						t.CompletedAt = nil
					} else {
						t.Status = "completed"
						now := time.Now()
						t.CompletedAt = &now
					}
					m.rows = buildRows(m.tasks, m.searchQuery(), m.filter)
					return m, toggleDoneCmd(t)
				}
			}
		}
		return m, nil

	case tea.MouseMotionMsg:
		if m.view == viewList {
			m.hoverRow = m.rowHitTest(msg.Y - appPadV)
		}
		return m, nil

	case spinner.TickMsg:
		if m.syncing || m.loading {
			var cmd tea.Cmd
			m.sp, cmd = m.sp.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	// ── pomodoro view ─────────────────────────────────────────────────────
	if m.view == viewPomodoro {
		switch msg.String() {
		case "esc", "q":
			m.pomRunning = false
			m.view = viewList
		}
		return m, nil
	}

	// ── stats view ────────────────────────────────────────────────────────
	if m.view == viewStats {
		m.view = viewList
		return m, nil
	}

	// ── help overlay ──────────────────────────────────────────────────────
	if m.view == viewHelp {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc", "?":
			m.view = viewList
			return m, nil
		}
		var cmd tea.Cmd
		m.helpVP, cmd = m.helpVP.Update(msg)
		return m, cmd
	}

	// ── task detail popup ────────────────────────────────────────────────
	if m.view == viewDetail {
		t := m.detailTarget

		if m.addingSubtask {
			switch msg.String() {
			case "enter":
				title := strings.TrimSpace(m.subtaskInput.Value())
				m.addingSubtask = false
				m.subtaskInput.Blur()
				m.subtaskInput.SetValue("")
				if title != "" && t != nil {
					t.Subtasks = append(t.Subtasks, models.Subtask{Title: title})
					m.subtaskCursor = len(t.Subtasks) - 1
					return m, persistSubtaskEditCmd(t)
				}
				return m, nil
			case "esc":
				m.addingSubtask = false
				m.subtaskInput.Blur()
				m.subtaskInput.SetValue("")
				return m, nil
			}
			var cmd tea.Cmd
			m.subtaskInput, cmd = m.subtaskInput.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q", "esc", "enter":
			m.view = viewList
			m.detailTarget = nil
			m.subtaskCursor = 0
			return m, nil
		case "j", "down":
			if t != nil && m.subtaskCursor < len(t.Subtasks)-1 {
				m.subtaskCursor++
			}
		case "k", "up":
			if m.subtaskCursor > 0 {
				m.subtaskCursor--
			}
		case "space":
			if t != nil && m.subtaskCursor < len(t.Subtasks) {
				t.Subtasks[m.subtaskCursor].Done = !t.Subtasks[m.subtaskCursor].Done
				return m, persistSubtaskEditCmd(t)
			}
		case "a":
			if t != nil {
				m.addingSubtask = true
				m.subtaskInput.SetValue("")
				return m, m.subtaskInput.Focus()
			}
		case "x":
			if t != nil && m.subtaskCursor < len(t.Subtasks) {
				t.Subtasks = append(t.Subtasks[:m.subtaskCursor], t.Subtasks[m.subtaskCursor+1:]...)
				if m.subtaskCursor >= len(t.Subtasks) {
					m.subtaskCursor = max(0, len(t.Subtasks)-1)
				}
				return m, persistSubtaskEditCmd(t)
			}
		case "o":
			if t != nil {
				if u := effectiveURL(t); u != "" {
					return m, openURLCmd(u)
				}
			}
		case "p":
			if t != nil {
				m.pomTask = t
				m.pomStart = time.Now()
				m.pomRunning = true
				m.view = viewPomodoro
				m.detailTarget = nil
				return m, tick()
			}
		case "e":
			if t != nil {
				m.listEntries = uniqueListEntries(m.tasks)
				m.view = viewCreate
				m.inputs = prefillForm(t)
				m.editTarget = t
				m.inputIdx = 0
				m.listPickerIdx = 0
				m.detailTarget = nil
				return m, tea.Batch(m.inputs[fTitle].Focus(), loadAllListNamesCmd())
			}
		case "d":
			if t != nil {
				m.deleteTarget = t
				m.view = viewList
				m.detailTarget = nil
			}
		}
		return m, nil
	}

	// ── create/edit form ──────────────────────────────────────────────────
	if m.view == viewCreate {
		// list picker: ↑/↓ navigate all entries; input gets just the list name
		if m.inputIdx == fList && len(m.listEntries) > 0 {
			switch msg.String() {
			case "up":
				if m.listPickerIdx > 0 {
					m.listPickerIdx--
					m.inputs[fList].SetValue(m.listEntries[m.listPickerIdx].Name)
				}
				return m, nil
			case "down":
				if m.listPickerIdx < len(m.listEntries)-1 {
					m.listPickerIdx++
					m.inputs[fList].SetValue(m.listEntries[m.listPickerIdx].Name)
				}
				return m, nil
			}
		}

		switch msg.String() {
		case "esc":
			m.view = viewList
			m.editTarget = nil
			return m, nil
		case "tab":
			m.inputs[m.inputIdx].Blur()
			m.inputIdx = (m.inputIdx + 1) % fCount
			m.listPickerIdx = 0
			return m, m.inputs[m.inputIdx].Focus()
		case "shift+tab":
			m.inputs[m.inputIdx].Blur()
			m.inputIdx = (m.inputIdx - 1 + fCount) % fCount
			m.listPickerIdx = 0
			return m, m.inputs[m.inputIdx].Focus()
		case "enter":
			if m.inputIdx < fCount-1 {
				m.inputs[m.inputIdx].Blur()
				m.inputIdx++
				m.listPickerIdx = 0
				return m, m.inputs[m.inputIdx].Focus()
			}
			return m.submitForm()
		case "ctrl+s":
			return m.submitForm()
		}
		var cmd tea.Cmd
		m.inputs[m.inputIdx], cmd = m.inputs[m.inputIdx].Update(msg)
		return m, cmd
	}

	// ── command palette mode ──────────────────────────────────────────────
	if m.inPalette {
		closePalette := func(mm Model) Model {
			mm.inPalette = false
			mm.paletteInput.Blur()
			mm.paletteInput.SetValue("")
			mm.paletteCursor = 0
			return mm
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return closePalette(m), nil
		case "up", "ctrl+p":
			if m.paletteCursor > 0 {
				m.paletteCursor--
			}
			return m, nil
		case "down", "ctrl+n":
			matches := palette.Match(paletteCommands, m.paletteInput.Value())
			if m.paletteCursor < len(matches)-1 {
				m.paletteCursor++
			}
			return m, nil
		case "enter":
			matches := palette.Match(paletteCommands, m.paletteInput.Value())
			if len(matches) == 0 {
				return closePalette(m), nil
			}
			if m.paletteCursor >= len(matches) {
				m.paletteCursor = len(matches) - 1
			}
			chosen := matches[m.paletteCursor]
			m = closePalette(m)
			replay := tea.KeyPressMsg{Text: chosen.Key, Code: []rune(chosen.Key)[0]}
			if chosen.Key == "enter" {
				replay = tea.KeyPressMsg{Code: tea.KeyEnter}
			} else if chosen.Key == " " {
				replay = tea.KeyPressMsg{Text: " ", Code: tea.KeySpace}
			}
			newM, cmd := m.Update(replay)
			return newM.(Model), cmd
		}
		var cmd tea.Cmd
		m.paletteInput, cmd = m.paletteInput.Update(msg)
		m.paletteCursor = 0
		return m, cmd
	}

	// ── search mode ───────────────────────────────────────────────────────
	if m.searching {
		switch msg.String() {
		case "esc", "enter":
			m.searching = false
			m.rows = buildRows(m.tasks, m.searchQuery(), m.filter)
			m.cursor = firstTaskRow(m.rows)
			return m, nil
		}
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		m.rows = buildRows(m.tasks, m.searchQuery(), m.filter)
		m.cursor = firstTaskRow(m.rows)
		return m, cmd
	}

	// ── delete confirm ────────────────────────────────────────────────────
	if m.deleteTarget != nil {
		switch msg.String() {
		case "y":
			t := m.deleteTarget
			m.deleteTarget = nil
			return m, deleteTaskCmd(t)
		default:
			m.deleteTarget = nil
		}
		return m, nil
	}

	// ── batch select mode ─────────────────────────────────────────────────
	if m.selecting {
		switch msg.String() {
		case "esc":
			m.selecting = false
			m.selected = nil
			m.rows = buildRows(m.tasks, m.searchQuery(), m.filter)
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < len(m.rows) && m.rows[m.cursor].isHeader && m.cursor > 0 {
					m.cursor--
				}
			}
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				if m.cursor < len(m.rows) && m.rows[m.cursor].isHeader && m.cursor < len(m.rows)-1 {
					m.cursor++
				}
			}
		case "space":
			if t := cursorTask(m); t != nil {
				if m.selected[t.ID] {
					delete(m.selected, t.ID)
				} else {
					m.selected[t.ID] = true
				}
			}
		case "A":
			// select all
			for _, r := range m.rows {
				if !r.isHeader && r.task != nil {
					m.selected[r.task.ID] = true
				}
			}
		case "enter", "ctrl+d":
			// complete all selected — flip visually first, then async
			if len(m.selected) > 0 {
				sel := m.selectedTasks()
				now := time.Now()
				for _, t := range sel {
					t.Status = "completed"
					t.CompletedAt = &now
				}
				m.selecting = false
				m.selected = nil
				m.rows = buildRows(m.tasks, m.searchQuery(), m.filter)
				return m, batchCompleteCmd(sel)
			}
		case "d", "D":
			if len(m.selected) > 0 {
				sel := m.selectedTasks()
				return m, batchDeleteCmd(sel)
			}
		}
		return m, nil
	}

	// ── list view ─────────────────────────────────────────────────────────
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "?":
		m = m.openHelp()

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			if m.cursor < len(m.rows) && m.rows[m.cursor].isHeader && m.cursor > 0 {
				m.cursor--
			}
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
			if m.cursor < len(m.rows) && m.rows[m.cursor].isHeader && m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		}

	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// jump to the nth visible (on-screen) task row, headers not counted
		n := int(msg.String()[0] - '0')
		visible, start := m.visibleRowsWithStart(m.listHeight())
		count := 0
		for i, r := range visible {
			if r.isHeader {
				continue
			}
			count++
			if count == n {
				m.cursor = start + i
				break
			}
		}

	case "s":
		if !m.syncing {
			m.syncing = true
			m.err = nil
			return m, tea.Batch(syncCmd(), m.sp.Tick)
		}

	case "c":
		m.showDone = !m.showDone
		return m, loadTasks(m.showDone)

	case "t":
		if m.filter == filterFocus {
			m.filter = filterNone
		} else {
			m.filter = filterFocus
		}
		m.saveUIState()
		m.rows = buildRows(m.tasks, m.searchQuery(), m.filter)
		m.cursor = firstTaskRow(m.rows)
		return m, nil

	case "O":
		if m.filter == filterOverdue {
			m.filter = filterNone
		} else {
			m.filter = filterOverdue
		}
		m.saveUIState()
		m.rows = buildRows(m.tasks, m.searchQuery(), m.filter)
		m.cursor = firstTaskRow(m.rows)
		return m, nil

	case "v":
		m.selecting = true
		if m.selected == nil {
			m.selected = make(map[string]bool)
		}
		if t := cursorTask(m); t != nil {
			m.selected[t.ID] = true
		}
		return m, nil

	case "space":
		if t := cursorTask(m); t != nil {
			if t.Done() {
				t.Status = "needsAction"
				t.CompletedAt = nil
			} else {
				t.Status = "completed"
				now := time.Now()
				t.CompletedAt = &now
			}
			m.rows = buildRows(m.tasks, m.searchQuery(), m.filter)
			return m, toggleDoneCmd(t)
		}

	case "y":
		if t := cursorTask(m); t != nil {
			m.flash = "Copied to clipboard"
			return m, tea.Batch(copyToClipboardCmd(t.Title), clearFlashCmd(m.flash))
		}

	case "S":
		if t := cursorTask(m); t != nil {
			tomorrow := time.Now().AddDate(0, 0, 1)
			t.DueDate = &tomorrow
			m.rows = buildRows(m.tasks, m.searchQuery(), m.filter)
			return m, postponeCmd(t, tomorrow)
		}

	case "u":
		if m.lastDeleted != nil {
			t := m.lastDeleted
			m.lastDeleted = nil
			return m, undoDeleteCmd(t)
		}

	case "p":
		if t := cursorTask(m); t != nil {
			m.pomTask = t
			m.pomStart = time.Now()
			m.pomRunning = true
			m.view = viewPomodoro
			return m, tick()
		}

	case "/":
		m.searching = true
		m.searchInput.SetValue("")
		return m, m.searchInput.Focus()

	case ":":
		m.inPalette = true
		m.paletteCursor = 0
		m.paletteInput.SetValue("")
		return m, m.paletteInput.Focus()

	case "i":
		m.view = viewStats
		return m, loadStats()

	case "enter":
		if t := cursorTask(m); t != nil {
			m.detailTarget = t
			m.subtaskCursor = 0
			m.view = viewDetail
		}
		return m, nil

	case "n":
		m.listEntries = uniqueListEntries(m.tasks)
		m.view = viewCreate
		m.inputs = newFormInputs(config.Active.DefaultList)
		m.editTarget = nil
		m.inputIdx = 0
		m.listPickerIdx = 0
		return m, tea.Batch(m.inputs[fTitle].Focus(), loadAllListNamesCmd())

	case "e":
		if t := cursorTask(m); t != nil {
			m.listEntries = uniqueListEntries(m.tasks)
			m.view = viewCreate
			m.inputs = prefillForm(t)
			m.editTarget = t
			m.inputIdx = 0
			m.listPickerIdx = 0
			return m, tea.Batch(m.inputs[fTitle].Focus(), loadAllListNamesCmd())
		}

	case "d":
		if t := cursorTask(m); t != nil {
			m.deleteTarget = t
		}

	case "o":
		if t := cursorTask(m); t != nil {
			if u := effectiveURL(t); u != "" {
				return m, openURLCmd(u)
			}
		}
	}
	return m, nil
}

// appPadV/appPadH inset the whole app from the terminal edges (matching
// notectl's request for breathing room). Applied by shrinking the model's
// effective width/height before any sub-render runs, then wrapping the
// result in a matching lipgloss.Padding — so every width/height computation
// downstream (dividers, popup sizing, listHeight) already accounts for it.
// Mouse Y coordinates need the same offset subtracted; see the
// tea.MouseMsg case in Update.
const appPadV, appPadH = 1, 2

func (m Model) View() tea.View {
	v := tea.NewView(m.viewContent())
	// v1's tea.WithAltScreen()/WithMouseAllMotion() Program options are gone
	// in v2 — they are per-View fields now.
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

func (m Model) viewContent() string {
	if m.loading {
		return "\n  " + m.sp.View() + styleSubhead.Render(" Loading tasks…") + "\n"
	}
	m.width -= appPadH * 2
	m.height -= appPadV * 2

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
		content = overlay.Center(m.renderList(), m.renderHelpPopup(), m.width, m.height, 0)
	case viewDetail:
		content = overlay.Center(m.renderList(), m.renderDetailPopup(), m.width, m.height, 0)
	default:
		content = m.renderList()
	}
	return lipgloss.NewStyle().Padding(appPadV, appPadH).Render(content)
}

// ── Render ────────────────────────────────────────────────────────────────────

// renderHeader is the one header shared by every view: app name + current
// section on the left, the live date right-aligned — a constant anchor no
// matter which screen is active. Degrades to just the left side if the
// terminal is too narrow for both.
func (m Model) renderHeader(section string) string {
	left := styleHeader.Render("taskctl") + styleSubhead.Render(" · "+section)
	right := styleSubhead.Render(time.Now().Format("Mon, 02 Jan 2006"))
	if pad := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2; pad >= 1 {
		return "  " + left + strings.Repeat(" ", pad) + right
	}
	return "  " + left
}

// renderDivider draws the rule under the header, full terminal width.
func (m Model) renderDivider() string {
	return styleSep.Render(strings.Repeat("─", max(0, m.width)))
}

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

func (m Model) renderList() string {
	var b strings.Builder
	b.WriteString(m.renderHeader("Tasks") + "\n")
	b.WriteString(m.renderDivider() + "\n")

	extra := ""
	if m.syncing {
		extra = "  " + m.sp.View() + styleSubhead.Render(" syncing…")
	} else if !m.lastSynced.IsZero() {
		extra = "  " + styleSubhead.Render("synced "+humanize.TimeAgo(m.lastSynced))
	}
	switch m.filter {
	case filterFocus:
		extra += "  " + styleFocusBadge.Render("focus: today & overdue")
	case filterOverdue:
		extra += "  " + styleFocusBadge.Render("overdue only")
	}
	if m.selecting {
		extra += "  " + theme.Selected.Render(fmt.Sprintf("select: %d", len(m.selected))) +
			styleSubhead.Render("  space toggle  A all  enter done  d delete  esc cancel")
	}
	// Blank line always follows, even when extra is empty — otherwise a
	// non-empty summary/badge line here sits flush against the first list
	// header right below it.
	b.WriteString(extra + "\n\n")

	if m.searching {
		b.WriteString("  " + styleKey.Render("/") + " " + m.searchInput.View() + "  (enter/esc to close)\n\n")
	}

	if m.inPalette {
		b.WriteString("  " + styleKey.Render(":") + " " + m.paletteInput.View() + "\n")
		matches := palette.Match(paletteCommands, m.paletteInput.Value())
		if len(matches) > 6 {
			matches = matches[:6]
		}
		if len(matches) == 0 {
			b.WriteString("    " + styleSubhead.Render("no matching command") + "\n")
		}
		for i, c := range matches {
			row := fmt.Sprintf("%-11s %s", c.Name, c.Desc)
			if i == m.paletteCursor {
				b.WriteString("    " + styleSelected.Render("▶ "+row) + "\n")
			} else {
				b.WriteString("      " + styleSubhead.Render(row) + "\n")
			}
		}
		b.WriteString("\n")
	}

	query := m.searchQuery()
	listHeight := m.listHeight()
	counts := m.groupCounts()
	listColors := make(map[string]string, len(m.listEntries))
	for _, e := range m.listEntries {
		if e.Color != "" {
			listColors[e.Name] = e.Color
		}
	}

	linesWritten := 0
	if len(m.rows) == 0 {
		var msg string
		switch {
		case m.searchQuery() != "":
			msg = "No tasks match your search."
		case m.filter == filterFocus:
			msg = "No tasks due today or overdue — press t to show all tasks."
		case m.filter == filterOverdue:
			msg = "No overdue tasks — press O to show all tasks."
		case len(m.tasks) == 0:
			msg = "No tasks yet — press n to add one, or s to sync with Apple Reminders."
		default:
			msg = "No tasks found."
		}
		// Vertically center in the space the footer's fixed-bottom padding
		// (below) leaves available, instead of sitting flush at the top
		// with a dead void beneath it.
		for topPad := max(0, (listHeight-1)/2); topPad > 0; topPad-- {
			b.WriteString("\n")
			linesWritten++
		}
		centered := lipgloss.NewStyle().Width(max(0, m.width)).Align(lipgloss.Center).Render(styleSubhead.Render(msg))
		b.WriteString(centered + "\n")
		linesWritten++
	}

	visible, start := m.visibleRowsWithStart(listHeight)
	// visibleRowsWithStart windows by row COUNT, but a section header costs
	// 2-3 physical lines (blank + label + rule) against a budget sized in
	// row units — with several list groups on screen that mismatch renders
	// more physical lines than listHeight allows, scrolling the terminal
	// and pushing the app header (printed above this loop) off the top.
	// Stop hard at the real line budget rather than trusting the row-count
	// window alone (same fix as calctl's renderList).
	for localI, r := range visible {
		if linesWritten >= listHeight {
			break
		}
		i := start + localI
		if r.isHeader {
			if i > 0 {
				b.WriteString("\n")
				linesWritten++
			}
			badge := " " + styleCountBadge.Render(fmt.Sprintf("%d", counts[r.label]))
			bullet := ""
			if c := listColors[r.label]; c != "" {
				bullet = lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("● ")
			}
			b.WriteString("  " + bullet + styleHeader.Render(r.label) + badge + "\n")
			b.WriteString("  " + styleSep.Render(strings.Repeat("─", max(0, m.width-2))) + "\n")
			linesWritten += 2
			continue
		}
		t := r.task
		// selection checkbox vs done mark
		var mark string
		if m.selecting {
			if m.selected[t.ID] {
				mark = styleSelected.Render("[x]")
			} else {
				mark = styleSubhead.Render("[ ]")
			}
		} else if t.Done() {
			mark = "✓"
		} else {
			mark = "○"
		}

		// priority indicator
		prio := ""
		switch t.Priority {
		case 1:
			prio = styleUrgent.Render("‼ ")
		case 5:
			prio = styleImportant.Render("! ")
		case 9:
			prio = styleSubhead.Render("↓ ")
		}

		var line string
		switch {
		case t.Done() && !m.selecting:
			line = styleDone.Render(t.Title)
		case i == m.cursor:
			// The cursor row wraps its whole line in a single
			// styleCursor.Render() call below — nesting highlighted
			// (real-ANSI) text here would clobber that background for
			// everything after it, so the cursor row's title stays plain.
			line = prio + styleTitle.Render(t.Title)
		default:
			line = prio + highlightMatches(t.Title, fuzzyMatchIndexes(query, t.Title), styleTitle)
		}

		due := ""
		if t.DueDate != nil {
			now := time.Now()
			switch {
			case t.DueDate.Before(dateutil.StartOfDay(now)):
				due = "  " + styleOverdue.Render("overdue "+t.DueDate.Format("Jan 02"))
			case !t.DueDate.After(dateutil.EndOfDay(now)):
				due = "  " + styleToday.Render("due today")
			default:
				due = "  " + styleDue.Render("due "+t.DueDate.Format("Mon Jan 02"))
			}
		}
		recur := ""
		if t.Recurrence != "" {
			recur = "  " + styleRecur.Render("↻ "+t.Recurrence)
		}
		link := ""
		if effectiveURL(t) != "" {
			link = "  " + styleRecur.Render("↗")
		}
		// One leading column is reserved for the cursor's list-color accent
		// bar, applied outside styleCursor.Render() below — nesting an
		// already-colored glyph inside that call would clobber its color
		// (same hazard noted above for the cursor row's title).
		row := fmt.Sprintf(" %s  %s%s%s%s", mark, line, due, recur, link)
		prefix := " "
		switch {
		case i == m.cursor:
			barStyle := lipgloss.NewStyle().Foreground(colorBlue)
			if c := listColors[t.List]; c != "" {
				barStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c))
			}
			prefix = barStyle.Render("▎")
			row = prefix + styleCursor.Render(row)
		case i == m.hoverRow:
			row = prefix + theme.HoverV2.Render(row)
		default:
			row = prefix + row
		}
		b.WriteString(row + "\n")
		linesWritten++
	}

	// Pin the footer to the bottom of the screen instead of letting it
	// glue itself right under a short list — pad the body out to its
	// full line budget first, matching notectl's fixed-height list pane.
	for ; linesWritten < listHeight; linesWritten++ {
		b.WriteString("\n")
	}

	if m.lastDeleted != nil {
		b.WriteString("\n  " + styleSubhead.Render(fmt.Sprintf("Deleted %q — press u to undo", m.lastDeleted.Title)) + "\n")
	}
	if m.flash != "" {
		b.WriteString("\n  " + styleSelected.Render(m.flash) + "\n")
	}
	if m.err != nil {
		b.WriteString("\n  " + styleErr.Render(m.err.Error()) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(m.renderStatusBar())
	return b.String()
}

// visibleRowsWithStart returns the scroll-windowed slice of m.rows that
// keeps m.cursor in view (row-count budget, not exact lines — headers
// spanning multiple lines get the same treatment renderList and
// rowHitTest agree on), plus its start index into m.rows so callers can
// map a local index back to the global one.
func (m Model) visibleRowsWithStart(height int) ([]row, int) {
	if len(m.rows) == 0 {
		return nil, 0
	}
	if height < 1 {
		height = 1
	}
	start := 0
	end := len(m.rows)
	if end-start > height {
		mid := m.cursor - height/2
		if mid < 0 {
			mid = 0
		}
		if mid+height > end {
			mid = end - height
		}
		start = mid
		end = start + height
	}
	return m.rows[start:end], start
}

// rowHitTest returns the m.rows index at screen row y, or -1 if the click
// missed (landed on a section header, blank line, or outside the list).
// Mirrors the exact line-counting renderList uses: header, divider,
// extra/summary line, its trailing blank line (4 lines, hence row := 4),
// then an optional 2-line search bar, then each row consumes 1 line —
// except section headers, which consume 2 lines (label + rule), plus a
// leading blank line for every header after the first. Walks the same
// scroll window renderList computes, so a click lands on the row it
// visually appears to be over even once the list has scrolled.
func (m Model) rowHitTest(y int) int {
	row := 4
	if m.searching {
		row += 2
	}
	visible, start := m.visibleRowsWithStart(m.listHeight())
	for localI, r := range visible {
		i := start + localI
		if r.isHeader {
			if i > 0 {
				row++
			}
			if y >= row && y < row+2 {
				return -1
			}
			row += 2
			continue
		}
		if y == row {
			return i
		}
		row++
	}
	return -1
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
		Row("c", "show / hide completed tasks").
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

// narrowFooter reports whether the terminal is too narrow for the full
// two-line key legend, which crowds/wraps below this width.
func (m Model) narrowFooter() bool { return m.width > 0 && m.width < 90 }

func (m Model) renderStatusBar() string {
	key := func(k string) string { return styleKey.Render(k) }
	// keyed renders a (possibly multi-key, e.g. "↑/↓") hint in the
	// suite-wide "key:label" format — only the key glyphs are styled, the
	// colon is plain like every other tool's footer.
	keyed := func(k string) string { return styleKey.Render(k) + ":" }

	if m.deleteTarget != nil {
		return fmt.Sprintf("  Delete %q?  %sconfirm  any cancel\n",
			m.deleteTarget.Title, keyed("y"))
	}
	if m.narrowFooter() {
		return fmt.Sprintf("  %snav  %sdone  %snew  %ssearch  %shelp  %squit\n",
			keyed("↑/↓"), keyed("space"), keyed("n"), keyed("/"), keyed("?"), keyed("q"))
	}
	doneLabel := "show done"
	if m.showDone {
		doneLabel = "hide done"
	}
	line1 := fmt.Sprintf(
		"  %snav  %sdone  %sdetails  %snew/edit/delete  %sopen url  %spostpone",
		keyed("↑/↓"),
		keyed("space"),
		keyed("enter"),
		keyed("n/e/d"),
		keyed("o"),
		keyed("S"),
	)
	line2 := fmt.Sprintf(
		"  %sundo  %spomo  %sselect  %sfocus  %ssearch  %sstats  %ssync  %s%s  %shelp  %squit",
		keyed("u"),
		keyed("p"),
		keyed("v"),
		keyed("t"),
		keyed("/"),
		keyed("i"),
		keyed("s"),
		key("c"), ":"+doneLabel,
		keyed("?"),
		keyed("q"),
	)
	return line1 + "\n" + line2 + "\n"
}

func (m Model) statusBarHeight() int {
	if m.narrowFooter() {
		return 1
	}
	return 2
}

// listHeight is the line budget available for task rows. The "6" is the
// fixed overhead empirically verified against the rendered output: header,
// divider, extra/summary line, the blank breathing-room line now after it,
// plus the pre-footer blank line and one more line of slack accounted for
// by testing rather than a clean derivation from the render calls alone.
// Changing anything in that fixed top/bottom block requires re-checking
// this against an actual render (see rowHitTest's matching row := 4).
func (m Model) listHeight() int {
	h := m.height - 6 - m.statusBarHeight()
	if m.searching {
		h -= 2
	}
	if m.inPalette {
		// input line + up to 6 match rows + trailing blank — must match
		// what the palette block in View() actually renders, or the task
		// list below overflows the terminal and pushes the input/matches
		// themselves off the top of the screen.
		h -= 8
	}
	return h
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

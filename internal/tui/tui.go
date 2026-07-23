package tui

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/aeon022/taskctl/internal/nlpdate"
	"github.com/aeon022/taskctl/internal/reminders"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// ── Views ────────────────────────────────────────────────────────────────────

type view int

const (
	viewList     view = 0
	viewCreate   view = 1
	viewPomodoro view = 2
	viewStats    view = 3
	viewHelp     view = 4
)

// ── Form fields ───────────────────────────────────────────────────────────────

const (
	fTitle      = 0
	fList       = 1
	fDue        = 2
	fNotes      = 3
	fRecurrence = 4
	fCount      = 5
)

var formLabels = [fCount]string{"Title", "List", "Due", "Notes", "Repeat (daily/weekly/monthly)"}

const pomodoroDuration = 25 * time.Minute

// ── Messages ──────────────────────────────────────────────────────────────────

type tasksLoadedMsg struct{ tasks []models.Task }
type syncDoneMsg struct {
	tasks []models.Task
	err   error
}
type taskSavedMsg struct{ err error }
type toggleDonedMsg struct{ err error }
type taskDeletedMsg struct {
	task *models.Task
	err  error
}
type postponeMsg struct{ err error }
type statsMsg struct {
	today, week, total int
	daily              []int
}
type listNamesMsg struct{ entries []models.ListEntry }
type batchDoneMsg struct{ err error }
type batchDeletedMsg struct{ count int; err error }
type tickMsg time.Time

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	colorBlue   = lipgloss.AdaptiveColor{Light: "25",  Dark: "33"}
	colorGreen  = lipgloss.AdaptiveColor{Light: "28",  Dark: "42"}
	colorRed    = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	colorAmber  = lipgloss.AdaptiveColor{Light: "214", Dark: "220"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "243", Dark: "246"}
	colorSubtle = lipgloss.AdaptiveColor{Light: "250", Dark: "239"}

	styleHeader   = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	styleSubhead  = lipgloss.NewStyle().Foreground(colorMuted)
	styleSep      = lipgloss.NewStyle().Foreground(colorSubtle)
	styleDone     = lipgloss.NewStyle().Foreground(colorMuted).Strikethrough(true)
	styleTitle    = lipgloss.NewStyle()
	styleDue      = lipgloss.NewStyle().Foreground(colorAmber)
	styleOverdue  = lipgloss.NewStyle().Foreground(colorRed)
	styleCursor   = lipgloss.NewStyle().
				Background(lipgloss.AdaptiveColor{Light: "189", Dark: "17"}).
				Foreground(lipgloss.AdaptiveColor{Light: "16",  Dark: "255"}).
				Bold(true)
	styleKey      = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	styleLabel    = lipgloss.NewStyle().Foreground(colorMuted).Width(28)
	styleBox      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBlue).Padding(1, 2)
	styleErr      = lipgloss.NewStyle().Foreground(colorRed)
	styleRecur    = lipgloss.NewStyle().Foreground(colorGreen)
	stylePomo     = lipgloss.NewStyle().Bold(true).Foreground(colorAmber)
	styleStats    = lipgloss.NewStyle().Foreground(colorBlue)
	styleUrgent   = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	styleImportant = lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	styleSelected = lipgloss.NewStyle().Foreground(colorGreen)
)

// ── Model ─────────────────────────────────────────────────────────────────────

type row struct {
	isHeader bool
	label    string
	task     *models.Task
}

type Model struct {
	tasks    []models.Task
	rows     []row
	cursor   int
	view     view
	loading  bool
	syncing  bool
	sp       spinner.Model
	showDone bool
	err      error
	width    int
	height   int
	// form
	inputs        [fCount]textinput.Model
	inputIdx      int
	submitting    bool
	editTarget    *models.Task
	listEntries   []models.ListEntry
	listPickerIdx int
	// delete confirm
	deleteTarget *models.Task
	// undo
	lastDeleted *models.Task
	// focus mode (today + overdue only)
	focusMode bool
	// batch select
	selecting bool
	selected  map[string]bool
	// search
	searching   bool
	searchInput textinput.Model
	// pomodoro
	pomTask    *models.Task
	pomStart   time.Time
	pomRunning bool
	// stats
	statsData *statsMsg
}

func newModel() Model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = styleSubhead

	si := textinput.New()
	si.Placeholder = "search…"
	si.CharLimit = 80
	return Model{loading: true, searchInput: si, sp: sp}
}

// ── Init / Update / View ──────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadTasks(m.showDone), loadCachedListEntriesCmd(), loadAllListNamesCmd())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tasksLoadedMsg:
		m.tasks = msg.tasks
		m.rows = buildRows(m.tasks, m.searchQuery(), m.focusMode)
		m.loading = false
		m.cursor = firstTaskRow(m.rows)
		// pre-populate list entries from loaded tasks so picker works immediately
		if len(m.listEntries) == 0 {
			m.listEntries = uniqueListEntries(m.tasks)
		}

	case syncDoneMsg:
		m.syncing = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.tasks = msg.tasks
			m.rows = buildRows(m.tasks, m.searchQuery(), m.focusMode)
			m.cursor = firstTaskRow(m.rows)
			m.err = nil
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
		if msg.task != nil {
			m.lastDeleted = msg.task
		}
		if msg.err != nil {
			// Reminders delete failed (e.g. non-iCloud list) — show warning
			// but still reload since we removed from local cache
			m.err = fmt.Errorf("removed locally (Reminders: %v)", msg.err)
		} else {
			m.err = nil
		}
		return m, loadTasks(m.showDone)

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

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < len(m.rows) && m.rows[m.cursor].isHeader && m.cursor > 0 {
					m.cursor--
				}
			}
		case tea.MouseButtonWheelDown:
			if m.cursor < len(m.rows)-1 {
				m.cursor++
				if m.cursor < len(m.rows) && m.rows[m.cursor].isHeader && m.cursor < len(m.rows)-1 {
					m.cursor++
				}
			}
		}
		return m, nil

	case spinner.TickMsg:
		if m.syncing {
			var cmd tea.Cmd
			m.sp, cmd = m.sp.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
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

	// ── search mode ───────────────────────────────────────────────────────
	if m.searching {
		switch msg.String() {
		case "esc", "enter":
			m.searching = false
			m.rows = buildRows(m.tasks, m.searchQuery(), m.focusMode)
			m.cursor = firstTaskRow(m.rows)
			return m, nil
		}
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		m.rows = buildRows(m.tasks, m.searchQuery(), m.focusMode)
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
			m.rows = buildRows(m.tasks, m.searchQuery(), m.focusMode)
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
		case " ":
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
				m.rows = buildRows(m.tasks, m.searchQuery(), m.focusMode)
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
		m.view = viewHelp

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
		m.focusMode = !m.focusMode
		m.rows = buildRows(m.tasks, m.searchQuery(), m.focusMode)
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

	case " ":
		if t := cursorTask(m); t != nil {
			if t.Done() {
				t.Status = "needsAction"
				t.CompletedAt = nil
			} else {
				t.Status = "completed"
				now := time.Now()
				t.CompletedAt = &now
			}
			m.rows = buildRows(m.tasks, m.searchQuery(), m.focusMode)
			return m, toggleDoneCmd(t)
		}

	case "S":
		if t := cursorTask(m); t != nil {
			tomorrow := time.Now().AddDate(0, 0, 1)
			t.DueDate = &tomorrow
			m.rows = buildRows(m.tasks, m.searchQuery(), m.focusMode)
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

	case "i":
		m.view = viewStats
		return m, loadStats()

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
	}
	return m, nil
}

func (m Model) View() string {
	if m.loading {
		return "\n  Loading tasks…\n"
	}
	switch m.view {
	case viewCreate:
		return m.renderForm()
	case viewPomodoro:
		return m.renderPomodoro()
	case viewStats:
		return m.renderStats()
	case viewHelp:
		return m.renderHelp()
	default:
		return m.renderList()
	}
}

// ── Render ────────────────────────────────────────────────────────────────────

// renderHeader is the one header shared by every view: app name + current
// section, so it stays a constant anchor no matter which screen is active.
func (m Model) renderHeader(section string) string {
	return "  " + styleHeader.Render("taskctl") + styleSubhead.Render(" · "+section)
}

func (m Model) renderList() string {
	var b strings.Builder
	b.WriteString("\n")

	status := ""
	if m.syncing {
		status = "  " + m.sp.View() + styleSubhead.Render(" syncing…")
	}
	focusLabel := ""
	if m.focusMode {
		focusLabel = styleOverdue.Render("  [focus: today & overdue]")
	}
	selectLabel := ""
	if m.selecting {
		selectLabel = styleSelected.Render(fmt.Sprintf("  [select: %d]  space toggle  A all  enter done  d delete  esc cancel", len(m.selected)))
	}
	b.WriteString(m.renderHeader("Tasks") + status + focusLabel + selectLabel + "\n\n")

	if m.searching {
		b.WriteString("  " + styleKey.Render("/") + " " + m.searchInput.View() + "  (enter/esc to close)\n\n")
	}

	if len(m.rows) == 0 {
		switch {
		case m.searchQuery() != "":
			b.WriteString("  No tasks match your search.\n")
		case len(m.tasks) == 0:
			b.WriteString("  No tasks yet — press n to add one, or s to sync with Apple Reminders.\n")
		default:
			b.WriteString("  No tasks found.\n")
		}
	}
	for i, r := range m.rows {
		if r.isHeader {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString("  " + styleHeader.Render(r.label) + "\n")
			b.WriteString("  " + styleSep.Render(strings.Repeat("─", len(r.label)+2)) + "\n")
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
		if t.Priority == 1 {
			prio = styleUrgent.Render("‼ ")
		} else if t.Priority == 5 {
			prio = styleImportant.Render("! ")
		}

		var line string
		if t.Done() && !m.selecting {
			line = styleDone.Render(t.Title)
		} else {
			line = prio + styleTitle.Render(t.Title)
		}

		due := ""
		if t.DueDate != nil {
			now := time.Now()
			if t.DueDate.Before(startOfDay(now)) {
				due = "  " + styleOverdue.Render("overdue "+t.DueDate.Format("Jan 02"))
			} else {
				due = "  " + styleDue.Render("due "+t.DueDate.Format("Mon Jan 02"))
			}
		}
		recur := ""
		if t.Recurrence != "" {
			recur = "  " + styleRecur.Render("↻ "+t.Recurrence)
		}
		row := fmt.Sprintf("  %s  %s%s%s", mark, line, due, recur)
		if i == m.cursor {
			row = styleCursor.Render(row)
		}
		b.WriteString(row + "\n")
	}

	if m.lastDeleted != nil {
		b.WriteString("\n  " + styleSubhead.Render(fmt.Sprintf("Deleted %q — press u to undo", m.lastDeleted.Title)) + "\n")
	}
	if m.err != nil {
		b.WriteString("\n  " + styleErr.Render(m.err.Error()) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(m.renderStatusBar())
	return b.String()
}

func (m Model) renderHelp() string {
	key := func(k string) string { return styleKey.Render(fmt.Sprintf("%-9s", k)) }
	row := func(k, desc string) string { return "  " + key(k) + styleSubhead.Render(desc) + "\n" }
	section := func(t string) string { return "\n  " + styleHeader.Render(t) + "\n" }

	var b strings.Builder
	b.WriteString("\n" + m.renderHeader("Help") + "\n")
	b.WriteString(section("Navigation"))
	b.WriteString(row("j / ↓", "move down"))
	b.WriteString(row("k / ↑", "move up"))
	b.WriteString(row("/", "search tasks (esc clears)"))
	b.WriteString(row("t", "focus mode — today & overdue only"))
	b.WriteString(row("c", "show / hide completed tasks"))
	b.WriteString(section("Tasks"))
	b.WriteString(row("space", "toggle done"))
	b.WriteString(row("n", "new task"))
	b.WriteString(row("e", "edit task"))
	b.WriteString(row("d", "delete task (asks to confirm)"))
	b.WriteString(row("S", "postpone to tomorrow"))
	b.WriteString(row("u", "undo last action"))
	b.WriteString(section("Batch & Extras"))
	b.WriteString(row("v", "select mode (space toggle, A all, enter done, d delete)"))
	b.WriteString(row("p", "pomodoro timer for selected task"))
	b.WriteString(row("i", "stats"))
	b.WriteString(row("s", "sync with Apple Reminders"))
	b.WriteString(section("Other"))
	b.WriteString(row("?", "toggle this help"))
	b.WriteString(row("q", "quit"))
	return b.String()
}

func (m Model) renderStatusBar() string {
	key := func(k string) string { return styleKey.Render(k) }

	if m.deleteTarget != nil {
		return fmt.Sprintf("  Delete %q?  %s confirm  any cancel\n",
			m.deleteTarget.Title, key("y"))
	}
	doneLabel := "show done"
	if m.showDone {
		doneLabel = "hide done"
	}
	return fmt.Sprintf(
		"  %s/%s nav  %s done  %s postpone  %s undo  %s pomo  %s/%s/%s tasks  %s select  %s focus  %s search  %s stats  %s sync  %s %s  %s help  %s quit\n",
		key("↑"), key("↓"),
		key("space"),
		key("S"),
		key("u"),
		key("p"),
		key("n"), key("e"), key("d"),
		key("v"),
		key("t"),
		key("/"),
		key("i"),
		key("s"),
		key("c"), doneLabel,
		key("?"),
		key("q"),
	)
}

func (m Model) renderForm() string {
	heading := "New Task"
	if m.editTarget != nil {
		heading = "Edit Task"
	}
	var inner strings.Builder
	inner.WriteString(styleHeader.Render(heading) + "\n\n")
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
					label += styleSubhead.Render(" ("+e.Account+")")
				}
				if j == m.listPickerIdx {
					inner.WriteString(strings.Repeat(" ", 30) + styleKey.Render("▶ ") + styleKey.Render(e.Name) + styleSubhead.Render(func() string {
						if e.Account != "" {
							return " (" + e.Account + ")"
						}
						return ""
					}()) + "\n")
				} else {
					inner.WriteString(strings.Repeat(" ", 30) + styleSubhead.Render("  "+label) + "\n")
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
	var b strings.Builder
	b.WriteString("\n" + m.renderHeader(heading) + "\n\n")
	b.WriteString(styleBox.Render(inner.String()))
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

	var b strings.Builder
	b.WriteString("\n" + m.renderHeader("Pomodoro") + "\n\n")
	b.WriteString("  " + styleHeader.Render(title) + "\n\n")
	b.WriteString("  " + stylePomo.Render(timerStr) + "\n\n")
	b.WriteString("  " + styleSubhead.Render(bar) + "\n\n")
	if done {
		b.WriteString("  " + styleHeader.Render("Time's up! Take a break.") + "\n\n")
	} else {
		b.WriteString("  " + styleSubhead.Render(fmt.Sprintf("%d min focus session", int(pomodoroDuration.Minutes()))) + "\n\n")
	}
	b.WriteString("  " + styleKey.Render("esc") + " / " + styleKey.Render("q") + "  cancel\n")
	return b.String()
}

func (m Model) renderStats() string {
	var b strings.Builder
	b.WriteString("\n" + m.renderHeader("Stats") + "\n\n")
	b.WriteString("  " + styleHeader.Render("Productivity") + "\n\n")

	if m.statsData == nil {
		b.WriteString("  Loading…\n")
		return b.String()
	}

	st := m.statsData
	b.WriteString(fmt.Sprintf("  %-14s %s\n", "Today", styleStats.Render(fmt.Sprintf("%d ✓", st.today))))
	b.WriteString(fmt.Sprintf("  %-14s %s\n", "This week", styleStats.Render(fmt.Sprintf("%d ✓", st.week))))
	b.WriteString(fmt.Sprintf("  %-14s %s\n", "Total", styleStats.Render(fmt.Sprintf("%d ✓", st.total))))

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

// ── Cmds ──────────────────────────────────────────────────────────────────────

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func loadTasks(showDone bool) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath())
		if err != nil {
			return tasksLoadedMsg{}
		}
		defer s.Close()
		ctx := context.Background()
		// remove taskctl shadows that now have an apple counterpart
		_ = s.RemoveShadowedLocal(ctx)
		status := "needsAction"
		if showDone {
			status = ""
		}
		tasks, _ := s.ListTasks(ctx, store.ListFilter{Status: status})
		return tasksLoadedMsg{tasks}
	}
}

func loadStats() tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath())
		if err != nil {
			return statsMsg{}
		}
		defer s.Close()
		ctx := context.Background()
		today, week, total, _ := s.Counts(ctx)
		daily, _ := s.DailyCompletions(ctx, 10)
		return statsMsg{today: today, week: week, total: total, daily: daily}
	}
}

func syncCmd() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// Apple Reminders
		tasks, err := reminders.FetchTasks("")
		if err != nil {
			return syncDoneMsg{err: err}
		}

		s, err := store.New(config.DBPath())
		if err != nil {
			return syncDoneMsg{err: err}
		}
		defer s.Close()

		_ = s.DeleteBySource(ctx, "apple")
		s.OverrideWithPendingStatus(ctx, tasks)
		for i := range tasks {
			if s.IsPendingDelete(ctx, tasks[i].Title, tasks[i].List) {
				continue
			}
			_ = s.UpsertTask(ctx, &tasks[i])
		}

		if entries, err := reminders.ListListsWithAccounts(); err == nil && len(entries) > 0 {
			_ = s.StoreListEntries(ctx, entries, "apple")
		}

		_ = s.RemoveShadowedLocal(ctx)
		_ = s.PrunePendingDeletes(ctx)
		_ = s.PrunePendingStatus(ctx)
		loaded, _ := s.ListTasks(ctx, store.ListFilter{Status: "needsAction"})
		return syncDoneMsg{tasks: loaded}
	}
}

func saveTaskCmd(inputs [fCount]textinput.Model, editTarget *models.Task) tea.Cmd {
	return func() tea.Msg {
		rawTitle := strings.TrimSpace(inputs[fTitle].Value())
		if rawTitle == "" {
			return taskSavedMsg{fmt.Errorf("title is required")}
		}
		title, priority := parsePriority(rawTitle)
		listName := strings.TrimSpace(inputs[fList].Value())
		dueStr := strings.TrimSpace(inputs[fDue].Value())
		notes := strings.TrimSpace(inputs[fNotes].Value())
		recurrence := strings.ToLower(strings.TrimSpace(inputs[fRecurrence].Value()))

		t := &models.Task{
			ID:         "taskctl-" + uuid.New().String(),
			Title:      title,
			Priority:   priority,
			List:       listName,
			Notes:      notes,
			Recurrence: recurrence,
			Status:     "needsAction",
			Source:     "taskctl",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		if dueStr != "" {
			d, err := nlpdate.Parse(dueStr)
			if err != nil {
				return taskSavedMsg{fmt.Errorf("datum nicht erkannt – versuche: morgen, nächsten montag, 2026-07-05")}
			}
			t.DueDate = d
		}

		s, err := store.New(config.DBPath())
		if err != nil {
			return taskSavedMsg{err}
		}
		defer s.Close()
		ctx := context.Background()

		if editTarget != nil {
			_ = s.DeleteByID(ctx, editTarget.ID)
			go providerDelete(editTarget)
		}

		// if a same-named task was previously deleted, clear the guard
		_ = s.ClearPendingDelete(ctx, t.Title, t.List)
		// write to local cache immediately → instant UI response
		_ = s.UpsertTask(ctx, t)
		// sync to backend provider in background
		go providerCreate(t)

		return taskSavedMsg{}
	}
}

func providerDelete(t *models.Task)                        { _ = reminders.DeleteTask(t) }
func providerCreate(t *models.Task)                        { _ = reminders.CreateTask(t) }
func providerPostpone(t *models.Task, d time.Time) error   { return reminders.PostponeTask(t, d) }
func providerToggle(t *models.Task, wantDone bool) {
	if wantDone {
		_ = reminders.CompleteTask(t)
	} else {
		_ = reminders.UncompleteTask(t)
	}
}

func deleteTaskCmd(t *models.Task) tea.Cmd {
	taskCopy := *t
	return func() tea.Msg {
		ctx := context.Background()
		s, err := store.New(config.DBPath())
		if err == nil {
			defer s.Close()
			_ = s.DeleteByID(ctx, taskCopy.ID)
			// guard: sync must not re-add this task even if backend delete is slow
			_ = s.AddPendingDelete(ctx, &taskCopy)
		}
		go providerDelete(&taskCopy)
		return taskDeletedMsg{task: &taskCopy}
	}
}

func toggleDoneCmd(t *models.Task) tea.Cmd {
	wantDone := t.Done()
	taskCopy := *t
	return func() tea.Msg {
		ctx := context.Background()
		s, sErr := store.New(config.DBPath())
		if sErr != nil {
			return taskSavedMsg{}
		}
		defer s.Close()

		// persist status locally immediately — sync must not revert this
		_ = s.UpsertTask(ctx, &taskCopy)
		_ = s.AddPendingStatus(ctx, taskCopy.Title, taskCopy.List, taskCopy.Status)

		// backend update in background — don't block the UI
		go func() {
			providerToggle(&taskCopy, wantDone)
			// clear guard once backend confirmed the change
			if s2, err := store.New(config.DBPath()); err == nil {
				_ = s2.ClearPendingStatus(context.Background(), taskCopy.Title, taskCopy.List)
				s2.Close()
			}
		}()

		// spawn next occurrence for recurring tasks
		if wantDone && taskCopy.Recurrence != "" {
			spawn := &models.Task{
				ID:         "taskctl-" + uuid.New().String(),
				Title:      taskCopy.Title,
				List:       taskCopy.List,
				Notes:      taskCopy.Notes,
				Recurrence: taskCopy.Recurrence,
				Status:     "needsAction",
				Source:     "taskctl",
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			}
			d := taskCopy.SpawnDate()
			spawn.DueDate = &d
			_ = s.UpsertTask(ctx, spawn)
			go providerCreate(spawn)
		}
		return toggleDonedMsg{}
	}
}

func postponeCmd(t *models.Task, newDue time.Time) tea.Cmd {
	taskCopy := *t
	return func() tea.Msg {
		if err := providerPostpone(&taskCopy, newDue); err != nil {
			return postponeMsg{err}
		}
		s, err := store.New(config.DBPath())
		if err != nil {
			return postponeMsg{}
		}
		defer s.Close()
		_ = s.UpdateDueDate(context.Background(), taskCopy.ID, &newDue)
		return postponeMsg{}
	}
}

func undoDeleteCmd(t *models.Task) tea.Cmd {
	return func() tea.Msg {
		t.ID = "taskctl-" + uuid.New().String()
		t.Status = "needsAction"
		t.CompletedAt = nil
		s, err := store.New(config.DBPath())
		if err != nil {
			return taskSavedMsg{}
		}
		defer s.Close()
		_ = s.ClearPendingDelete(context.Background(), t.Title, t.List)
		_ = s.UpsertTask(context.Background(), t)
		go providerCreate(t)
		return taskSavedMsg{}
	}
}

func batchCompleteCmd(tasks []*models.Task) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath())
		if err != nil {
			return batchDoneMsg{err}
		}
		defer s.Close()
		ctx := context.Background()
		now := time.Now()
		for _, t := range tasks {
			tc := t
			go providerToggle(tc, true)
			t.Status = "completed"
			t.CompletedAt = &now
			_ = s.UpsertTask(ctx, t)
			_ = s.AddPendingStatus(ctx, t.Title, t.List, "completed")
		}
		return batchDoneMsg{}
	}
}

func batchDeleteCmd(tasks []*models.Task) tea.Cmd {
	copies := make([]models.Task, len(tasks))
	for i, t := range tasks {
		copies[i] = *t
	}
	return func() tea.Msg {
		ctx := context.Background()
		s, _ := store.New(config.DBPath())
		if s != nil {
			defer s.Close()
		}
		for i := range copies {
			if s != nil {
				_ = s.DeleteByID(ctx, copies[i].ID)
				_ = s.AddPendingDelete(ctx, &copies[i])
			}
			go providerDelete(&copies[i])
		}
		return batchDeletedMsg{count: len(copies)}
	}
}

func (m Model) selectedTasks() []*models.Task {
	var out []*models.Task
	for _, r := range m.rows {
		if !r.isHeader && r.task != nil && m.selected[r.task.ID] {
			out = append(out, r.task)
		}
	}
	return out
}

// parsePriority extracts `!` / `!!` prefix from title and returns clean title + priority.
func parsePriority(title string) (string, int) {
	if strings.HasPrefix(title, "!! ") {
		return strings.TrimPrefix(title, "!! "), 1
	}
	if strings.HasPrefix(title, "! ") {
		return strings.TrimPrefix(title, "! "), 5
	}
	return title, 0
}

func notifyPomodoro(t *models.Task) {
	title := "Pomodoro complete!"
	msg := "25 minutes done. Time for a break."
	if t != nil {
		msg = fmt.Sprintf("Done: %s", t.Title)
	}
	script := fmt.Sprintf(`display notification "%s" with title "%s" sound name "Glass"`, msg, title)
	_ = exec.Command("osascript", "-e", script).Run()
}

func (m Model) submitForm() (Model, tea.Cmd) {
	title := strings.TrimSpace(m.inputs[fTitle].Value())
	if title == "" {
		m.err = fmt.Errorf("title is required")
		return m, nil
	}
	m.submitting = true
	m.err = nil
	return m, saveTaskCmd(m.inputs, m.editTarget)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (m Model) searchQuery() string {
	return strings.ToLower(strings.TrimSpace(m.searchInput.Value()))
}

func buildRows(tasks []models.Task, query string, focusMode bool) []row {
	eod := endOfDay(time.Now())
	var rows []row
	curList := ""
	for i := range tasks {
		t := &tasks[i]
		if focusMode && (t.DueDate == nil || t.DueDate.After(eod)) {
			continue
		}
		if query != "" {
			if !strings.Contains(strings.ToLower(t.Title), query) &&
				!strings.Contains(strings.ToLower(t.Notes), query) {
				continue
			}
		}
		if t.List != curList {
			curList = t.List
			rows = append(rows, row{isHeader: true, label: curList})
		}
		rows = append(rows, row{task: t})
	}
	return rows
}

func firstTaskRow(rows []row) int {
	for i, r := range rows {
		if !r.isHeader {
			return i
		}
	}
	return 0
}

func cursorTask(m Model) *models.Task {
	if m.cursor >= len(m.rows) || m.rows[m.cursor].isHeader {
		return nil
	}
	return m.rows[m.cursor].task
}

func newFormInputs(defaultList string) [fCount]textinput.Model {
	var inputs [fCount]textinput.Model
	placeholders := [fCount]string{
		"Buy groceries",
		defaultList,
		"morgen, nächsten montag, 2026-07-05",
		"optional notes",
		"daily / weekly / monthly",
	}
	for i := range inputs {
		t := textinput.New()
		t.Placeholder = placeholders[i]
		t.CharLimit = 200
		inputs[i] = t
	}
	if defaultList != "" {
		inputs[fList].SetValue(defaultList)
	}
	return inputs
}

func prefillForm(t *models.Task) [fCount]textinput.Model {
	inputs := newFormInputs(t.List)
	inputs[fTitle].SetValue(t.Title)
	inputs[fList].SetValue(t.List)
	if t.DueDate != nil {
		inputs[fDue].SetValue(t.DueDate.Format("2006-01-02"))
	}
	inputs[fNotes].SetValue(t.Notes)
	inputs[fRecurrence].SetValue(t.Recurrence)
	return inputs
}

func startOfDay(t time.Time) time.Time {
	y, mo, d := t.Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, t.Location())
}

func loadCachedListEntriesCmd() tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath())
		if err != nil {
			return listNamesMsg{}
		}
		defer s.Close()
		entries, _ := s.GetListEntries(context.Background())
		return listNamesMsg{entries}
	}
}

func loadAllListNamesCmd() tea.Cmd {
	return func() tea.Msg {
		entries, _ := reminders.ListListsWithAccounts()
		// persist to SQLite cache so next startup is instant
		if len(entries) > 0 {
			if s, err := store.New(config.DBPath()); err == nil {
				_ = s.StoreListEntries(context.Background(), entries, "apple")
				s.Close()
			}
		}
		return listNamesMsg{entries}
	}
}

// uniqueListEntries builds list entries from loaded tasks (no account info).
func uniqueListEntries(tasks []models.Task) []models.ListEntry {
	seen := make(map[string]bool)
	var out []models.ListEntry
	for _, t := range tasks {
		if t.List != "" && !seen[t.List] {
			seen[t.List] = true
			out = append(out, models.ListEntry{Name: t.List})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func endOfDay(t time.Time) time.Time {
	y, mo, d := t.Date()
	return time.Date(y, mo, d, 23, 59, 59, 0, t.Location())
}

// Run starts the TUI.
func Run() error {
	p := tea.NewProgram(newModel(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

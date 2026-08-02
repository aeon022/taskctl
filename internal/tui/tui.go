package tui

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/aeon022/missionctl-core/humanize"
	"github.com/aeon022/missionctl-core/lastsync"
	"github.com/aeon022/missionctl-core/overlay"
	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/missionctl-core/uistate"
	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/aeon022/taskctl/internal/nlpdate"
	"github.com/aeon022/taskctl/internal/reminders"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/sahilm/fuzzy"
)

// ── Views ────────────────────────────────────────────────────────────────────

type view int

const (
	viewList     view = 0
	viewCreate   view = 1
	viewPomodoro view = 2
	viewStats    view = 3
	viewHelp     view = 4
	viewDetail   view = 5
)

// listFilterMode narrows the list view. At most one is active at a time —
// turning one on turns the other off, rather than letting them combine
// into a state neither key's own label describes.
type listFilterMode int

const (
	filterNone listFilterMode = iota
	filterFocus
	filterOverdue
)

// ── Form fields ───────────────────────────────────────────────────────────────

const (
	fTitle      = 0
	fList       = 1
	fDue        = 2
	fNotes      = 3
	fURL        = 4
	fRecurrence = 5
	fCount      = 6
)

var formLabels = [fCount]string{"Title", "List", "Due", "Notes", "URL", "Repeat (daily/weekly/monthly)"}

// formLabelWidth is styleLabel's fixed width; the list-picker rows below
// the List field indent past it (+2 for the "  " separator) to line up
// under the field's value column instead of its label.
const formLabelWidth = 28

const pomodoroDuration = 25 * time.Minute
const doubleClickWindow = 400 * time.Millisecond

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
type listNamesMsg struct {
	entries []models.ListEntry
	err     error
}
type batchDoneMsg struct{ err error }
type batchDeletedMsg struct {
	count int
	err   error
}
type tickMsg time.Time
type clearDeletedToastMsg struct{ id string }
type clearFlashMsg struct{ text string }

const deletedToastDuration = 5 * time.Second
const flashDuration = 2 * time.Second

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	// Shared across the suite via missionctl-core/theme.
	colorBlue   = theme.Blue
	colorGreen  = theme.Green
	colorRed    = theme.Red
	colorAmber  = theme.Amber
	colorMuted  = theme.Muted
	colorSubtle = theme.Subtle

	styleHeader  = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	styleSubhead = lipgloss.NewStyle().Foreground(colorMuted)
	styleSep     = lipgloss.NewStyle().Foreground(colorSubtle)
	styleDone    = lipgloss.NewStyle().Foreground(colorMuted).Strikethrough(true)
	styleTitle   = lipgloss.NewStyle()
	styleDue     = lipgloss.NewStyle().Foreground(colorAmber)
	styleToday   = lipgloss.NewStyle().Bold(true).Foreground(colorAmber)
	styleOverdue = lipgloss.NewStyle().Foreground(colorRed)
	styleCursor  = lipgloss.NewStyle().
			Background(theme.SelectedBg).
			Foreground(theme.SelectedFg).
			Bold(true)
	styleKey         = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	styleLabel       = lipgloss.NewStyle().Foreground(colorMuted).Width(formLabelWidth)
	stylePopupBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBlue).Padding(1, 2)
	styleErr         = lipgloss.NewStyle().Foreground(colorRed)
	styleRecur       = lipgloss.NewStyle().Foreground(colorGreen)
	stylePomo        = lipgloss.NewStyle().Bold(true).Foreground(colorAmber)
	styleStats       = lipgloss.NewStyle().Foreground(colorBlue)
	styleUrgent      = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	styleImportant   = lipgloss.NewStyle().Foreground(colorAmber).Bold(true)
	styleSelected    = lipgloss.NewStyle().Foreground(colorGreen)
	styleFocusBadge  = lipgloss.NewStyle().Background(colorRed).Foreground(theme.SelectedFg).Padding(0, 1)
	styleCountBadge  = lipgloss.NewStyle().Foreground(colorMuted).Background(theme.HoverBg).Padding(0, 1)
	styleTitleBar    = lipgloss.NewStyle().Bold(true).Foreground(theme.SelectedFg).Background(colorBlue)
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
	hoverRow int // m.rows index under the mouse cursor, -1 when none

	// openTaskID, when set (via `taskctl --task <id>`, e.g. jumping in from
	// timectl's linked-entry), pre-selects and opens that task's detail
	// popup as soon as tasks finish loading, then clears itself so it only
	// fires once — a normal "u" undo etc. afterward must not keep re-firing it.
	openTaskID string
	// double-click detection: a second left-click on the same row within
	// doubleClickWindow opens the detail popup instead of just selecting.
	lastClickRow int
	lastClickAt  time.Time
	view         view
	loading      bool
	syncing      bool
	lastSynced   time.Time // zero = never synced this install
	sp           spinner.Model
	showDone     bool
	err          error
	width        int
	height       int
	// form
	inputs        [fCount]textinput.Model
	inputIdx      int
	submitting    bool
	editTarget    *models.Task
	listEntries   []models.ListEntry
	listPickerIdx int
	// delete confirm
	deleteTarget *models.Task
	// detail popup (enter)
	detailTarget  *models.Task
	subtaskCursor int
	addingSubtask bool
	subtaskInput  textinput.Model
	// undo
	lastDeleted *models.Task
	// transient confirmation (e.g. "Copied to clipboard"), auto-clears
	flash string
	// list filter: at most one of focus (today+overdue) or overdue-only active
	filter listFilterMode
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

	// "?" transient help popup
	helpVP   viewport.Model
	helpPopW int
	helpPopH int
}

func newModel(openTaskID string) Model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = styleSubhead

	si := textinput.New()
	si.Placeholder = "search…"
	si.CharLimit = 80

	sti := textinput.New()
	sti.Placeholder = "Subtask title…"
	sti.CharLimit = 200

	var state persistedState
	uistate.Load(config.UIStatePath(), &state)

	return Model{
		loading:      true,
		searchInput:  si,
		subtaskInput: sti,
		sp:           sp,
		hoverRow:     -1,
		lastClickRow: -1,
		openTaskID:   openTaskID,
		filter:       listFilterMode(state.Filter),
	}
}

// persistedState is what newModel restores from and saveUIState saves to —
// see missionctl-core/uistate.
type persistedState struct {
	Filter int `json:"filter"` // listFilterMode value
}

func (m Model) saveUIState() {
	_ = uistate.Save(config.UIStatePath(), persistedState{Filter: int(m.filter)})
}

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
		m.height = msg.Height

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
		case tea.MouseButtonLeft:
			if msg.Action != tea.MouseActionPress || m.view != viewList {
				return m, nil
			}
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
		case tea.MouseButtonRight:
			if msg.Action != tea.MouseActionPress || m.view != viewList {
				return m, nil
			}
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
		case tea.MouseButtonNone:
			if msg.Action == tea.MouseActionMotion && m.view == viewList {
				m.hoverRow = m.rowHitTest(msg.Y - appPadV)
			}
		}
		return m, nil

	case spinner.TickMsg:
		if m.syncing || m.loading {
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
		case " ":
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

func (m Model) View() string {
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
	for localI, r := range visible {
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
			case t.DueDate.Before(startOfDay(now)):
				due = "  " + styleOverdue.Render("overdue "+t.DueDate.Format("Jan 02"))
			case !t.DueDate.After(endOfDay(now)):
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
			row = prefix + theme.Hover.Render(row)
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
	key := func(k string) string { return styleKey.Render(fmt.Sprintf("%-9s", k)) }
	row := func(k, desc string) string { return "  " + key(k) + styleSubhead.Render(desc) + "\n" }
	section := func(t string) string { return "\n  " + styleHeader.Render(t) + "\n" }

	var b strings.Builder
	b.WriteString(section("Navigation"))
	b.WriteString(row("j / ↓", "move down"))
	b.WriteString(row("k / ↑", "move up"))
	b.WriteString(row("1-9", "jump to the nth visible task"))
	b.WriteString(row("/", "search tasks (esc clears)"))
	b.WriteString(row("t", "focus mode — today & overdue only"))
	b.WriteString(row("O", "filter — overdue only"))
	b.WriteString(row("c", "show / hide completed tasks"))
	b.WriteString(section("Tasks"))
	b.WriteString(row("space", "toggle done"))
	b.WriteString(row("enter", "task details (a subtask, space toggle, x remove)"))
	b.WriteString(row("n", "new task"))
	b.WriteString(row("e", "edit task"))
	b.WriteString(row("d", "delete task (asks to confirm)"))
	b.WriteString(row("o", "open task URL in browser"))
	b.WriteString(row("S", "postpone to tomorrow"))
	b.WriteString(row("y", "copy title to clipboard"))
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

	vp := viewport.New(popW-6, popH-6) // border 1+1, padding(1,2) → 2 rows/4 cols; -1 row for title bar, -1 for footer
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
	if m.helpVP.TotalLineCount() > m.helpVP.Height {
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

// ── Cmds ──────────────────────────────────────────────────────────────────────

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// clearDeletedToastCmd auto-dismisses the "Deleted X — press u to undo"
// toast after deletedToastDuration, carrying the task ID so a stale timer
// from an older delete can't wipe a newer toast that replaced it.
func clearDeletedToastCmd(id string) tea.Cmd {
	return tea.Tick(deletedToastDuration, func(time.Time) tea.Msg {
		return clearDeletedToastMsg{id: id}
	})
}

// copyToClipboardCmd shells out to pbcopy — same approach mailctl uses for
// its "y" copy shortcut, no clipboard library dependency needed.
func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
		return nil
	}
}

func clearFlashCmd(text string) tea.Cmd {
	return tea.Tick(flashDuration, func(time.Time) tea.Msg {
		return clearFlashMsg{text: text}
	})
}

func loadTasks(showDone bool) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
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
		s, err := store.New(config.DBPath(), config.Shared())
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

		s, err := store.New(config.DBPath(), config.Shared())
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
		if listName == "" {
			// Resolve to the same list CreateTask would fall back to, so the
			// local echo and the Apple-side reminder always agree — otherwise
			// the local row stays "" forever while Apple creates it under its
			// real default list, leaving a permanent phantom duplicate.
			listName = reminders.DefaultList()
		}
		dueStr := strings.TrimSpace(inputs[fDue].Value())
		notes := strings.TrimSpace(inputs[fNotes].Value())
		url := strings.TrimSpace(inputs[fURL].Value())
		recurrence := strings.ToLower(strings.TrimSpace(inputs[fRecurrence].Value()))

		t := &models.Task{
			ID:         "taskctl-" + uuid.New().String(),
			Title:      title,
			Priority:   priority,
			List:       listName,
			Notes:      notes,
			URL:        url,
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

		s, err := store.New(config.DBPath(), config.Shared())
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

// openURLCmd opens a URL in the default browser (macOS).
func openURLCmd(url string) tea.Cmd {
	return func() tea.Msg {
		_ = exec.Command("open", url).Start()
		return nil
	}
}

// effectiveURL returns t.URL if set, otherwise the first link found in
// Notes. The fallback matters more than it looks: EKReminder.url only
// round-trips for URLs taskctl itself wrote via EventKit. Reminders added
// through Reminders.app/Safari's share sheet can show a URL in the app's UI
// that neither EventKit's r.url nor AppleScript's `url of reminder` exposes
// — confirmed directly (a real reminder with a visible ikea.com link in
// Reminders.app came back with an empty url from both APIs). No known
// public-API fix; pasting the link into Notes is the only reliable path
// for those.
func effectiveURL(t *models.Task) string {
	if t.URL != "" {
		return t.URL
	}
	return firstURL(t.Notes)
}

// firstURL returns the first http(s):// link found in s, or "".
func firstURL(s string) string {
	lower := strings.ToLower(s)
	hi := strings.Index(lower, "https://")
	if hi < 0 {
		hi = strings.Index(lower, "http://")
	}
	if hi < 0 {
		return ""
	}
	url := s[hi:]
	for i, r := range url {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' ||
			r == '<' || r == '>' || r == '"' || r == '\'' || r == ')' {
			url = url[:i]
			break
		}
	}
	return url
}

func providerDelete(t *models.Task)                      { _ = reminders.DeleteTask(t) }
func providerCreate(t *models.Task)                      { _ = reminders.CreateTask(t) }
func providerPostpone(t *models.Task, d time.Time) error { return reminders.PostponeTask(t, d) }
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
		s, err := store.New(config.DBPath(), config.Shared())
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
		s, sErr := store.New(config.DBPath(), config.Shared())
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
			if s2, err := store.New(config.DBPath(), config.Shared()); err == nil {
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
				URL:        taskCopy.URL,
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
		s, err := store.New(config.DBPath(), config.Shared())
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
		s, err := store.New(config.DBPath(), config.Shared())
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

// persistSubtaskEditCmd persists a task after a subtask mutation
// (add/toggle/delete). The subtask edit already mutated t in place (it
// points into m.tasks), so the UI reflects the change immediately — this
// just writes it through to the local cache, same "flip locally, save
// async" approach batch mode uses.
func persistSubtaskEditCmd(t *models.Task) tea.Cmd {
	tCopy := *t
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return nil
		}
		defer s.Close()
		_ = s.UpsertTask(context.Background(), &tCopy)
		return nil
	}
}

func batchCompleteCmd(tasks []*models.Task) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
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
		s, _ := store.New(config.DBPath(), config.Shared())
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

// buildRows filters tasks into display rows, grouped by list (unchanged).
// Query matching now fuzzy-matches the title (github.com/sahilm/fuzzy)
// instead of a plain substring check, falling back to a substring match on
// notes — but unlike habctl's filterHabits, it does NOT re-rank by match
// quality: reordering by fuzzy score would scatter a single list's tasks
// across non-contiguous positions, fragmenting the "isHeader" grouping
// this function builds. Fuzzy only widens WHICH tasks match; the original
// list-grouped order is preserved.
func buildRows(tasks []models.Task, query string, filter listFilterMode) []row {
	now := time.Now()
	eod := endOfDay(now)
	sod := startOfDay(now)

	var titleMatch map[int]bool
	if query != "" {
		titles := make([]string, len(tasks))
		for i, t := range tasks {
			titles[i] = t.Title
		}
		matches := fuzzy.Find(query, titles)
		titleMatch = make(map[int]bool, len(matches))
		for _, mt := range matches {
			titleMatch[mt.Index] = true
		}
	}

	var rows []row
	curList := ""
	for i := range tasks {
		t := &tasks[i]
		switch filter {
		case filterFocus:
			if t.DueDate == nil || t.DueDate.After(eod) {
				continue
			}
		case filterOverdue:
			if t.DueDate == nil || !t.DueDate.Before(sod) {
				continue
			}
		}
		if query != "" && !titleMatch[i] && !strings.Contains(strings.ToLower(t.Notes), query) {
			continue
		}
		if t.List != curList {
			curList = t.List
			rows = append(rows, row{isHeader: true, label: curList})
		}
		rows = append(rows, row{task: t})
	}
	return rows
}

// fuzzyMatchIndexes returns the rune indexes within s that q fuzzy-matched,
// or nil if q is empty or doesn't match at all.
func fuzzyMatchIndexes(q, s string) []int {
	if q == "" {
		return nil
	}
	matches := fuzzy.Find(q, []string{s})
	if len(matches) == 0 {
		return nil
	}
	return matches[0].MatchedIndexes
}

// highlightMatches renders s with the rune positions in idxs (from
// fuzzyMatchIndexes) styled via a warm, underlined variant of base, and
// every other character via base itself — fzf-style match highlighting.
//
// Renders one character at a time rather than nesting a highlighted span
// inside a single outer Render() call: lipgloss's Render() ends every
// string with a full SGR reset, so an inner Render() call's reset would
// wipe out the outer style for everything after the first highlighted
// character. Per-character rendering keeps every segment self-contained.
// Only used for non-cursor rows here — the cursor row wraps its whole line
// in a single styleCursor.Render() call, and nesting highlighted text
// inside that would reintroduce exactly this bug for the cursor's own
// background.
func highlightMatches(s string, idxs []int, base lipgloss.Style) string {
	if len(idxs) == 0 {
		return base.Render(s)
	}
	hi := base.Foreground(colorAmber).Underline(true)
	matchSet := make(map[int]bool, len(idxs))
	for _, i := range idxs {
		matchSet[i] = true
	}
	var b strings.Builder
	for i, r := range []rune(s) {
		if matchSet[i] {
			b.WriteString(hi.Render(string(r)))
		} else {
			b.WriteString(base.Render(string(r)))
		}
	}
	return b.String()
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
	return taskAtRow(m, m.cursor)
}

func taskAtRow(m Model, i int) *models.Task {
	if i < 0 || i >= len(m.rows) || m.rows[i].isHeader {
		return nil
	}
	return m.rows[i].task
}

func newFormInputs(defaultList string) [fCount]textinput.Model {
	var inputs [fCount]textinput.Model
	placeholders := [fCount]string{
		"Buy groceries",
		defaultList,
		"morgen, nächsten montag, 2026-07-05",
		"optional notes",
		"https://…",
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
	inputs[fURL].SetValue(t.URL)
	inputs[fRecurrence].SetValue(t.Recurrence)
	return inputs
}

func startOfDay(t time.Time) time.Time {
	y, mo, d := t.Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, t.Location())
}

func loadCachedListEntriesCmd() tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return listNamesMsg{}
		}
		defer s.Close()
		entries, _ := s.GetListEntries(context.Background())
		return listNamesMsg{entries: entries}
	}
}

func loadAllListNamesCmd() tea.Cmd {
	return func() tea.Msg {
		entries, err := reminders.ListListsWithAccounts()
		// persist to SQLite cache so next startup is instant
		if len(entries) > 0 {
			if s, dbErr := store.New(config.DBPath(), config.Shared()); dbErr == nil {
				_ = s.StoreListEntries(context.Background(), entries, "apple")
				s.Close()
			}
		}
		return listNamesMsg{entries: entries, err: err}
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

// Run starts the TUI. openTaskID, if non-empty, pre-selects and opens that
// task's detail popup as soon as tasks finish loading — used by `taskctl
// --task <id>` to jump in directly from another tool's linked entry.
func Run(openTaskID string) error {
	p := tea.NewProgram(newModel(openTaskID), tea.WithAltScreen(), tea.WithMouseAllMotion())
	_, err := p.Run()
	return err
}

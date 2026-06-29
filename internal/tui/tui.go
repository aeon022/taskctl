package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/aeon022/taskctl/internal/reminders"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// ── Views ────────────────────────────────────────────────────────────────────

type view int

const (
	viewList   view = 0
	viewCreate view = 1
)

// ── Form fields ───────────────────────────────────────────────────────────────

const (
	fTitle = 0
	fList  = 1
	fDue   = 2
	fNotes = 3
	fCount = 4
)

var formLabels = [fCount]string{"Title", "List", "Due (YYYY-MM-DD)", "Notes"}

// ── Messages ──────────────────────────────────────────────────────────────────

type tasksLoadedMsg struct{ tasks []models.Task }
type syncDoneMsg struct {
	tasks []models.Task
	err   error
}
type taskSavedMsg struct{ err error }
type taskDeletedMsg struct {
	id  string
	err error
}

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	styleHeader  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	styleSubhead = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styleSep     = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	styleDone    = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Strikethrough(true)
	styleTitle   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styleDue     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleOverdue = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleCursor  = lipgloss.NewStyle().Background(lipgloss.Color("62")).Foreground(lipgloss.Color("230"))
	styleKey     = lipgloss.NewStyle().Foreground(lipgloss.Color("99")).Bold(true)
	styleLabel   = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Width(18)
	styleBox     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")).Padding(1, 2)
	styleErr     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

// ── Model ─────────────────────────────────────────────────────────────────────

type row struct {
	isHeader bool
	label    string
	task     *models.Task
}

type Model struct {
	tasks        []models.Task
	rows         []row
	cursor       int
	view         view
	loading      bool
	syncing      bool
	showDone     bool
	err          error
	width        int
	height       int
	// form
	inputs      [fCount]textinput.Model
	inputIdx    int
	submitting  bool
	editTarget  *models.Task
	// delete confirm
	deleteTarget *models.Task
}

func newModel() Model {
	return Model{loading: true}
}

// ── Init / Update / View ──────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return loadTasks(m.showDone)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tasksLoadedMsg:
		m.tasks = msg.tasks
		m.rows = buildRows(m.tasks)
		m.loading = false
		m.cursor = firstTaskRow(m.rows)

	case syncDoneMsg:
		m.syncing = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.tasks = msg.tasks
			m.rows = buildRows(m.tasks)
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

	case taskDeletedMsg:
		m.deleteTarget = nil
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
			return m, loadTasks(m.showDone)
		}

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	// ── create/edit form ──────────────────────────────────────────────────
	if m.view == viewCreate {
		switch msg.String() {
		case "esc":
			m.view = viewList
			m.editTarget = nil
			return m, nil
		case "tab", "down":
			m.inputs[m.inputIdx].Blur()
			m.inputIdx = (m.inputIdx + 1) % fCount
			return m, m.inputs[m.inputIdx].Focus()
		case "shift+tab", "up":
			m.inputs[m.inputIdx].Blur()
			m.inputIdx = (m.inputIdx - 1 + fCount) % fCount
			return m, m.inputs[m.inputIdx].Focus()
		case "enter":
			if m.inputIdx < fCount-1 {
				m.inputs[m.inputIdx].Blur()
				m.inputIdx++
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

	// ── list view ─────────────────────────────────────────────────────────
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			if m.cursor < len(m.rows) && m.rows[m.cursor].isHeader {
				if m.cursor > 0 {
					m.cursor--
				}
			}
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
			if m.cursor < len(m.rows) && m.rows[m.cursor].isHeader {
				if m.cursor < len(m.rows)-1 {
					m.cursor++
				}
			}
		}

	case "s":
		if !m.syncing {
			m.syncing = true
			m.err = nil
			return m, syncCmd()
		}

	case "c":
		m.showDone = !m.showDone
		return m, loadTasks(m.showDone)

	case " ":
		if t := cursorTask(m); t != nil {
			return m, toggleDoneCmd(t)
		}

	case "n":
		m.view = viewCreate
		m.inputs = newFormInputs("")
		m.editTarget = nil
		m.inputIdx = 0
		return m, m.inputs[fTitle].Focus()

	case "e":
		if t := cursorTask(m); t != nil {
			m.view = viewCreate
			m.inputs = prefillForm(t)
			m.editTarget = t
			m.inputIdx = 0
			return m, m.inputs[fTitle].Focus()
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
	default:
		return m.renderList()
	}
}

// ── Render ────────────────────────────────────────────────────────────────────

func (m Model) renderList() string {
	var b strings.Builder

	// header
	b.WriteString("\n")
	title := styleHeader.Render("taskctl")
	status := ""
	if m.syncing {
		status = styleSubhead.Render("  syncing…")
	}
	b.WriteString("  " + title + status + "\n\n")

	// task rows
	if len(m.rows) == 0 {
		b.WriteString("  No tasks. Run: taskctl sync\n")
	}
	for i, r := range m.rows {
		if r.isHeader {
			b.WriteString("  " + styleHeader.Render(r.label) + "\n")
			b.WriteString("  " + styleSep.Render(strings.Repeat("─", len(r.label)+2)) + "\n")
			continue
		}
		t := r.task
		mark := "○"
		line := ""
		if t.Done() {
			mark = "✓"
			line = styleDone.Render(t.Title)
		} else {
			line = styleTitle.Render(t.Title)
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
		row := fmt.Sprintf("  %s  %s%s", mark, line, due)
		if i == m.cursor {
			row = styleCursor.Render(row)
		}
		b.WriteString(row + "\n")
	}

	// error
	if m.err != nil {
		b.WriteString("\n  " + styleErr.Render(m.err.Error()) + "\n")
	}

	// status bar
	b.WriteString("\n")
	b.WriteString(m.renderStatusBar())
	return b.String()
}

func (m Model) renderStatusBar() string {
	key := func(k string) string { return styleKey.Render(k) }

	if m.deleteTarget != nil {
		return fmt.Sprintf("  Delete %q?  %s confirm  any cancel\n",
			m.deleteTarget.Title, key("y"))
	}

	doneToggle := "show done"
	if m.showDone {
		doneToggle = "hide done"
	}
	return fmt.Sprintf("  %s/%s navigate  %s done  %s new  %s edit  %s delete  %s sync  %s %s  %s quit\n",
		key("↑"), key("↓"),
		key("space"),
		key("n"), key("e"), key("d"),
		key("s"),
		key("c"), doneToggle,
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
	}
	if m.err != nil {
		inner.WriteString("\n" + styleErr.Render(m.err.Error()))
	}
	if m.submitting {
		inner.WriteString("\n" + styleSubhead.Render("Saving…"))
	}

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(styleBox.Render(inner.String()))
	b.WriteString("\n\n")

	key := func(k string) string { return styleKey.Render(k) }
	b.WriteString(fmt.Sprintf("  %s next field  %s next/save  %s save  %s cancel\n",
		key("tab"), key("enter"), key("ctrl+s"), key("esc")))
	return b.String()
}

// ── Cmds ──────────────────────────────────────────────────────────────────────

func loadTasks(showDone bool) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath())
		if err != nil {
			return tasksLoadedMsg{}
		}
		defer s.Close()
		status := "needsAction"
		if showDone {
			status = ""
		}
		tasks, _ := s.ListTasks(context.Background(), store.ListFilter{Status: status})
		return tasksLoadedMsg{tasks}
	}
}

func syncCmd() tea.Cmd {
	return func() tea.Msg {
		tasks, err := reminders.FetchTasks("")
		if err != nil {
			return syncDoneMsg{err: err}
		}
		s, err := store.New(config.DBPath())
		if err != nil {
			return syncDoneMsg{err: err}
		}
		defer s.Close()
		ctx := context.Background()
		_ = s.DeleteBySource(ctx, "apple")
		for i := range tasks {
			_ = s.UpsertTask(ctx, &tasks[i])
		}
		// reload after sync
		status := "needsAction"
		loaded, _ := s.ListTasks(ctx, store.ListFilter{Status: status})
		return syncDoneMsg{tasks: loaded}
	}
}

func saveTaskCmd(inputs [fCount]textinput.Model, editTarget *models.Task) tea.Cmd {
	return func() tea.Msg {
		title := strings.TrimSpace(inputs[fTitle].Value())
		if title == "" {
			return taskSavedMsg{fmt.Errorf("title is required")}
		}
		listName := strings.TrimSpace(inputs[fList].Value())
		dueStr := strings.TrimSpace(inputs[fDue].Value())
		notes := strings.TrimSpace(inputs[fNotes].Value())

		t := &models.Task{
			ID:        "taskctl-" + uuid.New().String(),
			Title:     title,
			List:      listName,
			Notes:     notes,
			Status:    "needsAction",
			Source:    "taskctl",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if dueStr != "" {
			d, err := time.ParseInLocation("2006-01-02", dueStr, time.Local)
			if err != nil {
				return taskSavedMsg{fmt.Errorf("invalid due date %q", dueStr)}
			}
			t.DueDate = &d
		}

		s, err := store.New(config.DBPath())
		if err != nil {
			return taskSavedMsg{err}
		}
		defer s.Close()
		ctx := context.Background()

		if editTarget != nil {
			_ = reminders.DeleteTask(editTarget)
			_ = s.DeleteByID(ctx, editTarget.ID)
		}

		if err := reminders.CreateTask(t); err != nil {
			return taskSavedMsg{err}
		}
		_ = s.UpsertTask(ctx, t)
		return taskSavedMsg{}
	}
}

func deleteTaskCmd(t *models.Task) tea.Cmd {
	return func() tea.Msg {
		if err := reminders.DeleteTask(t); err != nil {
			return taskDeletedMsg{err: err}
		}
		s, err := store.New(config.DBPath())
		if err != nil {
			return taskDeletedMsg{id: t.ID}
		}
		defer s.Close()
		_ = s.DeleteByID(context.Background(), t.ID)
		return taskDeletedMsg{id: t.ID}
	}
}

func toggleDoneCmd(t *models.Task) tea.Cmd {
	return func() tea.Msg {
		var err error
		if t.Done() {
			err = reminders.UncompleteTask(t)
		} else {
			err = reminders.CompleteTask(t)
		}
		if err != nil {
			return taskSavedMsg{err}
		}
		s, sErr := store.New(config.DBPath())
		if sErr == nil {
			defer s.Close()
			newStatus := "completed"
			if t.Done() {
				newStatus = "needsAction"
			}
			t.Status = newStatus
			_ = s.UpsertTask(context.Background(), t)
		}
		return taskSavedMsg{}
	}
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

func firstTaskRow(rows []row) int {
	for i, r := range rows {
		if !r.isHeader {
			return i
		}
	}
	return 0
}

func buildRows(tasks []models.Task) []row {
	var rows []row
	curList := ""
	for i := range tasks {
		t := &tasks[i]
		if t.List != curList {
			curList = t.List
			rows = append(rows, row{isHeader: true, label: curList})
		}
		rows = append(rows, row{task: t})
	}
	return rows
}

func cursorTask(m Model) *models.Task {
	if m.cursor >= len(m.rows) || m.rows[m.cursor].isHeader {
		return nil
	}
	return m.rows[m.cursor].task
}

func newFormInputs(defaultList string) [fCount]textinput.Model {
	var inputs [fCount]textinput.Model
	placeholders := [fCount]string{"Buy groceries", defaultList, "YYYY-MM-DD", "optional notes"}
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
	return inputs
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// Run starts the TUI.
func Run() error {
	p := tea.NewProgram(newModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

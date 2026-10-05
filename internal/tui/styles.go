package tui

import (
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/missionctl-core/uistate"
	"github.com/aeon022/taskctl/internal/config"
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	// Shared across the suite via missionctl-core/theme.
	colorBlue   = theme.BlueV2
	colorGreen  = theme.GreenV2
	colorRed    = theme.RedV2
	colorAmber  = theme.AmberV2
	colorMuted  = theme.MutedV2
	colorSubtle = theme.SubtleV2

	styleHeader  = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	styleSubhead = lipgloss.NewStyle().Foreground(colorMuted)
	styleSep     = lipgloss.NewStyle().Foreground(colorSubtle)
	styleDone    = lipgloss.NewStyle().Foreground(colorMuted).Strikethrough(true)
	styleTitle   = lipgloss.NewStyle()
	styleDue     = lipgloss.NewStyle().Foreground(colorAmber)
	styleToday   = lipgloss.NewStyle().Bold(true).Foreground(colorAmber)
	styleOverdue = lipgloss.NewStyle().Foreground(colorRed)
	styleCursor  = lipgloss.NewStyle().
			Background(theme.SelectedBgV2).
			Foreground(theme.SelectedFgV2).
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
	styleFocusBadge  = lipgloss.NewStyle().Background(colorRed).Foreground(theme.SelectedFgV2).Padding(0, 1)
	styleCountBadge  = lipgloss.NewStyle().Foreground(colorMuted).Background(theme.HoverBgV2).Padding(0, 1)
	styleTitleBar    = lipgloss.NewStyle().Bold(true).Foreground(theme.SelectedFgV2).Background(colorBlue)
)

// ── command palette (":") ────────────────────────────────────────────────────
//
// Types out full words instead of memorizing single-key shortcuts. Reuses
// the exact same key handling every shortcut already goes through (the
// list-view switch in Update) by replaying the mapped keypress through
// Update itself, so behavior is guaranteed identical to typing the key
// directly. Matching logic lives in missionctl-core/palette (shared across
// the suite); this list of commands is taskctl-specific.
var paletteCommands = []palette.Command{
	{Name: "new", Desc: "New task", Key: "n"},
	{Name: "edit", Desc: "Edit selected task", Key: "e"},
	{Name: "delete", Desc: "Delete selected task", Key: "d"},
	{Name: "toggle", Desc: "Toggle done", Key: " "},
	{Name: "detail", Desc: "Task details", Key: "enter"},
	{Name: "postpone", Desc: "Postpone to tomorrow", Key: "S"},
	{Name: "copy", Desc: "Copy title to clipboard", Key: "y"},
	{Name: "undo", Desc: "Undo last action", Key: "u"},
	{Name: "search", Desc: "Search tasks", Key: "/"},
	{Name: "focus", Desc: "Focus mode — today & overdue only", Key: "t"},
	{Name: "overdue", Desc: "Filter — overdue only", Key: "O"},
	{Name: "completed", Desc: "Show / hide completed tasks", Key: "c"},
	{Name: "select", Desc: "Select mode (batch actions)", Key: "v"},
	{Name: "pomodoro", Desc: "Pomodoro timer for selected task", Key: "p"},
	{Name: "stats", Desc: "Stats", Key: "i"},
	{Name: "sync", Desc: "Sync with Apple Reminders", Key: "s"},
	{Name: "help", Desc: "Show help", Key: "?"},
	{Name: "quit", Desc: "Quit taskctl", Key: "q"},
}

func newModel(openTaskID string) Model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = styleSubhead

	si := textinput.New()
	si.Placeholder = "search…"
	si.CharLimit = 80
	si.SetWidth(40) // v2: width 0 clips the placeholder to 1 char

	pi := textinput.New()
	pi.Placeholder = "command…"
	pi.CharLimit = 40
	pi.SetWidth(40) // v2: width 0 clips the placeholder to 1 char

	sti := textinput.New()
	sti.Placeholder = "Subtask title…"
	sti.CharLimit = 200
	sti.SetWidth(40) // v2: width 0 clips the placeholder to 1 char

	var state persistedState
	uistate.Load(config.UIStatePath(), &state)

	return Model{
		loading:      true,
		searchInput:  si,
		paletteInput: pi,
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

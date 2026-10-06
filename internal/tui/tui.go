package tui

import (
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	"github.com/aeon022/taskctl/internal/models"
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
	lastLoad     time.Time // when tasks last arrived; FocusMsg reloads only if stale
	focusLoading bool      // a focus-triggered reload is in flight (keeps cursor, no spinner)
	lastSynced   time.Time // zero = never synced this install
	sp           spinner.Model
	showDone     bool
	err          error
	width        int
	height       int
	contentSized bool // set on the render copy once width/height exclude the app padding (see dims)
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
	// ":" command palette
	inPalette     bool
	paletteInput  textinput.Model
	paletteCursor int
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

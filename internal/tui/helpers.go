package tui

import (
	"context"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/dateutil"
	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/aeon022/taskctl/internal/reminders"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/sahilm/fuzzy"
)

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
	return buildRowsWith(tasks, query, filter, nil)
}

// buildRowsWith is buildRows narrowed further by keep (nil = keep all). The
// rows point into tasks, so callers must pass m.tasks itself.
func buildRowsWith(tasks []models.Task, query string, filter listFilterMode, keep func(*models.Task) bool) []row {
	now := time.Now()
	eod := dateutil.EndOfDay(now)
	sod := dateutil.StartOfDay(now)

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
		if keep != nil && !keep(t) {
			continue
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
		t.SetWidth(60) // v2: width 0 clips the placeholder to 1 char
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

// motionThrottleFilter drops MouseMotionMsg messages arriving <16ms apart.
func motionThrottleFilter() func(tea.Model, tea.Msg) tea.Msg {
	var lastMotion time.Time
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		if _, ok := msg.(tea.MouseMotionMsg); !ok {
			return msg
		}
		now := time.Now()
		if now.Sub(lastMotion) < 16*time.Millisecond {
			return nil
		}
		lastMotion = now
		return msg
	}
}

// Run starts the TUI. openTaskID, if non-empty, pre-selects and opens that
// task's detail popup as soon as tasks finish loading — used by `taskctl
// --task <id>` to jump in directly from another tool's linked entry.
func Run(openTaskID string) error {
	// WithFPS(30) + motionThrottleFilter: all-motion mouse mode re-renders on every
	// pixel of movement, which at 60fps can overwhelm the terminal.
	p := tea.NewProgram(newModel(openTaskID), tea.WithFilter(motionThrottleFilter()), tea.WithFPS(30))
	_, err := p.Run()
	return err
}

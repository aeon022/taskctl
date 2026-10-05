package tui

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/aeon022/taskctl/internal/nlpdate"
	"github.com/aeon022/taskctl/internal/reminders"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/google/uuid"
)

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

// copyToClipboardCmd copies via OSC 52 (works over SSH/tmux) and also shells
// out to pbcopy, which Terminal.app needs since it ignores OSC 52.
func copyToClipboardCmd(text string) tea.Cmd {
	return tea.Batch(tea.SetClipboard(text), func() tea.Msg {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
		return nil
	})
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

// taskFromForm builds the task a submitted form describes. When editing, the
// original's local-only subtasks carry over (the edit path replaces the row
// under a fresh ID, so anything not copied here is lost).
func taskFromForm(inputs [fCount]textinput.Model, editTarget *models.Task) (*models.Task, error) {
	rawTitle := strings.TrimSpace(inputs[fTitle].Value())
	if rawTitle == "" {
		return nil, fmt.Errorf("title is required")
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
	t := &models.Task{
		ID:         "taskctl-" + uuid.New().String(),
		Title:      title,
		Priority:   priority,
		List:       listName,
		Notes:      strings.TrimSpace(inputs[fNotes].Value()),
		URL:        strings.TrimSpace(inputs[fURL].Value()),
		Recurrence: strings.ToLower(strings.TrimSpace(inputs[fRecurrence].Value())),
		Status:     "needsAction",
		Source:     "taskctl",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if editTarget != nil {
		t.Subtasks = editTarget.Subtasks
	}
	if dueStr := strings.TrimSpace(inputs[fDue].Value()); dueStr != "" {
		d, err := nlpdate.Parse(dueStr)
		if err != nil {
			return nil, fmt.Errorf("datum nicht erkannt – versuche: morgen, nächsten montag, 2026-07-05")
		}
		t.DueDate = d
	}
	return t, nil
}

func saveTaskCmd(inputs [fCount]textinput.Model, editTarget *models.Task) tea.Cmd {
	return func() tea.Msg {
		t, err := taskFromForm(inputs, editTarget)
		if err != nil {
			return taskSavedMsg{err}
		}

		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return taskSavedMsg{err}
		}
		defer s.Close()
		ctx := context.Background()

		if editTarget != nil {
			_ = s.DeleteByID(ctx, editTarget.ID)
			go func() { _ = reminders.DeleteTask(editTarget) }()
		}

		// if a same-named task was previously deleted, clear the guard
		_ = s.ClearPendingDelete(ctx, t.Title, t.List)
		// write to local cache immediately → instant UI response
		_ = s.UpsertTask(ctx, t)
		// sync to backend provider in background
		go func() { _ = reminders.CreateTask(t) }()

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

var urlRe = regexp.MustCompile("(?i)https?://[^\\s<>\"')]*")

// firstURL returns the first http(s):// link found in s, or "". Matches on s
// itself: indexing a lowercased copy shifts byte offsets for runes whose
// lowercase form has a different UTF-8 length (e.g. "İ").
func firstURL(s string) string { return urlRe.FindString(s) }

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
		go func() { _ = reminders.DeleteTask(&taskCopy) }()
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
			if wantDone {
				_ = reminders.CompleteTask(&taskCopy)
			} else {
				_ = reminders.UncompleteTask(&taskCopy)
			}
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
			go func() { _ = reminders.CreateTask(spawn) }()
		}
		return toggleDonedMsg{}
	}
}

func postponeCmd(t *models.Task, newDue time.Time) tea.Cmd {
	taskCopy := *t
	return func() tea.Msg {
		if err := reminders.PostponeTask(&taskCopy, newDue); err != nil {
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
		go func() { _ = reminders.CreateTask(t) }()
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
			go func() { _ = reminders.CompleteTask(tc) }()
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
			go func() { _ = reminders.DeleteTask(&copies[i]) }()
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

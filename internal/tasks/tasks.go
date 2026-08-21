// Package tasks holds the "resolve list/task → write local cache → call
// the Reminders provider" sequences shared by the CLI commands
// (cmd/add.go, cmd/done.go) and the MCP server (internal/mcpserver), so
// both entry points stay in lockstep instead of hand-rolling the same
// local-cache-then-provider dance twice.
package tasks

import (
	"context"
	"time"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/aeon022/taskctl/internal/reminders"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/google/uuid"
)

// Create resolves the target list (falling back to the configured default,
// then Reminders' own default list), builds a new task, writes it to the
// local cache, and creates it in Reminders. due may be nil. s may be nil
// (e.g. if store.New failed) — the local cache write is then skipped, but
// the task is still created in Reminders. The returned task is populated
// even on error.
func Create(s *store.Store, title, list, notes, url string, due *time.Time) (*models.Task, error) {
	if list == "" {
		list = config.Active.DefaultList
	}
	if list == "" {
		// Same list CreateTask falls back to — resolve it here too so the
		// local cache entry matches what Apple actually creates.
		list = reminders.DefaultList()
	}

	t := &models.Task{
		ID:        "taskctl-" + uuid.New().String(),
		Title:     title,
		List:      list,
		Notes:     notes,
		URL:       url,
		DueDate:   due,
		Status:    "needsAction",
		Source:    "taskctl",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	ctx := context.Background()
	if s != nil {
		_ = s.ClearPendingDelete(ctx, t.Title, t.List)
		_ = s.UpsertTask(ctx, t)
	}

	if err := reminders.CreateTask(t); err != nil {
		return t, err
	}
	return t, nil
}

// Complete marks title/list as completed in the local cache first (guarded
// by AddPendingStatus so a concurrent sync can't revert it), then completes
// it in Reminders, then clears the guard. s may be nil, in which case the
// local cache steps are skipped.
func Complete(s *store.Store, title, list string) error {
	ctx := context.Background()
	if s != nil {
		tasks, _ := s.ListTasks(ctx, store.ListFilter{List: list, Status: "needsAction"})
		for i := range tasks {
			if tasks[i].Title == title {
				tasks[i].Status = "completed"
				_ = s.UpsertTask(ctx, &tasks[i])
				break
			}
		}
		_ = s.AddPendingStatus(ctx, title, list, "completed")
	}

	if err := reminders.CompleteTask(&models.Task{Title: title, List: list}); err != nil {
		return err
	}

	if s != nil {
		_ = s.ClearPendingStatus(ctx, title, list)
	}
	return nil
}

// Delete removes title/list from the local cache first (guarded by
// AddPendingDelete so a concurrent sync can't re-add it), then deletes it
// in Reminders. s may be nil, in which case the local cache steps are
// skipped.
func Delete(s *store.Store, title, list string) error {
	t := &models.Task{Title: title, List: list}

	ctx := context.Background()
	if s != nil {
		found, _ := s.ListTasks(ctx, store.ListFilter{List: list})
		for i := range found {
			if found[i].Title == title {
				t = &found[i]
				break
			}
		}
		_ = s.DeleteByID(ctx, t.ID)
		_ = s.AddPendingDelete(ctx, t)
	}

	return reminders.DeleteTask(t)
}

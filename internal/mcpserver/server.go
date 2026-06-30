package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/googletasks"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/aeon022/taskctl/internal/reminders"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func Serve() error {
	s := server.NewMCPServer("taskctl", "0.1.0",
		server.WithToolCapabilities(true),
	)
	s.AddTool(toolToday(), handleToday)
	s.AddTool(toolListTasks(), handleListTasks)
	s.AddTool(toolSync(), handleSync)
	s.AddTool(toolCreateTask(), handleCreateTask)
	s.AddTool(toolCompleteTask(), handleCompleteTask)
	s.AddTool(toolDeleteTask(), handleDeleteTask)
	return server.ServeStdio(s)
}

// ── Tool definitions ──────────────────────────────────────────────────────────

func toolToday() mcp.Tool {
	return mcp.NewTool("today_tasks",
		mcp.WithDescription("Get tasks due today or overdue. Good for morning briefings."),
	)
}

func toolListTasks() mcp.Tool {
	return mcp.NewTool("list_tasks",
		mcp.WithDescription("List tasks from the local cache, optionally filtered by list or status."),
		mcp.WithString("list", mcp.Description("Filter by reminder list name")),
		mcp.WithString("status", mcp.Description("'needsAction' (default) or 'completed' or 'all'")),
	)
}

func toolSync() mcp.Tool {
	return mcp.NewTool("sync",
		mcp.WithDescription("Sync tasks from Apple Reminders into the local cache. Call this if task data seems stale."),
		mcp.WithString("list", mcp.Description("Sync only this list (optional)")),
	)
}

func toolCreateTask() mcp.Tool {
	return mcp.NewTool("create_task",
		mcp.WithDescription("Create a new task in Apple Reminders."),
		mcp.WithString("title", mcp.Required(), mcp.Description("Task title")),
		mcp.WithString("list", mcp.Description("Reminder list name")),
		mcp.WithString("due_date", mcp.Description("Due date in YYYY-MM-DD format")),
		mcp.WithString("notes", mcp.Description("Optional notes")),
	)
}

func toolCompleteTask() mcp.Tool {
	return mcp.NewTool("complete_task",
		mcp.WithDescription("Mark a task as completed in Apple Reminders."),
		mcp.WithString("title", mcp.Required(), mcp.Description("Exact task title")),
		mcp.WithString("list", mcp.Description("Reminder list name — helps find the task faster")),
	)
}

func toolDeleteTask() mcp.Tool {
	return mcp.NewTool("delete_task",
		mcp.WithDescription("Delete a task from Apple Reminders. Use list_tasks first to confirm the exact title."),
		mcp.WithString("title", mcp.Required(), mcp.Description("Exact task title")),
		mcp.WithString("list", mcp.Description("Reminder list name")),
	)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func handleToday(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s, err := store.New(config.DBPath())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer s.Close()

	tasks, err := s.ListTasks(context.Background(), store.ListFilter{Status: "needsAction"})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	today := time.Now()
	eod := endOfDay(today)
	var due []models.Task
	for _, t := range tasks {
		if t.DueDate != nil && !t.DueDate.After(eod) {
			due = append(due, t)
		}
	}
	return mcp.NewToolResultText(formatTasks(due, "Tasks due today/overdue")), nil
}

func handleListTasks(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	listName := req.GetString("list", "")
	statusArg := req.GetString("status", "needsAction")
	if statusArg == "all" {
		statusArg = ""
	}

	s, err := store.New(config.DBPath())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer s.Close()

	tasks, err := s.ListTasks(context.Background(), store.ListFilter{List: listName, Status: statusArg})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(formatTasks(tasks, "Tasks")), nil
}

func handleSync(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	listName := req.GetString("list", "")
	tasks, err := reminders.FetchTasks(listName)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	s, err := store.New(config.DBPath())
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer s.Close()
	ctx := context.Background()
	_ = s.DeleteBySource(ctx, "apple")
	s.OverrideWithPendingStatus(ctx, tasks)
	for i := range tasks {
		if s.IsPendingDelete(ctx, tasks[i].Title, tasks[i].List) {
			continue
		}
		_ = s.UpsertTask(ctx, &tasks[i])
	}

	total := len(tasks)
	if listName == "" && googletasks.IsConfigured() && googletasks.IsAuthenticated() {
		if gTasks, err := googletasks.FetchTasks(ctx); err == nil {
			_ = s.DeleteBySource(ctx, "google")
			s.OverrideWithPendingStatus(ctx, gTasks)
			for i := range gTasks {
				if s.IsPendingDelete(ctx, gTasks[i].Title, gTasks[i].List) {
					continue
				}
				_ = s.UpsertTask(ctx, &gTasks[i])
			}
			total += len(gTasks)
		}
	}

	_ = s.RemoveShadowedLocal(ctx)
	_ = s.PrunePendingDeletes(ctx)
	_ = s.PrunePendingStatus(ctx)
	return mcp.NewToolResultText(fmt.Sprintf("Synced %d tasks", total)), nil
}

func handleCreateTask(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	title := req.GetString("title", "")
	listName := req.GetString("list", "")
	dueStr := req.GetString("due_date", "")
	notes := req.GetString("notes", "")

	if title == "" {
		return mcp.NewToolResultError("title is required"), nil
	}

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
			return mcp.NewToolResultError("invalid due_date: " + err.Error()), nil
		}
		t.DueDate = &d
	}

	ctx := context.Background()
	s, err := store.New(config.DBPath())
	if err == nil {
		defer s.Close()
		_ = s.ClearPendingDelete(ctx, t.Title, t.List)
		_ = s.UpsertTask(ctx, t)
	}

	provider := "apple"
	if s != nil {
		provider = s.ProviderForList(ctx, listName)
	}
	var createErr error
	switch provider {
	case "google":
		createErr = googletasks.CreateTask(ctx, t)
	default:
		createErr = reminders.CreateTask(t)
	}
	if createErr != nil {
		return mcp.NewToolResultError("create failed: " + createErr.Error()), nil
	}

	due := ""
	if t.DueDate != nil {
		due = " due " + t.DueDate.Format("Mon, Jan 02 2006")
	}
	return mcp.NewToolResultText(fmt.Sprintf("Created: %s%s", title, due)), nil
}

func handleCompleteTask(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	title := req.GetString("title", "")
	listName := req.GetString("list", "")

	if title == "" {
		return mcp.NewToolResultError("title is required"), nil
	}

	// write SQLite first, then call AppleScript
	ctx := context.Background()
	s, err := store.New(config.DBPath())
	if err == nil {
		defer s.Close()
		tasks, _ := s.ListTasks(ctx, store.ListFilter{List: listName, Status: "needsAction"})
		for i := range tasks {
			if tasks[i].Title == title {
				tasks[i].Status = "completed"
				_ = s.UpsertTask(ctx, &tasks[i])
				break
			}
		}
		_ = s.AddPendingStatus(ctx, title, listName, "completed")
	}

	t := &models.Task{Title: title, List: listName, Source: "apple"}
	if s != nil {
		t.Source = s.ProviderForList(ctx, listName)
	}
	var completeErr error
	switch t.Source {
	case "google":
		completeErr = googletasks.CompleteTask(ctx, t)
	default:
		completeErr = reminders.CompleteTask(t)
	}
	if completeErr != nil {
		return mcp.NewToolResultError("complete failed: " + completeErr.Error()), nil
	}

	if s != nil {
		_ = s.ClearPendingStatus(ctx, title, listName)
	}
	return mcp.NewToolResultText(fmt.Sprintf("Completed: %s", title)), nil
}

func handleDeleteTask(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	title := req.GetString("title", "")
	listName := req.GetString("list", "")

	if title == "" {
		return mcp.NewToolResultError("title is required"), nil
	}

	t := &models.Task{Title: title, List: listName}

	// write SQLite first, then call AppleScript
	ctx := context.Background()
	s, err := store.New(config.DBPath())
	if err == nil {
		defer s.Close()
		tasks, _ := s.ListTasks(ctx, store.ListFilter{List: listName})
		for i := range tasks {
			if tasks[i].Title == title {
				t = &tasks[i]
				break
			}
		}
		_ = s.DeleteByID(ctx, t.ID)
		_ = s.AddPendingDelete(ctx, t)
	}

	var deleteErr error
	switch t.Source {
	case "google":
		deleteErr = googletasks.DeleteTask(ctx, t)
	default:
		deleteErr = reminders.DeleteTask(t)
	}
	if deleteErr != nil {
		return mcp.NewToolResultError("delete failed: " + deleteErr.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Deleted: %s", title)), nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func formatTasks(tasks []models.Task, heading string) string {
	if len(tasks) == 0 {
		return heading + ": no tasks found"
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s (%d):\n\n", heading, len(tasks)))
	curList := ""
	for _, t := range tasks {
		if t.List != curList {
			curList = t.List
			b.WriteString(fmt.Sprintf("[%s]\n", curList))
		}
		mark := "○"
		if t.Done() {
			mark = "✓"
		}
		due := ""
		if t.DueDate != nil {
			due = "  (due " + t.DueDate.Format("Mon Jan 02") + ")"
		}
		b.WriteString(fmt.Sprintf("  %s %s%s\n", mark, t.Title, due))
	}
	return b.String()
}

func endOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 23, 59, 59, 0, t.Location())
}

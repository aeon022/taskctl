package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/aeon022/taskctl/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
)

// setupTestDB points config.DBPath() at a temporary database and seeds it with
// a couple of tasks. Only handlers that read/write the local SQLite DB are
// exercised here — handleSync/handleCreateTask/handleCompleteTask/handleDeleteTask
// also shell out to AppleScript against the real Reminders app and are
// deliberately not smoke-tested.
func setupTestDB(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "taskctl.db")
	config.DBPathOverride = path
	t.Cleanup(func() { config.DBPathOverride = "" })

	s, err := store.New(path)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	due := time.Now()
	ctx := context.Background()
	tasks := []*models.Task{
		{ID: "1", Title: "Due today", List: "Personal", Status: "needsAction", DueDate: &due, Source: "taskctl", CreatedAt: due, UpdatedAt: due},
		{ID: "2", Title: "No due date", List: "Work", Status: "needsAction", Source: "taskctl", CreatedAt: due, UpdatedAt: due},
	}
	for _, task := range tasks {
		if err := s.UpsertTask(ctx, task); err != nil {
			t.Fatalf("UpsertTask: %v", err)
		}
	}
	return s
}

func callTool(t *testing.T, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handler returned an error result: %+v", res.Content)
	}
	return res
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

func TestToolsAreRegisteredWithValidSchema(t *testing.T) {
	for _, tc := range []struct {
		name string
		tool mcp.Tool
	}{
		{"today_tasks", toolToday()},
		{"week_tasks", toolWeekTasks()},
		{"list_tasks", toolListTasks()},
		{"sync", toolSync()},
		{"create_task", toolCreateTask()},
		{"complete_task", toolCompleteTask()},
		{"delete_task", toolDeleteTask()},
	} {
		if tc.tool.Name != tc.name {
			t.Errorf("expected tool name %q, got %q", tc.name, tc.tool.Name)
		}
		if tc.tool.Description == "" {
			t.Errorf("tool %q has no description", tc.name)
		}
	}
}

func TestHandleListTasks(t *testing.T) {
	setupTestDB(t)

	res := callTool(t, handleListTasks, nil)
	text := resultText(t, res)
	if !strings.Contains(text, "Due today") || !strings.Contains(text, "No due date") {
		t.Errorf("expected both seeded tasks in output, got:\n%s", text)
	}
}

func TestHandleListTasksFiltersByList(t *testing.T) {
	setupTestDB(t)

	res := callTool(t, handleListTasks, map[string]any{"list": "Work"})
	text := resultText(t, res)
	if strings.Contains(text, "Due today") {
		t.Errorf("expected Personal-list task to be filtered out, got:\n%s", text)
	}
	if !strings.Contains(text, "No due date") {
		t.Errorf("expected Work-list task in output, got:\n%s", text)
	}
}

func TestHandleToday(t *testing.T) {
	setupTestDB(t)

	res := callTool(t, handleToday, nil)
	text := resultText(t, res)
	if !strings.Contains(text, "Due today") {
		t.Errorf("expected today's task in output, got:\n%s", text)
	}
	if strings.Contains(text, "No due date") {
		t.Errorf("expected task without a due date to be excluded, got:\n%s", text)
	}
}

func TestHandleWeekTasks(t *testing.T) {
	setupTestDB(t)

	res := callTool(t, handleWeekTasks, nil)
	_ = resultText(t, res) // just assert it doesn't error against a real DB
}

package cmd

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aeon022/taskctl/internal/config"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/aeon022/taskctl/internal/store"
)

// seeded points the commands at a temp DB holding a known set of tasks.
func seeded(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	config.DBPathOverride = filepath.Join(t.TempDir(), "t.db")
	t.Cleanup(func() {
		config.DBPathOverride = ""
		flagJSON, listList, listAll, listToday = false, "", false, false
	})

	s, err := store.New(config.DBPath(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	at := func(days int) *time.Time { d := now.AddDate(0, 0, days); return &d }
	for _, tk := range []models.Task{
		{ID: "1", Title: "overdue thing", List: "Home", Status: "needsAction", DueDate: at(-3)},
		{ID: "2", Title: "today thing", List: "Home", Status: "needsAction", DueDate: at(0)},
		{ID: "3", Title: "later thing", List: "Work", Status: "needsAction", DueDate: at(40)},
		{ID: "4", Title: "no date", List: "Work", Status: "needsAction"},
		{ID: "5", Title: "finished", List: "Work", Status: "completed", DueDate: at(-1)},
	} {
		tk.Source, tk.CreatedAt, tk.UpdatedAt = "apple", now, now
		if err := s.UpsertTask(context.Background(), &tk); err != nil {
			t.Fatal(err)
		}
	}
}

// run executes `taskctl <args>` and returns what it printed to stdout.
func run(t *testing.T, args ...string) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if err != nil {
		t.Fatalf("taskctl %v: %v", args, err)
	}
	return string(out)
}

func decode(t *testing.T, out string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	return m
}

func titles(m map[string]any) (out []string) {
	for _, d := range m["data"].([]any) {
		out = append(out, d.(map[string]any)["title"].(string))
	}
	return
}

func TestListDefaultsToOpenTasksGroupedByList(t *testing.T) {
	seeded(t)
	out := run(t, "list")
	for _, want := range []string{"Home", "Work", "overdue thing", "no date", "○"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "finished") {
		t.Errorf("completed tasks must be hidden without --all:\n%s", out)
	}
	if strings.Index(out, "Home") > strings.Index(out, "Work") {
		t.Error("lists must be printed in order")
	}
}

func TestListAllIncludesCompletedWithCheckMark(t *testing.T) {
	seeded(t)
	out := run(t, "list", "--all")
	if !strings.Contains(out, "✓  finished") {
		t.Errorf("--all must show completed tasks with ✓:\n%s", out)
	}
}

func TestListFilters(t *testing.T) {
	seeded(t)
	if got := titles(decode(t, run(t, "list", "--json", "--list", "Work"))); len(got) != 2 {
		t.Errorf("--list Work = %v, want 2 open tasks", got)
	}
	flagJSON, listList = false, ""
	m := decode(t, run(t, "list", "--json", "--today"))
	if got := strings.Join(titles(m), "|"); got != "overdue thing|today thing" {
		t.Errorf("--today = %s, want overdue + due today only", got)
	}
	if m["count"].(float64) != 2 || m["tool"] != "taskctl" || m["command"] != "list" {
		t.Errorf("envelope = %v", m)
	}
}

func TestListEmptyHintsAtSync(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	config.DBPathOverride = filepath.Join(t.TempDir(), "empty.db")
	t.Cleanup(func() { config.DBPathOverride = "" })
	if out := run(t, "list"); !strings.Contains(out, "taskctl sync") {
		t.Errorf("empty cache must point at sync: %q", out)
	}
}

func TestTodayShowsOverdueAndDueOnly(t *testing.T) {
	seeded(t)
	out := run(t, "today")
	if !strings.Contains(out, "[overdue ") || !strings.Contains(out, "today thing") || !strings.Contains(out, "!  overdue thing") {
		t.Errorf("today output:\n%s", out)
	}
	for _, bad := range []string{"later thing", "no date", "finished"} {
		if strings.Contains(out, bad) {
			t.Errorf("today must not list %q:\n%s", bad, out)
		}
	}

	m := decode(t, run(t, "today", "--json"))
	if m["overdue"].(float64) != 1 || m["due_today"].(float64) != 1 || len(titles(m)) != 2 {
		t.Errorf("today json = %v", m)
	}
}

func TestTodayAllClear(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	config.DBPathOverride = filepath.Join(t.TempDir(), "empty.db")
	t.Cleanup(func() { config.DBPathOverride = ""; flagJSON = false })
	if out := run(t, "today"); !strings.Contains(out, "All clear") {
		t.Errorf("out = %q", out)
	}
}

func TestWeekJSONCoversMondayToSundayOnly(t *testing.T) {
	seeded(t)
	m := decode(t, run(t, "week", "--json"))
	from, _ := time.ParseInLocation("2006-01-02", m["from"].(string), time.Local)
	to, _ := time.ParseInLocation("2006-01-02", m["to"].(string), time.Local)
	if from.Weekday() != time.Monday || to.Weekday() != time.Sunday || to.Sub(from) != 6*24*time.Hour {
		t.Errorf("range %v..%v is not Mon–Sun", m["from"], m["to"])
	}
	for _, ti := range titles(m) {
		if ti == "later thing" || ti == "no date" || ti == "finished" {
			t.Errorf("%q is not an open task due this week", ti)
		}
	}
	flagJSON = false
	text := run(t, "week")
	if !strings.HasPrefix(text, "Week ") {
		t.Errorf("text header = %q", text)
	}
}

func TestIsJSONFollowsFlag(t *testing.T) {
	flagJSON = true
	defer func() { flagJSON = false }()
	if !isJSON() {
		t.Error("isJSON must reflect --json")
	}
}

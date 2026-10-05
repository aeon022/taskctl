package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/aeon022/taskctl/internal/models"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "taskctl.db"), false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func task(id, title, list string, mod func(*models.Task)) *models.Task {
	now := time.Now().UTC().Truncate(time.Second)
	t := &models.Task{ID: id, Title: title, List: list, Status: "needsAction", Source: "apple", CreatedAt: now, UpdatedAt: now}
	if mod != nil {
		mod(t)
	}
	return t
}

func mustUpsert(t *testing.T, s *Store, tk *models.Task) {
	t.Helper()
	if err := s.UpsertTask(context.Background(), tk); err != nil {
		t.Fatalf("UpsertTask(%s): %v", tk.ID, err)
	}
}

func TestUpsertRoundTripAndUpdate(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	due := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	in := task("1", "Milch", "Einkauf", func(x *models.Task) {
		x.Notes, x.URL, x.Priority, x.Recurrence = "Bio", "https://x.test", 1, "weekly"
		x.DueDate = &due
		x.Subtasks = []models.Subtask{{Title: "a"}, {Title: "b", Done: true}}
	})
	mustUpsert(t, s, in)

	got, err := s.ListTasks(ctx, ListFilter{})
	if err != nil || len(got) != 1 {
		t.Fatalf("ListTasks = %d tasks, err %v", len(got), err)
	}
	g := got[0]
	if g.Title != "Milch" || g.Notes != "Bio" || g.URL != "https://x.test" || g.Priority != 1 || g.Recurrence != "weekly" {
		t.Errorf("round trip lost fields: %+v", g)
	}
	if g.DueDate == nil || !g.DueDate.Equal(due) {
		t.Errorf("DueDate = %v, want %v", g.DueDate, due)
	}
	if len(g.Subtasks) != 2 || !g.Subtasks[1].Done {
		t.Errorf("Subtasks = %+v", g.Subtasks)
	}

	// same ID again = update in place, not a second row
	in.Title, in.Status = "Hafermilch", "completed"
	mustUpsert(t, s, in)
	got, _ = s.ListTasks(ctx, ListFilter{})
	if len(got) != 1 || got[0].Title != "Hafermilch" || !got[0].Done() {
		t.Errorf("after update: %+v", got)
	}
}

func TestListTasksFilterAndOrder(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	d1 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	mustUpsert(t, s, task("a", "no-prio", "Arbeit", nil))
	mustUpsert(t, s, task("b", "low", "Arbeit", func(x *models.Task) { x.Priority = 9 }))
	mustUpsert(t, s, task("c", "high-late", "Arbeit", func(x *models.Task) { x.Priority = 1; x.DueDate = &d2 }))
	mustUpsert(t, s, task("d", "high-early", "Arbeit", func(x *models.Task) { x.Priority = 1; x.DueDate = &d1 }))
	mustUpsert(t, s, task("e", "done", "Privat", func(x *models.Task) { x.Status = "completed" }))

	got, _ := s.ListTasks(ctx, ListFilter{List: "Arbeit"})
	var titles []string
	for _, g := range got {
		titles = append(titles, g.Title)
	}
	want := []string{"high-early", "high-late", "low", "no-prio"} // priority (0 = last), then due date
	if len(titles) != len(want) {
		t.Fatalf("titles = %v", titles)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("order = %v, want %v", titles, want)
		}
	}
	if got, _ := s.ListTasks(ctx, ListFilter{Status: "completed"}); len(got) != 1 || got[0].ID != "e" {
		t.Errorf("status filter = %+v", got)
	}
}

func TestDeleteAndUpdateDue(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	mustUpsert(t, s, task("1", "x", "L", nil))
	mustUpsert(t, s, task("2", "y", "L", func(x *models.Task) { x.Source = "taskctl" }))

	due := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	if err := s.UpdateDueDate(ctx, "1", &due); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ListTasks(ctx, ListFilter{})
	for _, g := range got {
		if g.ID == "1" && (g.DueDate == nil || !g.DueDate.Equal(due)) {
			t.Errorf("due not updated: %v", g.DueDate)
		}
	}
	if err := s.UpdateDueDate(ctx, "1", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBySource(ctx, "taskctl"); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.ListTasks(ctx, ListFilter{}); len(got) != 1 || got[0].DueDate != nil {
		t.Errorf("after DeleteBySource / clearing due: %+v", got)
	}
	_ = s.DeleteByID(ctx, "1")
	if got, _ = s.ListTasks(ctx, ListFilter{}); len(got) != 0 {
		t.Errorf("DeleteByID left %d rows", len(got))
	}
}

func TestPendingDeleteGuard(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	tk := task("1", "weg", "L", nil)
	if s.IsPendingDelete(ctx, "weg", "L") {
		t.Fatal("pending before add")
	}
	if err := s.AddPendingDelete(ctx, tk); err != nil {
		t.Fatal(err)
	}
	if !s.IsPendingDelete(ctx, "weg", "L") || s.IsPendingDelete(ctx, "weg", "other") {
		t.Error("guard must match title+list exactly")
	}
	_ = s.ClearPendingDelete(ctx, "weg", "L")
	if s.IsPendingDelete(ctx, "weg", "L") {
		t.Error("still pending after clear")
	}
}

func TestPendingStatusOverridesSync(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	if err := s.AddPendingStatus(ctx, "t", "L", "completed"); err != nil {
		t.Fatal(err)
	}
	synced := []models.Task{{Title: "t", List: "L", Status: "needsAction"}, {Title: "t", List: "other", Status: "needsAction"}}
	s.OverrideWithPendingStatus(ctx, synced)
	if synced[0].Status != "completed" || synced[1].Status != "needsAction" {
		t.Errorf("override = %q / %q", synced[0].Status, synced[1].Status)
	}
	_ = s.ClearPendingStatus(ctx, "t", "L")
	again := []models.Task{{Title: "t", List: "L", Status: "needsAction"}}
	s.OverrideWithPendingStatus(ctx, again)
	if again[0].Status != "needsAction" {
		t.Error("override persisted after clear")
	}
}

func TestPruneKeepsRecent(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	_ = s.AddPendingStatus(ctx, "t", "L", "completed")
	_ = s.AddPendingDelete(ctx, task("1", "d", "L", nil))
	if err := s.PrunePendingStatus(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.PrunePendingDeletes(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.IsPendingDelete(ctx, "d", "L") {
		t.Error("recent pending delete was pruned")
	}
	// age them past the 14-day cutoff, then prune
	old := time.Now().AddDate(0, 0, -15).UTC().Format(time.RFC3339)
	s.db.Exec(`UPDATE pending_deletes SET deleted_at=?`, old)
	s.db.Exec(`UPDATE pending_status SET updated_at=?`, old)
	_ = s.PrunePendingDeletes(ctx)
	_ = s.PrunePendingStatus(ctx)
	if s.IsPendingDelete(ctx, "d", "L") {
		t.Error("old pending delete survived prune")
	}
}

func TestRemoveShadowedLocal(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	t0 := time.Now().UTC().Truncate(time.Second)
	// local echo created first, apple copy synced later (different list!) → echo goes
	mustUpsert(t, s, task("l1", "Brot", "Default", func(x *models.Task) { x.Source = "taskctl"; x.CreatedAt = t0 }))
	mustUpsert(t, s, task("a1", "Brot", "Einkauf", func(x *models.Task) { x.CreatedAt = t0.Add(time.Minute) }))
	// pre-existing apple task older than the local one → local one must stay
	mustUpsert(t, s, task("l2", "Käse", "Default", func(x *models.Task) { x.Source = "taskctl"; x.CreatedAt = t0 }))
	mustUpsert(t, s, task("a2", "Käse", "Einkauf", func(x *models.Task) { x.CreatedAt = t0.Add(-time.Hour) }))

	if err := s.RemoveShadowedLocal(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ListTasks(ctx, ListFilter{})
	ids := map[string]bool{}
	for _, g := range got {
		ids[g.ID] = true
	}
	if ids["l1"] || !ids["a1"] {
		t.Errorf("confirmed echo should be removed, apple copy kept: %v", ids)
	}
	if !ids["l2"] || !ids["a2"] {
		t.Errorf("echo older than apple task must stay: %v", ids)
	}
}

func TestListEntriesReplacePerProvider(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	_ = s.StoreListEntries(ctx, []models.ListEntry{{Name: "A", Account: "iCloud", Color: "#FF0000"}, {Name: "B", Account: "iCloud"}}, "apple")
	_ = s.StoreListEntries(ctx, []models.ListEntry{{Name: "G"}}, "google")
	_ = s.StoreListEntries(ctx, []models.ListEntry{{Name: "A2"}}, "apple") // replaces apple only
	got, err := s.GetListEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, e := range got {
		names[e.Name] = e.Provider
	}
	if len(names) != 2 || names["A2"] != "apple" || names["G"] != "google" {
		t.Errorf("entries = %v", names)
	}
}

func TestCountsAndDailyCompletions(t *testing.T) {
	s, ctx := newTestStore(t), context.Background()
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	mustUpsert(t, s, task("1", "heute", "L", func(x *models.Task) { x.Status = "completed"; x.CompletedAt = &now }))
	mustUpsert(t, s, task("2", "gestern", "L", func(x *models.Task) { x.Status = "completed"; x.CompletedAt = &yesterday }))
	mustUpsert(t, s, task("3", "offen", "L", nil))

	_, week, total, err := s.Counts(ctx)
	if err != nil || total != 2 || week != 2 {
		t.Errorf("Counts week=%d total=%d err=%v, want 2/2", week, total, err)
	}
	days, err := s.DailyCompletions(ctx, 3)
	if err != nil || len(days) != 3 {
		t.Fatalf("DailyCompletions = %v, %v", days, err)
	}
	sum := 0
	for _, d := range days {
		sum += d
	}
	if sum != 2 {
		t.Errorf("DailyCompletions = %v, want 2 completions in window", days)
	}
}

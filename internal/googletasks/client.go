package googletasks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/taskctl/internal/models"
	"github.com/google/uuid"
	gtasks "google.golang.org/api/tasks/v1"
	"google.golang.org/api/option"
)

func newService(ctx context.Context) (*gtasks.Service, error) {
	cfg := loadGoogleConfig()
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, fmt.Errorf("google_tasks not configured in ~/.config/taskctl/config.yaml")
	}
	hc, err := newHTTPClient(ctx, cfg.ClientID, cfg.ClientSecret)
	if err != nil {
		return nil, err
	}
	return gtasks.NewService(ctx, option.WithHTTPClient(hc))
}

// FetchTasks returns all incomplete (and recently completed) tasks from all Google Task Lists.
func FetchTasks(ctx context.Context) ([]models.Task, error) {
	svc, err := newService(ctx)
	if err != nil {
		return nil, err
	}

	lists, err := svc.Tasklists.List().MaxResults(100).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("list task lists: %w", err)
	}

	var tasks []models.Task
	for _, tl := range lists.Items {
		call := svc.Tasks.List(tl.Id).
			ShowCompleted(true).
			ShowHidden(false).
			MaxResults(100).
			Context(ctx)
		resp, err := call.Do()
		if err != nil {
			continue
		}
		for _, gt := range resp.Items {
			t := googleTaskToModel(gt, tl.Title)
			tasks = append(tasks, t)
		}
	}
	return tasks, nil
}

// ListTaskLists returns all Google Task List names as ListEntry objects.
func ListTaskLists(ctx context.Context) ([]models.ListEntry, error) {
	svc, err := newService(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := svc.Tasklists.List().MaxResults(100).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	entries := make([]models.ListEntry, 0, len(resp.Items))
	for _, tl := range resp.Items {
		entries = append(entries, models.ListEntry{
			Name:     tl.Title,
			Account:  "Google",
			Provider: "google",
		})
	}
	return entries, nil
}

// CreateTask creates a task in the matching Google Task List.
func CreateTask(ctx context.Context, t *models.Task) error {
	svc, err := newService(ctx)
	if err != nil {
		return err
	}
	tlID, err := findTaskListID(ctx, svc, t.List)
	if err != nil {
		return err
	}
	gt := &gtasks.Task{
		Title: t.Title,
		Notes: t.Notes,
	}
	if t.DueDate != nil {
		gt.Due = t.DueDate.UTC().Format(time.RFC3339)
	}
	_, err = svc.Tasks.Insert(tlID, gt).Context(ctx).Do()
	return err
}

// CompleteTask marks the first matching task as completed.
func CompleteTask(ctx context.Context, t *models.Task) error {
	svc, err := newService(ctx)
	if err != nil {
		return err
	}
	gt, tlID, err := findTask(ctx, svc, t.Title, t.List)
	if err != nil {
		return err
	}
	gt.Status = "completed"
	now := time.Now().UTC().Format(time.RFC3339)
	gt.Completed = &now
	_, err = svc.Tasks.Update(tlID, gt.Id, gt).Context(ctx).Do()
	return err
}

// UncompleteTask marks the first matching task as needsAction.
func UncompleteTask(ctx context.Context, t *models.Task) error {
	svc, err := newService(ctx)
	if err != nil {
		return err
	}
	gt, tlID, err := findTask(ctx, svc, t.Title, t.List)
	if err != nil {
		return err
	}
	gt.Status = "needsAction"
	gt.Completed = nil
	_, err = svc.Tasks.Update(tlID, gt.Id, gt).Context(ctx).Do()
	return err
}

// DeleteTask deletes the first matching task.
func DeleteTask(ctx context.Context, t *models.Task) error {
	svc, err := newService(ctx)
	if err != nil {
		return err
	}
	gt, tlID, err := findTask(ctx, svc, t.Title, t.List)
	if err != nil {
		return err
	}
	return svc.Tasks.Delete(tlID, gt.Id).Context(ctx).Do()
}

// PostponeTask updates the due date of a task.
func PostponeTask(ctx context.Context, t *models.Task, newDue time.Time) error {
	svc, err := newService(ctx)
	if err != nil {
		return err
	}
	gt, tlID, err := findTask(ctx, svc, t.Title, t.List)
	if err != nil {
		return err
	}
	gt.Due = newDue.UTC().Format(time.RFC3339)
	_, err = svc.Tasks.Update(tlID, gt.Id, gt).Context(ctx).Do()
	return err
}

// ── helpers ───────────────────────────────────────────────────────────────────

func findTaskListID(ctx context.Context, svc *gtasks.Service, name string) (string, error) {
	resp, err := svc.Tasklists.List().MaxResults(100).Context(ctx).Do()
	if err != nil {
		return "", err
	}
	for _, tl := range resp.Items {
		if strings.EqualFold(tl.Title, name) {
			return tl.Id, nil
		}
	}
	return "", fmt.Errorf("task list %q not found", name)
}

func findTask(ctx context.Context, svc *gtasks.Service, title, listName string) (*gtasks.Task, string, error) {
	tlID, err := findTaskListID(ctx, svc, listName)
	if err != nil {
		return nil, "", err
	}
	resp, err := svc.Tasks.List(tlID).ShowCompleted(true).MaxResults(100).Context(ctx).Do()
	if err != nil {
		return nil, "", err
	}
	for _, gt := range resp.Items {
		if strings.EqualFold(gt.Title, title) {
			return gt, tlID, nil
		}
	}
	return nil, "", fmt.Errorf("task %q not found in list %q", title, listName)
}

func googleTaskToModel(gt *gtasks.Task, listName string) models.Task {
	t := models.Task{
		ID:         "google-" + gt.Id,
		Title:      gt.Title,
		List:       listName,
		Notes:      gt.Notes,
		Status:     gt.Status,
		ExternalID: gt.Id,
		Source:     "google",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	if t.ID == "google-" {
		t.ID = "google-" + uuid.New().String()
	}
	if gt.Due != "" {
		d, err := time.Parse(time.RFC3339, gt.Due)
		if err == nil {
			dl := d.Local()
			t.DueDate = &dl
		}
	}
	if gt.Completed != nil && *gt.Completed != "" {
		c, err := time.Parse(time.RFC3339, *gt.Completed)
		if err == nil {
			cl := c.Local()
			t.CompletedAt = &cl
		}
	}
	if t.Status == "" {
		t.Status = "needsAction"
	}
	return t
}

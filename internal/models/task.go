package models

import "time"

type Task struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	List        string     `json:"list"`
	Notes       string     `json:"notes"`
	Status      string     `json:"status"` // "needsAction" | "completed"
	DueDate     *time.Time `json:"due_date,omitempty"`
	Priority    int        `json:"priority"` // 0=none, 1=high, 5=medium, 9=low
	Recurrence  string     `json:"recurrence"` // "" | "daily" | "weekly" | "monthly"
	ExternalID  string     `json:"external_id"`
	Source      string     `json:"source"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

func (t *Task) Done() bool { return t.Status == "completed" }

func (t *Task) SpawnDate() time.Time {
	base := time.Now()
	if t.DueDate != nil {
		base = *t.DueDate
	}
	switch t.Recurrence {
	case "daily":
		return base.AddDate(0, 0, 1)
	case "weekly":
		return base.AddDate(0, 0, 7)
	case "monthly":
		return base.AddDate(0, 1, 0)
	}
	return base.AddDate(0, 0, 1)
}

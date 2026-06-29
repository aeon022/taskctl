package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/aeon022/taskctl/internal/models"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_journal=WAL&_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &Store{db: db}
	return s, s.migrate()
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id           TEXT PRIMARY KEY,
			title        TEXT NOT NULL,
			list         TEXT NOT NULL DEFAULT '',
			notes        TEXT NOT NULL DEFAULT '',
			status       TEXT NOT NULL DEFAULT 'needsAction',
			due_date     TEXT,
			priority     INTEGER NOT NULL DEFAULT 0,
			recurrence   TEXT NOT NULL DEFAULT '',
			external_id  TEXT NOT NULL DEFAULT '',
			source       TEXT NOT NULL DEFAULT 'apple',
			created_at   TEXT NOT NULL,
			updated_at   TEXT NOT NULL,
			completed_at TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_tasks_list   ON tasks(list);
		CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
		CREATE INDEX IF NOT EXISTS idx_tasks_due    ON tasks(due_date);
		CREATE TABLE IF NOT EXISTS pending_deletes (
			title      TEXT NOT NULL,
			list       TEXT NOT NULL,
			deleted_at TEXT NOT NULL,
			PRIMARY KEY (title, list)
		);
	`)
	if err != nil {
		return err
	}
	// add recurrence column to existing tables (ignored if already present)
	_, _ = s.db.Exec(`ALTER TABLE tasks ADD COLUMN recurrence TEXT NOT NULL DEFAULT ''`)
	return nil
}

func (s *Store) UpsertTask(ctx context.Context, t *models.Task) error {
	var due, completedAt *string
	if t.DueDate != nil {
		v := t.DueDate.UTC().Format(time.RFC3339)
		due = &v
	}
	if t.CompletedAt != nil {
		v := t.CompletedAt.UTC().Format(time.RFC3339)
		completedAt = &v
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tasks (id,title,list,notes,status,due_date,priority,recurrence,external_id,source,created_at,updated_at,completed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title, list=excluded.list, notes=excluded.notes,
			status=excluded.status, due_date=excluded.due_date, priority=excluded.priority,
			recurrence=excluded.recurrence,
			updated_at=excluded.updated_at, completed_at=excluded.completed_at
	`,
		t.ID, t.Title, t.List, t.Notes, t.Status, due, t.Priority, t.Recurrence,
		t.ExternalID, t.Source,
		t.CreatedAt.UTC().Format(time.RFC3339),
		t.UpdatedAt.UTC().Format(time.RFC3339),
		completedAt,
	)
	return err
}

type ListFilter struct {
	List   string
	Status string // "" = all, "needsAction", "completed"
}

func (s *Store) ListTasks(ctx context.Context, f ListFilter) ([]models.Task, error) {
	query := `SELECT id,title,list,notes,status,due_date,priority,recurrence,external_id,source,created_at,updated_at,completed_at FROM tasks WHERE 1=1`
	var args []any
	if f.List != "" {
		query += ` AND list = ?`
		args = append(args, f.List)
	}
	if f.Status != "" {
		query += ` AND status = ?`
		args = append(args, f.Status)
	}
	query += ` ORDER BY list, CASE WHEN priority=0 THEN 99 ELSE priority END, COALESCE(due_date,'9999'), title`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTasks(rows)
}

func (s *Store) ListNames(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT list FROM tasks ORDER BY list`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

func (s *Store) UpdateDueDate(ctx context.Context, id string, due *time.Time) error {
	var v *string
	if due != nil {
		str := due.UTC().Format(time.RFC3339)
		v = &str
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET due_date=?, updated_at=? WHERE id=?`,
		v, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// DailyCompletions returns completion counts for the last `days` days (oldest first).
func (s *Store) DailyCompletions(ctx context.Context, days int) ([]int, error) {
	since := time.Now().AddDate(0, 0, -(days - 1))
	sinceStr := time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, time.UTC).Format(time.RFC3339)

	rows, err := s.db.QueryContext(ctx, `
		SELECT DATE(completed_at) as day, COUNT(*) as cnt
		FROM tasks
		WHERE status = 'completed' AND completed_at >= ?
		GROUP BY day ORDER BY day
	`, sinceStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDay := make(map[string]int)
	for rows.Next() {
		var day string
		var cnt int
		if err := rows.Scan(&day, &cnt); err != nil {
			continue
		}
		byDay[day] = cnt
	}

	counts := make([]int, days)
	for i := range days {
		d := time.Now().AddDate(0, 0, -(days-1-i))
		key := fmt.Sprintf("%04d-%02d-%02d", d.Year(), d.Month(), d.Day())
		counts[i] = byDay[key]
	}
	return counts, rows.Err()
}

func (s *Store) Counts(ctx context.Context) (today, week, total int, err error) {
	now := time.Now()
	todayStr := fmt.Sprintf("%04d-%02d-%02d", now.Year(), now.Month(), now.Day())
	weekAgo := now.AddDate(0, 0, -7).UTC().Format(time.RFC3339)

	row := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE status='completed' AND DATE(completed_at)=?`, todayStr)
	_ = row.Scan(&today)

	row = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE status='completed' AND completed_at>=?`, weekAgo)
	_ = row.Scan(&week)

	row = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE status='completed'`)
	_ = row.Scan(&total)
	return
}

func (s *Store) DeleteByID(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteBySource(ctx context.Context, source string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE source = ?`, source)
	return err
}

func (s *Store) AddPendingDelete(ctx context.Context, t *models.Task) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO pending_deletes (title, list, deleted_at) VALUES (?,?,?)`,
		t.Title, t.List, time.Now().UTC().Format(time.RFC3339))
	return err
}

// IsPendingDelete returns true if a task with this title+list was user-deleted
// and should not be re-added by sync.
func (s *Store) IsPendingDelete(ctx context.Context, title, list string) bool {
	var n int
	_ = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pending_deletes WHERE title=? AND list=?`, title, list).Scan(&n)
	return n > 0
}

// ClearPendingDelete removes a task from the pending_deletes guard
// (call when a new task with the same title+list is intentionally created).
func (s *Store) ClearPendingDelete(ctx context.Context, title, list string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM pending_deletes WHERE title=? AND list=?`, title, list)
	return err
}

// PrunePendingDeletes removes entries older than 14 days.
func (s *Store) PrunePendingDeletes(ctx context.Context) error {
	cutoff := time.Now().AddDate(0, 0, -14).UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_deletes WHERE deleted_at < ?`, cutoff)
	return err
}

// RemoveShadowedLocal deletes taskctl-created tasks that now have an apple
// counterpart with the same title+list, meaning the background sync succeeded.
func (s *Store) RemoveShadowedLocal(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM tasks
		WHERE source = 'taskctl'
		  AND (title || '|' || list) IN (
		      SELECT title || '|' || list FROM tasks WHERE source = 'apple'
		  )
	`)
	return err
}

func scanTasks(rows *sql.Rows) ([]models.Task, error) {
	var tasks []models.Task
	for rows.Next() {
		var t models.Task
		var due, completedAt sql.NullString
		var createdStr, updatedStr string
		if err := rows.Scan(
			&t.ID, &t.Title, &t.List, &t.Notes, &t.Status, &due, &t.Priority, &t.Recurrence,
			&t.ExternalID, &t.Source, &createdStr, &updatedStr, &completedAt,
		); err != nil {
			return nil, err
		}
		t.CreatedAt = parseTime(createdStr)
		t.UpdatedAt = parseTime(updatedStr)
		if due.Valid && due.String != "" {
			d := parseTime(due.String)
			t.DueDate = &d
		}
		if completedAt.Valid && completedAt.String != "" {
			c := parseTime(completedAt.String)
			t.CompletedAt = &c
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t.Local()
}

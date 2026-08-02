package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aeon022/missionctl-core/syncdir"
	"github.com/aeon022/taskctl/internal/models"
	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

// taskctl opens a fresh *Store per operation rather than holding one open
// for the process's lifetime, and flock(2) isn't reentrant within a
// process — locks reference-counts the real OS-level lock per path so the
// same process's own concurrent/sequential opens don't conflict with
// themselves; only the first open of a path acquires it for real, and only
// the last matching Close() releases it. A conflict is reported only when
// a genuinely different process holds it.
var (
	lockMu sync.Mutex
	locks  = map[string]*lockEntry{}
)

type lockEntry struct {
	lock  *syncdir.Lock
	count int
}

func acquireLock(path string) error {
	lockMu.Lock()
	defer lockMu.Unlock()
	e, ok := locks[path]
	if !ok {
		l, err := syncdir.Acquire(path)
		if err != nil {
			return err
		}
		e = &lockEntry{lock: l}
		locks[path] = e
	}
	e.count++
	return nil
}

func releaseLock(path string) {
	lockMu.Lock()
	defer lockMu.Unlock()
	e, ok := locks[path]
	if !ok {
		return
	}
	e.count--
	if e.count == 0 {
		e.lock.Release()
		delete(locks, path)
	}
}

// New opens the database at path. shared must reflect whether path is a
// user-configured (possibly folder-synced) directory rather than the
// tool's private default — see config.Shared.
func New(path string, shared bool) (*Store, error) {
	if isPlaceholder, placeholder := syncdir.ICloudPlaceholder(path); isPlaceholder {
		return nil, fmt.Errorf("%s hasn't finished downloading from iCloud yet (found %s) — open Finder and download it, or disable \"Optimize Mac Storage\" for this folder", path, placeholder)
	}

	if err := acquireLock(path); err != nil {
		if errors.Is(err, syncdir.ErrLocked) {
			return nil, fmt.Errorf("taskctl is already running elsewhere, or a previous session crashed — remove %s.lock if you're sure nothing else is using it", path)
		}
		return nil, err
	}

	db, err := sql.Open("sqlite", path+"?_journal="+syncdir.JournalMode(shared)+"&_timeout=5000")
	if err != nil {
		releaseLock(path)
		return nil, fmt.Errorf("open db: %w", err)
	}
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		releaseLock(path)
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	err := s.db.Close()
	releaseLock(s.path)
	return err
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id           TEXT PRIMARY KEY,
			title        TEXT NOT NULL,
			list         TEXT NOT NULL DEFAULT '',
			notes        TEXT NOT NULL DEFAULT '',
			url          TEXT NOT NULL DEFAULT '',
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
		CREATE TABLE IF NOT EXISTS lists (
			name     TEXT NOT NULL,
			account  TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT 'apple',
			PRIMARY KEY (name, account)
		);
		CREATE TABLE IF NOT EXISTS pending_status (
			title      TEXT NOT NULL,
			list       TEXT NOT NULL,
			status     TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (title, list)
		);
	`)
	if err != nil {
		return err
	}
	// add columns to existing tables (ignored if already present)
	_, _ = s.db.Exec(`ALTER TABLE tasks ADD COLUMN recurrence TEXT NOT NULL DEFAULT ''`)
	_, _ = s.db.Exec(`ALTER TABLE tasks ADD COLUMN url TEXT NOT NULL DEFAULT ''`)
	_, _ = s.db.Exec(`ALTER TABLE lists ADD COLUMN provider TEXT NOT NULL DEFAULT 'apple'`)
	_, _ = s.db.Exec(`ALTER TABLE lists ADD COLUMN color TEXT NOT NULL DEFAULT ''`)
	_, _ = s.db.Exec(`ALTER TABLE tasks ADD COLUMN subtasks TEXT NOT NULL DEFAULT '[]'`)
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
	subtasks, err := json.Marshal(t.Subtasks)
	if err != nil {
		return fmt.Errorf("marshal subtasks: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO tasks (id,title,list,notes,url,status,due_date,priority,recurrence,subtasks,external_id,source,created_at,updated_at,completed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title, list=excluded.list, notes=excluded.notes, url=excluded.url,
			status=excluded.status, due_date=excluded.due_date, priority=excluded.priority,
			recurrence=excluded.recurrence, subtasks=excluded.subtasks,
			updated_at=excluded.updated_at, completed_at=excluded.completed_at
	`,
		t.ID, t.Title, t.List, t.Notes, t.URL, t.Status, due, t.Priority, t.Recurrence, string(subtasks),
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
	query := `SELECT id,title,list,notes,url,status,due_date,priority,recurrence,subtasks,external_id,source,created_at,updated_at,completed_at FROM tasks WHERE 1=1`
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
		d := time.Now().AddDate(0, 0, -(days - 1 - i))
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

// StoreListEntries replaces all entries for a given provider.
func (s *Store) StoreListEntries(ctx context.Context, entries []models.ListEntry, provider string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM lists WHERE provider=?`, provider); err != nil {
		tx.Rollback()
		return err
	}
	for _, e := range entries {
		p := e.Provider
		if p == "" {
			p = provider
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO lists (name, account, provider, color) VALUES (?,?,?,?)`,
			e.Name, e.Account, p, e.Color); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetListEntries(ctx context.Context) ([]models.ListEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, account, provider, color FROM lists ORDER BY name, account`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []models.ListEntry
	for rows.Next() {
		var e models.ListEntry
		if err := rows.Scan(&e.Name, &e.Account, &e.Provider, &e.Color); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ProviderForList returns the provider ("apple" | "google") for a given list name.
func (s *Store) ProviderForList(ctx context.Context, listName string) string {
	var p string
	_ = s.db.QueryRowContext(ctx,
		`SELECT provider FROM lists WHERE name=? LIMIT 1`, listName).Scan(&p)
	if p == "" {
		return "apple"
	}
	return p
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

func (s *Store) AddPendingStatus(ctx context.Context, title, list, status string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO pending_status (title, list, status, updated_at) VALUES (?,?,?,?)`,
		title, list, status, time.Now().UTC().Format(time.RFC3339))
	return err
}

// OverrideWithPendingStatus updates a task slice with any locally-pending
// status changes so that sync cannot revert them.
func (s *Store) OverrideWithPendingStatus(ctx context.Context, tasks []models.Task) {
	rows, err := s.db.QueryContext(ctx, `SELECT title, list, status FROM pending_status`)
	if err != nil {
		return
	}
	defer rows.Close()
	pending := make(map[string]string)
	for rows.Next() {
		var title, list, status string
		if rows.Scan(&title, &list, &status) == nil {
			pending[title+"|"+list] = status
		}
	}
	for i := range tasks {
		if st, ok := pending[tasks[i].Title+"|"+tasks[i].List]; ok {
			tasks[i].Status = st
		}
	}
}

func (s *Store) ClearPendingStatus(ctx context.Context, title, list string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM pending_status WHERE title=? AND list=?`, title, list)
	return err
}

func (s *Store) PrunePendingStatus(ctx context.Context) error {
	cutoff := time.Now().AddDate(0, 0, -14).UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_status WHERE updated_at < ?`, cutoff)
	return err
}

// PrunePendingDeletes removes entries older than 14 days.
func (s *Store) PrunePendingDeletes(ctx context.Context) error {
	cutoff := time.Now().AddDate(0, 0, -14).UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_deletes WHERE deleted_at < ?`, cutoff)
	return err
}

// RemoveShadowedLocal deletes taskctl-created tasks that now have an apple
// counterpart synced AFTER the local task was created — meaning the background
// CreateTask goroutine succeeded and the sync confirmed it.
// We compare created_at so a pre-existing apple task (older than our local one)
// does NOT cause the new local task to be deleted.
//
// Matches by title only, NOT list: the local row is just an optimistic echo
// of "this will show up in Apple soon", and it can land in a different list
// than the one it was created with (empty-list default-list resolution,
// a CreateTask failure the user then redid by hand in Reminders.app, etc).
// Once any apple-sourced task with the same title exists, the echo has
// served its purpose — keeping it around only produces a phantom duplicate.
func (s *Store) RemoveShadowedLocal(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM tasks
		WHERE source = 'taskctl'
		  AND id IN (
		      SELECT tc.id
		      FROM tasks tc
		      JOIN tasks ap
		        ON tc.title = ap.title
		       AND ap.source = 'apple'
		       AND ap.created_at >= tc.created_at
		  )
	`)
	return err
}

func scanTasks(rows *sql.Rows) ([]models.Task, error) {
	var tasks []models.Task
	for rows.Next() {
		var t models.Task
		var due, completedAt sql.NullString
		var createdStr, updatedStr, subtasksStr string
		if err := rows.Scan(
			&t.ID, &t.Title, &t.List, &t.Notes, &t.URL, &t.Status, &due, &t.Priority, &t.Recurrence, &subtasksStr,
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
		_ = json.Unmarshal([]byte(subtasksStr), &t.Subtasks)
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t.Local()
}

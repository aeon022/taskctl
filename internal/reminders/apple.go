package reminders

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aeon022/taskctl/internal/models"
	"github.com/google/uuid"
)

// swiftScript fetches all reminders via EventKit.
// Usage: swift fetch_reminders.swift [LIST_NAME]  (omit LIST_NAME for all lists)
const swiftScript = `#!/usr/bin/swift
import EventKit
import Foundation

let args = CommandLine.arguments
let filterList = args.count > 1 ? args[1] : ""

let store = EKEventStore()
let sema = DispatchSemaphore(value: 0)

store.requestFullAccessToReminders { granted, _ in
    guard granted else { fputs("Reminders access denied\n", stderr); sema.signal(); return }

    let cals = store.calendars(for: .reminder).filter { cal in
        filterList.isEmpty || cal.title == filterList
    }
    let pred = store.predicateForReminders(in: cals)

    store.fetchReminders(matching: pred) { reminders in
        let iso = ISO8601DateFormatter()
        iso.formatOptions = [.withInternetDateTime]

        for r in reminders ?? [] {
            let title     = r.title ?? ""
            let list      = r.calendar?.title ?? ""
            let notes     = r.notes ?? ""
            let done      = r.isCompleted ? "1" : "0"
            let uid       = r.calendarItemExternalIdentifier ?? ""
            let priority  = r.priority

            var dueStr = ""
            if let dc = r.dueDateComponents,
               let yr = dc.year, let mo = dc.month, let dy = dc.day {
                let hr = dc.hour ?? 0
                let mn = dc.minute ?? 0
                dueStr = String(format: "%04d-%02d-%02dT%02d:%02d:00", yr, mo, dy, hr, mn)
            }

            var completedStr = ""
            if r.isCompleted, let cd = r.completionDate {
                completedStr = iso.string(from: cd)
            }

            print("TITLE:\(title)\nLIST:\(list)\nNOTES:\(notes)\nDONE:\(done)\nUID:\(uid)\nDUE:\(dueStr)\nPRIORITY:\(priority)\nCOMPLETED:\(completedStr)\n---TASK---")
        }
        sema.signal()
    }
}
sema.wait()
`

func scriptPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "taskctl", "fetch_reminders.swift")
}

func ensureScript() (string, error) {
	p := scriptPath()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return "", err
	}
	existing, _ := os.ReadFile(p)
	if string(existing) != swiftScript {
		if err := os.WriteFile(p, []byte(swiftScript), 0644); err != nil {
			return "", err
		}
	}
	return p, nil
}

// FetchTasks returns all reminders, optionally filtered by list name.
func FetchTasks(listName string) ([]models.Task, error) {
	if _, err := exec.LookPath("swift"); err == nil {
		return fetchViaEventKit(listName)
	}
	return fetchViaAppleScript(listName)
}

func fetchViaEventKit(listName string) ([]models.Task, error) {
	script, err := ensureScript()
	if err != nil {
		return fetchViaAppleScript(listName)
	}
	args := []string{script}
	if listName != "" {
		args = append(args, listName)
	}
	cmd := exec.Command("swift", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("swift: %s", string(exitErr.Stderr))
		}
		return nil, err
	}
	return parseTasks(strings.TrimSpace(string(out))), nil
}

// ListEntry holds a reminder list name together with its account name.
type ListEntry struct {
	Name    string
	Account string
}

// ListLists returns all reminder list names (without account info).
func ListLists() ([]string, error) {
	entries, err := ListListsWithAccounts()
	if err != nil {
		return nil, err
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out, nil
}

// ListListsWithAccounts returns all reminder lists with their account names.
func ListListsWithAccounts() ([]ListEntry, error) {
	script := `
tell application "Reminders"
	set output to ""
	repeat with a in accounts
		set aName to name of a
		repeat with l in lists of a
			set output to output & (name of l) & "|" & aName & linefeed
		end repeat
	end repeat
	return output
end tell`
	out, err := runAppleScript(script)
	if err != nil {
		return nil, err
	}
	var entries []ListEntry
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		e := ListEntry{Name: strings.TrimSpace(parts[0])}
		if len(parts) == 2 {
			e.Account = strings.TrimSpace(parts[1])
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// CreateTask creates a new reminder in Apple Reminders.
func CreateTask(t *models.Task) error {
	listName := t.List
	if listName == "" {
		listName = defaultList()
	}
	dueLine := ""
	if t.DueDate != nil {
		iso := t.DueDate.Format("2006-01-02T15:04:05")
		dueLine = fmt.Sprintf(`set due date of newTask to (current date) + ((do shell script "date -jf '%%Y-%%m-%%dT%%H:%%M:%%S' '%s' '+%%s'") as integer - (do shell script "date '+%%s'") as integer)`, iso)
	}
	notesLine := ""
	if t.Notes != "" {
		notesLine = fmt.Sprintf(`set body of newTask to "%s"`, escapeAS(t.Notes))
	}
	prioLine := ""
	if t.Priority > 0 {
		prioLine = fmt.Sprintf(`set priority of newTask to %d`, t.Priority)
	}
	script := fmt.Sprintf(`
tell application "Reminders"
	set theList to list "%s"
	set newTask to make new reminder in theList with properties {name:"%s"}
	%s
	%s
	%s
end tell
`, escapeAS(listName), escapeAS(t.Title), dueLine, notesLine, prioLine)
	_, err := runAppleScript(script)
	return err
}

// CompleteTask marks a reminder as completed, searching all accounts.
func CompleteTask(t *models.Task) error {
	listName := t.List
	if listName == "" {
		listName = defaultList()
	}
	script := fmt.Sprintf(`
tell application "Reminders"
	repeat with a in accounts
		try
			set theList to list "%s" of a
			set matchedTasks to (reminders in theList whose name is "%s" and completed is false)
			if (count of matchedTasks) > 0 then
				set completed of item 1 of matchedTasks to true
				return
			end if
		end try
	end repeat
end tell
`, escapeAS(listName), escapeAS(t.Title))
	_, err := runAppleScript(script)
	return err
}

// UncompleteTask marks a reminder as not completed, searching all accounts.
func UncompleteTask(t *models.Task) error {
	listName := t.List
	if listName == "" {
		listName = defaultList()
	}
	script := fmt.Sprintf(`
tell application "Reminders"
	repeat with a in accounts
		try
			set theList to list "%s" of a
			set matchedTasks to (reminders in theList whose name is "%s" and completed is true)
			if (count of matchedTasks) > 0 then
				set completed of item 1 of matchedTasks to false
				return
			end if
		end try
	end repeat
end tell
`, escapeAS(listName), escapeAS(t.Title))
	_, err := runAppleScript(script)
	return err
}

// PostponeTask updates the due date of a reminder, searching all accounts.
func PostponeTask(t *models.Task, newDue time.Time) error {
	listName := t.List
	if listName == "" {
		listName = defaultList()
	}
	iso := newDue.Format("2006-01-02T15:04:05")
	script := fmt.Sprintf(`
set newDate to (current date) + ((do shell script "date -jf '%%Y-%%m-%%dT%%H:%%M:%%S' '%s' '+%%s'") as integer - (do shell script "date '+%%s'") as integer)
tell application "Reminders"
	repeat with a in accounts
		try
			set theList to list "%s" of a
			set matchedTasks to (reminders in theList whose name is "%s" and completed is false)
			if (count of matchedTasks) > 0 then
				set due date of item 1 of matchedTasks to newDate
				return
			end if
		end try
	end repeat
end tell
`, iso, escapeAS(listName), escapeAS(t.Title))
	_, err := runAppleScript(script)
	return err
}

// DeleteTask deletes a reminder, searching all accounts.
func DeleteTask(t *models.Task) error {
	listName := t.List
	if listName == "" {
		listName = defaultList()
	}
	script := fmt.Sprintf(`
tell application "Reminders"
	repeat with a in accounts
		try
			set theList to list "%s" of a
			set matchedTasks to (reminders in theList whose name is "%s")
			if (count of matchedTasks) > 0 then
				delete item 1 of matchedTasks
				return
			end if
		end try
	end repeat
end tell
`, escapeAS(listName), escapeAS(t.Title))
	_, err := runAppleScript(script)
	return err
}

func fetchViaAppleScript(listName string) ([]models.Task, error) {
	listFilter := ""
	if listName != "" {
		listFilter = fmt.Sprintf(`set theList to list "%s"
		set allReminders to reminders of theList`, escapeAS(listName))
	} else {
		listFilter = `set allReminders to {}
		repeat with a in accounts
			repeat with l in lists of a
				set allReminders to allReminders & (reminders of l)
			end repeat
		end repeat`
	}
	script := fmt.Sprintf(`
tell application "Reminders"
	%s
	set output to ""
	repeat with r in allReminders
		set rTitle to name of r
		set rList to name of containing list of r
		set rNotes to ""
		try
			if body of r is not missing value then set rNotes to body of r
		end try
		set rDone to "0"
		if completed of r then set rDone to "1"
		set rDue to ""
		try
			if due date of r is not missing value then
				set d to due date of r
				set rDue to ((year of d) as text) & "-"
			end if
		end try
		set output to output & "TITLE:" & rTitle & "\nLIST:" & rList & "\nNOTES:" & rNotes & "\nDONE:" & rDone & "\nUID:\nDUE:" & rDue & "\nPRIORITY:0\nCOMPLETED:\n---TASK---\n"
	end repeat
	return output
end tell
`, listFilter)
	out, err := runAppleScript(script)
	if err != nil {
		return nil, fmt.Errorf("applescript: %w", err)
	}
	return parseTasks(out), nil
}

func parseTasks(raw string) []models.Task {
	var tasks []models.Task
	for _, block := range strings.Split(raw, "---TASK---") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		t := models.Task{
			Source:    "apple",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Status:    "needsAction",
		}
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			key, val, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			val = strings.TrimSpace(val)
			switch key {
			case "TITLE":
				t.Title = val
			case "LIST":
				t.List = val
			case "NOTES":
				t.Notes = val
			case "DONE":
				if val == "1" {
					t.Status = "completed"
				}
			case "UID":
				t.ExternalID = val
				if val != "" {
					t.ID = "apple-" + val
				}
			case "DUE":
				if val != "" {
					d, err := time.ParseInLocation("2006-01-02T15:04:05", val, time.Local)
					if err == nil {
						t.DueDate = &d
					}
				}
			case "PRIORITY":
				// ignore Apple-side priority; taskctl uses !! / ! title prefix instead
			case "COMPLETED":
				if val != "" {
					c, err := time.Parse(time.RFC3339, val)
					if err == nil {
						cl := c.Local()
						t.CompletedAt = &cl
					}
				}
			}
		}
		if t.Title == "" {
			continue
		}
		if t.ID == "" {
			t.ID = "apple-" + uuid.New().String()
		}
		tasks = append(tasks, t)
	}
	return tasks
}

func defaultList() string {
	script := `tell application "Reminders" to return name of default list`
	out, err := runAppleScript(script)
	if err != nil || strings.TrimSpace(out) == "" {
		return "Reminders"
	}
	return strings.TrimSpace(out)
}

// NotifyDueTasks sends a macOS notification if tasks are due today or overdue.
func NotifyDueTasks(tasks []models.Task) {
	today := time.Now()
	eod := time.Date(today.Year(), today.Month(), today.Day(), 23, 59, 59, 0, time.Local)
	var titles []string
	for _, t := range tasks {
		if !t.Done() && t.DueDate != nil && !t.DueDate.After(eod) {
			titles = append(titles, t.Title)
		}
	}
	if len(titles) == 0 {
		return
	}
	msg := fmt.Sprintf("%d task(s) due: %s", len(titles), strings.Join(titles[:min(3, len(titles))], ", "))
	if len(titles) > 3 {
		msg += "…"
	}
	script := fmt.Sprintf(`display notification "%s" with title "taskctl — Due Today" sound name "Ping"`,
		escapeAS(msg))
	_ = exec.Command("osascript", "-e", script).Run()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func runAppleScript(script string) (string, error) {
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("osascript: %s", string(exitErr.Stderr))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func escapeAS(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

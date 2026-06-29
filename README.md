# taskctl

Local-first task manager for macOS. Syncs with Apple Reminders via EventKit — iCloud, Exchange/Office365, and Google accounts all work. Single Go binary, TUI + CLI, MCP-ready.

---

## Install

```bash
./setup.sh
```

Builds the binary and installs it to `~/.local/bin/taskctl`. Also registers the MCP server in `~/.claude.json`.

---

## TUI

```bash
taskctl
```

| Key | Action |
|-----|--------|
| `n` | New task |
| `e` | Edit task |
| `d` | Delete task (confirm with `y`) |
| `space` | Toggle done (task stays greyed-out, disappears on next sync) |
| `s` | Sync with Apple Reminders |
| `S` | Postpone to tomorrow |
| `u` | Undo last delete |
| `p` | Start Pomodoro (25 min) |
| `t` | Focus mode — only today & overdue |
| `v` | Batch select (space toggle, A all, enter done, d delete) |
| `/` | Search |
| `i` | Stats (completions, sparkline) |
| `c` | Toggle show completed |
| `q` | Quit |

### New/Edit task form

| Field | Notes |
|-------|-------|
| Title | `!! Titel` = urgent (red), `! Titel` = important (yellow) |
| List | Dropdown with all Reminders lists + account name in `()` |
| Due | NLP: `morgen`, `übermorgen`, `nächsten montag`, `in 3 tagen`, `2026-07-15` |
| Notes | Free text |
| Repeat | `daily` / `weekly` / `monthly` — spawns next task on completion |

`tab` / `enter` — next field · `ctrl+s` — save · `esc` — cancel

---

## CLI

```bash
# Sync from Apple Reminders
taskctl sync

# List all tasks
taskctl list
taskctl list --json

# Tasks due today / this week
taskctl today
taskctl week

# Add a task
taskctl add "Zahnarzt anrufen" --list Aufgaben --due 2026-07-15

# Complete a task by ID
taskctl done <id>

# List all reminder lists
taskctl lists

# Run as MCP server (stdio)
taskctl mcp
```

---

## How sync works

- **Read**: EventKit Swift script fetches all reminders from all accounts (fast, native API)
- **Write**: AppleScript searches all accounts for the matching list/task (Create, Complete, Delete, Postpone)
- **Local cache**: SQLite at `~/Library/Application Support/taskctl/taskctl.db`
- **Conflict resolution**: local status changes (done, delete) are protected by `pending_status` / `pending_deletes` tables — a sync cannot revert them until Apple Reminders confirms the change

### Sync flow
1. `taskctl sync` or `s` in TUI
2. Fetches all Apple Reminders via EventKit
3. Filters out tasks in `pending_deletes` (user deleted locally)
4. Applies `pending_status` overrides (user toggled done locally)
5. Removes taskctl-sourced duplicates that now have a confirmed Apple counterpart

---

## MCP tools

When running as MCP server (`taskctl mcp`), Claude can:

| Tool | Description |
|------|-------------|
| `list_tasks` | List tasks, optionally filtered by list or status |
| `create_task` | Create a task with title, list, due date, notes |
| `complete_task` | Mark a task as completed |
| `delete_task` | Delete a task |
| `sync_tasks` | Trigger a sync from Apple Reminders |

---

## Architecture

```
taskctl/
├── cmd/            # Cobra CLI commands (add, sync, today, week, lists, mcp…)
├── internal/
│   ├── models/     # Task, ListEntry types
│   ├── reminders/  # Apple Reminders bridge (Swift EventKit + AppleScript)
│   ├── store/      # SQLite cache (tasks, lists, pending_deletes, pending_status)
│   ├── nlpdate/    # German/English NLP date parser
│   ├── config/     # DB path, defaults
│   ├── tui/        # Bubbletea TUI
│   └── mcpserver/  # MCP server (mark3labs/mcp-go)
└── setup.sh        # Build + install + MCP registration
```

**Stack**: Go · Cobra · Bubbletea · Lipgloss · modernc/sqlite · mcp-go

---

## Roadmap

### v0.2
- Google Tasks provider
- Background daemon (`taskctl daemon`) for periodic sync + notifications

### v0.3
- Microsoft To Do / Graph API

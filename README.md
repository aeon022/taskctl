# taskctl

Local-first task manager for macOS. Syncs with Apple Reminders via EventKit. Part of the missionctl suite.

---

## Quick Start

1. Clone and build:
   ```bash
   git clone https://github.com/aeon022/taskctl && cd taskctl
   ./setup.sh
   ```
2. Pull your tasks from Apple Reminders:
   ```bash
   taskctl sync
   ```
3. Open the TUI:
   ```bash
   taskctl
   ```
4. (Optional) Install the background sync daemon:
   ```bash
   taskctl daemon --install
   ```
5. (Optional) Connect to Claude Desktop — see [MCP — AI Integration](#mcp--ai-integration).

**Requirements:** macOS with Apple Reminders configured. Go 1.21+ (only needed to build from source). Works with iCloud, Exchange/Office365, and Google accounts.

---

## Cheatsheet

### CLI

| Command | What it does |
|---------|-------------|
| `taskctl` | Open TUI |
| `taskctl sync` | Pull from Apple Reminders |
| `taskctl list` | List tasks |
| `taskctl today` | Tasks due today and overdue |
| `taskctl week` | Tasks due this week (Mon–Sun) |
| `taskctl add TITLE` | Create a new task |
| `taskctl done TITLE` | Mark a task completed |
| `taskctl lists` | Show all Reminders list names |
| `taskctl daemon --install` | Install background sync as LaunchAgent |
| `taskctl mcp` | Start MCP server (stdio) |

### TUI — Main List

| Key | Action |
|-----|--------|
| `j` / `k` or arrow keys | Navigate |
| `space` | Toggle done (grayed out, removed on next sync) |
| `enter` / `ctrl+d` | Mark done |
| `n` | New task (form) |
| `i` | Inline quick-add |
| `e` | Edit selected task |
| `d` | Delete (confirm with `y`) |
| `u` | Undo last delete |
| `S` | Postpone to tomorrow |
| `s` | Sync with Apple Reminders |
| `t` | Focus mode — today and overdue only |
| `c` | Toggle show completed |
| `v` | Stats view |
| `p` | Start Pomodoro (25 min) |
| `/` | Search |
| `A` | Enter batch mode |
| `q` | Quit |

---

## CLI Reference

### `taskctl`

Opens the TUI. No subcommand required.

```bash
taskctl
```

---

### `taskctl sync`

Pulls all reminders from Apple Reminders via EventKit and updates the local SQLite cache.

```bash
taskctl sync
taskctl sync --list "Work"
```

| Flag | Description |
|------|-------------|
| `--list NAME` | Sync only the named Reminders list |

---

### `taskctl list`

Lists tasks from the local cache.

```bash
taskctl list
taskctl list --list "Work"
taskctl list --status completed
taskctl list --json
```

| Flag | Description |
|------|-------------|
| `--list NAME` | Filter by Reminders list name |
| `--status VALUE` | `needsAction` (default), `completed`, or `all` |
| `--json` | Output as JSON |

---

### `taskctl today`

Shows tasks due today and all overdue tasks.

```bash
taskctl today
taskctl today --json
```

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |

---

### `taskctl week`

Shows tasks due in the current week (Monday through Sunday).

```bash
taskctl week
taskctl week --json
```

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |

---

### `taskctl add`

Creates a new task. The task is written to Apple Reminders via AppleScript and cached locally.

```bash
taskctl add "Call the dentist"
taskctl add "Review PR" --list "Work" --due 2026-07-15
taskctl add "Buy groceries" --due tomorrow --notes "Milk, eggs, bread"
```

| Flag | Description |
|------|-------------|
| `--list NAME` | Add to this Reminders list (defaults to the default list) |
| `--due YYYY-MM-DD` | Due date |
| `--notes TEXT` | Task notes |

---

### `taskctl done`

Marks a task completed by title.

```bash
taskctl done "Call the dentist"
taskctl done "Review PR" --list "Work"
```

| Flag | Description |
|------|-------------|
| `--list NAME` | Disambiguate when the title matches tasks in multiple lists |

---

### `taskctl lists`

Lists all Reminders list names visible to taskctl.

```bash
taskctl lists
taskctl lists --json
```

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON |

---

### `taskctl daemon`

Runs a background sync process that polls Apple Reminders at a regular interval and triggers macOS notifications for due tasks.

```bash
taskctl daemon                # run in foreground, 5-minute interval
taskctl daemon --interval 2   # run in foreground, every 2 minutes
taskctl daemon --install      # install as LaunchAgent (auto-starts at login)
taskctl daemon --stop         # stop the running daemon
taskctl daemon --status       # check whether the daemon is running
```

| Flag | Description |
|------|-------------|
| `--interval N` | Sync interval in minutes (default: 5) |
| `--install` | Install as a macOS LaunchAgent |
| `--stop` | Stop the running daemon |
| `--status` | Print daemon status |

---

### `taskctl mcp`

Starts the MCP server over stdio. Intended to be launched by a host application (e.g. Claude Desktop), not called directly.

```bash
taskctl mcp
```

---

## TUI Guide

Start the TUI with `taskctl` (no subcommand).

### Main List

| Key | Action |
|-----|--------|
| `j` / `k` | Move cursor down / up |
| `↑` / `↓` | Move cursor up / down |
| `space` | Toggle done — grays the task out locally; removed from list on next sync |
| `enter` | Mark done |
| `ctrl+d` | Mark done |
| `n` | Open new task form |
| `i` | Inline quick-add — type a title and press enter |
| `e` | Edit selected task in form |
| `d` | Delete selected task (prompts `y` to confirm) |
| `u` | Undo the last delete |
| `S` | Postpone selected task to tomorrow |
| `s` | Sync with Apple Reminders |
| `t` | Toggle focus mode — shows only today and overdue tasks |
| `c` | Toggle display of completed tasks |
| `v` | Open stats view |
| `p` | Start a 25-minute Pomodoro timer (shown in header; notification on completion) |
| `/` | Search tasks by title |
| `A` | Enter batch mode |
| `q` | Quit |

### Batch Mode

Activate with `A` from the main list.

| Key | Action |
|-----|--------|
| `space` | Toggle selection on current task |
| `A` | Select all tasks |
| `enter` | Mark selected tasks done |
| `d` | Delete selected tasks |
| `esc` | Exit batch mode |

### New / Edit Task Form

| Field | Notes |
|-------|-------|
| Title | Prefix `!!` for urgent (displayed in red); prefix `!` for important (displayed in yellow) |
| List | Dropdown of all Reminders lists; account name shown in parentheses |
| Due | Accepts ISO dates or natural language (see below) |
| Notes | Free-form text |
| Repeat | `daily`, `weekly`, or `monthly` — a new task is created automatically on completion |

**Form keys:** `tab` or `enter` — advance to next field; `ctrl+s` — save; `esc` — cancel.

### NLP Due Dates

The due date field understands natural-language input in English and German.

| Input | Resolves to |
|-------|------------|
| `tomorrow` | Next calendar day |
| `next monday` | The coming Monday |
| `in 3 days` | Three days from today |
| `2026-07-15` | That exact date |
| `übermorgen` | Day after tomorrow |
| `nächsten montag` | The coming Monday |

### Stats View

Press `v` to open. Displays a completion sparkline for recent activity. Press `esc` to return to the main list.

---

## Sync and Conflict Resolution

### How sync works

taskctl bridges two systems: Apple Reminders (the source of truth for data) and a local SQLite database (the working cache). Each sync is a one-way pull with local-write protection.

**Read path:** A native EventKit Swift script fetches all reminders from all configured accounts (iCloud, Exchange/Office365, Google). This uses the native macOS API and is fast.

**Write path:** When taskctl creates, completes, or deletes a task, it issues an AppleScript command directly to the Reminders app. AppleScript searches all accounts to find the matching list and task.

**Local cache:** `~/Library/Application Support/taskctl/taskctl.db` (SQLite). All reads in the TUI and CLI hit this cache; syncs update it.

### Conflict resolution

Local changes made between syncs are protected by two tables in the SQLite database:

| Table | Purpose |
|-------|---------|
| `pending_deletes` | Records tasks the user has deleted locally. Sync skips these rows — they are not restored from Reminders. |
| `pending_status` | Records local completion toggles. Sync applies these as overrides rather than reverting them. |

**Sync flow:**

1. Fetch all reminders from Apple Reminders via EventKit.
2. Filter out any tasks present in `pending_deletes` — these stay deleted.
3. Apply `pending_status` overrides to incoming task states — locally toggled completion is preserved.
4. Merge the result into the local cache, removing taskctl-sourced duplicates that now have a confirmed Apple Reminders counterpart.

This means toggling a task done or deleting it in taskctl is safe to do offline — the next sync will not undo those changes.

---

## Daemon

The daemon runs `taskctl sync` on a repeating interval and delivers macOS notifications for tasks that become due.

```bash
# Run in the foreground (Ctrl+C to stop)
taskctl daemon

# Run every 2 minutes instead of the default 5
taskctl daemon --interval 2

# Install as a LaunchAgent — starts automatically at login
taskctl daemon --install

# Stop a running LaunchAgent daemon
taskctl daemon --stop

# Check whether the daemon is currently running
taskctl daemon --status
```

The LaunchAgent plist is written to `~/Library/LaunchAgents/`. Uninstall by running `--stop` and removing the plist manually, or by running `launchctl unload` on it.

---

## MCP — AI Integration

taskctl exposes all its capabilities as an MCP (Model Context Protocol) server. This lets Claude Desktop (or any MCP-compatible host) read, create, and complete tasks on your behalf.

### Claude Desktop configuration

Add the following to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "taskctl": {
      "command": "taskctl",
      "args": ["mcp"]
    }
  }
}
```

Restart Claude Desktop. taskctl will appear as a connected tool server.

### MCP tools

| Tool | Description |
|------|-------------|
| `today_tasks` | Returns tasks due today or overdue |
| `week_tasks` | Returns tasks due this week (Monday through Sunday) |
| `list_tasks` | Returns tasks, optionally filtered by list name or status |
| `sync` | Triggers a sync from Apple Reminders |
| `create_task` | Creates a task with title, list, due date, and notes |
| `complete_task` | Marks a task completed by title |
| `delete_task` | Deletes a task by title |

### AI workflow examples

**Morning briefing**

> "What's on my plate today? Summarize my overdue and today's tasks, group them by list, and suggest an order to tackle them."

Claude calls `sync` to pull fresh data, then `today_tasks` to retrieve the list, and responds with a prioritized summary.

**Capture tasks from meeting notes**

> "Here are my notes from the standup: [paste notes]. Extract any action items and add them to my Work list with appropriate due dates."

Claude parses the notes, calls `create_task` for each action item with inferred due dates, and confirms what was created.

**Weekly review**

> "Give me a weekly review: what did I complete this week, what's still open, and what's coming up next week?"

Claude calls `week_tasks` with `status=all` to get the full picture, then organizes the response into a done/open/upcoming structure.

---

## Architecture

```
Apple Reminders (EventKit Swift script + AppleScript)
        |
        v
SQLite  ~/Library/Application Support/taskctl/taskctl.db
        |
        +---> TUI (Bubbletea)
        +---> CLI (Cobra)
        +---> MCP server (stdio, mcp-go)
```

**Stack:** Go 1.21 — Cobra (CLI) — Bubbletea + Lipgloss (TUI) — modernc/sqlite (embedded SQLite) — mark3labs/mcp-go (MCP server)

**Data location:** `~/Library/Application Support/taskctl/taskctl.db`

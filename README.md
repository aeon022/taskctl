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
| `enter` | Task details popup (subtasks, notes) |
| `n` | New task (form) |
| `1`–`9` | Jump to the nth visible task |
| `e` | Edit selected task |
| `d` | Delete (confirm with `y`) |
| `u` | Undo last delete |
| `S` | Postpone to tomorrow |
| `s` | Sync with Apple Reminders |
| `t` | Focus mode — today and overdue only |
| `c` | Done view (completed tasks) — press again to go back to All |
| `tab` / `]` · `shift+tab` / `[` | Next / previous view: All · Today · Overdue · Next 7 days · No date · Done |
| `f` | Filter by list or priority (`l`, `p`; `x` clears everything) |
| `x` / `esc` | Clear the view tab and all filters |
| `h` / `←` | Focus the Lists sidebar (terminal ≥ 140 columns) |
| `O` | Overdue only |
| `i` | Stats view |
| `v` | Select mode (batch) |
| `p` | Start Pomodoro (25 min) |
| `y` | Copy title to clipboard |
| `o` | Open the task's URL |
| `/` | Search |
| `:` | Command palette |
| `?` | Help |
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
| `enter` | Open the task details popup |
| `n` | Open new task form |
| `1`–`9` | Jump to the nth visible task |
| `e` | Edit selected task in form |
| `d` | Delete selected task (prompts `y` to confirm) |
| `u` | Undo the last delete |
| `S` | Postpone selected task to tomorrow |
| `s` | Sync with Apple Reminders |
| `t` | Toggle focus mode — shows only today and overdue tasks |
| `c` | Done view — shows only completed tasks; `c` again returns to All |
| `tab` / `]`, `shift+tab` / `[` | Cycle the view tabs (see below) |
| `f` | Open the filter menu (see below) |
| `x` / `esc` | Clear the view tab, chips and the focus/overdue filter |
| `h` / `←` | Move into the Lists sidebar (≥ 140 columns) |
| `i` | Open stats view |
| `v` | Select mode: `space` marks, `A` selects all, `enter`/`ctrl+d` completes, `d` deletes the selection |
| `O` | Overdue only |
| `y` | Copy title to clipboard |
| `o` | Open the task's URL |
| `:` | Command palette |
| `?` | Help |
| `p` | Start a 25-minute Pomodoro timer (shown in header; notification on completion) |
| `/` | Search tasks by title |
| `q` | Quit |

### Views, filters and the Lists sidebar

Under the header a tab row shows the **views** with live counts:

`All 75 · Today 1 · Overdue 5 · Next 7 days 8 · No date 3 · Done`

| View | Shows |
|------|-------|
| All | every open task |
| Today | due today |
| Overdue | due before today |
| Next 7 days | due tomorrow up to and including the day 7 days ahead |
| No date | no due date |
| Done | completed tasks (this is what `c` toggles; they are loaded only while this view is open, so its count appears once you have opened it) |

Switch with `tab` / `]` (next) and `shift+tab` / `[` (previous, wrapping around), or click a tab.
The counts reflect every filter *except* the view itself, so they always tell you what you would
get by switching.

**Filter chips.** `f` opens a one-line menu: `l` → pick a list with `1`–`9` (`0` = any list),
`p` → pick a priority (`1` high · `2` medium · `3` low, `0` = any), `x` → clear everything,
`esc` → cancel. Active filters appear as chips under the tabs — `Baby ×`, `P1 ×`, and `/query ×`
for a search you have confirmed with `enter`. Click a chip to remove it; `x` or `esc` in the list
clears the view tab and all chips at once. Filters combine with each other and with the view;
the older `t` (today + overdue) and `O` (overdue only) modes still work on top.

**Lists sidebar (terminal ≥ 140 columns).** A panel on the left lists every list with its open
count and a red dot when it has overdue tasks. Click a list to filter by it (click it again, or
"All lists", to clear). From the keyboard press `h` or `←` to move into it, `j`/`k` to move,
`enter` to apply, `esc` or `l`/`→` to go back. Below 140 columns the sidebar is hidden and the
two-panel layout (≥ 120) or the plain list is used as before.

**Overview.** When the Details panel has room, a "This week" block appears below the selected
task (or alone when nothing is selected): overdue / today / next 7 days, a seven-day sparkline of
what is due per day starting today, and the lists with the most overdue tasks. The view and
filter state is not remembered between runs.

### Batch Mode

Activate with `v` from the main list.

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

### Syncing the cache file across devices

This is a different concern from the Apple Reminders sync above: that pulls task data *from* Apple's servers; this is about sharing taskctl's own local cache file *between your own devices*. By default the cache is local to this machine. To share it, set `data_dir` (in `~/.config/taskctl/config.yaml`) or the `TASKCTL_DATA_DIR` env var to a folder you already sync yourself — iCloud Drive, Dropbox, Syncthing, etc:

```bash
export TASKCTL_DATA_DIR="$HOME/Library/Mobile Documents/com~apple~CloudDocs/taskctl"
```

Once set, taskctl automatically switches its SQLite journal mode from WAL to rollback-journal — WAL splits the database across multiple files that a folder-sync client can't update atomically together, so this switch keeps the directory down to a single consistent file whenever taskctl isn't actively writing. A same-machine lock also prevents two taskctl processes from opening the cache at once (run `taskctl doctor` to see the current mode and path). This only protects against the same-machine and stale-snapshot failure modes, not two machines editing at the exact same instant; an undownloaded iCloud file is reported explicitly rather than as a bare error.

Note: if you also use timectl's task-picker ("T"), it resolves taskctl's data directory the same way (via `TASKCTL_DATA_DIR`) — but only if you set it as an env var, not if you only set `data_dir` in taskctl's config file.

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

## Recent changes (October 2026)

- **Window focus.** When the terminal window regains focus, the list reloads from the local database — at most every 5 seconds, and only while you are just browsing (never while a form, editor, search, palette or confirmation is open, so nothing you are typing is lost). Terminals that don't report focus events simply never trigger it.

- **Clipboard.** `y` copies the selected task's title — now through OSC 52 as well as `pbcopy`, so it also works over SSH and inside tmux (your terminal must allow OSC 52; locally `pbcopy` still does the job).

- **Footer and empty states.** The key-hint footer is the suite-wide one: it never wraps and drops the least important hints first on narrow terminals. Empty lists and loading screens show a short message with a hint what to press.

- **Editing keeps subtasks.** Editing a task with `e` used to recreate it under a new ID and lose its (local) subtasks; they are now carried over.

- **Daemon.** `taskctl daemon` refuses an invalid PID file (PID ≤ 0) instead of signalling a process group.

- The TUI now runs on Bubble Tea v2; key bindings are unchanged.

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

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/tuitest"
	"github.com/charmbracelet/x/ansi"
)

// Every view/mode taskctl has, each left again via esc. Commands returned by
// Update are never run (no DB, no Reminders.app).
var smokeKeys = []string{
	"j", "k", "down", "up", "enter", "esc", // detail popup
	"n", "tab", "tab", "esc", // create form
	"p", "esc", // pomodoro
	"i", "esc", // stats
	"?", "j", "esc", // help popup, scrolled
	"/", "a", "esc", // search
	":", "esc", // command palette
	"v", "space", "esc", // multi-select
	"t", "O", "t", // focus / overdue filters
	"d", "n", // delete confirm → cancel
	"u", "c", "c", // undo, show/hide done
	"e", "esc", // edit form
	"S", "r", // postpone, reload
}

func TestSmokeWideAndPopulated(t *testing.T) { tuitest.Smoke(t, loaded(t), smokeKeys...) }

func TestSmokeEmptyData(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newModel("")
	m2, _ := tuitest.Send(m, tea.WindowSizeMsg{Width: 100, Height: 30}, tasksLoadedMsg{})
	tuitest.Smoke(t, m2, smokeKeys...)
	if out := tuitest.Text(m2); !strings.Contains(out, "No tasks yet") || !strings.Contains(out, "press n to add one") {
		t.Errorf("empty list should show the empty state with its hint:\n%s", out)
	}
}

// Narrow and short terminals: no panics, never an empty frame, nothing wider
// than the terminal.
func TestSmokeSmallTerminal(t *testing.T) {
	m := loaded(t)
	mi, _ := tuitest.Send(m, tuitest.Resize(60, 15))
	for _, k := range smokeKeys {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panic at 60x15 after key %q: %v", k, r)
				}
			}()
			mi, _ = tuitest.Keys(mi, k)
			out := tuitest.Text(mi)
			if strings.TrimSpace(out) == "" {
				t.Fatalf("empty view at 60x15 after key %q", k)
			}
		}()
	}
}

func TestFooterNeverWiderThanTerminal(t *testing.T) {
	for _, w := range []int{40, 60, 80, 100, 140} {
		m := loaded(t)
		m, _ = send(m, tea.WindowSizeMsg{Width: w, Height: 30})
		for i, line := range strings.Split(strings.TrimRight(ansi.Strip(m.renderStatusBar()), "\n"), "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("width %d: footer line %d is %d cells wide: %q", w, i, lw, line)
			}
		}
	}
}

func TestFooterKeepsMostImportantHintsWhenNarrow(t *testing.T) {
	m := loaded(t)
	m, _ = send(m, tea.WindowSizeMsg{Width: 40, Height: 30})
	out := ansi.Strip(m.renderStatusBar())
	if !strings.Contains(out, "↑/↓ nav") {
		t.Errorf("highest-priority hint must survive: %q", out)
	}
}

package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/tuitest"
)

// Every secondary view shares the main list's chrome: nothing wider than the
// terminal, the same constant height, and the full-screen ones carry the
// header and a ONE-line footer that starts with the way back.
func TestSecondaryViewsShareTheChrome(t *testing.T) {
	views := []struct {
		name  string
		keys  []string
		title string // text that proves the view is showing
		full  bool   // full-screen (header + footer chrome) vs modal popup
	}{
		{"create form", []string{"n"}, "New Task", true},
		{"edit form", []string{"e"}, "Edit Task", true},
		{"help popup", []string{"?"}, "╭─ Help", false},
		{"detail popup", []string{"enter"}, "╭─ buy milk", false},
		{"pomodoro popup", []string{"p"}, "╭─ ", false},
	}
	for _, w := range []int{40, 60, 80, 100, 140, 170} {
		base, _ := tuitest.Send(loaded(t), tuitest.Resize(w, 30))
		listLines := strings.Count(tuitest.Text(base), "\n")
		for _, v := range views {
			m, _ := tuitest.Keys(base, v.keys...)
			out := tuitest.Text(m)
			if !strings.Contains(out, v.title) {
				t.Errorf("%s @%d: expected %q on screen:\n%s", v.name, w, v.title, out)
				continue
			}
			lines := strings.Split(out, "\n")
			for i, l := range lines {
				if lw := lipgloss.Width(l); lw > w {
					t.Errorf("%s @%d: line %d is %d cells wide: %q", v.name, w, i, lw, l)
				}
			}
			if got := strings.Count(out, "\n"); got != listLines {
				t.Errorf("%s @%d: %d lines, the main list uses %d (constant height)", v.name, w, got, listLines)
			}
			if v.full {
				if !strings.Contains(lines[0]+lines[1], "taskctl") {
					t.Errorf("%s @%d: header missing:\n%s", v.name, w, out)
				}
				footer := ""
				for i := len(lines) - 1; i >= 0; i-- {
					if strings.TrimSpace(lines[i]) != "" {
						footer = strings.TrimSpace(lines[i])
						break
					}
				}
				if !strings.HasPrefix(footer, "esc") {
					t.Errorf("%s @%d: footer should start with the way back (esc …), got %q", v.name, w, footer)
				}
			}
		}
	}
}

func TestStatsViewChromeAndWayBack(t *testing.T) {
	m, _ := tuitest.Send(loaded(t), tuitest.Resize(100, 30))
	m, _ = tuitest.Keys(m, "i")
	m, _ = tuitest.Send(m, statsMsg{today: 2, week: 9, total: 120, daily: []int{0, 1, 3, 2, 0, 4, 1, 0, 2, 2}})
	out := tuitest.Text(m)
	for _, want := range []string{"taskctl", "Stats", "Productivity", "120 ✓", "esc back", "any key close"} {
		if !strings.Contains(out, want) {
			t.Errorf("stats view missing %q:\n%s", want, out)
		}
	}
	m, _ = tuitest.Keys(m, "esc")
	if strings.Contains(tuitest.Text(m), "Productivity") {
		t.Error("esc must leave the stats view")
	}
}

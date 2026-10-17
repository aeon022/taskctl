package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/missionctl-core/tuitest"
)

func TestThemeKeySwitchesLiveAndSaves(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	orig := theme.Current()
	t.Cleanup(func() { _ = theme.Apply(orig) })

	m := newModel("")
	m, _ = send(m, tea.WindowSizeMsg{Width: 100, Height: 30}, tasksLoadedMsg{})
	before := styleHeader.GetForeground()
	m, _ = send(m, tuitest.Key("T"))

	if theme.Current() == orig {
		t.Fatalf("T must switch to the next theme, still %q", orig)
	}
	if styleHeader.GetForeground() == before {
		t.Error("cached styles must be rebuilt when the theme changes")
	}
	if !strings.Contains(tuitest.Text(m), "Theme: "+theme.Current()) {
		t.Errorf("a short confirmation should name the theme:\n%s", tuitest.Text(m))
	}
	b, err := os.ReadFile(filepath.Join(home, ".config", "missionctl", "theme.yaml"))
	if err != nil || !strings.Contains(string(b), "preset: "+theme.Current()) {
		t.Errorf("the choice is saved for the whole suite: %v %q", err, b)
	}
}

func TestOnboardingTicksTheSyncStep(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newModel("")
	m, _ = send(m, tea.WindowSizeMsg{Width: 100, Height: 30}, tasksLoadedMsg{})
	if out := tuitest.Text(m); !strings.Contains(out, "○  s   sync") {
		t.Errorf("a never-synced list starts with an open sync step:\n%s", out)
	}
	m.lastSynced = time.Now()
	if out := tuitest.Text(m); !strings.Contains(out, "✓  s   sync") {
		t.Errorf("after a sync the step is ticked:\n%s", out)
	}
}

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

func TestThemeKeyOpensPickerWithLivePreviewAndSaves(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	orig := theme.Current()
	t.Cleanup(func() { _ = theme.Apply(orig) })

	m := newModel("")
	m, _ = send(m, tea.WindowSizeMsg{Width: 100, Height: 30}, tasksLoadedMsg{})
	m, _ = send(m, tuitest.Key("T"))
	if !strings.Contains(tuitest.Text(m), "Colour theme") {
		t.Fatalf("T opens the theme popup:\n%s", tuitest.Text(m))
	}
	before := styleHeader.GetForeground()
	m, _ = send(m, tuitest.Key("j"))
	if theme.Current() == orig || styleHeader.GetForeground() == before {
		t.Error("moving in the popup previews the theme live and rebuilds the cached styles")
	}
	m, _ = send(m, tuitest.Key("enter"))
	if strings.Contains(tuitest.Text(m), "Colour theme") || !strings.Contains(tuitest.Text(m), "Theme: "+theme.Current()) {
		t.Errorf("enter closes the popup and confirms the theme:\n%s", tuitest.Text(m))
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

func TestDoneTabCountIsKnownBeforeOpeningIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newModel("")
	m, _ = send(m, tea.WindowSizeMsg{Width: 120, Height: 30}, tasksLoadedMsg{doneTotal: 7})
	if got := m.tabCounts()[tabDone]; got != 7 {
		t.Errorf("Done tab count before opening it = %d, want 7", got)
	}
}

package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpOverlay_OpenScrollClose(t *testing.T) {
	m := Model{width: 100, height: 30}

	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = mi.(Model)
	if m.view != viewHelp {
		t.Fatalf("expected viewHelp after '?', got %v", m.view)
	}
	if m.helpVP.TotalLineCount() == 0 {
		t.Fatal("expected help content to be populated")
	}

	before := m.helpVP.ScrollPercent()
	for i := 0; i < 5; i++ {
		mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = mi.(Model)
	}
	if m.helpVP.ScrollPercent() <= before {
		t.Errorf("expected scroll to advance after pressing j, stayed at %v", before)
	}

	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mi.(Model)
	if m.view != viewList {
		t.Errorf("expected esc to close help back to viewList, got %v", m.view)
	}
}

func TestHelpOverlay_FitsWithinBackgroundHeight(t *testing.T) {
	m := Model{width: 100, height: 30}
	m = m.openHelp()
	bgLines := len(strings.Split(m.renderList(), "\n"))
	if m.helpPopH > bgLines {
		t.Errorf("popup height %d exceeds background height %d", m.helpPopH, bgLines)
	}
}

func TestHelpOverlay_PopupBorderColumnIsConsistent(t *testing.T) {
	// Regression guard: must compare rune indexes, not byte indexes — the
	// header line contains "·" (a 2-byte UTF-8 character), which threw off
	// a byte-based comparison used while first debugging this rollout and
	// produced a false "misalignment" that wasn't actually there.
	m := Model{width: 100, height: 30}
	m = m.openHelp()

	lines := strings.Split(m.View(), "\n")
	col := -1
	for i, l := range lines {
		idx := -1
		for j, r := range []rune(l) {
			if r == '╭' || r == '│' || r == '╰' {
				idx = j
				break
			}
		}
		if idx < 0 {
			continue
		}
		if col == -1 {
			col = idx
			continue
		}
		if idx != col {
			t.Errorf("line %d: popup border at rune column %d, want %d (same as other rows)", i, idx, col)
		}
	}
	if col == -1 {
		t.Fatal("expected to find popup border characters in the rendered view")
	}
}

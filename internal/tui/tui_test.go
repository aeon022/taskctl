package tui

import (
	"strings"
	"testing"

	"github.com/aeon022/taskctl/internal/models"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
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

func TestBuildRows_FuzzyMatchesTitle(t *testing.T) {
	tasks := []models.Task{
		{ID: "1", Title: "budgetctl release", List: "Work"},
		{ID: "2", Title: "write docs", List: "Work"},
	}
	rows := buildRows(tasks, "bgt", false)
	var titles []string
	for _, r := range rows {
		if !r.isHeader {
			titles = append(titles, r.task.Title)
		}
	}
	if len(titles) != 1 || titles[0] != "budgetctl release" {
		t.Errorf("expected fuzzy 'bgt' to match only 'budgetctl release', got %+v", titles)
	}
}

func TestBuildRows_FallsBackToNotesSubstring(t *testing.T) {
	tasks := []models.Task{
		{ID: "1", Title: "unrelated", Notes: "about budgetctl imports", List: "Work"},
	}
	rows := buildRows(tasks, "budgetctl", false)
	var titles []string
	for _, r := range rows {
		if !r.isHeader {
			titles = append(titles, r.task.Title)
		}
	}
	if len(titles) != 1 {
		t.Errorf("expected the notes substring match to keep the task, got %+v", titles)
	}
}

func TestBuildRows_PreservesListGroupingRatherThanRankingByMatchQuality(t *testing.T) {
	// Regression test: unlike habctl's filterHabits, buildRows must NOT
	// re-sort by fuzzy match quality — that would scatter a single list's
	// tasks across non-contiguous positions and fragment the "isHeader"
	// grouping this function builds. Two lists, both containing a match,
	// must stay as two contiguous header groups in their original order.
	tasks := []models.Task{
		{ID: "1", Title: "budgetctl release", List: "Work"},
		{ID: "2", Title: "buy groceries budget", List: "Personal"},
	}
	rows := buildRows(tasks, "budget", false)
	if len(rows) != 4 {
		t.Fatalf("expected 2 headers + 2 tasks = 4 rows, got %d: %+v", len(rows), rows)
	}
	if !rows[0].isHeader || rows[0].label != "Work" {
		t.Errorf("expected row 0 to be the 'Work' header, got %+v", rows[0])
	}
	if rows[1].isHeader || rows[1].task.Title != "budgetctl release" {
		t.Errorf("expected row 1 to be 'budgetctl release', got %+v", rows[1])
	}
	if !rows[2].isHeader || rows[2].label != "Personal" {
		t.Errorf("expected row 2 to be the 'Personal' header, got %+v", rows[2])
	}
	if rows[3].isHeader || rows[3].task.Title != "buy groceries budget" {
		t.Errorf("expected row 3 to be 'buy groceries budget', got %+v", rows[3])
	}
}

func TestBuildRows_EmptyQueryReturnsAllUnfiltered(t *testing.T) {
	tasks := []models.Task{{ID: "1", Title: "a", List: "L"}, {ID: "2", Title: "b", List: "L"}}
	rows := buildRows(tasks, "", false)
	count := 0
	for _, r := range rows {
		if !r.isHeader {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected empty query to keep all tasks, got %d", count)
	}
}

func TestHighlightMatches_ColorsOnlyMatchedRunes(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	idxs := fuzzyMatchIndexes("bgt", "budgetctl")
	if idxs == nil {
		t.Fatal("expected 'bgt' to fuzzy-match 'budgetctl'")
	}
	out := highlightMatches("budgetctl", idxs, styleTitle)
	if out == styleTitle.Render("budgetctl") {
		t.Error("expected highlightMatches to differ from a plain render for a real match")
	}
}

func TestHighlightMatches_NoMatchRendersPlain(t *testing.T) {
	out := highlightMatches("hello", nil, styleTitle)
	if out != styleTitle.Render("hello") {
		t.Errorf("expected nil idxs to render plain, got %q want %q", out, styleTitle.Render("hello"))
	}
}

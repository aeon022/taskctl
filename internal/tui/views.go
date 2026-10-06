package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/dateutil"
	"github.com/aeon022/missionctl-core/ui"
	"github.com/aeon022/taskctl/internal/models"
	"github.com/charmbracelet/x/ansi"
)

// View tabs (All · Today · Overdue · Next 7 days · No date · Done), filter
// chips (list, priority, search), the Lists sidebar and the overview — all the
// "what should I look at" machinery of the list view. The tab/condition logic
// is pure (tabCond, inTab, filterView); the rest are Model methods that the
// renderer and the mouse hit-tests share, so they cannot drift apart.

type listTab int

const (
	tabAll listTab = iota
	tabToday
	tabOverdue
	tabNext7
	tabNoDate
	tabDone
	tabCount
)

var tabLabels = [tabCount]string{"All", "Today", "Overdue", "Next 7 days", "No date", "Done"}

// tabCond is the date/status condition of a tab, used to build the list.
// It does not look at completion for the open tabs, so a task completed just
// now stays visible (greyed out) until the next reload, as it always did.
//
//   - Today:      due within today.
//   - Overdue:    due before today.
//   - Next 7 days: tomorrow up to and including the day 7 days ahead.
//   - No date:    no due date.
//   - Done:       completed.
func tabCond(t *models.Task, tab listTab, now time.Time) bool {
	sod, eod := dateutil.StartOfDay(now), dateutil.EndOfDay(now)
	d := t.DueDate
	switch tab {
	case tabToday:
		return d != nil && !d.Before(sod) && !d.After(eod)
	case tabOverdue:
		return d != nil && d.Before(sod)
	case tabNext7:
		return d != nil && d.After(eod) && d.Before(sod.AddDate(0, 0, 8))
	case tabNoDate:
		return d == nil
	case tabDone:
		return t.Done()
	}
	return true
}

// inTab is tabCond for counting: the open tabs count open tasks only.
func inTab(t *models.Task, tab listTab, now time.Time) bool {
	if tab == tabDone {
		return t.Done()
	}
	return !t.Done() && tabCond(t, tab, now)
}

// filterView returns the indexes of the tasks that belong to tab at now.
func filterView(tasks []models.Task, tab listTab, now time.Time) []int {
	var idx []int
	for i := range tasks {
		if inTab(&tasks[i], tab, now) {
			idx = append(idx, i)
		}
	}
	return idx
}

// ── filters ───────────────────────────────────────────────────────────────────

type filterMenuState int

const (
	menuNone filterMenuState = iota
	menuMain
	menuList
	menuPrio
)

// keepFn is the task predicate behind the rows: the list and priority filters
// plus the tab. strict switches the tab to its counting form (inTab).
func (m Model) keepFn(tab listTab, strict bool) func(*models.Task) bool {
	now := nowFn()
	return func(t *models.Task) bool {
		if m.listFilter != "" && t.List != m.listFilter {
			return false
		}
		if m.prioFilter != 0 && t.Priority != m.prioFilter {
			return false
		}
		if strict {
			return inTab(t, tab, now)
		}
		return tabCond(t, tab, now)
	}
}

// rebuildRows is buildRows with everything the model filters by.
func (m Model) rebuildRows() []row {
	return buildRowsWith(m.tasks, m.searchQuery(), m.filter, m.keepFn(m.tab, false))
}

// tabCounts are the numbers on the tabs: each tab's open (Done: completed)
// tasks under every filter except the tab itself.
// ponytail: rebuilds the rows once per tab per frame — fine for a few hundred
// tasks; cache per tasks/filters change if lists ever get huge.
func (m Model) tabCounts() []int {
	counts := make([]int, tabCount)
	for tab := range counts {
		for _, r := range buildRowsWith(m.tasks, m.searchQuery(), m.filter, m.keepFn(listTab(tab), true)) {
			if !r.isHeader {
				counts[tab]++
			}
		}
	}
	return counts
}

// setTab switches the view tab. Done is the old "show completed" mode, so
// entering/leaving it flips showDone and reloads (completed tasks are only
// loaded while it is on).
func (m Model) setTab(t listTab) (Model, tea.Cmd) {
	m.tab = t
	var cmd tea.Cmd
	if want := t == tabDone; want != m.showDone {
		m.showDone = want
		cmd = loadTasks(m.showDone)
	}
	m.rows = m.rebuildRows()
	m.cursor = firstTaskRow(m.rows)
	return m, cmd
}

func (m Model) cycleTab(delta int) (Model, tea.Cmd) {
	return m.setTab(listTab((int(m.tab) + delta + int(tabCount)) % int(tabCount)))
}

// anyFilter reports whether anything narrows the list (tab, chips, or the
// legacy focus/overdue modes).
func (m Model) anyFilter() bool {
	return m.tab != tabAll || len(m.chips()) > 0 || m.filter != filterNone
}

// clearFilters resets the tab, the chips and the focus/overdue mode.
func (m Model) clearFilters() (Model, tea.Cmd) {
	m.listFilter, m.prioFilter = "", 0
	m.searchInput.SetValue("")
	if m.filter != filterNone {
		m.filter = filterNone
		m.saveUIState()
	}
	return m.setTab(tabAll)
}

// filterLists are the lists that have tasks, sorted — the picker and the
// sidebar both use this order.
func (m Model) filterLists() []string {
	var names []string
	for _, e := range uniqueListEntries(m.tasks) {
		names = append(names, e.Name)
	}
	return names
}

// ── chips ─────────────────────────────────────────────────────────────────────

type chipKind int

const (
	chipList chipKind = iota
	chipPrio
	chipSearch
)

type chip struct {
	kind  chipKind
	label string
}

var prioLabel = map[int]string{1: "P1", 5: "P2", 9: "P3"}

// chips are the active filters outside the tab, in the order they are drawn.
func (m Model) chips() []chip {
	var cs []chip
	if m.listFilter != "" {
		cs = append(cs, chip{chipList, m.listFilter})
	}
	if m.prioFilter != 0 {
		cs = append(cs, chip{chipPrio, prioLabel[m.prioFilter]})
	}
	if q := m.searchQuery(); q != "" && !m.searching {
		cs = append(cs, chip{chipSearch, "/" + q})
	}
	return cs
}

func (m Model) removeChip(k chipKind) Model {
	switch k {
	case chipList:
		m.listFilter = ""
	case chipPrio:
		m.prioFilter = 0
	case chipSearch:
		m.searchInput.SetValue("")
	}
	m.rows = m.rebuildRows()
	m.cursor = firstTaskRow(m.rows)
	return m
}

func chipText(c chip) string { return ui.Pill(c.label+" ×", ui.Info) }

// chipsLine is the row of chips (empty when there are none).
func (m Model) chipsLine() string {
	cs := m.chips()
	if len(cs) == 0 {
		return ""
	}
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = chipText(c)
	}
	w, _ := m.dims()
	return ansi.Truncate("  "+strings.Join(parts, " "), max(w, 0), "…")
}

// chipHitTest returns the index into m.chips() drawn at content cell (x, y),
// or -1. The chips row is right below the tabs row.
func (m Model) chipHitTest(x, y int) int {
	if y != 3 {
		return -1
	}
	col := 2
	for i, c := range m.chips() {
		w := lipgloss.Width(chipText(c))
		if x >= col && x < col+w {
			return i
		}
		col += w + 1
	}
	return -1
}

// ── tabs row ──────────────────────────────────────────────────────────────────

// tabTexts are the visible labels with their counts, as ui.Tabs draws them.
func (m Model) tabTexts(counts []int) []string {
	out := make([]string, tabCount)
	for i := range out {
		out[i] = tabLabels[i]
		if counts[i] > 0 {
			out[i] += " " + strconv.Itoa(counts[i])
		}
	}
	return out
}

func (m Model) tabsLine() string {
	w, _ := m.dims()
	return "  " + ui.Tabs(max(w-2, 0), tabLabels[:], int(m.tab), m.tabCounts())
}

// tabHitTest returns the tab drawn at content cell (x, y), or -1. It finds
// each label in the rendered (ANSI-stripped) row, so it follows ui.Tabs'
// windowing without duplicating it; the pill padding counts as part of a tab.
func (m Model) tabHitTest(x, y int) int {
	if y != 2 {
		return -1
	}
	plain := ansi.Strip(m.tabsLine())
	pos := 0
	for i, text := range m.tabTexts(m.tabCounts()) {
		idx := strings.Index(plain[pos:], text)
		if idx < 0 {
			continue
		}
		start := pos + idx
		x0 := lipgloss.Width(plain[:start])
		if x >= x0-1 && x < x0+lipgloss.Width(text)+1 {
			return i
		}
		pos = start + len(text)
	}
	return -1
}

// ── "f" filter menu ───────────────────────────────────────────────────────────

func (m Model) filterMenuHeight() int {
	if m.filterMenu == menuNone {
		return 0
	}
	return 1
}

func (m Model) filterMenuLine() string {
	w, _ := m.dims()
	k := func(s string) string { return styleKey.Render(s) }
	var s string
	switch m.filterMenu {
	case menuMain:
		s = "filter  " + k("l") + " list · " + k("p") + " priority · " + k("x") + " clear all · " + k("esc") + " cancel"
	case menuList:
		lists := m.filterLists()
		var parts []string
		for i, name := range lists {
			if i == 9 {
				break // ponytail: digits 1-9 only; add a scrolling picker if anyone has more lists
			}
			parts = append(parts, k(strconv.Itoa(i+1))+" "+name)
		}
		s = "list  " + strings.Join(parts, "  ") + "  " + k("0") + " any · " + k("esc")
	case menuPrio:
		s = "priority  " + k("1") + " high  " + k("2") + " medium  " + k("3") + " low  " + k("0") + " any · " + k("esc")
	}
	return ansi.Truncate("  "+s, max(w, 0), "…")
}

// handleFilterMenuKey drives the three-step "f" menu: pick what to filter,
// then pick the value with a digit.
func (m Model) handleFilterMenuKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if key == "esc" {
		m.filterMenu = menuNone
		return m, nil
	}
	apply := func(m Model) (Model, tea.Cmd) {
		m.filterMenu = menuNone
		m.rows = m.rebuildRows()
		m.cursor = firstTaskRow(m.rows)
		return m, nil
	}
	switch m.filterMenu {
	case menuMain:
		switch key {
		case "l":
			if len(m.filterLists()) > 0 {
				m.filterMenu = menuList
			}
		case "p":
			m.filterMenu = menuPrio
		case "x":
			m.filterMenu = menuNone
			return m.clearFilters()
		}
	case menuList:
		lists := m.filterLists()
		if key == "0" {
			m.listFilter = ""
			return apply(m)
		}
		if n, err := strconv.Atoi(key); err == nil && n >= 1 && n <= len(lists) && n <= 9 {
			m.listFilter = lists[n-1]
			return apply(m)
		}
	case menuPrio:
		switch key {
		case "0":
			m.prioFilter = 0
			return apply(m)
		case "1":
			m.prioFilter = 1
			return apply(m)
		case "2":
			m.prioFilter = 5
			return apply(m)
		case "3":
			m.prioFilter = 9
			return apply(m)
		}
	}
	return m, nil
}

// ── Lists sidebar ─────────────────────────────────────────────────────────────

// sidebarMin is the terminal width from which the Lists sidebar is drawn.
const sidebarMin = 140

func (m Model) sidebar() bool {
	w, _ := m.dims()
	return w+2*appPadH >= sidebarMin
}

// sideWidth is the sidebar panel's outer width (0 when hidden).
func (m Model) sideWidth() int {
	if !m.sidebar() {
		return 0
	}
	w, _ := m.dims()
	return min(max(w/6, 22), 30)
}

type listStat struct {
	name          string
	open, overdue int
}

// listStats are the open and overdue task counts per list (not narrowed by
// the filters: the sidebar is the overview, the tabs are the narrowing).
func (m Model) listStats() []listStat {
	now := nowFn()
	by := map[string]*listStat{}
	for i := range m.tasks {
		t := &m.tasks[i]
		if t.Done() {
			continue
		}
		s := by[t.List]
		if s == nil {
			s = &listStat{name: t.List}
			by[t.List] = s
		}
		s.open++
		if inTab(t, tabOverdue, now) {
			s.overdue++
		}
	}
	out := make([]listStat, 0, len(by))
	for _, s := range by {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// sideEntries is the sidebar's selectable rows: "All lists", then each list.
func (m Model) sideEntries() []listStat {
	stats := m.listStats()
	all := listStat{name: "All lists"}
	for _, s := range stats {
		all.open += s.open
		all.overdue += s.overdue
	}
	return append([]listStat{all}, stats...)
}

// sideActive is the sidebar index of the active list filter (0 = none).
func (m Model) sideActive() int {
	if m.listFilter == "" {
		return 0
	}
	for i, s := range m.sideEntries() {
		if i > 0 && s.name == m.listFilter {
			return i
		}
	}
	return 0
}

// sideWindow is the first visible sidebar entry when h rows fit: the one
// that is highlighted (focused or active) is always inside.
func (m Model) sideWindow(h, n int) int {
	cur := m.sideActive()
	if m.sideFocus {
		cur = m.sideCursor
	}
	return max(min(cur-h+1, n-h), 0)
}

func (m Model) sidebarText(width, height int) string {
	entries := m.sideEntries()
	start := m.sideWindow(height, len(entries))
	hl := m.sideActive()
	if m.sideFocus {
		hl = m.sideCursor
	}
	var lines []string
	for i := start; i < len(entries) && i < start+height; i++ {
		e := entries[i]
		right := strconv.Itoa(e.open)
		if e.overdue > 0 {
			right = ui.Dot(ui.Err) + " " + right
		}
		name := ansi.Truncate(e.name, max(width-2-lipgloss.Width(right)-1, 1), "…")
		gap := strings.Repeat(" ", max(width-2-lipgloss.Width(name)-lipgloss.Width(right), 1))
		lines = append(lines, ui.Row(width, i == hl, name+gap+right))
	}
	return strings.Join(lines, "\n")
}

// sideHitTest returns the sidebar entry at content cell (x, y), or -1.
func (m Model) sideHitTest(x, y int) int {
	sw := m.sideWidth()
	if sw == 0 || x < 1 || x >= sw-1 {
		return -1
	}
	n := len(m.sideEntries())
	h := max(m.bodyHeight()-2, 1)
	i := y - (m.chromeAbove() + 1) + m.sideWindow(h, n)
	if y < m.chromeAbove()+1 || y >= m.chromeAbove()+1+h || i < 0 || i >= n {
		return -1
	}
	return i
}

// pickSide applies the sidebar entry i as the list filter; picking the
// active list again (or "All lists") clears it.
func (m Model) pickSide(i int) Model {
	entries := m.sideEntries()
	if i <= 0 || i >= len(entries) || entries[i].name == m.listFilter {
		m.listFilter = ""
	} else {
		m.listFilter = entries[i].name
	}
	m.rows = m.rebuildRows()
	m.cursor = firstTaskRow(m.rows)
	return m
}

func (m Model) handleSideKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	n := len(m.sideEntries())
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "up", "k":
		m.sideCursor = max(m.sideCursor-1, 0)
	case "down", "j":
		m.sideCursor = min(m.sideCursor+1, n-1)
	case "enter", "space":
		m = m.pickSide(m.sideCursor)
		m.sideFocus = false
	case "right", "l", "esc":
		m.sideFocus = false
	}
	return m, nil
}

// ── overview (Details panel) ──────────────────────────────────────────────────

// overviewLines are the lines of the "This week" summary: overdue / today /
// next 7 days, a per-day sparkline of what is due, and the lists with the most
// overdue tasks. 5 lines at most.
func (m Model) overviewLines() []string {
	now := nowFn()
	var overdue, today, next7 int
	var perDay [7]float64
	sod := dateutil.StartOfDay(now)
	for i := range m.tasks {
		t := &m.tasks[i]
		if t.Done() {
			continue
		}
		switch {
		case inTab(t, tabOverdue, now):
			overdue++
		case inTab(t, tabToday, now):
			today++
		case inTab(t, tabNext7, now):
			next7++
		}
		if t.DueDate != nil {
			if d := int(t.DueDate.Sub(sod).Hours() / 24); d >= 0 && d < 7 && !t.DueDate.Before(sod) {
				perDay[d]++
			}
		}
	}
	dim := func(s string) string { return styleSubhead.Render(s) }
	initials := make([]string, 7)
	for i := range initials {
		initials[i] = now.AddDate(0, 0, i).Format("Mon")[:1]
	}
	lines := []string{
		dim("This week"),
		fmt.Sprintf("%s %d  ·  %s %d  ·  %s %d", dim("overdue"), overdue, dim("today"), today, dim("next 7 days"), next7),
		ui.Spark(perDay[:]),
		dim(strings.Join(initials, "")),
	}
	var tops []listStat
	for _, s := range m.listStats() {
		if s.overdue > 0 {
			tops = append(tops, s)
		}
	}
	sort.SliceStable(tops, func(i, j int) bool { return tops[i].overdue > tops[j].overdue })
	if len(tops) > 3 {
		tops = tops[:3]
	}
	if len(tops) > 0 {
		var parts []string
		for _, s := range tops {
			parts = append(parts, fmt.Sprintf("%s %d", s.name, s.overdue))
		}
		lines = append(lines, dim("most overdue  ")+strings.Join(parts, " · "))
	}
	return lines
}

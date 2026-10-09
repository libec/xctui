package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/libec/xctui/internal/xcode"
)

var (
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Red)
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Green)
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.BrightBlack)
	boldStyle    = lipgloss.NewStyle().Bold(true)
	borderStyle  = dimStyle
	focusedStyle = lipgloss.NewStyle().Foreground(lipgloss.Blue).Bold(true)
)

type lineKind int

const (
	plainLine lineKind = iota
	warningLine
	errorLine
)

// kindOf spots compiler diagnostics ("File.swift:3:1: error: ...") and xcodebuild's own errors.
// The space matters: the backtrace xcodebuild prints when interrupted is full of
// Objective-C selectors like "doBuildForBuildAction:colorizeFailure:error:]".
func kindOf(line string) lineKind {
	switch {
	case strings.Contains(line, "error: "):
		return errorLine
	case strings.Contains(line, "warning: "):
		return warningLine
	}
	return plainLine
}

// leftWidth is the left column: a third of the window, at least enough for a simulator row.
const minLeftWidth = 32

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m Model) render() string {
	if m.width == 0 {
		return "" // no WindowSizeMsg yet
	}
	body := m.height - 1 // key help
	left := max(minLeftWidth, m.width/3)
	right := m.width - left

	var column string
	if m.expanded {
		column = m.leftBox(m.leftFocus, left, body)
	} else {
		statusH, schemeH, simulatorH, runsH := m.leftHeights(body)
		column = strings.Join([]string{
			m.leftBox(statusPane, left, statusH),
			m.leftBox(schemePane, left, schemeH),
			m.leftBox(simulatorPane, left, simulatorH),
			m.leftBox(runsPane, left, runsH),
		}, "\n")
	}
	detail := m.box(detailPane, m.detailTitle(), right, body, m.detailRows())

	screen := lipgloss.JoinHorizontal(lipgloss.Top, column, detail) + "\n" + m.footer()
	var popup string
	switch {
	case m.deleting:
		popup = m.deleteBox()
	case m.renaming.UDID != "":
		popup = m.renameBox()
	case m.showHelp:
		popup = m.helpBox()
	default:
		return screen
	}
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(screen),
		lipgloss.NewLayer(popup).X(max(0, (m.width-lipgloss.Width(popup))/2)).Y(max(0, (body-lipgloss.Height(popup))/2)).Z(1),
	).Render()
}

// leftBox renders one of the left panes at the given size.
func (m Model) leftBox(p pane, width, height int) string {
	switch p {
	case statusPane:
		return m.box(p, "Status", width, height, m.statusRows())
	case schemePane:
		return m.box(p, tabsTitle(schemeTabNames, int(m.tab), width-6), width, height, m.schemeRows(height-2, width-2))
	case simulatorPane:
		return m.box(p, tabsTitle(destinationTabs, m.platform, width-6), width, height, m.simulatorRows(height-2, width-2))
	}
	return m.box(p, "Runs", width, height, m.runRows(height-2, width-2))
}

// leftHeights splits the left column: Status is fixed, the other three panes
// take a third each of what is left, like lazygit's equal panels.
func (m Model) leftHeights(body int) (status, scheme, simulator, runs int) {
	status = 4
	rest := body - status
	scheme, simulator = rest/3, rest/3
	runs = rest - scheme - simulator
	return status, scheme, simulator, runs
}

// tabsTitle is a pane title made of tabs, the active one bold, like lazygit.
// When the roomy form does not fit in width the separators shrink, and when
// that does not fit either only the active tab shows.
func tabsTitle(names []string, active, width int) string {
	var tabs []string
	for i, name := range names {
		if i == active {
			tabs = append(tabs, focusedStyle.Render(name))
		} else {
			tabs = append(tabs, dimStyle.Render(name))
		}
	}
	title := strings.Join(tabs, dimStyle.Render(" · "))
	if lipgloss.Width(title) > width {
		title = strings.Join(tabs, dimStyle.Render("·"))
	}
	if lipgloss.Width(title) > width {
		title = tabs[active]
	}
	return title
}

// box draws a bordered pane titled like lazygit: ┌[2]─Scheme──┐, with the pane's
// filter, if any, on the bottom border.
func (m Model) box(p pane, title string, width, height int, rows []string) string {
	return frame(fmt.Sprintf("[%d]─%s", p, title), m.filterLabel(p), p == m.focus, width, height, rows)
}

func frame(title, bottom string, focused bool, width, height int, rows []string) string {
	style := borderStyle
	if focused {
		style = focusedStyle
	}
	inner := width - 2
	top := fit(title, inner)
	lines := []string{style.Render("┌"+top) + style.Render(strings.Repeat("─", inner-lipgloss.Width(top))+"┐")}
	for i := 0; i < max(0, height-2); i++ {
		row := ""
		if i < len(rows) {
			row = fit(rows[i], inner)
		}
		row += strings.Repeat(" ", inner-lipgloss.Width(row))
		lines = append(lines, style.Render("│")+row+style.Render("│"))
	}
	bottom = fit(bottom, inner)
	lines = append(lines, style.Render("└"+bottom+strings.Repeat("─", inner-lipgloss.Width(bottom))+"┘"))
	return strings.Join(lines, "\n")
}

// filterLabel is "/text" for a pane with a filter, with a cursor while typing into it.
func (m Model) filterLabel(p pane) string {
	text := m.filters[p]
	if p == detailPane {
		text = ""
		if ph := m.shownPhase(); ph != nil {
			text = ph.filter
		}
	}
	typing := m.typing && m.focus == p
	if text == "" && !typing {
		return ""
	}
	label := "/" + text
	if typing {
		label += "▏"
	}
	return label
}

// statusRows: the last job's verdict (or a message), then the container.
func (m Model) statusRows() []string {
	first := dimStyle.Render("no runs yet · b builds")
	switch {
	case m.status != "":
		first = m.statusStyle.Render(m.status)
	case m.current != nil:
		first = m.runStyle(m.current).Render(m.current.summary())
	case len(m.runs) > 0:
		first = dimStyle.Render("last time: " + m.runs[0].summary())
	}
	return []string{first, dimStyle.Render(filepath.Base(m.project.Name))}
}

// item is one row of a list pane; index is its position in the model's slice.
type item struct {
	label string
	style lipgloss.Style
	index int
}

func (m Model) schemeRows(rows, width int) []string {
	var items []item
	for _, i := range m.schemeIndices() {
		items = append(items, item{label: m.schemes[i].Name, index: i})
	}
	current := slices.IndexFunc(m.schemes, func(s xcode.Scheme) bool { return s.Name == m.scheme.Name })
	return m.list(items, current, at(m.schemeIndices(), m.schemeCursor), m.focus == schemePane, rows, width)
}

func (m Model) simulatorRows(rows, width int) []string {
	var items []item
	for _, i := range m.simulatorIndices() {
		s := m.simulators[i]
		label := s.Name + " " + s.Runtime
		if m.platform != 0 || s.Platform() == "iOS" {
			label = s.Name + " " + strings.TrimPrefix(s.Runtime, s.Platform()+" ") // the tab or the norm says which OS
		}
		if s.State == "Booted" {
			label += " ●"
		}
		items = append(items, item{label: label, index: i})
	}
	current := slices.IndexFunc(m.simulators, func(s xcode.Simulator) bool { return s.UDID == m.simulator.UDID })
	return m.list(items, current, at(m.simulatorIndices(), m.simulatorCursor), m.focus == simulatorPane, rows, width)
}

// runItems lists the history newest first, one level deep: a run that reached its
// app gets a console row right above its own row.
func (m Model) runItems() []item {
	var items []item
	for i, r := range m.runs {
		if r.console() != nil {
			items = append(items, item{r.consoleRow(), m.runStyle(r), encodeRunIndex(i, 1)})
		}
		items = append(items, item{r.row(), m.runStyle(r), encodeRunIndex(i, -1)})
	}
	return items
}

func (m Model) runRows(rows, width int) []string {
	items := m.runItems()
	selected := -1
	if len(items) > 0 {
		selected = items[clamp(m.runCursor, len(items))].index
	}
	return m.list(items, selected, selected, m.focus == runsPane, rows, width)
}

// at is idx[cursor], or -1 when the (filtered) list is empty.
func at(idx []int, cursor int) int {
	if cursor < len(idx) {
		return idx[cursor]
	}
	return -1
}

// list renders items with "*" on the current one and the cursor row filled across
// the pane's width when it has focus, windowed so the cursor row stays visible.
func (m Model) list(items []item, current, cursor int, focused bool, rows, width int) []string {
	pos := max(0, slices.IndexFunc(items, func(it item) bool { return it.index == cursor }))
	first := max(0, pos-rows+1)
	var out []string
	for i := first; i < min(first+rows, len(items)); i++ {
		it := items[i]
		marker := "  "
		if it.index == current {
			marker = "* "
		}
		row := it.style.Render(marker + it.label)
		if focused && it.index == cursor {
			row = it.style.Inherit(m.selection).Width(width).Render(fit(marker+it.label, width))
		}
		out = append(out, row)
	}
	return out
}

func (m Model) detailTitle() string {
	r, p := m.shownRun(), m.shownPhase()
	if r == nil {
		return "Detail"
	}
	title := "Detail: " + r.buildSummary()
	if p == r.console() {
		title = "Detail: " + r.consoleSummary()
	}
	if m.showIssues && p.filter == "" {
		title += " · issues only"
	}
	if !p.shown(m.showIssues).follow {
		title += " · G follows"
	}
	return title
}

func (m Model) detailRows() []string {
	p := m.shownPhase()
	if p == nil {
		return []string{dimStyle.Render("b build · r run on the simulator · t test · c clean")}
	}
	var rows []string
	for _, line := range p.shown(m.showIssues).visible() {
		style := styleOf(line)
		for i, row := range wrap(p.display(line), p.width) {
			if stamp, text := splitStamp(row); i == 0 && p.stamps && p.name == "console" && stamp != "" {
				row = dimStyle.Render(stamp) + style.Render(text)
			} else {
				row = style.Render(row)
			}
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		hint := "waiting for output"
		switch {
		case p.filter != "":
			hint = "no line matches"
		case m.showIssues:
			hint = "No errors or warnings. i shows the full log."
		}
		rows = []string{dimStyle.Render(hint)}
	}
	return rows
}

func (m Model) runStyle(r *run) lipgloss.Style {
	switch r.outcome {
	case "succeeded", "exited":
		return successStyle
	case "failed":
		return errorStyle
	case "stopped":
		return warningStyle
	}
	return lipgloss.NewStyle()
}

func styleOf(line string) lipgloss.Style {
	switch kindOf(line) {
	case errorLine:
		return errorStyle
	case warningLine:
		return warningStyle
	}
	return lipgloss.NewStyle()
}

func count(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// keys is every binding, in help order. The footer shows the first few.
var keys = [][2]string{
	{"b", "build"}, {"r", "run"}, {"t", "test"}, {"c", "clean"}, {"p", "resolve packages"}, {"x", "stop"},
	{"0-4", "focus a pane"}, {"tab / shift+tab", "next / previous pane"}, {"[ / ]", "tabs of Scheme (schemes, packages) or Destinations (all, iOS, tvOS, …)"},
	{"j / k", "move in the pane"}, {"enter / space", "select the scheme or destination"},
	{"R", "rename the simulator under the cursor"},
	{"/", "filter the pane (lists, or the shown log); enter keeps, esc clears"},
	{"g / G", "top / bottom"}, {"ctrl+u / ctrl+d", "half a page of the log"}, {"pgup / pgdn", "a page"},
	{"+ / _", "expand the left pane / restore"}, {"i", "issues only"}, {"s", "show / hide console timestamps"}, {"e", "open the shown log in $EDITOR"},
	{"D", "delete the run history"}, {"? / F1", "this help"}, {"q", "quit"},
}

func (m Model) footer() string {
	if m.typing {
		return fit(dimStyle.Render("type to filter  ")+boldStyle.Render("enter")+dimStyle.Render(" keep  ")+boldStyle.Render("esc")+dimStyle.Render(" clear"), m.width)
	}
	var parts []string
	for _, k := range [][2]string{keys[0], keys[1], keys[2], keys[3], keys[4], keys[5], {"0-4/tab", "focus"}, {"e", "editor"}, {"?", "help"}, {"q", "quit"}} {
		parts = append(parts, boldStyle.Render(k[0])+" "+dimStyle.Render(k[1]))
	}
	return fit(strings.Join(parts, "  "), m.width)
}

func (m Model) helpBox() string {
	rows := []string{}
	for _, k := range keys {
		rows = append(rows, boldStyle.Render(fmt.Sprintf("%-16s", k[0]))+" "+k[1])
	}
	rows = append(rows, "", dimStyle.Render("any key closes"))
	return frame("Keys", "", true, 64, len(rows)+2, rows)
}

// deleteBox asks before D deletes the history.
func (m Model) deleteBox() string {
	rows := []string{
		"Delete " + count(m.deletableRuns(), "run") + " from the history?",
		"",
		boldStyle.Render("y") + dimStyle.Render(" yes  ") + boldStyle.Render("n") + dimStyle.Render(" no"),
	}
	return frame("Delete history", "", true, 40, len(rows)+2, rows)
}

// renameBox edits the simulator's name for R; as wide as the help, for long names.
func (m Model) renameBox() string {
	rows := []string{
		m.renameText + "▏",
		"",
		boldStyle.Render("enter") + dimStyle.Render(" rename  ") + boldStyle.Render("esc") + dimStyle.Render(" cancel"),
	}
	return frame("Rename simulator", "", true, 64, len(rows)+2, rows)
}

// fit cuts s to one row of at most width cells.
func fit(s string, width int) string {
	return lipgloss.NewStyle().Inline(true).MaxWidth(width).Render(s)
}

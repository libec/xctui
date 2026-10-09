package tui

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/libec/xctui/internal/xcode"
)

type state int

const (
	idle       state = iota
	building         // xcodebuild build, clean or test
	installing       // build settings, boot, install, launch
	running          // the app runs; its console streams into the log
)

// pane is what has keyboard focus. The numbers double as the keys that jump to it.
type pane int

const (
	detailPane    pane = iota // 0
	statusPane                // 1
	schemePane                // 2
	simulatorPane             // 3
	runsPane                  // 4
	paneCount
)

// schemeTab is which schemes the Scheme pane lists; [ and ] switch.
type schemeTab int

const (
	ownSchemes schemeTab = iota
	packageSchemes
	schemeTabCount
)

var schemeTabNames = []string{"Schemes", "Packages"}

// destinationTabs are the Destinations pane's tabs: every destination, then one per platform.
var destinationTabs = []string{"Destinations", "iOS", "tvOS", "watchOS", "macOS"}

type Model struct {
	project    xcode.Project
	schemes    []xcode.Scheme
	simulators []xcode.Simulator
	scheme     xcode.Scheme
	simulator  xcode.Simulator
	prefsPath  string
	remembered selection
	historyDir string // saved runs; empty disables saving (tests)

	state     state
	runAfter  bool // r: install and launch once the build succeeds
	stopping  bool
	cancel    context.CancelFunc // stops the current step
	stream    *xcode.Stream      // output of the current step, if it has one
	logStream *xcode.Stream      // the app's unified log while it runs
	current   *run               // the run the current job writes to

	runs           []*run // newest first
	showIssues     bool
	showTimestamps bool // s: console lines show their time of day

	focus     pane
	leftFocus pane              // the left pane shown alone when expanded
	filters   [paneCount]string // / narrows the Scheme or Destinations list; Detail keeps its filter per phase
	typing    bool              // keys go into the focused pane's filter until enter or esc
	expanded  bool              // +: the focused left pane takes the whole column
	showHelp  bool
	deleting  bool // D: the popup asking to delete the history is open
	tab       schemeTab
	platform  int // index into destinationTabs

	renaming   xcode.Simulator // R: the simulator the rename popup edits; no UDID while it is closed
	renameText string

	schemeCursor    int
	simulatorCursor int
	runCursor       int // a row of runItems: a run, or a phase under it

	status      string // a message that replaces the run summary in Status until the next job
	statusStyle lipgloss.Style
	selection   lipgloss.Style // the cursor row: a tint of the terminal's background, once known
	width       int
	height      int
}

type schemesMsg struct {
	schemes []xcode.Scheme
	err     error
}

type simulatorsMsg struct {
	simulators []xcode.Simulator
	err        error
}

// streamStartedMsg reports that a step's process runs, or why it could not start.
// For the launch step it also carries the app's unified log stream and name.
type streamStartedMsg struct {
	stream *xcode.Stream
	logs   *xcode.Stream
	app    string
	err    error
}

type linesMsg struct {
	stream *xcode.Stream
	lines  []string
}

// streamDoneMsg reports that a stream's process has exited.
type streamDoneMsg struct {
	stream *xcode.Stream
	err    error
}

// editorDoneMsg reports that the editor opened with e has exited.
type editorDoneMsg struct{ err error }

// renamedMsg reports how simctl rename went for R; simulator is as it was before.
type renamedMsg struct {
	simulator xcode.Simulator
	name      string
	err       error
}

func New(project xcode.Project, prefsPath string) Model {
	dir := historyDir(prefsPath, project.Name)
	return Model{
		project:    project,
		prefsPath:  prefsPath,
		remembered: loadPrefs(prefsPath)[project.Name],
		historyDir: dir,
		runs:       loadRuns(dir),
		focus:      schemePane,
		leftFocus:  schemePane,
		selection:  lipgloss.NewStyle().Reverse(true).Bold(true),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadSchemes(m.project), loadSimulators, tea.RequestBackgroundColor)
}

func loadSchemes(project xcode.Project) tea.Cmd {
	return func() tea.Msg {
		schemes, err := xcode.ParseSchemes(project)
		return schemesMsg{schemes, err}
	}
}

func loadSimulators() tea.Msg {
	simulators, err := xcode.ParseSimulators()
	return simulatorsMsg{simulators, err}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.relayout()

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.BackgroundColorMsg:
		// One solid fill a shade off the background, like lazygit's selectedLineBgColor;
		// reverse would flip each coloured span on its own.
		bg := lipgloss.Darken(msg.Color, 0.09)
		if msg.IsDark() {
			bg = lipgloss.Lighten(msg.Color, 0.08)
		}
		m.selection = lipgloss.NewStyle().Background(bg).Bold(true)

	case schemesMsg:
		if msg.err != nil {
			m.setStatus("Couldn't list schemes: "+msg.err.Error(), errorStyle)
			break
		}
		m.schemes = msg.schemes
		if i := slices.IndexFunc(m.schemes, func(s xcode.Scheme) bool { return s.Name == m.remembered.Scheme }); i >= 0 {
			m.scheme = m.schemes[i]
		} else {
			m.scheme, _ = xcode.DefaultScheme(m.project, m.schemes)
			if m.remembered.Scheme != "" {
				m.setStatus(fmt.Sprintf("No scheme %q; using %s", m.remembered.Scheme, m.scheme.Name), warningStyle)
			}
		}
		if m.scheme.Package {
			m.tab = packageSchemes
		}
		m.schemeCursor = max(0, slices.Index(m.schemeIndices(), slices.Index(m.schemes, m.scheme)))

	case simulatorsMsg:
		if msg.err != nil {
			m.setStatus("Couldn't list simulators: "+msg.err.Error(), errorStyle)
			break
		}
		m.simulators = msg.simulators
		if i := slices.IndexFunc(m.simulators, func(s xcode.Simulator) bool { return s.UDID == m.remembered.Simulator }); i >= 0 {
			m.simulator = m.simulators[i]
		} else {
			m.simulator, _ = xcode.DefaultSimulator(m.simulators)
		}
		m.simulatorCursor = max(0, slices.Index(m.simulatorIndices(), slices.Index(m.simulators, m.simulator)))

	case streamStartedMsg:
		if msg.err != nil {
			return m.finish(msg.err)
		}
		m.stream = msg.stream
		cmds := []tea.Cmd{waitForLines(m.stream)}
		if m.state == installing {
			m.state = running
			m.current.status = "attached"
			m.current.last().append("▸ Launched " + msg.app + ". Its output is in the console row above.")
			m.current.app, m.current.attached = msg.app, time.Now()
			m.current.addPhase("console", m.layout())
			if msg.logs != nil {
				m.logStream = msg.logs
				cmds = append(cmds, waitForLines(m.logStream))
			}
		}
		return m, tea.Batch(cmds...)

	case linesMsg:
		var lines []string
		switch {
		case m.isLogStream(msg.stream):
			for _, line := range msg.lines {
				if clean, ok := xcode.CleanLogLine(line); ok {
					lines = append(lines, clean) // keeps the log's own time
				}
			}
		case m.state == running: // mirrored os_log lines carry their time; the app's stdout does not
			stamp := time.Now().Format(stampLayout) + " "
			for _, line := range msg.lines {
				if mirrored, ok := xcode.CleanMirroredLine(line); ok {
					lines = append(lines, mirrored)
				} else {
					lines = append(lines, stamp+line)
				}
			}
		default:
			lines = msg.lines
		}
		m.appendLines(lines...)
		return m, waitForLines(msg.stream)

	case streamDoneMsg:
		if m.isLogStream(msg.stream) {
			m.logStream = nil // the app's log ends with the app; the launch stream reports that
			return m, nil
		}
		return m.finish(msg.err)

	case editorDoneMsg:
		if msg.err != nil {
			m.setStatus("Editor: "+msg.err.Error(), errorStyle)
		}

	case renamedMsg:
		if msg.err != nil {
			m.setStatus("Couldn't rename the simulator: "+msg.err.Error(), errorStyle)
			break
		}
		for i := range m.simulators {
			if m.simulators[i].UDID == msg.simulator.UDID {
				m.simulators[i].Name = msg.name
			}
		}
		if m.simulator.UDID == msg.simulator.UDID {
			m.simulator.Name = msg.name
		}
		m.setStatus("Renamed "+msg.simulator.Name+" to "+msg.name, dimStyle)
	}
	return m, nil
}

// isLogStream tells the app's unified log apart from the job's own stream.
func (m Model) isLogStream(s *xcode.Stream) bool {
	return m.logStream != nil && s == m.logStream
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.showHelp {
		m.showHelp = key == "" // any key closes it
		return m, nil
	}
	if m.deleting {
		switch key {
		case "y":
			m.deleting = false
			m.deleteHistory()
		case "n", "esc":
			m.deleting = false
		case "ctrl+c":
			return m, tea.Quit
		}
		return m, nil // the popup takes every other key
	}
	if m.renaming.UDID != "" {
		switch key {
		case "enter":
			return m.rename()
		case "esc":
			m.renaming = xcode.Simulator{}
		case "backspace":
			m.renameText = dropLastRune(m.renameText)
		case "ctrl+c":
			return m, tea.Quit
		default:
			m.renameText += msg.Text
		}
		return m, nil
	}
	if m.typing {
		text := m.filterText()
		switch key {
		case "esc":
			text, m.typing = "", false
		case "enter":
			m.typing = false
		case "backspace":
			text = dropLastRune(text)
		case "ctrl+c":
			return m, tea.Quit
		default:
			text += msg.Text
		}
		m.setFilterText(text)
		m.move(0) // keep the cursor inside the narrowed list
		return m, nil
	}
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "/":
		if m.focus != statusPane && m.focus != runsPane && (m.focus != detailPane || m.shownPhase() != nil) {
			m.typing = true
		}
	case "esc":
		m.setFilterText("")
	case "]":
		m.switchTab(1)
	case "[":
		m.switchTab(-1)
	case "?", "f1":
		m.showHelp = true
	case "+":
		m.expanded = true
	case "_":
		m.expanded = false
	case "e":
		return m, m.openEditor()
	case "i":
		m.showIssues = !m.showIssues
	case "s":
		m.showTimestamps = !m.showTimestamps
		m.relayout()
	case "ctrl+d":
		m.scrollDetail(m.detailHeight() / 2)
	case "ctrl+u":
		m.scrollDetail(-m.detailHeight() / 2)
	case "b":
		return m.startBuild(xcode.ActionBuild, false)
	case "r":
		return m.startBuild(xcode.ActionBuild, true)
	case "t":
		return m.startBuild(xcode.ActionTest, false)
	case "c":
		return m.startBuild(xcode.ActionClean, false)
	case "p":
		project := m.project
		return m.startJob("resolve", false, func(ctx context.Context) (*xcode.Stream, error) {
			return xcode.ResolvePackages(ctx, project)
		})
	case "x":
		m.stop()
	case "D":
		switch {
		case m.deletableRuns() > 0:
			m.deleting = true
		case m.state != idle:
			m.setStatus("Only the running job is left; x stops it", warningStyle)
		default:
			m.setStatus("No runs to delete", warningStyle)
		}
	case "R":
		if idx := m.simulatorIndices(); m.focus == simulatorPane && len(idx) > 0 && !m.simulators[idx[m.simulatorCursor]].IsMac() {
			m.renaming = m.simulators[idx[m.simulatorCursor]]
			m.renameText = m.renaming.Name
		}
	case "0", "1", "2", "3", "4":
		m.setFocus(pane(key[0] - '0'))
	case "tab":
		m.setFocus(m.nextPane(1))
	case "shift+tab":
		m.setFocus(m.nextPane(-1))
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "pgdown":
		m.move(m.detailHeight())
	case "pgup":
		m.move(-m.detailHeight())
	case "g", "home":
		m.move(-1 << 30)
	case "G", "end":
		m.move(1 << 30)
	case "enter", "space":
		m.choose()
	}
	return m, nil
}

// switchTab cycles the focused pane's tabs: the Scheme pane's or the Destinations pane's.
func (m *Model) switchTab(step int) {
	switch m.focus {
	case schemePane:
		m.tab = (m.tab + schemeTab(step) + schemeTabCount) % schemeTabCount
	case simulatorPane:
		n := len(destinationTabs)
		m.platform = (m.platform + step + n) % n
	default:
		return
	}
	m.move(0)
}

func (m *Model) setFocus(p pane) {
	if !m.focusable(p) {
		return
	}
	m.focus = p
	m.typing = false
	if p != detailPane {
		m.leftFocus = p
	}
}

// focusable is false for a pane with nothing in it: there is nothing to do there.
func (m Model) focusable(p pane) bool {
	switch p {
	case schemePane:
		return len(m.schemes) > 0
	case simulatorPane:
		return len(m.simulators) > 0
	case runsPane, detailPane:
		return len(m.runs) > 0
	}
	return true
}

// nextPane is the next (or previous) focusable pane after the focused one.
func (m Model) nextPane(step int) pane {
	p := m.focus
	for range paneCount {
		p = (p + pane(step) + paneCount) % paneCount
		if m.focusable(p) {
			return p
		}
	}
	return m.focus
}

// filterText is the focused pane's filter: the shown phase's for Detail.
func (m Model) filterText() string {
	if m.focus == detailPane {
		if p := m.shownPhase(); p != nil {
			return p.filter
		}
		return ""
	}
	return m.filters[m.focus]
}

func (m *Model) setFilterText(text string) {
	if m.focus == detailPane {
		if p := m.shownPhase(); p != nil {
			p.setFilter(text)
		}
		return
	}
	m.filters[m.focus] = text
}

// schemeIndices are the schemes shown in the pane, after the tab and the filter.
func (m Model) schemeIndices() []int {
	var idx []int
	for i, s := range m.schemes {
		if s.Package != (m.tab == packageSchemes) {
			continue
		}
		if matches(s.Name, m.filters[schemePane]) {
			idx = append(idx, i)
		}
	}
	return idx
}

// simulatorIndices are the destinations shown in the pane, after the platform tab
// and the filter.
func (m Model) simulatorIndices() []int {
	var idx []int
	for i, s := range m.simulators {
		if m.platform != 0 && s.Platform() != destinationTabs[m.platform] {
			continue
		}
		if matches(s.Name+" "+s.Runtime, m.filters[simulatorPane]) {
			idx = append(idx, i)
		}
	}
	return idx
}

func matches(text, filter string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(filter))
}

// scrollDetail scrolls the shown log whichever pane has focus (ctrl+u/ctrl+d).
func (m *Model) scrollDetail(delta int) {
	if p := m.shownPhase(); p != nil {
		p.shown(m.showIssues).scroll(delta)
	}
}

// openEditor writes the shown log to a temp file and opens it in $VISUAL or
// $EDITOR; Bubble Tea pauses until the editor exits.
func (m *Model) openEditor() tea.Cmd {
	p := m.shownPhase()
	if p == nil {
		m.setStatus("No run to open", warningStyle)
		return nil
	}
	f, err := os.CreateTemp("", "xctui-"+p.name+"-*.log")
	if err == nil {
		_, err = f.WriteString(strings.Join(p.shown(m.showIssues).lines, "\n") + "\n")
		f.Close()
	}
	if err != nil {
		m.setStatus("Couldn't write the log: "+err.Error(), errorStyle)
		return nil
	}
	editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	c := exec.Command(editor, f.Name())
	return tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{err} })
}

// move applies j/k to whichever pane has focus.
func (m *Model) move(delta int) {
	switch m.focus {
	case detailPane:
		m.scrollDetail(delta)
	case schemePane:
		m.schemeCursor = clamp(m.schemeCursor+delta, len(m.schemeIndices()))
	case simulatorPane:
		m.simulatorCursor = clamp(m.simulatorCursor+delta, len(m.simulatorIndices()))
	case runsPane:
		m.runCursor = clamp(m.runCursor+delta, len(m.runItems()))
	}
}

// choose applies enter: the cursor's scheme or simulator becomes the current one.
func (m *Model) choose() {
	if m.focus != schemePane && m.focus != simulatorPane {
		return
	}
	if m.state != idle {
		m.setStatus(busyStatus, warningStyle) // a run installs whatever was built
		return
	}
	if idx := m.schemeIndices(); m.focus == schemePane && len(idx) > 0 {
		m.scheme = m.schemes[idx[m.schemeCursor]]
	}
	if idx := m.simulatorIndices(); m.focus == simulatorPane && len(idx) > 0 {
		m.simulator = m.simulators[idx[m.simulatorCursor]]
	}
	m.savePrefs()
}

// rename closes the R popup and renames the simulator in the background; a blank
// or unchanged name does nothing.
func (m Model) rename() (tea.Model, tea.Cmd) {
	simulator, name := m.renaming, strings.TrimSpace(m.renameText)
	m.renaming = xcode.Simulator{}
	if name == "" || name == simulator.Name {
		return m, nil
	}
	return m, func() tea.Msg {
		return renamedMsg{simulator, name, xcode.Rename(simulator, name)}
	}
}

func dropLastRune(s string) string {
	runes := []rune(s)
	return string(runes[:max(0, len(runes)-1)])
}

func clamp(i, n int) int {
	return max(0, min(i, n-1))
}

const busyStatus = "Busy — press x to stop"

// deletableRuns is how many runs D deletes: all but a running job, which is the newest.
func (m Model) deletableRuns() int {
	if m.state != idle {
		return len(m.runs) - 1
	}
	return len(m.runs)
}

// deleteHistory empties Runs and removes the project's saved runs from disk; a
// job that runs stays.
func (m *Model) deleteHistory() {
	n := m.deletableRuns()
	if err := os.RemoveAll(m.historyDir); err != nil {
		m.setStatus("Couldn't delete the runs: "+err.Error(), errorStyle)
		return
	}
	m.runs, m.runCursor = m.runs[:len(m.runs)-n], 0
	if len(m.runs) == 0 {
		m.current = nil
	}
	if !m.focusable(m.focus) {
		m.setFocus(statusPane)
	}
	m.setStatus("Deleted "+count(n, "run"), dimStyle)
}

func (m Model) startBuild(action xcode.Action, runAfter bool) (tea.Model, tea.Cmd) {
	if m.scheme.Name == "" || m.simulator.UDID == "" {
		m.setStatus("Pick a scheme (2) and a destination (3) first", warningStyle)
		return m, nil
	}
	kind := string(action)
	if runAfter {
		kind = "run"
	}
	project, scheme, simulator := m.project, m.scheme, m.simulator
	return m.startJob(kind, runAfter, func(ctx context.Context) (*xcode.Stream, error) {
		return xcode.Build(ctx, action, project, scheme, simulator)
	})
}

// startJob records a new run and starts its xcodebuild process.
func (m Model) startJob(kind string, runAfter bool, start func(context.Context) (*xcode.Stream, error)) (tea.Model, tea.Cmd) {
	if m.state != idle {
		m.setStatus(busyStatus, warningStyle)
		return m, nil
	}
	m.state, m.runAfter = building, runAfter
	m.current = newRun(kind, m.layout())
	if runAfter {
		m.current.status = "building"
	}
	m.runs = append([]*run{m.current}, m.runs...)
	if len(m.runs) > maxRuns {
		m.runs = m.runs[:maxRuns]
	}
	m.runCursor = 0 // Detail follows the new job
	m.setStatus("", lipgloss.NewStyle())

	ctx := m.newContext()
	return m, func() tea.Msg {
		stream, err := start(ctx)
		return streamStartedMsg{stream: stream, err: err}
	}
}

// installAndLaunch runs the steps after a successful build for r, then follows
// the app's console and its unified log.
func installAndLaunch(ctx context.Context, project xcode.Project, scheme xcode.Scheme, simulator xcode.Simulator) tea.Cmd {
	return func() tea.Msg {
		settings, err := xcode.ParseBuildSettings(ctx, project, scheme, simulator)
		if err != nil {
			return streamStartedMsg{err: err}
		}
		var stream *xcode.Stream
		if simulator.IsMac() {
			stream, err = xcode.LaunchMac(ctx, settings)
		} else if err = xcode.Install(ctx, simulator, settings.AppPath); err == nil {
			stream, err = xcode.Launch(ctx, simulator, settings.BundleID)
		}
		if err != nil {
			return streamStartedMsg{err: err}
		}
		logs, _ := xcode.LogStream(ctx, simulator, settings) // best effort
		return streamStartedMsg{stream: stream, logs: logs, app: settings.BundleID}
	}
}

// maxBatch caps how many lines one linesMsg carries.
const maxBatch = 1000

// waitForLines reads the stream's next lines. Update issues it again after every
// linesMsg, so there is always exactly one read in flight per stream until it ends.
func waitForLines(stream *xcode.Stream) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-stream.Lines
		if !ok {
			return streamDoneMsg{stream, stream.Wait()}
		}
		// Take the lines already waiting too: Bubble Tea redraws after every message,
		// and a build prints thousands of lines.
		lines := []string{line}
		for len(lines) < maxBatch {
			select {
			case line, ok := <-stream.Lines:
				if !ok {
					return linesMsg{stream, lines} // the next read reports the end
				}
				lines = append(lines, line)
			default:
				return linesMsg{stream, lines}
			}
		}
		return linesMsg{stream, lines}
	}
}

// newContext starts a step; x cancels the context it returns.
func (m *Model) newContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	return ctx
}

func (m *Model) stop() {
	if m.state == idle || m.stopping {
		return
	}
	m.stopping = true
	m.cancel()
}

// finish ends the current step: its process exited, or it could not start.
// A build started with r moves on to install and launch.
func (m Model) finish(err error) (tea.Model, tea.Cmd) {
	m.cancel() // releases the context; the app's log stream ends with it
	m.stream = nil
	step, stopped := m.state, m.stopping
	m.state, m.stopping = idle, false

	switch {
	case stopped:
		m.current.finish("stopped")
	case step == building && err != nil:
		m.current.finish("failed")
		m.appendLines(fmt.Sprintf("error: %v", err))
	case step == building && m.runAfter:
		m.state = installing
		m.current.status = "installing"
		m.current.finishBuild("succeeded")
		m.appendLines("▸ Installing and launching on " + m.simulator.Name)
		return m, installAndLaunch(m.newContext(), m.project, m.scheme, m.simulator)
	case step == building:
		m.current.finish("succeeded")
	case step == installing:
		m.current.finish("failed")
		m.appendLines(strings.Split(fmt.Sprintf("error: %v", err), "\n")...)
	case step == running:
		m.current.finish("exited") // the app exited on its own
	}
	if err := saveRun(m.historyDir, m.current); err != nil {
		m.setStatus("Couldn't save the run: "+err.Error(), errorStyle)
	}
	return m, nil
}

// appendLines adds to the phase the job is in, with paths relative to the project.
func (m *Model) appendLines(lines ...string) {
	dir := projectDir(m.project.Name)
	for i, line := range lines {
		lines[i] = strings.ReplaceAll(line, dir, "")
	}
	m.current.last().append(lines...)
}

// shownPhase is the phase Detail shows: the run's build for a run row, the app's
// output for a console row.
func (m Model) shownPhase() *phase {
	items := m.runItems()
	if len(items) == 0 {
		return nil
	}
	r, j := decodeRunIndex(items[clamp(m.runCursor, len(items))].index)
	p := m.runs[r].phases[max(0, j)]
	if p.file != "" {
		p.setLayout(m.layout())
		p.load()
	}
	return p
}

// shownRun is the run under the Runs cursor.
func (m Model) shownRun() *run {
	items := m.runItems()
	if len(items) == 0 {
		return nil
	}
	r, _ := decodeRunIndex(items[clamp(m.runCursor, len(items))].index)
	return m.runs[r]
}

// Runs rows are encoded as run*4 + phase+1, phase -1 being the run's own row.
func encodeRunIndex(run, phase int) int { return run*4 + phase + 1 }
func decodeRunIndex(index int) (run, phase int) {
	return index / 4, index%4 - 1
}

func (m *Model) savePrefs() {
	if m.prefsPath == "" {
		return
	}
	s := selection{Scheme: m.scheme.Name, Simulator: m.simulator.UDID}
	if err := savePrefs(m.prefsPath, m.project.Name, s); err != nil {
		m.setStatus("Couldn't remember the selection: "+err.Error(), errorStyle)
	}
}

func (m *Model) setStatus(status string, style lipgloss.Style) {
	m.status, m.statusStyle = status, style
}

// detailHeight is the number of log rows the Detail pane shows: the window minus
// the key help, minus the pane's own border.
func (m Model) detailHeight() int {
	return max(0, m.height-3)
}

// layout is what Detail gives its phases: its inner size, and whether console
// lines show their time of day.
func (m Model) layout() layout {
	return layout{width: max(0, m.width-max(minLeftWidth, m.width/3)-2), height: m.detailHeight(), stamps: m.showTimestamps}
}

// relayout hands the layout to every phase of every run.
func (m Model) relayout() {
	for _, r := range m.runs {
		r.setLayout(m.layout())
	}
}

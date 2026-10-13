package tui

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/libec/xctui/internal/xcode"
)

var (
	iPhone17Pro = xcode.Simulator{Name: "iPhone 17 Pro", Runtime: "iOS 26.5", State: "Booted", UDID: "A"}
	iPhoneAir   = xcode.Simulator{Name: "iPhone Air", Runtime: "iOS 26.5", State: "Shutdown", UDID: "B"}
	appleTV     = xcode.Simulator{Name: "Apple TV 4K", Runtime: "tvOS 26.2", State: "Shutdown", UDID: "C"}
)

// newTestModel is a model after startup: sized, schemes and simulators loaded.
func newTestModel(prefsPath string) Model {
	m := New(xcode.Project{Name: "/src/Puzzle.xcodeproj"}, prefsPath)
	m, _ = update(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = update(m, schemesMsg{schemes: []xcode.Scheme{{Name: "PuzzleKit"}, {Name: "Puzzle"}, {Name: "Net", Package: true}}})
	m, _ = update(m, simulatorsMsg{simulators: []xcode.Simulator{iPhone17Pro, iPhoneAir, appleTV}})
	return m
}

func update(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

// press sends key presses; the returned command is the last key's.
func press(m Model, keys ...string) (Model, tea.Cmd) {
	var cmd tea.Cmd
	for _, k := range keys {
		msg := tea.KeyPressMsg{Code: rune(k[0]), Text: k}
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "shift+tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
		}
		m, cmd = update(m, msg)
	}
	return m, cmd
}

// lines feeds output of the current job stream, as the reader would.
func lines(m Model, text ...string) Model {
	m, _ = update(m, linesMsg{m.stream, text})
	return m
}

func done(m Model, err error) (Model, tea.Cmd) {
	return update(m, streamDoneMsg{m.stream, err})
}

func TestDefaultsAfterLoading(t *testing.T) {
	m := newTestModel("")
	if m.scheme.Name != "Puzzle" || m.schemeCursor != 1 {
		t.Errorf("scheme = %q (cursor %d), want the one named after the project", m.scheme.Name, m.schemeCursor)
	}
	if m.simulator != iPhone17Pro || m.simulatorCursor != 0 {
		t.Errorf("simulator = %v, want the booted one", m.simulator)
	}
	if m.focus != schemePane {
		t.Errorf("focus = %v, want Scheme", m.focus)
	}
}

func TestEmptyPanesAreSkipped(t *testing.T) {
	m := newTestModel("")
	m, _ = press(m, "4")
	if m.focus != schemePane {
		t.Errorf("4 with no runs should do nothing, got %v", m.focus)
	}
	m, _ = press(m, "3", "tab")
	if m.focus != statusPane {
		t.Errorf("tab from Destinations should skip empty Runs and Detail, got %v", m.focus)
	}
	m, _ = press(m, "shift+tab")
	if m.focus != simulatorPane {
		t.Errorf("shift+tab back should skip them too, got %v", m.focus)
	}
	m, _ = press(m, "b", "4")
	if m.focus != runsPane {
		t.Errorf("4 with a run should focus Runs, got %v", m.focus)
	}
}

func TestFocusKeys(t *testing.T) {
	m := newTestModel("")
	m, _ = press(m, "b") // Runs and Detail need a run to be focusable
	for key, want := range map[string]pane{"0": detailPane, "1": statusPane, "2": schemePane, "3": simulatorPane, "4": runsPane} {
		m, _ = press(m, key)
		if m.focus != want {
			t.Errorf("%s: focus = %v, want %v", key, m.focus, want)
		}
	}
	m, _ = press(m, "4", "tab")
	if m.focus != detailPane {
		t.Errorf("tab after Runs should wrap to Detail, got %v", m.focus)
	}
}

func TestChooseSchemeAndSimulator(t *testing.T) {
	prefsPath := filepath.Join(t.TempDir(), "prefs.json")
	m := newTestModel(prefsPath)

	m, _ = press(m, "2", "k", "enter")
	if m.scheme.Name != "PuzzleKit" {
		t.Errorf("enter in Scheme should pick PuzzleKit, got %q", m.scheme.Name)
	}
	m, _ = press(m, "3", "j")
	if m.simulator != iPhone17Pro {
		t.Error("moving the cursor must not change the simulator until enter")
	}
	m, _ = press(m, "space")
	if m.simulator != iPhoneAir {
		t.Errorf("space in Simulator should pick iPhone Air, got %v", m.simulator)
	}

	// S1: a new session restores both picks.
	m = newTestModel(prefsPath)
	if m.scheme.Name != "PuzzleKit" || m.simulator != iPhoneAir {
		t.Errorf("remembered %q / %v, want PuzzleKit / iPhone Air", m.scheme.Name, m.simulator)
	}
}

func TestSchemeArgument(t *testing.T) {
	m := New(xcode.Project{Name: "/src/Puzzle.xcodeproj"}, "")
	m.remembered.Scheme = "Nope"
	m, _ = update(m, schemesMsg{schemes: []xcode.Scheme{{Name: "PuzzleKit"}, {Name: "Puzzle"}}})
	if m.scheme.Name != "Puzzle" || !strings.Contains(m.status, `No scheme "Nope"`) {
		t.Errorf("unknown scheme: got %q, status %q; want the default and a warning", m.scheme.Name, m.status)
	}
}

func TestBusyGuard(t *testing.T) {
	m := newTestModel("")
	m, cmd := press(m, "b")
	if m.state != building || cmd == nil {
		t.Fatalf("b should start a build, got state %v", m.state)
	}

	for _, k := range []string{"b", "r", "c", "t", "p"} {
		m, cmd = press(m, k)
		if cmd != nil || m.state != building || m.runs[0].kind != "build" || len(m.runs) != 1 {
			t.Errorf("%s while building should do nothing", k)
		}
		if m.status != busyStatus {
			t.Errorf("%s: status = %q, want %q", k, m.status, busyStatus)
		}
	}
	m, _ = press(m, "2", "k", "enter")
	if m.scheme.Name != "Puzzle" {
		t.Error("changing the scheme while busy should be refused")
	}
}

func TestStepResults(t *testing.T) {
	failed := errors.New("exit status 65")

	tests := []struct {
		name        string
		keys        []string
		doneErr     error
		wantState   state
		wantKind    string
		wantOutcome string
	}{
		{"build succeeds", []string{"b"}, nil, idle, "build", "succeeded"},
		{"build fails", []string{"b"}, failed, idle, "build", "failed"},
		{"clean", []string{"c"}, nil, idle, "clean", "succeeded"},
		{"resolve packages", []string{"p"}, nil, idle, "resolve", "succeeded"},
		{"test fails", []string{"t"}, failed, idle, "test", "failed"},
		{"run moves on to install", []string{"r"}, nil, installing, "run", "running"},
		{"run stops when the build fails", []string{"r"}, failed, idle, "run", "failed"},
		{"stopped build", []string{"b", "x"}, failed, idle, "build", "stopped"},
		{"stopped run does not install", []string{"r", "x"}, nil, idle, "run", "stopped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel("")
			m, _ = press(m, tt.keys...)
			m, cmd := done(m, tt.doneErr)

			r := m.runs[0]
			if m.state != tt.wantState || r.kind != tt.wantKind || r.outcome != tt.wantOutcome {
				t.Errorf("got state %v, run %s %s; want %v, %s %s", m.state, r.kind, r.outcome, tt.wantState, tt.wantKind, tt.wantOutcome)
			}
			if (cmd != nil) != (tt.wantState == installing) {
				t.Errorf("only a successful run should return a command, got %v", cmd)
			}
		})
	}
}

func TestRunHistory(t *testing.T) {
	m := newTestModel("")
	m, _ = press(m, "b")
	m = lines(m, "first build")
	m, _ = done(m, nil)
	m, _ = press(m, "4", "0", "k") // scroll Detail away from the tail
	m, _ = press(m, "t")
	m = lines(m, "the test")

	if len(m.runs) != 2 || m.runs[0].kind != "test" || m.runs[1].kind != "build" {
		t.Fatalf("runs = %v, want the test first, then the build", m.runs)
	}
	if m.runCursor != 0 || !m.shownPhase().log.follow {
		t.Error("a new job should select itself in Runs and follow its tail")
	}
	if got := m.shownPhase().log.lines; !slices.Equal(got, []string{"the test"}) {
		t.Errorf("Detail shows %v, want the running test's log", got)
	}

	m, _ = press(m, "4", "j")
	if got := m.shownPhase().log.lines; !slices.Equal(got, []string{"first build"}) {
		t.Errorf("j in Runs should show the older build, got %v", got)
	}

	for range maxRuns + 5 {
		m, _ = press(m, "c")
		m, _ = done(m, nil)
	}
	if len(m.runs) != maxRuns {
		t.Errorf("history holds %d runs, want the cap of %d", len(m.runs), maxRuns)
	}
}

func TestLogLines(t *testing.T) {
	m := newTestModel("")
	m, _ = press(m, "b")
	m = lines(m,
		"CompileSwift normal arm64 /src/Puzzle/Game.swift",
		"/src/Puzzle/Game.swift:3:1: error: cannot find 'x' in scope",
		"/src/Puzzle/Game.swift:3:1: error: cannot find 'x' in scope",
		"/src/Puzzle/Board.swift:9:5: warning: unused variable",
		" 13   -[Xcode3CommandLineBuildTool doBuildForBuildAction:timingSection:colorize:colorizeFailure:error:] (in Xcode3Core)",
		"** BUILD FAILED **",
	)

	p := m.runs[0].phases[0]
	if p.errors != 1 || p.warnings != 1 {
		t.Errorf("got %d errors, %d warnings; want 1 and 1, repeats counted once", p.errors, p.warnings)
	}
	if len(p.log.lines) != 6 {
		t.Errorf("log has %d lines, want all 6", len(p.log.lines))
	}
	if want := "Puzzle/Game.swift:3:1: error: cannot find 'x' in scope"; p.log.lines[1] != want {
		t.Errorf("got %q, want the path relative to the project folder", p.log.lines[1])
	}

	m, _ = press(m, "i")
	if got := m.shownPhase().shown(m.showIssues).lines; len(got) != 2 {
		t.Errorf("i should show only the 2 unique issues, got %v", got)
	}
}

func TestRunPhasesAndConsole(t *testing.T) {
	m := newTestModel("")
	m, _ = update(m, tea.WindowSizeMsg{Width: 160, Height: 24}) // room for the Detail titles
	m, _ = press(m, "r")
	m = lines(m, "CompileSwift", "** BUILD SUCCEEDED **")
	m, cmd := done(m, nil)
	r := m.runs[0]
	if m.state != installing || cmd == nil || r.status != "installing" || len(r.phases) != 1 || r.buildOutcome != "succeeded" {
		t.Fatalf("after the build: state %v, status %q, phases %d, build %q", m.state, r.status, len(r.phases), r.buildOutcome)
	}
	if !strings.Contains(m.render(), "✓ run") || !slices.Contains(r.phases[0].log.lines, "▸ Installing and launching on iPhone 17 Pro") {
		t.Error("the run row shows the build's verdict and the launch steps go in the build log")
	}

	app := &xcode.Stream{}
	logs := &xcode.Stream{}
	m, _ = update(m, streamStartedMsg{stream: app, logs: logs, app: "com.example.Puzzle"})
	m, _ = update(m, linesMsg{app, []string{"hello from stdout"}})
	m, _ = update(m, linesMsg{logs, []string{
		`Filtering the log data using "process == \"Puzzle\""`,
		"2026-09-25 10:23:23.361 Df Puzzle[19921:134cebb] [com.apple.UIKit:Application] os_log line",
	}})
	if m.state != running || r.status != "attached" || len(r.phases) != 2 || r.app != "com.example.Puzzle" {
		t.Fatalf("after the launch: state %v, status %q, phases %d", m.state, r.status, len(r.phases))
	}
	console := r.console()
	var texts []string
	for _, line := range console.log.lines {
		stamp, text := splitStamp(line)
		if stamp == "" {
			t.Errorf("console line %q has no timestamp", line)
		}
		texts = append(texts, text)
	}
	if want := []string{"hello from stdout", "[com.apple.UIKit:Application] os_log line"}; !slices.Equal(texts, want) {
		t.Errorf("console = %v, want stdout and the cleaned os_log line", console.log.lines)
	}
	if console.log.lines[1] != "10:23:23.361 [com.apple.UIKit:Application] os_log line" {
		t.Errorf("an os_log line keeps its own time, got %q", console.log.lines[1])
	}
	if screen := ansi.Strip(m.render()); strings.Contains(screen, "10:23:23.361") || !strings.Contains(screen, "│[com.apple.UIKit") {
		t.Errorf("timestamps are hidden by default:\n%s", screen)
	}
	m, _ = press(m, "s")
	if screen := ansi.Strip(m.render()); !strings.Contains(screen, "10:23:23.361 [com.apple.UIKit") {
		t.Errorf("s shows timestamps:\n%s", screen)
	}
	m, _ = press(m, "s")
	if slices.Contains(r.phases[0].log.lines, "hello from stdout") {
		t.Error("app output must not land in the build phase")
	}

	// Runs lists the console row right above the run's row; Detail follows the cursor.
	screen := m.render()
	for _, want := range []string{"● conso", "✓ run", "Detail: ● console com.example.Puzzle · attached"} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
	if strings.Contains(screen, "[5]") || strings.Count(screen, "[0]─Detail") != 1 || strings.Contains(screen, "launch") {
		t.Errorf("one Detail pane, no launch row:\n%s", screen)
	}
	if rows := m.runItems(); len(rows) != 2 || strings.Contains(rows[0].label, "run ") {
		t.Errorf("runs rows = %v, want console then run", rows)
	}
	m, _ = press(m, "4", "j") // console row -> run row
	if p := m.shownPhase(); p.name != "build" || !slices.Contains(p.log.lines, "CompileSwift") || !strings.Contains(m.render(), "run build succeeded") {
		t.Errorf("cursor on the run row should show its build log, got %q", p.name)
	}

	// The log stream ending does not end the run; the app's own stream does.
	m, _ = update(m, streamDoneMsg{logs, nil})
	if m.state != running || m.logStream != nil {
		t.Error("the unified log ending must not finish the run")
	}
	m, _ = update(m, streamDoneMsg{app, nil})
	if r.outcome != "exited" || m.state != idle || !strings.Contains(m.render(), "✓ run exited") {
		t.Errorf("app exit: outcome %q, state %v", r.outcome, m.state)
	}
	m, _ = press(m, "k")
	if !strings.Contains(m.render(), "Detail: ✓ console com.example.Puzzle · exited") {
		t.Error("the console row shows the app exited")
	}
}

func TestFiltersPersistPerPane(t *testing.T) {
	m := newTestModel("")
	m, _ = press(m, "2", "/", "k", "i", "t")
	if !m.typing || m.filters[schemePane] != "kit" || len(m.schemeIndices()) != 1 {
		t.Fatalf("typing kit: filter %q, typing %v, shown %v", m.filters[schemePane], m.typing, m.schemeIndices())
	}
	if screen := m.render(); !strings.Contains(screen, "└/kit▏") || strings.Contains(screen, "* Puzzle ") {
		t.Errorf("the filter belongs on the bottom border and Puzzle should be hidden:\n%s", screen)
	}
	m, _ = press(m, "backspace", "backspace", "backspace", "n", "e", "enter", "]")
	if m.typing || m.filters[schemePane] != "ne" || m.schemes[m.schemeIndices()[0]].Name != "Net" {
		t.Errorf("filter %q shows %v", m.filters[schemePane], m.schemeIndices())
	}
	m, _ = press(m, "3", "/", "a", "i", "r", "enter", "2")
	if m.filters[schemePane] != "ne" || m.filters[simulatorPane] != "air" || len(m.simulatorIndices()) != 1 {
		t.Error("each pane keeps its own filter across focus changes")
	}
	if screen := m.render(); !strings.Contains(screen, "└/ne─") || !strings.Contains(screen, "└/air─") {
		t.Errorf("both filters should show on their panes:\n%s", screen)
	}
	m, _ = press(m, "esc")
	if m.filters[schemePane] != "" || m.filters[simulatorPane] != "air" {
		t.Error("esc clears only the focused pane's filter")
	}
	m, _ = press(m, "/", "q", "q")
	if len(m.schemeIndices()) != 0 || m.render() == "" {
		t.Error("an empty filtered list should still render")
	}
}

func TestDetailFilterPerPhase(t *testing.T) {
	m := newTestModel("")
	m, _ = press(m, "b")
	m = lines(m, "one apple", "two pears", "three apples")
	m, _ = press(m, "0", "/", "app", "enter")
	p := m.shownPhase()
	if got := p.shown(false).lines; !slices.Equal(got, []string{"one apple", "three apples"}) {
		t.Errorf("filter app: got %v", got)
	}
	m = lines(m, "four apples", "five plums")
	if got := p.shown(false).lines; len(got) != 3 || got[2] != "four apples" {
		t.Errorf("matching lines keep appending: %v", got)
	}
	if !strings.Contains(m.render(), "└/app─") {
		t.Error("the Detail filter shows on its bottom border")
	}
	m, _ = press(m, "4", "0")
	if p.filter != "app" {
		t.Error("the phase keeps its filter when focus leaves Detail")
	}
	m, _ = press(m, "esc")
	if p.filter != "" || len(p.shown(false).lines) != 5 {
		t.Error("esc in Detail clears the phase's filter")
	}
}

func TestSchemeTabs(t *testing.T) {
	m := newTestModel("")
	if m.tab != ownSchemes || len(m.schemeIndices()) != 2 || strings.Contains(m.render(), "Net") {
		t.Errorf("the pane starts on the project's schemes, got tab %v", m.tab)
	}
	m, _ = press(m, "2", "]")
	if m.tab != packageSchemes || len(m.schemeIndices()) != 1 || strings.Contains(m.render(), "* Puzzle") {
		t.Errorf("] should show only packages, got tab %v", m.tab)
	}
	m, _ = press(m, "space")
	if m.scheme.Name != "Net" {
		t.Errorf("selecting in the Packages tab should pick Net, got %q", m.scheme.Name)
	}
	m, _ = press(m, "[")
	if m.tab != ownSchemes {
		t.Errorf("[ should be back on Schemes, got %v", m.tab)
	}
	if screen := m.render(); !strings.Contains(screen, "Schemes") || !strings.Contains(screen, "Packages") {
		t.Error("the tabs belong in the pane title")
	}
}

func TestDestinationTabs(t *testing.T) {
	m := newTestModel("")
	if screen := m.render(); !strings.Contains(screen, "Apple TV 4K tvOS 26.2") || !strings.Contains(screen, "iPhone Air 26.5") {
		t.Errorf("Destinations shows every platform, naming the OS for non-iOS rows:\n%s", screen)
	}
	m, _ = press(m, "3", "]")
	if m.platform != 1 || len(m.simulatorIndices()) != 2 || strings.Contains(m.render(), "Apple TV") {
		t.Errorf("] should show iOS only, got tab %d", m.platform)
	}
	m, _ = press(m, "]")
	if m.platform != 2 || len(m.simulatorIndices()) != 1 || !strings.Contains(m.render(), "Apple TV 4K 26.2") {
		t.Errorf("] again should show tvOS only, got tab %d:\n%s", m.platform, m.render())
	}
	m, _ = press(m, "space")
	if m.simulator != appleTV {
		t.Errorf("selecting in the tvOS tab should pick the Apple TV, got %v", m.simulator)
	}
	m, _ = press(m, "[", "[")
	if m.platform != 0 {
		t.Errorf("[ twice should be back on Destinations, got %d", m.platform)
	}
	if screen := ansi.Strip(m.render()); !strings.Contains(screen, "[3]─Destinations─") {
		t.Errorf("too narrow for every tab at 80 columns, the title shows the active one:\n%s", screen)
	}
	m, _ = update(m, tea.WindowSizeMsg{Width: 140, Height: 24})
	if screen := ansi.Strip(m.render()); !strings.Contains(screen, "[3]─Destinations·iOS·tvOS·watchOS·macOS") {
		t.Errorf("with room, every tab shows in the pane title:\n%s", screen)
	}
}

func TestExpandAndHelp(t *testing.T) {
	m := newTestModel("")
	m, _ = press(m, "3", "+")
	screen := m.render()
	if !m.expanded || strings.Contains(screen, "[2]─") || !strings.Contains(screen, "[3]─") {
		t.Errorf("+ should show only the focused Simulator pane on the left:\n%s", screen)
	}
	m, _ = press(m, "0", "_")
	if m.expanded || !strings.Contains(m.render(), "[2]─") {
		t.Error("_ should restore all four panes")
	}

	m, _ = press(m, "?")
	if !m.showHelp || !strings.Contains(m.render(), "open the shown log in $EDITOR") {
		t.Error("? should show the key help")
	}
	m, cmd := press(m, "b")
	if m.showHelp || cmd != nil || m.state != idle {
		t.Error("the key that closes the help must not also act")
	}
}

func TestHalfPageScrollFromAnyPane(t *testing.T) {
	m := newTestModel("")
	m, _ = press(m, "b")
	var text []string
	for i := range 100 {
		text = append(text, fmt.Sprint(i))
	}
	m = lines(m, text...)
	m, _ = press(m, "4")
	before := m.shownPhase().log.offset
	m, _ = press(m, "ctrl+u")
	if got := m.shownPhase().log.offset; got != before-m.detailHeight()/2 {
		t.Errorf("ctrl+u from Runs: offset %d, want %d", got, before-m.detailHeight()/2)
	}
	m, _ = press(m, "ctrl+d")
	if m.shownPhase().log.offset != before || !m.shownPhase().log.follow {
		t.Error("ctrl+d should scroll back to the tail and follow again")
	}
}

func TestEditorNeedsARun(t *testing.T) {
	m := newTestModel("")
	m, cmd := press(m, "e")
	if cmd != nil || m.status != "No run to open" {
		t.Errorf("e with no runs: cmd %v, status %q", cmd, m.status)
	}
	m, _ = press(m, "b")
	m = lines(m, "a line")
	if _, cmd := press(m, "e"); cmd == nil {
		t.Error("e with a run should return the editor command")
	}
}

func TestViewFitsTheWindow(t *testing.T) {
	for _, width := range []int{80, 160} {
		m := newTestModel("")
		m, _ = update(m, tea.WindowSizeMsg{Width: width, Height: 24})
		m, _ = press(m, "b")
		m = lines(m, strings.Repeat("very long line ", 50))

		for _, keys := range [][]string{nil, {"x"}, {"0"}} {
			m, _ = press(m, keys...)
			screen := m.render()
			rows := strings.Split(screen, "\n")
			if len(rows) != 24 {
				t.Errorf("width %d: %d rows, want 24", width, len(rows))
			}
			for i, row := range rows {
				if w := lipgloss.Width(row); w != width && i < 23 {
					t.Errorf("width %d: row %d is %d cells wide", width, i, w)
				}
			}
			for _, want := range []string{"[1]─Status", "[2]─", "Schemes", "[3]─", "tvOS", "[4]─Runs", "[0]─Detail", "* Puzzle", "* iPhone 17 Pro 26.5 ●", "Puzzle.xcodeproj"} {
				if !strings.Contains(screen, want) {
					t.Errorf("width %d: screen lacks %q:\n%s", width, want, screen)
				}
			}
		}
	}
}

func TestHistorySurvivesRestart(t *testing.T) {
	prefsPath := filepath.Join(t.TempDir(), "prefs.json")
	m := newTestModel(prefsPath)
	m, _ = press(m, "b")
	m = lines(m, "CompileSwift", "/src/Puzzle/A.swift:1:1: warning: unused", "** BUILD SUCCEEDED **")
	m, _ = done(m, nil)

	m, _ = press(m, "r")
	m = lines(m, "build two")
	m, _ = done(m, nil)
	app := &xcode.Stream{}
	m, _ = update(m, streamStartedMsg{stream: app, app: "com.example.Puzzle"})
	m, _ = update(m, linesMsg{app, []string{"hello from the app"}})
	m, _ = update(m, streamDoneMsg{app, nil})

	again := newTestModel(prefsPath)
	if len(again.runs) != 2 || again.runs[0].kind != "run" || again.runs[1].kind != "build" {
		t.Fatalf("reloaded runs = %v, want the run then the build", again.runs)
	}
	saved := again.runs[1]
	if saved.outcome != "succeeded" || saved.phases[0].warnings != 1 || saved.phases[0].file == "" {
		t.Errorf("build metadata: outcome %q, warnings %d, lazy file %q", saved.outcome, saved.phases[0].warnings, saved.phases[0].file)
	}
	if screen := again.render(); !strings.Contains(screen, "✓ conso") || !strings.Contains(screen, "last time: ✓ run exited") {
		t.Errorf("the reloaded history shows in Runs and Status:\n%s", screen)
	}
	again, _ = press(again, "4", "j", "j") // console -> run -> build
	p := again.shownPhase()
	if p.file != "" || !slices.Contains(p.log.lines, "CompileSwift") || len(p.issues.lines) != 1 {
		t.Errorf("selecting a saved run loads its log and issues: file %q, lines %v", p.file, p.log.lines)
	}
	again, _ = press(again, "k", "k")
	if got := again.shownPhase().log.lines; len(got) != 1 || !strings.HasSuffix(got[0], " hello from the app") {
		t.Errorf("the saved console reads back, got %v", got)
	}

	for range maxRuns + 3 {
		again, _ = press(again, "c")
		again, _ = done(again, nil)
	}
	if n := len(runFolders(again.historyDir)); n != maxRuns {
		t.Errorf("%d folders on disk, want the cap of %d", n, maxRuns)
	}
}

func TestQuitSavesTheRunningJob(t *testing.T) {
	prefsPath := filepath.Join(t.TempDir(), "prefs.json")
	m := newTestModel(prefsPath)
	m, _ = press(m, "t")
	m = lines(m, "Test Suite started")
	m.shutdown()
	again := newTestModel(prefsPath)
	if len(again.runs) != 1 || again.runs[0].outcome != "stopped" {
		t.Errorf("a job running at quit is saved as stopped, got %v", again.runs)
	}
}

func TestDeleteHistory(t *testing.T) {
	prefsPath := filepath.Join(t.TempDir(), "prefs.json")
	m := newTestModel(prefsPath)
	if m, _ = press(m, "D"); m.status != "No runs to delete" {
		t.Errorf("nothing to delete, status %q", m.status)
	}
	m, _ = press(m, "b")
	m, _ = done(m, nil)
	m, _ = press(m, "c")
	m, _ = done(m, nil)

	m, _ = press(m, "4", "D")
	if !m.deleting || !strings.Contains(m.render(), "Delete 2 runs from the history?") {
		t.Errorf("D opens the popup:\n%s", m.render())
	}
	m, _ = press(m, "b", "n")
	if m.deleting || m.state != idle || len(m.runs) != 2 {
		t.Errorf("the popup takes every key and n closes it: open %v, state %d, %d runs", m.deleting, m.state, len(m.runs))
	}

	m, _ = press(m, "D", "y")
	if len(m.runs) != 0 || m.focus != statusPane || m.status != "Deleted 2 runs" {
		t.Errorf("D y deletes: %d runs, focus %d, status %q", len(m.runs), m.focus, m.status)
	}
	if _, err := os.Stat(m.historyDir); !os.IsNotExist(err) {
		t.Errorf("the saved runs are still on disk: %v", err)
	}

	m, _ = press(m, "b")
	m, _ = press(m, "D")
	if len(m.runs) != 1 || m.deleting || m.status != "Only the running job is left; x stops it" {
		t.Errorf("the running job stays: %d runs, status %q", len(m.runs), m.status)
	}
	m, _ = done(m, nil)
	if again := newTestModel(prefsPath); len(again.runs) != 1 {
		t.Errorf("a job finished after the delete is saved again, got %d", len(again.runs))
	}
}

func TestRenameSimulator(t *testing.T) {
	m := newTestModel("")
	m, _ = update(m, simulatorsMsg{simulators: []xcode.Simulator{xcode.Mac, iPhone17Pro, iPhoneAir}})
	if m, _ = press(m, "2", "R"); m.renaming.UDID != "" {
		t.Error("R renames only in Destinations")
	}
	if m, _ = press(m, "3", "g", "R"); m.renaming.UDID != "" {
		t.Error("My Mac has no name to change")
	}

	m, _ = press(m, "j", "R", "b", "q")
	if m.renameText != "iPhone 17 Probq" || m.state != idle || !strings.Contains(m.render(), "iPhone 17 Probq▏") {
		t.Errorf("R opens the popup with the name and keys become text: %q\n%s", m.renameText, m.render())
	}
	if m, _ = press(m, "esc"); m.renaming.UDID != "" {
		t.Error("esc closes the popup")
	}
	if _, cmd := press(m, "R", "enter"); cmd != nil {
		t.Error("an unchanged name runs nothing")
	}

	m, cmd := press(m, "R", "backspace", "backspace", "backspace", "W", "o", "r", "k", "enter")
	if m.renaming.UDID != "" || cmd == nil {
		t.Fatal("enter closes the popup and renames")
	}
	m, _ = update(m, renamedMsg{iPhone17Pro, "iPhone 17 Work", nil})
	if m.simulators[1].Name != "iPhone 17 Work" || m.simulator.Name != "iPhone 17 Work" || m.status != "Renamed iPhone 17 Pro to iPhone 17 Work" {
		t.Errorf("the list and the current destination take the name: %v, %v, status %q", m.simulators[1], m.simulator, m.status)
	}

	m, _ = update(m, renamedMsg{iPhoneAir, "Air", errors.New("exit status 1")})
	if m.simulators[2].Name != "iPhone Air" || m.status != "Couldn't rename the simulator: exit status 1" {
		t.Errorf("a failure keeps the name: %v, status %q", m.simulators[2], m.status)
	}
}

func TestLogViewFollowsTheTail(t *testing.T) {
	v := newLogView(func(string) int { return 1 })
	v.setHeight(3)
	v.append("1", "2", "3", "4", "5")
	if got := v.visible(); !slices.Equal(got, []string{"3", "4", "5"}) {
		t.Errorf("got %v, want the last 3 lines", got)
	}

	v.scroll(-1)
	v.append("6")
	if got := v.visible(); !slices.Equal(got, []string{"2", "3", "4"}) || v.follow {
		t.Errorf("after scrolling up, new lines must not move the view, got %v", got)
	}

	v.scrollTo(100)
	v.append("7")
	if got := v.visible(); !slices.Equal(got, []string{"5", "6", "7"}) || !v.follow {
		t.Errorf("back at the bottom it follows again, got %v", got)
	}
}

func TestShortDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		5300 * time.Millisecond:                   "5s",
		32*time.Minute + 5*time.Second:            "32m 5s",
		time.Hour + 2*time.Minute + 3*time.Second: "1h 2m 3s",
		0: "0s",
	} {
		if got := short(d); got != want {
			t.Errorf("short(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestDevicesStandOut(t *testing.T) {
	phone := xcode.Simulator{Name: "Test iPhone", Runtime: "iOS 26.7.1", UDID: "00008140-D", Device: true}
	m := newTestModel("")
	m, _ = update(m, simulatorsMsg{simulators: []xcode.Simulator{xcode.Mac, phone, iPhone17Pro}})
	rows := m.simulatorRows(3, 30)
	if !strings.Contains(rows[1], "Test iPhone 26.7.1") || !strings.Contains(rows[1], "\x1b[36m") || strings.Contains(rows[2], "\x1b[36m") {
		t.Errorf("a device row is cyan, a simulator row is not: %q", rows)
	}
	if m, _ = press(m, "3", "g", "j", "R"); m.renaming.UDID != "" {
		t.Error("a device has no simctl name to change")
	}
	m, _ = press(m, "space")
	if screen := ansi.Strip(m.render()); m.simulator != phone || !strings.Contains(screen, "r run on the device") {
		t.Errorf("the hint names the device once it is current:\n%s", screen)
	}
}

func TestSelectionFillsTheRow(t *testing.T) {
	m, _ := update(newTestModel(""), tea.BackgroundColorMsg{Color: color.RGBA{0xf3, 0xea, 0xd3, 0xff}})
	rows := m.list([]item{{label: "Puzzle", style: warningStyle, index: 0}}, -1, 0, true, 1, 20)
	if ansi.StringWidth(rows[0]) != 20 || !strings.Contains(rows[0], "48;2;221;212;192") {
		t.Errorf("the cursor row should be a solid tint of the background across the pane: %q", rows[0])
	}
}

func TestLongLinesWrap(t *testing.T) {
	m := newTestModel("") // 80 columns: Detail rows are 46 cells
	m, _ = press(m, "b")
	m = lines(m, "head "+strings.Repeat("-", 60)+" tail", "next")
	screen := ansi.Strip(m.render())
	for _, want := range []string{
		"│head " + strings.Repeat("-", 41) + "│",
		"│" + strings.Repeat("-", 19) + " tail" + strings.Repeat(" ", 22) + "│",
		"│next",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen lacks %q:\n%s", want, screen)
		}
	}
}

func TestWrappedLinesCountRows(t *testing.T) {
	v := newLogView(func(line string) int { return len(line) }) // "bb" takes two rows
	v.setHeight(3)
	v.append("a", "bb", "c")
	if got := v.visible(); !slices.Equal(got, []string{"bb", "c"}) || !v.follow {
		t.Errorf("the tail is the last lines that fit in 3 rows together, got %v", got)
	}
	v.scroll(-1)
	if got := v.visible(); !slices.Equal(got, []string{"a", "bb"}) || v.follow {
		t.Errorf("one row up shows the line above, got %v", got)
	}
	v.scroll(1)
	if got := v.visible(); !slices.Equal(got, []string{"bb", "c"}) || !v.follow {
		t.Errorf("one row down is back at the tail, got %v", got)
	}
}

func TestConsoleRowsFollowTheTimestampToggle(t *testing.T) {
	p := newPhase("console", layout{width: 20, height: 3})
	line := "10:23:23.361 " + strings.Repeat("x", 15)
	if p.rows(line) != 1 {
		t.Errorf("without its stamp the line fits one row, got %d", p.rows(line))
	}
	p.setLayout(layout{width: 20, height: 3, stamps: true})
	if p.rows(line) != 2 {
		t.Errorf("with its stamp it wraps to two rows, got %d", p.rows(line))
	}
}

func TestMirroredLogLinesKeepTheirTime(t *testing.T) {
	m := newTestModel("")
	m, _ = press(m, "r")
	m = lines(m, "** BUILD SUCCEEDED **")
	m, _ = done(m, nil)
	app := &xcode.Stream{}
	m, _ = update(m, streamStartedMsg{stream: app, app: "com.example.Puzzle"})
	m, _ = update(m, linesMsg{app, []string{"hello from stdout", "2026-09-28 10:33:35.230501+0200 Puzzle[9836:34476303] [Session] Token expired"}})
	got := m.runs[0].console().log.lines
	if len(got) != 2 || !stampPrefix.MatchString(got[0]) || strings.HasPrefix(got[0], "10:33:35.230 ") || got[1] != "10:33:35.230 [Session] Token expired" {
		t.Errorf("console = %q; want stdout stamped on arrival and the mirrored line with its own time", got)
	}
}

package tui

import (
	"cmp"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// phase is one log of a run. A build, test, clean or resolve has one phase; a run
// has build, launch and console, like Xcode's report navigator.
type phase struct {
	name     string
	log      logView // every line
	issues   logView // unique error and warning lines, shown with i
	seen     map[string]bool
	errors   int
	warnings int
	filter   string  // / narrows to lines containing this, case-insensitive
	found    logView // the lines matching filter, kept so scrolling and follow work on them
	file     string  // a saved log not read yet; see load
	width    int     // Detail's inner width, which the lines wrap to
	stamps   bool    // console lines show their time of day (s)
}

// layout is what Detail gives its phases: the inner size the lines wrap to and
// whether console lines show their time of day.
type layout struct {
	width, height int
	stamps        bool
}

func newPhase(name string, l layout) *phase {
	p := &phase{name: name, seen: map[string]bool{}}
	p.log, p.issues, p.found = newLogView(p.rows), newLogView(p.rows), newLogView(p.rows)
	p.setLayout(l)
	return p
}

func (p *phase) append(lines ...string) {
	for _, line := range lines {
		p.log.append(line)
		if p.filter != "" && p.matches(line) {
			p.found.append(line)
		}
		kind := kindOf(line)
		if kind == plainLine || p.seen[line] {
			continue // xcodebuild repeats a diagnostic for every task that hits it
		}
		p.seen[line] = true
		p.issues.append(line)
		if kind == errorLine {
			p.errors++
		} else {
			p.warnings++
		}
	}
}

// setFilter rebuilds the matching lines from scratch; a keystroke costs one pass
// over the log.
func (p *phase) setFilter(filter string) {
	p.filter = filter
	p.found = newLogView(p.rows)
	p.found.setHeight(p.log.height)
	for _, line := range p.log.lines {
		if p.matches(line) {
			p.found.append(line)
		}
	}
}

func (p *phase) matches(line string) bool {
	return strings.Contains(strings.ToLower(line), strings.ToLower(p.filter))
}

// shown is the log on screen: the filter's matches, else only the issues, else everything.
func (p *phase) shown(issues bool) *logView {
	switch {
	case p.filter != "":
		return &p.found
	case issues:
		return &p.issues
	}
	return &p.log
}

func (p *phase) setLayout(l layout) {
	p.width, p.stamps = l.width, l.stamps
	p.log.setHeight(l.height)
	p.issues.setHeight(l.height)
	p.found.setHeight(l.height)
}

// display is a line as Detail shows it: a console line loses its time prefix
// while timestamps are off, and a line redrawn with carriage returns (git's
// progress) shows only its last drawing, as in a terminal. A raw \r would send
// the cursor back over the left panes.
func (p *phase) display(line string) string {
	stamp, text := "", line
	if p.name == "console" {
		stamp, text = splitStamp(line)
		if !p.stamps {
			stamp = ""
		}
	}
	text = strings.TrimRight(text, "\r")
	return stamp + text[strings.LastIndex(text, "\r")+1:]
}

// rows is how many rows of Detail a line takes once wrapped.
func (p *phase) rows(line string) int {
	return len(wrap(p.display(line), p.width))
}

// wrap cuts text into rows of at most width cells; width 0 leaves it whole.
func wrap(text string, width int) []string {
	return strings.Split(ansi.Hardwrap(text, width, true), "\n")
}

// Console lines start with their time of day, "10:23:23.361 "; Detail hides it
// unless s turns timestamps on.
const stampLayout = "15:04:05.000"

var stampPrefix = regexp.MustCompile(`^\d\d:\d\d:\d\d\.\d+ `)

// splitStamp cuts a console line into its timestamp, if any, and the rest.
func splitStamp(line string) (stamp, text string) {
	n := len(stampPrefix.FindString(line))
	return line[:n], line[n:]
}

// run is one job the user started. The Runs pane lists them newest first; a run
// that reached its app gets a console row above its own row. The Detail pane
// shows the selected phase.
type run struct {
	kind    string // build, test, clean, resolve, run
	status  string // while running, the phase a run is in: building, installing, attached
	started time.Time
	elapsed time.Duration // set once finished
	outcome string        // running, succeeded, exited, failed, stopped
	phases  []*phase      // the build (with the launch steps), then the console for a run

	// For a run: how its build went, the app it started and when.
	buildOutcome string // succeeded, failed, stopped; empty while building
	buildElapsed time.Duration
	app          string
	attached     time.Time
}

// maxRuns caps the history kept in memory.
const maxRuns = 50

func newRun(kind string, l layout) *run {
	first := kind
	if kind == "run" {
		first = "build"
	}
	return &run{kind: kind, started: time.Now(), outcome: "running", phases: []*phase{newPhase(first, l)}}
}

func (r *run) addPhase(name string, l layout) *phase {
	p := newPhase(name, l)
	r.phases = append(r.phases, p)
	return p
}

// last is the phase the job writes to.
func (r *run) last() *phase {
	return r.phases[len(r.phases)-1]
}

func (r *run) finish(outcome string) {
	r.outcome = outcome
	r.elapsed = time.Since(r.started).Round(100 * time.Millisecond)
	if r.buildOutcome == "" {
		r.buildOutcome = outcome
	}
}

// finishBuild records the build's verdict; a run goes on to its app.
func (r *run) finishBuild(outcome string) {
	r.buildOutcome = outcome
	r.buildElapsed = time.Since(r.started).Round(100 * time.Millisecond)
}

// console is the app's output phase, once the app has started.
func (r *run) console() *phase {
	if len(r.phases) < 2 {
		return nil
	}
	return r.phases[1]
}

func (r *run) setLayout(l layout) {
	for _, p := range r.phases {
		p.setLayout(l)
	}
}

// mark is the one-glyph verdict shown in Status and Runs.
func (r *run) mark() string {
	return markOf(r.outcome, r.status == "attached")
}

func markOf(outcome string, attached bool) string {
	switch outcome {
	case "succeeded", "exited":
		return "✓"
	case "failed":
		return "✗"
	case "stopped":
		return "■"
	}
	if attached {
		return "●"
	}
	return "⟳"
}

// summary is e.g. "✓ build succeeded 2s · 0 errors · 2 warnings" or "● run attached".
func (r *run) summary() string {
	if r.outcome == "running" {
		return fmt.Sprintf("%s %s %s", r.mark(), r.kind, cmp.Or(r.status, "running"))
	}
	build := r.phases[0]
	return fmt.Sprintf("%s %s %s %s · %s · %s", r.mark(), r.kind, r.outcome, short(r.elapsed),
		count(build.errors, "error"), count(build.warnings, "warning"))
}

// when is the run's start for a Runs row: the time today, the date otherwise.
func (r *run) when() string {
	if y, m, d := r.started.Date(); y == time.Now().Year() && m == time.Now().Month() && d == time.Now().Day() {
		return r.started.Format("15:04")
	}
	return r.started.Format("Jan 2 15:04")
}

// row is the run's Runs line, "✓ build    20:02  2s"; for a run it shows how the
// build went, the console row above it shows the app.
func (r *run) row() string {
	outcome, elapsed := r.outcome, r.elapsed
	if r.kind == "run" {
		outcome, elapsed = r.buildOutcome, r.buildElapsed
		if outcome == "" {
			outcome = "running"
		}
	}
	dur := "…"
	if outcome != "running" {
		dur = short(elapsed)
	}
	return runsRow(markOf(outcome, false), r.kind, r.when(), dur)
}

// consoleRow is the Runs line for the app: "● console  20:03  …".
func (r *run) consoleRow() string {
	dur := "…"
	if r.outcome != "running" {
		dur = short(r.elapsed)
	}
	return runsRow(r.mark(), "console", r.when(), dur)
}

// short is a duration to the second, spaced: "5s", "32m 5s", "1h 2m 3s".
func short(d time.Duration) string {
	d = d.Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// runsRow lays out a Runs line in columns wide enough for "console".
func runsRow(mark, kind, when, dur string) string {
	return fmt.Sprintf("%s %-7s  %s  %s", mark, kind, when, dur)
}

// buildSummary is the Detail title for a run's build: "✓ run build succeeded 2s · …".
func (r *run) buildSummary() string {
	if r.kind != "run" {
		return r.summary()
	}
	build := r.phases[0]
	if r.buildOutcome == "" {
		return "⟳ run building"
	}
	return fmt.Sprintf("%s run build %s %s · %s · %s", markOf(r.buildOutcome, false), r.buildOutcome, short(r.buildElapsed),
		count(build.errors, "error"), count(build.warnings, "warning"))
}

// consoleSummary is the Detail title for the app's output: "● console com.x · attached".
func (r *run) consoleSummary() string {
	state := r.outcome
	if state == "running" {
		state = "attached"
	}
	return fmt.Sprintf("%s console %s · %s", r.mark(), r.app, state)
}

func projectDir(container string) string {
	return filepath.Dir(container) + string(filepath.Separator)
}

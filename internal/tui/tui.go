// Package tui is xctui's Bubble Tea interface: one log view, pickers for the
// scheme and the simulator, and keys that build, run, test, clean and stop.
package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/libec/xctui/internal/xcode"
)

// Run shows the TUI for the Xcode project in dir until the user quits.
// A non-empty scheme is selected instead of the remembered one.
func Run(dir, scheme string) error {
	project, err := xcode.FindProject(dir)
	if err != nil {
		return err
	}
	m := New(project, defaultPrefsPath())
	if scheme != "" {
		m.remembered.Scheme = scheme
	}
	final, err := tea.NewProgram(m).Run()
	if m, ok := final.(Model); ok {
		m.shutdown()
	}
	return err
}

// shutdown stops the running step and waits for its process to exit, so quitting
// leaves no xcodebuild or simctl behind. It runs after Bubble Tea has returned,
// which covers q, ctrl+c and a SIGTERM alike.
func (m Model) shutdown() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.state != idle && m.current != nil {
		m.current.finish("stopped")
		saveRun(m.historyDir, m.current) // best effort on the way out
	}
	for _, s := range []*xcode.Stream{m.stream, m.logStream} {
		if s != nil {
			for range s.Lines { // the process can't exit while its output is unread
			}
			s.Wait()
		}
	}
}

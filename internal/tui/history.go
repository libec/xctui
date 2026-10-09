package tui

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// A run is saved as a folder <started>-<kind>/ holding meta.json and one .log
// per phase. The newest maxRuns folders per project are kept.
type runMeta struct {
	Kind, Status, Outcome, BuildOutcome, App string
	Started, Attached                        time.Time
	Elapsed, BuildElapsed                    time.Duration
	Phases                                   []phaseMeta
}

type phaseMeta struct {
	Name             string
	Errors, Warnings int
}

// historyDir is where a project's runs are saved: next to prefs.json, under a
// folder named after the container plus a hash of its path.
func historyDir(prefsPath, container string) string {
	if prefsPath == "" {
		return ""
	}
	sum := sha1.Sum([]byte(container))
	name := strings.TrimSuffix(filepath.Base(container), filepath.Ext(container))
	return filepath.Join(filepath.Dir(prefsPath), "runs", name+"-"+hex.EncodeToString(sum[:4]))
}

func saveRun(dir string, r *run) error {
	if dir == "" {
		return nil
	}
	d := filepath.Join(dir, r.started.Format("2006-01-02T15-04-05.000000000")+"-"+r.kind)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return err
	}
	meta := runMeta{Kind: r.kind, Status: r.status, Outcome: r.outcome, BuildOutcome: r.buildOutcome, App: r.app,
		Started: r.started, Attached: r.attached, Elapsed: r.elapsed, BuildElapsed: r.buildElapsed}
	for _, p := range r.phases {
		meta.Phases = append(meta.Phases, phaseMeta{p.name, p.errors, p.warnings})
		if err := os.WriteFile(filepath.Join(d, p.name+".log"), []byte(strings.Join(p.log.lines, "\n")+"\n"), 0o644); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(d, "meta.json"), data, 0o644); err != nil {
		return err
	}
	return pruneRuns(dir)
}

// runFolders lists a project's saved runs, newest first.
func runFolders(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	slices.Reverse(names)
	return names
}

func pruneRuns(dir string) error {
	for _, name := range runFolders(dir)[min(maxRuns, len(runFolders(dir))):] {
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// loadRuns reads the saved runs' metadata; each phase's log is read from disk
// the first time it is shown.
func loadRuns(dir string) []*run {
	if dir == "" {
		return nil
	}
	var runs []*run
	for _, name := range runFolders(dir) {
		data, err := os.ReadFile(filepath.Join(dir, name, "meta.json"))
		if err != nil {
			continue
		}
		var meta runMeta
		if json.Unmarshal(data, &meta) != nil || len(meta.Phases) == 0 {
			continue
		}
		r := &run{kind: meta.Kind, status: meta.Status, outcome: meta.Outcome, buildOutcome: meta.BuildOutcome, app: meta.App,
			started: meta.Started, attached: meta.Attached, elapsed: meta.Elapsed, buildElapsed: meta.BuildElapsed}
		for _, pm := range meta.Phases {
			p := newPhase(pm.Name, layout{})
			p.errors, p.warnings = pm.Errors, pm.Warnings
			p.file = filepath.Join(dir, name, pm.Name+".log")
			r.phases = append(r.phases, p)
		}
		runs = append(runs, r)
	}
	return runs
}

// load reads a saved phase's log once. Issue counts came with the metadata.
func (p *phase) load() {
	if p.file == "" {
		return
	}
	file := p.file
	p.file = ""
	data, err := os.ReadFile(file)
	if err != nil {
		p.log.append("(log missing: " + err.Error() + ")")
		return
	}
	for line := range strings.SplitSeq(strings.TrimSuffix(string(data), "\n"), "\n") {
		p.log.append(line)
		if kindOf(line) != plainLine && !p.seen[line] {
			p.seen[line] = true
			p.issues.append(line)
		}
	}
}

# Building xctui from this folder

This folder is xctui without code: `SPEC.md` says what the program does, `tests.yaml` says what
it must output for given inputs, `fixtures/` holds captured tool output, `screens/` holds exact
terminal screens. The prompt below turns them into an implementation in the language of your
choice. The reference implementation is Go with Bubble Tea; a rebuild is judged only by the
tests and by sitting next to the reference in a terminal.

Rebuilds from this folder have gone both ways. Strong models finished with every test green and
matched the Go build under the conformance checker: Rust in 35 minutes, then Swift, Ruby and a
second Rust. A weaker model, on its first try, wrote its own tests instead of the spec's, stubbed
the streaming commands and skipped the TUI. The build plan and the definition of done below exist
so that the second outcome does not repeat: follow them in order and do not stop early.

## The prompt

Paste this into your coding agent, in a folder that holds a copy of this `spec/` folder:

> Build **xctui** in **<LANGUAGE>** from the folder `spec/`. Name the binary **<BINARY>**
> (for example `xctuirs` for Rust) so it can live on PATH next to the reference `xctui`.
>
> Read `spec/INSTALL.md` first and follow its **build plan** phase by phase, committing at each
> gate; a phase is done only when its gate's tests pass. Then read `spec/SPEC.md` in full: it is
> the complete behaviour — every command line the program runs, every parsing and ordering
> rule, the CLI subcommands and their exact output, the TUI's layout, keys, state machine and
> screens. Do not read or port any other implementation. `spec/tests.yaml` is the test suite:
> your tests read that file and run every case in it, grouped as the file is; do not write a
> different test set in its place. You are done only when the **definition of done** in
> `spec/INSTALL.md` holds, and you report with the evidence it asks for. If something blocks you
> for more than twenty minutes, write down exactly what in `NOTES.md` and continue with the next
> phase; never replace a required part with a stub that pretends to work.

## Definition of done

1. A test harness reads `spec/tests.yaml` and runs every case in every group. The `cargo test`
   (or equivalent) summary shows the total and zero failures. Cases are not skipped, ignored or
   replaced by hand-written substitutes.
2. All 17 files in `spec/screens/` compare byte for byte, including every trailing space, with
   your rendered screen after ANSI escape sequences are stripped.
3. `<BINARY> build`, `test`, `clean`, `resolve` and `run` really run the tools and stream their
   output (the `cli` cases prove it with fake tools). A stub that prints "not implemented" fails
   this point even if every other test passes.
4. The TUI opens in the alternate screen, draws the five panes, takes every key of SPEC 12.6,
   runs jobs, and quits leaving no child process behind. When a reference build is on the
   machine, `ruby spec/conformance/check.rb <binary> tui <reference>` matches on every sequence:
   the fixture tests inject a clock and cannot see a live-only path (one rebuild passed all 17
   screens while its real TUI stamped every console line with an empty time).
5. A release build is installed so `<BINARY>` runs from any folder; `which <BINARY>` and
   `<BINARY> destinations` (it needs no project folder) prove it.
6. `README.md` lists the subcommands and keys (copied from the spec), how to build and install,
   and which tests exist. `NOTES.md` lists every ambiguity in the spec you met, what you assumed,
   the wall-clock time per phase, and the final test summary line, pasted.

## Build plan

The reference rebuild is about 4,000 lines: roughly 1,000 for the tool and process layers, 500
for the CLI and the job model, 1,500 for the TUI model and renderer, 1,000 of tests. Plan for
that size. Commit at every gate.

A strong model does the whole plan in one session (35 minutes measured). A smaller model does
not: two attempts stopped after a fraction of the work and installed stubs. For a smaller model
run **one phase per session**, in the same worktree, each session starting from the previous
commit; the per-phase prompt is at the end of this section. Rough time per phase for such a
model: 0 and 1 together 30 min, 2 30 min, 3 30 min, 4 45 min, 5 60 min, 6 30 min, 7 10 min.

| Phase | Build | Gate: these `tests.yaml` groups pass, with this many cases |
|---|---|---|
| 0 | Read SPEC.md and tests.yaml. Create the project, `NOTES.md` with a timeline, and the module split below. Write the tests.yaml loader (a YAML library; aliases spliced as the file's header says). | the loader parses the file and prints the cases per group; the numbers must match the table below |
| 1 | Data layer: container search (SPEC 3), schemes (4), destinations (5), build settings (8), log cleaning (9.3, 9.4), log processing (10). Pure functions, no processes yet. | `container` 5, `container_args` 2, `schemes` 11, `destinations` 25, `build_settings` 7, `log` 9, `processing` 43 — 102 cases |
| 2 | Process layer: the streamed command (6.1) with one pipe, the 4 MiB line limit, the bounded queue, cancel by signal then SIGKILL, process groups; the one-shot command (6.2). A **runner** abstraction with two implementations: real, and a fake that records argv and returns canned output, so the `commands` group can check argv and signal without executing anything. | `stream` 7 (real shell scripts), `commands` 18 — 25 cases |
| 3 | CLI (SPEC 11): every subcommand, exact output, exit codes, Ctrl-C. | `cli` 12, run against the built binary with fake `xcodebuild`, `xcrun` and `open` on PATH (recipe below) |
| 4 | Job model (12.7): jobs, phases, issues, the three log views with follow, the state machine, install-and-launch (9), prefs and history (13). No rendering yet; a model that takes the test steps (`resize`, `key`, `lines`, `done`, `launched`, `app_lines`, `log_lines`, `log_done`, `app_done`, `renamed`) directly. | `tui.after_loading` 1, `tui.keys` 31, `tui.rename` 7, `tui.job_results` 9, `tui.detail_filter` 1, `tui.history_in_memory` 1, `tui.logview` 5, `tui.half_page` 1, `persistence` 3 — 59 cases |
| 5 | Renderer (12.2 to 12.5, 12.8 to 12.10, 12.13): the frame function, lists, Status, Detail, tabs, footer, help overlay, into a grid of cells or a string. Start with `screens/startup-80x24.txt`, then the job screens. | `tui.screens` 17, all byte-exact |
| 6 | Terminal loop: alternate screen, raw mode, key decoding for every key in 12.6, resize, the background-colour query (optional; the fallback style is allowed), the editor (12.11), quit and shutdown (12.12). Smoke-test in a pty: open, press `b` with a fake xcodebuild on PATH, watch the Runs row, `q`. | manual, described in NOTES.md |
| 7 | Release build, install, README, final NOTES. | definition of done |

**Per-phase prompt** (fill in N, the gate groups from the table, and the worktree path):

> You are doing **phase N** of the build plan in `spec/INSTALL.md`, in the worktree `<path>`.
> Earlier phases are committed there; read `NOTES.md` and the code before you start, then the
> sections of `spec/SPEC.md` that the phase names. Build only this phase. It is done when the
> gate's `tests.yaml` groups pass in the harness with the case counts the plan lists (add the
> harness for those groups if it does not exist yet; every subgroup, every entry), all earlier
> tests still pass, the work is committed, and `NOTES.md` has the phase's time and any ambiguity
> you met. Report the test summary line and the cases run per subgroup. Do not stub anything.

**Case counts.** `tests.yaml` holds 215 cases. Per group, counting every entry of every
subgroup: `container` 5 · `container_args` 2 · `schemes` 11 (files_on_disk 2, package_products 1,
list_fallback 2, merge 1, default 3, display 2) · `destinations` 25 (runtime_name 5, platform 3,
destination_and_platform_name 5, sort 3, default 5, parse 1, labels 3) · `commands` 18
(xcodebuild_args 3, actions 4, list_schemes 1, list_simulators 1, rename 1, build_settings 1, install 3,
launch 1, launch_mac 1, log_stream 2) · `stream` 7 (scan_lines 3, exit_code 1, cancel 2,
one_shot_error 1) · `build_settings` 7 · `log` 9 (clean_line 6, mirror_line 3) · `processing` 43 (kind 5, issues 1,
strip_paths 2, short 4, when 2, count 3, marks 6, summaries 7, build_summaries 3,
console_summaries 3, runs_rows 7) · `cli` 12 · `tui` 73 (after_loading 1, screens 17, keys 31, rename 7,
job_results 9, detail_filter 1, history_in_memory 1, logview 5, half_page 1) · `persistence` 3.
A harness that reports fewer has skipped cases; a gate is not passed until its count is met.
Extra tests of your own beyond the 215 are welcome when they are labelled as extra in the summary.

## Test harness recipe

- **Steps.** `tui.screens` and the state groups list steps. Write one interpreter that applies a
  step to your model: `resize [w, h]` → the window size; `key x` → a key press (names as in
  SPEC 12.6: single characters, `enter`, `space`, `esc`, `backspace`, `tab`, `shift+tab`,
  `ctrl+u`, `ctrl+d`, `up`, `down`, `pgup`, `pgdown`, `home`, `end`, `f1`); `lines [..]` →
  output lines of the running job's process; `done ok` / `done "<error>"` → that process ended;
  `launched {app, logs}` → the install-and-launch step succeeded, the app stream (and, when
  `logs` is true, the unified log stream) started; `app_lines`, `log_lines` → lines from those two
  streams; `log_done`, `app_done` → those streams ended; `renamed ok` / `renamed "<error>"` → the
  `simctl rename` the keys started ended. A step that is itself a list is spliced.
- **Clock.** Inject a clock into the model: jobs start at the fixture's `jobs_start`; when a job
  finishes, set its elapsed (and a run's build elapsed) to `finished_elapsed_ms`; the wall-clock
  stamp for `app_lines` is `stdout_line_arrives`. "Today" for Runs rows uses the real clock
  (SPEC 10.4).
- **Screens.** Render to a string of exactly `height` lines, each exactly `width` cells except
  the last (the footer is cut to the width but not padded), no trailing newline after the last
  line, then strip ANSI (`\x1b[...m` and the like) and compare
  with the file. On a mismatch print the first differing line of both with `·` for spaces; the
  usual causes are a title cut one cell short, a missing trailing space, or the help box not
  composited over the panes. Print the differing lines with `inspect`-style escaping, not raw: a
  raw printout hides a trailing space, a tab, a stray escape sequence or a missing final newline,
  and two lines that look identical then fail forever.
- **Fake tools for the `cli` group.** For each case make a temp folder, create the `tree`
  (a path ending in `/` is a folder, a mapping value is file content), make a `bin/` with
  executable scripts named `xcodebuild`, `xcrun` and `open`, and run the built binary with
  `PATH=bin:$PATH` and the temp folder as the working directory. `xcodebuild` prints the named
  fixture for `-list` or `-showBuildSettings`, or the case's `tool_output` lines with `<tmp>`
  replaced by the temp folder, then sleeps 1.5 s, then exits with `tool_exit`. For
  `build_stopped` it prints the first line, waits, and on SIGTERM prints the second and exits;
  the test sends SIGINT to the binary 1.5 s after the first line has reached stdout (not after
  spawn: a slow-starting runtime would otherwise read `1s`). `xcrun simctl list …` prints
  `fixtures/simctl-list.json`; for `run_exited`, `simctl launch` prints `launch_output` and
  exits after `launch_exit_after_s`, `log stream` prints `log_output` and waits for SIGTERM,
  and `no_process_left` means no fake may survive the binary. Compare stdout, stderr and the
  exit code byte for byte, or line by line against `stdout_matches` / `stderr_matches` (one
  regular expression per line); `<tmp>` in the expected output is the temp folder's real path
  (macOS: `/private/var/…`, not `/var/…`). `conformance/check.rb` implements all of this and is
  the reference for the fake scripts, trap included.
- **Fake runner for the `commands` group.** The tool layer takes a runner; the fake records
  `(argv, signal)` and returns the fixture or `boot_error`. Assert the recorded calls equal the
  case's `argv` / `calls`.
- **Two worked examples.** A `container` case: make a temp folder, create every `tree` entry
  under it, call your container search with the temp folder, and assert the result equals
  `temp + "/" + want` (or that an error occurred, when the case has `error`). A `tui.screens`
  case: build the model from the `tui.fixture` block (container, schemes, destinations, no
  prefs, no history, the fixed clock), apply each step in order, render at the last `resize`
  size, strip ANSI, and assert the string equals the file's contents.

## Suggested module split

From the reference rebuild; any equivalent split is fine.

| Module | Holds |
|---|---|
| `xcode` | SPEC 3, 4, 5, 8, 9: types `Project`, `Scheme`, `Simulator`, `BuildSettings`; every argv; parsing |
| `stream` | SPEC 6: the streamed command, the one-shot command, signals, process groups |
| `runtime` | threads that pump a stream into events (`lines`, `done`) for the CLI and the TUI alike; install-and-launch as a sequence |
| `job` | SPEC 10 and 12.7: job, phase, issues, log views, marks, summaries, rows |
| `store` | SPEC 13: prefs and run history |
| `cli` | SPEC 11 |
| `tui/model` | SPEC 12.1, 12.6 to 12.9: state, keys, focus, filters, tabs |
| `tui/render` | SPEC 12.2 to 12.5, 12.10, 12.13: frame, lists, panes, help, footer, into a string |
| `tui/term` | raw mode, alternate screen, key decoding, resize, the editor |
| `main` | argument dispatch (SPEC 11, first bullet list) |

## Pitfalls seen in earlier rebuilds

- Writing a different test set instead of running `tests.yaml`: the definition of done fails.
- Stubbing `build`/`run` with a message and exit 0: fails point 3.
- The 4 MiB line limit must trigger even when the newline arrives in the same read as the
  overlong data.
- Rounded durations: fake tools sleep 1.5 s so the summary reads `2s` even on a loaded machine; a fake that sleeps
  exactly 2 s reads `3s` under load.
- Console marks: a run with a console phase is attached while it runs.
- A single pipe for stdout and stderr keeps line order; two pipes do not.
- Without process groups, Ctrl-C in the CLI reaches xcodebuild first and it prints a backtrace.

## Checking any build from outside

`conformance/check.rb` (needs ruby and tmux, nothing else) judges an installed binary without
its own test harness, which is how a weak model's build gets verified:

```
ruby spec/conformance/check.rb <binary> cli               # the 11 cli cases, fake tools on PATH
ruby spec/conformance/check.rb <binary> tui <reference>   # 14 screen sequences in tmux, diffed against a reference build
ruby spec/conformance/check.rb <binary> tui               # the same sequences, printed for a look
```

The TUI sequences run in a fake project with the fixture's schemes and destinations and fake
tools, in a temporary HOME, so no real project, simulator or history is touched. Times are masked
before the diff. A build that scores `cli: 11/11` and `tui: 14/14` against the reference has
met the definition of done's points 2 to 4 as far as a black box can tell; the yaml harness is
still required for the rest.

## Checking a rebuild by hand

On a Mac with Xcode, in a folder with a project:

1. `xctui project`, `xctui schemes`, `xctui destinations` — compare with the reference's output.
2. `xctui build <scheme>` — the log streams; Ctrl-C stops xcodebuild without a backtrace.
3. `xctui` — the TUI: `b`, `r`, `x`, `/`, `[` `]`, `+` `_`, `i`, `s`, `e`, `?`, `q`. Open the
   reference in a second terminal at the same size; the screens should match.
4. Quit while a build runs, then `ps aux | grep xcodebuild`: nothing should be left.

## Comparing with the reference

Both binaries on PATH, in a folder with a real project (the one-shot subcommands shell out to
the same Apple tools, so what differs is the program's own startup, parsing and rendering):

```
hyperfine --warmup 2 'xctui project' 'xctuirs project'
hyperfine --warmup 2 'xctui schemes' 'xctuirs schemes'
hyperfine --warmup 2 'xctui destinations' 'xctuirs destinations'
/usr/bin/time -l xctui schemes 2>&1 | grep -E 'real|maximum resident'
/usr/bin/time -l xctuirs schemes 2>&1 | grep -E 'real|maximum resident'
ls -l "$(which xctui)" "$(which xctuirs)"
```

For the TUI, open both at the same terminal size on the same project and compare: time from
Enter to the first frame, how a long build log scrolls, and whether the screens match
`screens/` after the same keys. `hyperfine` comes from Homebrew; a `for` loop with `time` does
the same job.

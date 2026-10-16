# xctui — specification

xctui is a lazygit-style terminal companion for Xcode projects. It finds the project in the
current folder, lists its schemes and run destinations, builds, tests, cleans, resolves packages,
installs and launches the app on a simulator or on the Mac, and shows every log in one pane
with a run history that survives restarts. It also works as a plain CLI.

This document is the whole program. Together with `tests.yaml` (expected outputs for given
inputs) and `INSTALL.md` (the prompt) it is meant to be enough to rebuild xctui in any language
without reading the reference implementation. The reference is the Go program on the `vibe`
branch of github.com/libec/xctui at commit d30ad77; where this text and that program disagree,
the program wins and this text has a bug.

Every rule below is observable: a command line, a string, a screen. Anything not stated is
free. Next to this file: `tests.yaml` (the cases), `fixtures/` (captured output of `simctl list`,
`xcodebuild -list` and `-showBuildSettings`, referenced by the cases) and `screens/` (exact TUI
screens the cases compare against).

## 1. Terms

- **Container** — the thing xcodebuild works on: a `.xcworkspace` or a `.xcodeproj` folder.
- **Project root** — the folder xctui was started in. Local Swift packages are looked up below it.
- **Scheme** — a name xcodebuild accepts for `-scheme`. Comes from `.xcscheme` files, from
  `xcodebuild -list`, or from a local Swift package's products.
- **Package scheme** — a scheme that is a product of a local Swift package (`Package: true`).
- **Destination** — where a build runs: a simulator (name, runtime, state, UDID), a **device**
  (an iPhone, iPad, Apple TV, … paired with this Mac) or **My Mac**.
- **Job** — one thing the user started: `build`, `test`, `clean`, `resolve` or `run`. Also called a
  **run** in the TUI, where it is a row in the Runs pane.
- **Phase** — one log of a job. Every job has a first phase named after its kind (`build` for a
  run). A run that reached its app gets a second phase, `console`.
- **Issue** — a log line that is a compiler diagnostic or a tool error (section 10.1).
- **Current** vs **cursor** — in a list, the current item is the one in effect (the scheme that
  builds), marked `* `; the cursor is where j/k moved to, drawn as a highlighted row.

## 2. Environment

- macOS with Xcode. Tools on `PATH`: `xcodebuild`, `xcrun` (for `simctl` and `log`), `open`.
  No private API, no Xcode plug-in.
- Config folder: the platform's user config dir, `~/Library/Application Support` on macOS,
  plus `xctui/`. Prefs live in `xctui/prefs.json`, run history under `xctui/runs/`.
- All subprocess output is read line by line. A line may be up to 4 MiB (swiftc invocations
  echoed by xcodebuild exceed 64 KB). A trailing `\r` is stripped (simctl's console pty ends
  lines with `\r\n`). If a line exceeds the limit, one line `scanner error: <reason>` is emitted
  and the rest of the output is drained and discarded so the child can exit.

## 3. Finding the container

Input: the start folder `dir`. Try, in this order, and take the first match:

1. `dir/*.xcworkspace`
2. `dir/*.xcodeproj`
3. `dir/*/*.xcworkspace`
4. `dir/*/*.xcodeproj`

A workspace wins over a project at the same depth because CocoaPods and similar tools only build
through the workspace. The start folder wins over a subfolder. A `.xcworkspace` whose parent
folder ends in `.xcodeproj` is skipped: every `.xcodeproj` holds a `project.xcworkspace`, which is
not a container. Glob matches come in the file system's lexical order.

Result: `Project{Name: <absolute path of the container>, Root: dir}`.
Error: `no .xcworkspace or .xcodeproj found in <dir> or one folder down`.

**Container args** — every xcodebuild call passes `-workspace <Name>` when `Name` ends in
`.xcworkspace`, else `-project <Name>`.

## 4. Schemes

`ParseSchemes(project)` returns `[]Scheme{Name, Package}` built in four steps.

### 4.1 Scheme files on disk (preferred)

Roots: the container; for a workspace also every `<folder of the container>/*.xcodeproj`
(the projects a workspace builds live next to it). Under each root, glob:

- `xcshareddata/xcschemes/*.xcscheme`
- `xcuserdata/*/xcschemes/*.xcscheme`

A scheme's name is the file's base name without `.xcscheme`. Names are sorted (byte order) and
duplicates removed.

Why not `xcodebuild -list`: it takes seconds on a project with packages and invents a scheme per
remote package product.

### 4.2 Fallback: `xcodebuild -list -json`

Only when 4.1 found nothing (Xcode autocreates the app scheme then, without a file):

    xcodebuild -list -json <container args>

Output is JSON with the schemes under `workspace.schemes` or `project.schemes` (string arrays),
depending on the container. If `workspace.schemes` is present (non-null) it wins, else
`project.schemes`. Anything else in the JSON is ignored. A failing command is an error
(section 6.2).

### 4.3 Local package products

Glob `Package.swift` at `root`, `root/*` and `root/*/*`. Skip a match whose path contains
`/.build/` or `/checkouts/` (checkouts of remote packages, build folders). Read each file:

- products = every capture of the regex `\.(?:library|executable)\(\s*name:\s*"([^"]+)"`
  over the whole file, in file order;
- if that finds nothing, the first capture of `Package\(\s*name:\s*"([^"]+)"` (the package
  itself, when it declares no products).

Unreadable files are skipped. Files are visited in glob order, depth 0, then 1, then 2.
Root empty → no products.

### 4.4 Merge

Start from the schemes of 4.1 or 4.2, in order. A scheme whose name is one of the products gets
`Package: true`. Every product not already listed is appended as `{Name: product, Package: true}`,
in product order. Then a **stable** sort puts non-package schemes before package schemes.

### 4.5 Default scheme

`DefaultScheme(project, schemes)`: the scheme whose name equals the container's base name without
extension (`Puzzle.xcodeproj` → `Puzzle`); else the first scheme; no schemes → none.

### 4.6 Display name

`scheme: <Name>`, or `package: <Name>` for a package scheme.

## 5. Destinations

### 5.1 Listing simulators

    xcrun simctl list devices available -j

JSON: `{"devices": {"<runtime id>": [{"name": …, "state": …, "udid": …, …}, …], …}}`. Every
device under every runtime becomes `Simulator{Name, Runtime, State, UDID}` with `Runtime` from
5.2. Other device fields are ignored.

**Devices** (best effort: when the command fails or its output does not parse, there are none
and the simulators are listed as usual):

    xcrun devicectl list devices --json-output - -q

JSON (stdout): `{"result": {"devices": [{"connectionProperties": {"pairingState": …,
"tunnelState": …}, "deviceProperties": {"name": …, "osVersionNumber": …},
"hardwareProperties": {"platform": …, "udid": …}, …}, …]}, …}`. A device is kept when
`pairingState` is `paired` and `tunnelState` is not `unavailable` (an idle device in reach flips
between `connected` and `disconnected`; both stay). It becomes
`Simulator{Name: name, Runtime: "<platform> <osVersionNumber>", State: "", UDID: udid,
Device: true}` — e.g. `iOS 26.7.1`. The `udid` is the id xcodebuild knows the device by
(devicectl's own `identifier` differs); devicectl's `--device` accepts it too. Two devices may
share a name; only the UDID tells them apart. Other fields are ignored; the fixture is
`fixtures/devicectl-list.json`.

### 5.2 Runtime name

`com.apple.CoreSimulator.SimRuntime.iOS-18-2` → `iOS 18.2`: strip the prefix
`com.apple.CoreSimulator.SimRuntime.`, cut at the first `-` into platform and version, replace
the remaining `-` in the version by `.`, join with one space. An id without the prefix is kept
as is; one without a `-` after the prefix is kept without the prefix.

### 5.3 Platform

`Platform()` is the word before the first space of the runtime name. `xrOS` is reported as
`visionOS`. An empty runtime name means `iOS`.

### 5.4 Order

Sort simulators **stably** by:

1. booted (`State == "Booted"`) before not booted;
2. platform order `iOS, tvOS, watchOS, visionOS, macOS` (unknown platforms sort before iOS,
   index -1);
3. runtime version descending, compared numerically part by part (`26.10` after `26.9`, and
   before it in the list);
4. otherwise the order simctl gave, which lists the newest models first.

Then prepend the devices of 5.1 in devicectl's order, and before them **My Mac**:
`Simulator{Name: "My Mac", Runtime: "macOS", State: "", UDID: "mac"}`. My Mac is always the
first destination: My Mac, devices, simulators.

### 5.5 Default destination

`DefaultSimulator(list)`: the first booted simulator whose platform is `iOS`; else the first
simulator (not a device) whose name starts with `iPhone`; else the first item (My Mac); empty
list → none. A device is never the default; only the user picks one. It expects the order of
5.4.

### 5.6 Derived values

| Value | Rule |
|---|---|
| `IsMac()` | `UDID == "mac"` |
| `Destination()` | Mac: `platform=macOS`; device: `platform=<Platform>,id=<UDID>`; else `platform=<Platform> Simulator,id=<UDID>` |
| `PlatformName()` | macOS → `macosx`, tvOS → `appletvsimulator`, watchOS → `watchsimulator`, visionOS → `xrsimulator`, anything else → `iphonesimulator`; for a device `os` replaces `simulator`: `iphoneos`, `appletvos`, `watchos`, `xros` |
| label (all platforms) | iOS: `<Name> <version>` where version is the runtime without the `iOS ` prefix; other platforms: `<Name> <Runtime>`; My Mac: `My Mac macOS`; then ` ●` when booted |
| label (one platform) | `<Name> ` + the runtime without its `<Platform> ` prefix (the platform is the tab), then ` ●` when booted; My Mac's runtime has no version, so it stays `My Mac macOS`; a visionOS runtime is named `xrOS 2.0`, which does not carry the `visionOS ` prefix, so it stays too |

### 5.7 Renaming

    xcrun simctl rename <UDID> <name>

A one-shot command (6.2), used by the TUI's `R` (12.15); the CLI has no rename. The UDID stays,
so a remembered destination (13.1) survives a rename.

## 6. Running tools

### 6.1 Streamed commands

Used for every xcodebuild action, the app launch, and the unified log. The command's stdout and
stderr go through **one** pipe, so lines keep the order they were written in. Lines are read
per section 2 into a bounded queue (the reference uses 256) that the consumer drains. When the
command exits the queue is closed; `Wait()` then returns nil for exit status 0 and the exit
error otherwise.

**Cancelling** sends the tool's stop signal, then SIGKILL if the process is still alive after
5 seconds:

| Tool | Signal | Why |
|---|---|---|
| `xcodebuild` (all actions) | SIGTERM | prints `** BUILD INTERRUPTED **` and exits; SIGINT makes it dump a backtrace full of `error:` lines |
| `xcrun simctl launch` | SIGINT | simctl then terminates the app; SIGTERM or SIGKILL would leave the app running |
| a macOS app run directly | SIGTERM | quits the app |
| `log stream` | SIGTERM | |

The consumer must keep draining the queue after cancelling, or the child blocks on a full pipe
and never exits.

Streamed tools run in their own process group: the child calls `setpgid(0, 0)` before exec
(Rust `Command::process_group(0)`, Go `SysProcAttr{Setpgid: true}`), and xctui signals the
child's own pid, never the group: a group-wide kill also hits the tool's own children, and a
shell script then prints `Terminated: 15` into the shared pipe before its trap runs. The group
exists only so that a terminal's Ctrl-C reaches xctui alone; xctui then sends the tool's stop
signal. Without this, xcodebuild would get the terminal's SIGINT first and print its backtrace.
This matters for the CLI; the TUI keeps the terminal in raw mode, where Ctrl-C is a key press.

### 6.2 One-shot commands

Used for `xcodebuild -list`, `simctl list`, `simctl rename` and `-showBuildSettings`. Capture stdout. On a
non-zero exit the error message is `<path to the executable>: <exit error>\n<stderr, trimmed>`,
e.g. `/usr/bin/xcodebuild: exit status 66\nxcodebuild: error: …` — the exit status alone says
nothing, the stderr line does.

A one-shot command that is cancelled (`x` during the install steps of 9.1, or quitting) is
killed outright: there is no graceful signal for `simctl boot`, `simctl install` or `open`, and
the job ends `stopped` without starting the remaining steps.

### 6.3 Argument order

    xcodebuild <action> <container args> -scheme <Scheme.Name> -destination <Destination()>

Every xcodebuild call that resolves build settings passes all three (container, scheme,
destination). Without them xcodebuild falls back to device paths and defaults.

## 7. Actions

| Job | Command (streamed, SIGTERM) |
|---|---|
| build | `xcodebuild build …` (6.3) |
| test | `xcodebuild test …` |
| clean | `xcodebuild clean …` |
| resolve | `xcodebuild -resolvePackageDependencies <container args>` |
| run | `xcodebuild build …`, then section 9 |

## 8. Build settings

    xcodebuild -showBuildSettings -json <container args> -scheme <name> -destination <dest>

Output: a JSON array, one object per target the scheme builds:
`[{"target": "Puzzle", "buildSettings": {"KEY": "value", …}}, …]`.

Pick the **first** target whose `WRAPPER_EXTENSION` is `app` **and** whose `PLATFORM_NAME`
equals the destination's `PlatformName()`. This skips test bundles (`xctest`), frameworks, app
extensions, a watch app embedded in an iOS app, and a device build when a simulator was asked
for. In that target these keys must be present and non-empty: `BUILT_PRODUCTS_DIR`,
`FULL_PRODUCT_NAME`, `PRODUCT_BUNDLE_IDENTIFIER`, `EXECUTABLE_PATH`; a missing one is the error
`target <target>: missing <KEY>`. No matching target: `the scheme builds no <PlatformName> app`.

Result `BuildSettings`:

- `AppPath` = `BUILT_PRODUCTS_DIR/FULL_PRODUCT_NAME`
- `BundleID` = `PRODUCT_BUNDLE_IDENTIFIER`
- `Executable` = `BUILT_PRODUCTS_DIR/EXECUTABLE_PATH`

## 9. Install, launch, console

Runs after a successful build of a `run` job, with the build's scheme and destination.

### 9.1 Simulator destination

Each step runs to completion with stdout and stderr captured; a failure is the error
`<name> <args joined by spaces>: <exit error>: <captured output, trimmed>` and stops the sequence.
Unlike 6.2, these steps capture stdout and stderr together, in one text, so a one-shot runner
serves both shapes only if it keeps the two streams (6.2 shows stderr alone).

1. `xcrun simctl boot <UDID>` — a failure whose message contains `current state: Booted` is not
   a failure (the simulator was already up).
2. `open -a Simulator` — shows the simulator window.
3. `xcrun simctl install <UDID> <AppPath>`
4. `xcrun simctl launch --console-pty --terminate-running-process <UDID> <BundleID>` — streamed
   (6.1, SIGINT), with `SIMCTL_CHILD_OS_ACTIVITY_DT_MODE=YES` added to its environment, which
   simctl hands to the app as `OS_ACTIVITY_DT_MODE=YES`. Its lines are the app's stdout and
   stderr. It ends when the app exits.

Under `OS_ACTIVITY_DT_MODE=YES` a process mirrors every os_log message of level info and above,
from every framework in it, to stderr in full, the way Xcode's console gets them; the unified log
cuts a long argument at about a kilobyte and marks the cut with `<…>`. Debug messages are not
mirrored (9.3). The mirrored lines carry their own time (9.4).

**A device** skips steps 1 and 2 (it is on, and Simulator.app has nothing to show) and replaces
3 and 4, with the same error shape:

3. `xcrun devicectl device install app --device <UDID> <AppPath>`
4. `xcrun devicectl device process launch --console --terminate-existing --device <UDID> <BundleID>`
   — streamed (6.1, SIGINT, which devicectl forwards to the app), with
   `DEVICECTL_CHILD_OS_ACTIVITY_DT_MODE=YES` added to its environment, which devicectl hands to
   the app as `OS_ACTIVITY_DT_MODE=YES`. Its lines are devicectl's own and the app's stdout and
   stderr, mirror included. It ends when the app exits. devicectl exits 1 both when the launch
   is refused (a locked device: `ERROR: The application failed to launch …`) and when the app
   ends by a signal, so the exit status tells nothing; either way the run ends `exited` (12.7)
   and the console holds devicectl's lines.

The build signs for the device with the project's own settings; xctui adds no signing flags.

### 9.2 My Mac

Run `<Executable>` directly, streamed (SIGTERM), with `OS_ACTIVITY_DT_MODE=YES` added to its
environment (9.1). Its lines are the app's stdout and stderr.

### 9.3 Unified log (best effort)

Started right after the launch on a simulator or the Mac; if it cannot start the run goes on
without it. A device has no such stream (devicectl offers none): its debug messages are lost.
The mirror of 9.1 leaves out debug messages; this stream adds the app's own.

- simulator: `xcrun simctl spawn <UDID> log stream --style compact --level debug --predicate <P>`
- Mac: `xcrun log stream --style compact --level debug --predicate <P>`

`P` = `process == "<base name of Executable>" AND senderImagePath CONTAINS "/<base name of AppPath>/" AND messageType == debug AND NOT subsystem BEGINSWITH "com.apple.Previews"`

That is: debug messages logged by the app's own binary, minus the previews stub Xcode builds into
the bundle; frameworks' debug chatter stays out. These lines are subject to the unified log's cut
(9.1). Cancel: SIGTERM. It is cancelled when the run ends.

**Cleaning a log line** (`CleanLogLine`): lines starting with `Filtering the log data`,
`Timestamp ` or `getpwuid_r did not find` are dropped (the stream's header and simctl spawn's
noise). Otherwise the prefix matching `^\d{4}-\d\d-\d\d (\d\d:\d\d:\d\d\.\d+) \S+\s+\S+\[[^\]]*\] `
is replaced by the captured time of day plus one space: `2026-09-25 10:23:23.361 Df Puzzle[19921:134cebb] [com.apple.UIKit:Application] Deactivation reason added`
→ `10:23:23.361 [com.apple.UIKit:Application] Deactivation reason added`. A line without that
prefix is kept unchanged.

### 9.4 The console phase

A line from the launch stream (9.1 step 4 or 9.2) that the mirror wrote carries its own time: the
prefix matching `^\d{4}-\d\d-\d\d (\d\d:\d\d:\d\d\.\d{3})\d*[+-]\d{4} .+?\[\d+:\d+\] ` (e.g.
`2026-09-28 10:33:35.230501+0200 Puzzle[9836:34476303] `) is replaced by the captured time of day
plus one space: `2026-09-28 10:33:35.230501+0200 Puzzle[9836:34476303] [Session] Token expired` →
`10:33:35.230 [Session] Token expired`. Every other launch-stream line (the app's stdout and
stderr) carries no time and is prefixed with the wall clock at arrival, `HH:MM:SS.mmm ` (24-hour,
milliseconds). Lines from the unified log keep their own time from 9.3. Both land in the run's `console` phase in arrival order; lines from the two
streams have no defined order relative to each other (a test that needs one delays one stream).

## 10. Log processing

### 10.1 Issues

A line's kind: it contains `error: ` → **error**; else it contains `warning: ` → **warning**;
else plain. The trailing space matters: the backtrace xcodebuild prints when interrupted is full
of Objective-C selectors like `doBuildForBuildAction:colorizeFailure:error:]`.

A phase keeps its **issues**: the error and warning lines, each exact line once (xcodebuild
repeats a diagnostic for every task that hits it), in first-seen order, and counts `errors` and
`warnings` over those unique lines.

### 10.2 Paths

Every line appended to a job's log has `<folder of the container>/` (the container's parent
folder, with a trailing separator) removed wherever it occurs, so paths read relative to the
project: `/src/Puzzle/Game.swift:3:1: error: …` → `Puzzle/Game.swift:3:1: error: …` for the
container `/src/Puzzle.xcodeproj`.

### 10.3 Outcomes and marks

A job's `outcome` is `running`, then one of `succeeded`, `failed`, `stopped` (the user stopped
it), `exited` (a run's app quit on its own). A run also keeps `buildOutcome` (`succeeded`,
`failed`, `stopped`; empty while building) and a `status` while running: `building`,
`installing`, `attached`.

| Mark | When |
|---|---|
| `✓` | succeeded, exited |
| `✗` | failed |
| `■` | stopped |
| `●` | running and attached: the run's status is `attached`, or it has a console phase (the two coincide; either one makes a running run attached), so console rows and summaries show `●` while running |
| `⟳` | running otherwise |

### 10.4 Durations and times

`short(d)`: round to the second; `5s`, `32m 5s`, `1h 2m 3s`; zero is `0s`. A job's `elapsed`
is measured from its start to its end; a run's `buildElapsed` from its start to the end of the
build.

`when(started)`: `15:04` (24-hour) when the job started today, else `Jan 2 15:04` (three-letter
month, day without leading zero). "Today" is judged by the real clock when rendering, never by a
test clock; the screen fixtures start jobs on 2026-09-26 and expect the date form, so they are
wrong on that one day of the year.

`count(n, noun)`: `1 error`, `0 errors`, `2 warnings`.

### 10.5 Summaries

- `summary()` of a job — running: `<mark> <kind> <status or "running">`, e.g. `⟳ build running`,
  `⟳ run building`, `⟳ run installing`, `● run attached`. Finished:
  `<mark> <kind> <outcome> <short elapsed> · <count errors> · <count warnings>` with the counts of
  the first phase, e.g. `✗ build failed 2s · 2 errors · 1 warning`, `✓ run exited 2s · 0 errors · 0 warnings`.
- `buildSummary()` of a run (the Detail title for its own row) — kind ≠ run: `summary()`;
  build not finished: `⟳ run building`; else
  `<mark of buildOutcome> run build <buildOutcome> <short buildElapsed> · <errors> · <warnings>`.
- `consoleSummary()` — `<mark> console <BundleID> · <"attached" while running, else outcome>`,
  e.g. `● console com.example.Puzzle · attached`, `✓ console com.example.Puzzle · exited`.

### 10.6 Runs rows

`<mark> <kind padded right to 7> <two spaces> <when> <two spaces> <duration>`, i.e. the format
`%s %-7s  %s  %s`. Duration is `…` while the row's outcome is running, else `short()`.

- A job's own row: mark and duration from `outcome`/`elapsed`; for a **run** from
  `buildOutcome`/`buildElapsed` instead (the row is the build's verdict; empty buildOutcome
  counts as running).
- A run's **console row** (only once the console phase exists): mark from `outcome` with the
  attached rule, kind `console`, duration from `elapsed`.

Examples: `✗ build    Sep 26 10:00  2s`, `● console  Sep 26 10:00  …`, `✓ run      20:03  2s`.

### 10.7 Failure lines

When a job's process ends with an error, one line `error: <error>` is appended to its phase
(so it shows in the log and counts as an issue), e.g. `error: exit status 65`. When install or
launch fails, the error text is split on newlines and each line appended the same way. A job
the user stopped gets no such line, and neither does a run whose app exits by itself, with any
exit status: that run ends `exited` (12.7).

## 11. CLI

`xctui` is one binary. Usage (printed to stderr, exit 2, on an unknown subcommand or missing
arguments):

    Usage:
      xctui                                 open the TUI for the project in the current folder
      xctui <scheme>                        same, with that scheme selected
      xctui project                         print the .xcworkspace or .xcodeproj xctui would use
      xctui schemes                         list schemes, then local package products
      xctui destinations                    list run destinations, default marked with *
      xctui build <scheme> [destination]    build; output streams, exit code is xcodebuild's
      xctui test <scheme> [destination]
      xctui clean <scheme> [destination]
      xctui resolve                         resolve package dependencies
      xctui settings <scheme> [destination] print the app path, bundle id and executable
      xctui run <scheme> [destination]      build, install, launch; the console streams

Common rules:

- The container is found from the current folder (section 3) by every subcommand except
  `destinations`, which needs no project. Any error is printed to stderr as `xctui: <message>`
  with exit 1.
- `<scheme>` must equal a scheme name from section 4, else `xctui: scheme "<arg>" not found`.
- `[destination]` is matched against the list of section 5, first by exact name (the first row
  with that name wins, so a name that repeats across runtimes means the booted or newest one),
  then by exact UDID (`mac` for My Mac). Missing → `xctui: destination "<arg>" not found`.
  Omitted → the default of 5.5.
- One argument that is not a subcommand name opens the TUI with that scheme (12.1), even when
  no scheme of that name exists (the TUI then warns and uses the default). Usage is printed only
  for a subcommand that lacks its arguments, or for two or more arguments that fit no form.
- The TUI forms need a terminal; without one (stdout not a tty) they fail like any other
  error, `xctui: <message>` and exit 1. The message text is free.

Subcommands:

- `project` — stdout: the container's absolute path, one line.
- `schemes` — one line per scheme in the order of 4.4: `* ` before the default scheme (4.5),
  two spaces before the others, then the name, then ` (package)` for a package scheme.
- `destinations` — one line per destination in the order of 5.4: `* ` before the default (5.5),
  two spaces otherwise, then the all-platforms label of 5.6, then ` (device)` for a device (the
  TUI's colour, 12.13, has no plain-text form), two spaces, the UDID.
- `settings` — three lines: `app: <AppPath>`, `bundle: <BundleID>`, `executable: <Executable>`.
- `build`, `test`, `clean`, `resolve` — stdout receives the job's log lines as they arrive
  (section 10.2 applied, and the failure line of 10.7 at the end of a failed job). When the
  process ends, `summary()` (10.5) goes to stderr. Exit code: 0 on success; the tool's exit
  code on failure (xcodebuild uses 65 for a failed build); 130 when stopped. Ctrl-C stops the
  job as in 6.1 (the tool is in its own process group; xctui sends SIGTERM, then SIGKILL after
  5 s), waits for it, and the summary reads `■ build stopped …`.
- `run` — as `build`; on success stderr gets the run's `buildSummary()` (10.5), e.g.
  `✓ run build succeeded 2s · 0 errors · 0 warnings`, and stdout gets
  `▸ Installing and launching on <destination Name>`, then section 9 runs. Then stdout gets
  `▸ Launched <BundleID>` (the TUI's version of this line, 12.7, adds a hint about its console
  row) and every console line (9.4, with its time prefix), until the app exits (exit 0, stderr `✓ run exited …`) or Ctrl-C (SIGINT
  to simctl or devicectl launch, exit 130, stderr `■ run stopped …`). A failing install or launch prints its
  error lines (10.7) to stdout, `✗ run failed …` to stderr, exit 1.

## 12. TUI

### 12.1 Startup and arguments

`xctui` with no arguments, or `xctui <scheme>`. The container comes from section 3; failure
prints `xctui: <message>` to stderr, exit 1. The TUI takes the whole terminal (alternate
screen) until `q` or Ctrl-C.

On start, concurrently: schemes (section 4), destinations (section 5), and a query for the
terminal's background colour (12.13). Prefs (13.1) and run history (13.2) are read first,
synchronously. Nothing is drawn until the terminal size is known.

When the schemes arrive: on error, Status shows `Couldn't list schemes: <error>` (error style).
Else the current scheme is the remembered one (prefs, or the command-line argument, which
overrides prefs) if a scheme of that name exists; otherwise the default (4.5), and when a name
was remembered but not found Status shows `No scheme "<name>"; using <default>` (warning style).
If the current scheme is a package scheme the Schemes pane switches to its Packages tab. The
scheme cursor goes to the current scheme's position in the shown list.

When the destinations arrive: on error, `Couldn't list simulators: <error>`. Else the current
destination is the remembered UDID if it exists, otherwise the default (5.5). The cursor goes to
its position.

Focus starts on the Schemes pane.

### 12.2 Layout

Terminal `width × height`. The last row is the footer (12.10). `body = height - 1`.

Left column width `left = max(32, width / 3)` (integer division); the right column
`right = width - left`. Both columns are `body` rows tall.

The right column is one pane, **[0] Detail**, `right × body`.

The left column stacks four panes, top to bottom: **[1] Status** (4 rows), **[2] Schemes**,
**[3] Destinations**, **[4] Runs**. With `rest = body - 4`: Schemes and Destinations get
`rest / 3` rows each, Runs gets the remainder `rest - 2 * (rest / 3)`.

**Expanded** (12.10): the left column is only the last-focused left pane, `left × body`.

A pane's inner area is its size minus a one-cell border on every side; a pane of height h shows
h − 2 rows. The Detail pane therefore shows `detailHeight = height - 3` log rows. Terminals
narrower than 40 columns or shorter than 8 rows are not supported; what they show is unspecified.

### 12.3 Frames

Every pane is a box drawn with `┌ ─ ┐ │ └ ┘`. Top border: `┌` then the title then `─` to the
right edge then `┐`. Bottom border: `└` then the bottom label (a filter, 12.8; empty otherwise)
then `─` then `┘`. Each inner row is `│`, the row's text cut to the inner width and padded
with spaces, `│`. Text is cut and never marked with an ellipsis; only Detail's log lines wrap,
and they reach the frame as rows already (12.4).

Titles: `[<number>]─<name>`, e.g. `[1]─Status`, `[0]─Detail: ✗ build failed 2s · 2 errors · 1 warning`.
The focused pane's border and title use the focused style; others the dim style (12.13).

### 12.4 Panes

**[1] Status** — two rows.
Row 1, in order of precedence: the status message if one is set (in its style); else the
`summary()` of the job started most recently in this session, running or finished (in the job's
style; keep a reference to that job, it is not the newest list entry once history is loaded and
nothing was started yet); else, when history was loaded, `last time: ` + the newest job's
`summary()` (dim); else `no runs yet · b builds` (dim).
Row 2: the container's base name, e.g. `Puzzle.xcodeproj` (dim).
A status message stays until the next job starts (which clears it) or another message replaces
it. Messages: `Couldn't list schemes: …`, `Couldn't list simulators: …`, `No scheme "…"; using …`,
`Busy — press x to stop`, `Pick a scheme (2) and a destination (3) first`, `No run to open`,
`Couldn't write the log: …`, `Editor: …`, `Couldn't save the run: …`,
`Couldn't remember the selection: …`, and those of `D` (12.14) and `R` (12.15).

**[2] Schemes** — title is the tab bar `Schemes · Packages` (12.9). Rows: the schemes of the
active tab (own schemes, or package schemes) that match the pane's filter; the label is the
name. `* ` marks the current scheme.

**[3] Destinations** — title is the tab bar `Destinations · iOS · tvOS · watchOS · macOS`.
Rows: every destination (first tab) or those of the tab's platform, that match the filter, with
the labels of 5.6 (the all-platforms label on the first tab, the one-platform label otherwise).
`* ` marks the current destination.

**[4] Runs** — title `Runs`. Rows: jobs newest first; a run that reached its app has its
`console` row directly **above** its own row (10.6). The cursor row is also the `* ` row. Rows
take the job's style (12.13).

**[0] Detail** — shows one phase: the phase under the Runs cursor (the console phase for a
console row, the first phase otherwise). Title (12.3): `Detail` with no jobs; else `Detail: ` +
`buildSummary()` for a job row or `consoleSummary()` for a console row; then ` · issues only`
when issues mode is on and the phase has no filter; then ` · G follows` when the shown view is
scrolled away from the tail. Rows: the shown view's visible lines (12.7), each **wrapped** into
rows of `right - 2` cells — cut at cell boundaries, spaces kept, a wide character that does not
fit a row starting the next, an empty line one empty row — and styled by the line's kind (10.1);
rows past `detailHeight` are dropped. For the console phase the time prefix
(`^\d\d:\d\d:\d\d\.\d+ `) is dropped before wrapping while timestamps are off, and kept and
shown dim at the start of the first row while they are on (`s`). A line a tool redrew with
carriage returns (git's clone progress) shows only the text after its last non-trailing `\r`,
as a terminal would: a raw `\r` sends the cursor back over the left panes. With no
lines the single row is a hint (dim): `no line matches` when the phase has a filter,
`No errors or warnings. i shows the full log.` in issues mode, else `waiting for output`. With
no jobs at all: `b build · r run on the simulator · t test · c clean`, with `device` for
`simulator` when the current destination is a device.

### 12.5 Lists

A list pane renders its items so that the cursor row is visible: with `rows` inner rows and the
cursor at position `pos` (0 when the cursor item is not in the list), the first shown item is
`max(0, pos - rows + 1)` and `rows` items follow. Each row is a two-character marker (`* ` for
the current item, two spaces otherwise) followed by the label. When the pane has focus, the
cursor row is drawn in the selection style across the whole inner width (12.13).

Cursor movement clamps to `[0, items - 1]`; an empty list keeps the cursor at 0.

### 12.6 Keys

Keys act on key press. `msg text` is the printable text of a key, empty for special keys.

**Help open** (12.10): any key closes the help; the key does nothing else.

**Delete popup open** (12.14): `y` deletes, `n` or `esc` closes the popup, `ctrl+c` quits;
every other key does nothing.

**Rename popup open** (12.15): `enter` renames, `esc` closes the popup, `backspace` removes the
last character, `ctrl+c` quits; any other key appends its text, `space` included (special keys
without text do nothing).

**Typing a filter** (12.8): `esc` clears the filter and stops typing; `enter` keeps it and stops
typing; `backspace` removes the last character; `ctrl+c` quits; any other key appends its text,
`space` included (special keys without text do nothing). After each change the cursor is re-clamped to the
narrowed list.

Otherwise:

| Key | Effect |
|---|---|
| `q`, `ctrl+c` | quit (12.12) |
| `b` | start a `build` job with the current scheme and destination |
| `r` | start a `run` job |
| `t` | start a `test` job |
| `c` | start a `clean` job |
| `p` | start a `resolve` job |
| `x` | stop the current job (12.7) |
| `0` … `4` | focus that pane if focusable |
| `tab`, `shift+tab` | focus the next / previous focusable pane in the order 0, 1, 2, 3, 4, wrapping |
| `j`, `down` / `k`, `up` | cursor down / up in a list pane; scroll the shown log by one row in Detail |
| `pgdown` / `pgup` | the same by `detailHeight` |
| `g`, `home` / `G`, `end` | first / last item, or top / bottom of the log |
| `ctrl+d` / `ctrl+u` | scroll the shown log by half of `detailHeight` down / up, whichever pane has focus |
| `enter`, `space` | in Schemes or Destinations: make the cursor item current and save prefs (13.1); refused with `Busy — press x to stop` while a job runs; elsewhere nothing |
| `R` | in Destinations, with the cursor on a simulator: rename it (12.15); elsewhere, on My Mac and on a device nothing |
| `/` | start typing the focused pane's filter (Schemes, Destinations, or Detail when it shows a phase); nothing in Status or Runs |
| `esc` | clear the focused pane's filter |
| `[`, `]` | previous / next tab of the focused Schemes or Destinations pane (wrapping); nothing elsewhere |
| `+` | expand: the last-focused left pane fills the left column |
| `_` | restore the four left panes |
| `i` | toggle issues mode (Detail shows only issues) |
| `s` | toggle console timestamps |
| `e` | open the shown log in an editor (12.11) |
| `D` | ask to delete the run history (12.14) |
| `?`, `f1` | show the help |

`b r t c` need a current scheme and destination, else Status shows
`Pick a scheme (2) and a destination (3) first` (warning) and nothing starts; `p` needs neither. Any job key while a
job runs shows `Busy — press x to stop` (warning) and nothing starts.

**Focusable**: Status always; Schemes when schemes are loaded and non-empty; Destinations when
destinations are non-empty; Runs and Detail when there is at least one job. A key to an
unfocusable pane does nothing; `tab` skips unfocusable panes. While typing a filter no key
changes focus: digits are text, `tab` has no text and does nothing, so typing ends only with
`enter` or `esc`. The
**last-focused left pane** (for `+`) is updated whenever a pane other than Detail gets focus;
it starts as Schemes.

### 12.7 Jobs

State: `idle`, `building` (xcodebuild runs: build, test, clean, resolve), `installing` (build
settings, boot, install, launch), `running` (the app runs). Only one job at a time.

Starting a job (`b r t c p` while idle): a new job with `kind` (`build`, `run`, `test`, `clean`,
`resolve`), started now, outcome `running`, one phase named `build` for a run and after the kind
otherwise; a run's status is `building`. The job goes to the **front** of the list; the list
keeps at most 50 jobs (the oldest drop off). The Runs cursor goes to row 0, so Detail follows the
new job. The status message is cleared. The process starts (section 7).

Lines arriving from the job's process are appended to its last phase (10.2 applied). In state
`running`, lines from the launch stream get the wall-clock prefix (9.4); lines from the unified
log are cleaned (9.3) and dropped when `CleanLogLine` says so.

When the job's process ends (or could not start), with `err` its error:

- if the user had pressed `x`: outcome `stopped`;
- else state `building` and `err`: outcome `failed`, and the failure line (10.7);
- else state `building` and kind `run`: `buildOutcome = succeeded`, `buildElapsed` set, state
  `installing`, status `installing`, the build phase gets `▸ Installing and launching on <destination Name>`,
  and section 9 starts (8, then 9.1 or 9.2, then 9.3);
- else state `building`: outcome `succeeded`;
- else state `installing` (install or launch failed): outcome `failed` with the error lines (10.7);
- else state `running` (the app exited by itself): outcome `exited`.

A finished job records `elapsed`; a run whose `buildOutcome` is still empty gets the job's
outcome as its build outcome. The job is saved (13.2); a save error shows
`Couldn't save the run: <error>`. State returns to `idle` unless the run moved on to
`installing`.

When the launch of a run succeeds: state `running`, status `attached`, the build phase gets
`▸ Launched <BundleID>. Its output is in the console row above.`, the run remembers the app
(`BundleID`) and the attach time, and a new phase `console` is added — from now on the Runs
pane shows the console row above the run row. The unified log's ending does **not** end the
run; the launch stream's ending does.

`x` while a job runs, in any of the three busy states: cancels it (6.1, 6.2) once; a second `x`
does nothing. The job then ends as `stopped`. A stopped run does not install; `x` while
installing cancels the step in progress and skips the rest. Log lines are never removed, so the
`▸ Installing and launching on …` line stays in a stopped run's log. `x` while idle does nothing.
The TUI never inspects a process's exit code; it only needs to know whether the process ended
with an error.

**Log views**: every phase has three views — the full log, the issues, and the filter matches —
each a window of `detailHeight` rows over a growing list of lines, with an `offset` (the first
line in view) and a `follow` flag. A line takes as many rows as its wrapping needs (12.4), so
every view knows Detail's inner width and the timestamp mode: every terminal size message and
`s` set the width, height and mode of every view of every job (history is loaded before the
terminal size is known, so a view may start at width and height 0). The **tail** is the
smallest offset whose lines all fit in the window together, or the last line alone when even
it does not fit. Appending while following keeps the offset at the tail. Scrolling by `n` rows
moves the offset over whole lines: forward past every line that fits in `n` rows together,
backward likewise, at least one line either way; a move of more rows than there are lines
lands on an end. The offset is then clamped to `[0, tail]` and `follow` set to whether it is
at the tail, so scrolling to the bottom turns following back on.
The **shown view** is: the filter matches when the phase has a filter; else the issues in
issues mode; else the full log.

### 12.8 Filters

Schemes and Destinations each keep one filter string; Detail's filter belongs to the shown
phase (each phase keeps its own). A filter narrows the list to items whose text contains it,
case-insensitively — the scheme's name; the destination's `<Name> <Runtime>`; a log line.
Changing a phase's filter rebuilds its matches from the whole log; new lines that match are
appended as they arrive.

The filter shows on the pane's bottom border as `/<text>`, with `▏` after it while typing.
While typing the footer reads `type to filter  enter keep  esc clear`. Filters persist across
focus changes; `esc` (when not typing) clears only the focused pane's filter.

### 12.9 Tabs

Schemes: `Schemes` (own schemes) and `Packages` (package schemes); starts on Schemes, or on
Packages when the current scheme is a package scheme. Destinations: `Destinations` (all), `iOS`,
`tvOS`, `watchOS`, `macOS`; starts on Destinations. `[` and `]` step through the focused pane's
tabs, wrapping, and re-clamp the cursor.

The tab bar is the pane's title, drawn with a budget of `pane width - 6` cells (the pane width
includes its borders): names joined by
` · `, the active name in the focused style and the others dim; if that is wider than the budget,
joined by `·`; if still wider, only the active name. At 80 or 100 columns the Destinations title
is therefore just the active tab, `Destinations`; at 140 it reads `Destinations·iOS·tvOS·watchOS·macOS`.

### 12.10 Expand, help, footer

`+` shows only the last-focused left pane in the left column, at full body height; `_` restores.
Detail is unaffected.

`?` or `f1` draws the help over the screen, replacing every cell under it (its inner rows are
padded with spaces, so the panes do not show through): a box titled `Keys` in the focused style,
64 cells wide, one row per binding — the key padded right to 16 characters, one space, the description —
then an empty row, then `any key closes` (dim), and the box's border. It is centred:
x = `(width - 64) / 2`, y = `(body - box height) / 2`, both at least 0. The bindings, in order:

    b                build
    r                run
    t                test
    c                clean
    p                resolve packages
    x                stop
    0-4              focus a pane
    tab / shift+tab  next / previous pane
    [ / ]            tabs of Scheme (schemes, packages) or Destinations (all, iOS, tvOS, …)
    j / k            move in the pane
    enter / space    select the scheme or destination
    R                rename the simulator under the cursor
    /                filter the pane (lists, or the shown log); enter keeps, esc clears
    g / G            top / bottom
    ctrl+u / ctrl+d  half a page of the log
    pgup / pgdn      a page
    + / _            expand the left pane / restore
    i                issues only
    s                show / hide console timestamps
    e                open the shown log in $EDITOR
    D                delete the run history
    ? / F1           this help
    q                quit

The footer (last row) shows, joined by two spaces, key in bold and label dim:
`b build  r run  t test  c clean  p resolve packages  x stop  0-4/tab focus  e editor  ? help  q quit`,
cut to the width. `D` and `R` are only in the help. The footer is the one row that is cut but not padded to the width; every
other row of the screen is exactly `width` cells.

### 12.11 Editor

`e` with no shown phase: Status `No run to open` (warning). Else the shown view's lines (so the
filter or issues mode applies) are written to a temporary file named `xctui-<phase name>-<random>.log`
with a trailing newline, and `$VISUAL`, else `$EDITOR`, else `vi` is run with that path as its
only argument, in the foreground: the editor stays in the terminal's own process group and the
TUI stops reading the terminal and drawing until it exits, then restores raw mode and redraws;
the variable's value is the executable name, not split on spaces. A failure to write shows
`Couldn't write the log: <error>`; the editor exiting with an error shows `Editor: <error>`.

### 12.12 Quitting

`ctrl+c` (also while typing), `q` when not typing, or SIGTERM. On the way out: the current job is cancelled
(6.1) and, if one was running, marked `stopped` and saved (13.2); every stream is drained and
waited for, so no xcodebuild, simctl or log process survives xctui.

### 12.13 Styles

| Style | Rendering |
|---|---|
| error | red foreground |
| warning | yellow foreground |
| success | green foreground |
| dim | bright black foreground |
| bold | bold |
| focused | blue foreground, bold |
| border | dim; focused pane: focused |
| device | cyan foreground: a device's row in Destinations, the only mark that tells it from a simulator |
| selection (cursor row) | bold on a background one shade off the terminal's: on a light background the background darkened by 9 %, on a dark one lightened by 8 %. Until the terminal has answered the background query: reverse video, bold. It fills the row across the pane. |

Job style by outcome: succeeded and exited → success; failed → error; stopped → warning;
running → plain. Runs rows and the Status summary use the job's style; Detail lines use the
line's kind (error, warning, plain); tab names and titles as in 12.9 and 12.3. Colours are the
terminal's ANSI colours.

### 12.14 Deleting the history

`D` deletes every job in the Runs pane except a running one (it is always the newest), with
`n` that number. `n = 0`: Status `Only the running job is left; x stops it` (warning) while a
job runs (a run counts as running until its app exits), else `No runs to delete` (warning).
Otherwise `D` opens a popup,
drawn and centred like the help (12.10): a box 40 cells wide titled `Delete history` in the
focused style, with the rows `Delete <count(n, "run")> from the history?`, an empty row, and
`y yes  n no` (keys bold, labels dim); `n` is counted when drawing. Its keys are in 12.6. `y`
closes it and deletes: it removes the project's history folder (13.2) with everything in it, drops the jobs from the list, puts the
Runs cursor on row 0, forgets the session's last job unless it runs (Status falls back per
12.4), moves focus to Status when the focused pane is no longer focusable, and shows
`Deleted <count(n, "run")>` (dim). A failure to remove the folder shows
`Couldn't delete the runs: <error>` and keeps the list. A job that finishes later is saved
into a new folder as usual.

### 12.15 Renaming a simulator

`R` with Destinations focused and its cursor on a simulator opens a popup; on My Mac, on a
device, in an empty list, or with another pane focused it does nothing. The popup is drawn and centred like the help
(12.10): a box 64 cells wide titled `Rename simulator` in the focused style, with the rows
`<text>▏`, an empty row, and `enter rename  esc cancel` (keys bold, labels dim). The text starts
as the simulator's name; the popup's keys are in 12.6, so `b`, `q` or a digit are text. `enter`
closes the popup and trims the text; a blank text or the unchanged name does nothing more.
Otherwise `simctl rename` (5.7) runs in the background, also while a job runs. When it succeeds,
the simulator with that UDID takes the new name in the Destinations list (in the same place)
and, when it is the current destination, there too; Status shows
`Renamed <old name> to <new name>` (dim). A failure shows `Couldn't rename the simulator: <error>`
(error) and keeps the name.

## 13. Persistence

Persistence belongs to the TUI: the CLI reads no prefs and saves no history.

### 13.1 Prefs

`<config>/xctui/prefs.json`: a JSON object keyed by the container's absolute path:

    {
      "/Users/me/src/Puzzle.xcodeproj": {
        "scheme": "Puzzle",
        "simulator": "6AFBF4E8-BDB3-408A-9458-21A2A91D7899"
      }
    }

`simulator` is the UDID (names repeat across runtimes; `mac` for My Mac). Written, indented by
two spaces and without a trailing newline, whenever the user selects a scheme or destination,
keeping the other containers' entries; the folder is created as needed. A missing or unreadable file means no prefs. A
failure to write shows `Couldn't remember the selection: <error>`. Reading and writing is
skipped when no config folder is known.

### 13.2 Run history

Folder: `<config>/xctui/runs/<container base name without extension>-<hex of the first 4 bytes of SHA-1(container path)>/`,
e.g. `runs/Puzzle-1a2b3c4d/`. Each saved job is a folder
`<start time as 2006-01-02T15-04-05.000000000>-<kind>/` holding:

- `meta.json` — `{"Kind", "Status", "Outcome", "BuildOutcome", "App", "Started", "Attached",
  "Elapsed", "BuildElapsed", "Phases": [{"Name", "Errors", "Warnings"}, …]}`, indented by two
  spaces, no trailing newline; times as RFC 3339 with nanoseconds and the local offset,
  `Attached` being the zero time `0001-01-01T00:00:00Z` for a job that never attached;
  durations as integer nanoseconds;
- `<phase name>.log` per phase — the phase's lines joined by `\n`, plus a trailing `\n`.

A job is saved when it finishes and when xctui quits while it runs (as `stopped`). After each
save, only the 50 newest folders (by name, descending) are kept. At startup the folders are read
newest first; a folder whose `meta.json` is missing, unreadable or has no phases is skipped. A
loaded phase has its counts from the metadata; its log file is read the first time the phase is
shown — that is, whenever the Runs cursor selects it, also in a model that never renders — and
its issues are rebuilt from the lines then (10.1). A missing log file shows one line
`(log missing: <error>)`. `D` removes the folder (12.14); from outside xctui, deleting the
folder (or all of `<config>/xctui/runs/`) does the same at the next start.

## 14. Non-goals

Physical devices; Xcode's own log store; a debugger; structured `.xcresult` parsing; editing
schemes or settings; anything Xcode-only such as previews or Instruments.

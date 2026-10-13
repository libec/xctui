package xcode

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// Install boots the simulator, shows it in Simulator.app and installs the app on it.
// A device needs only the install.
func Install(ctx context.Context, simulator Simulator, appPath string) error {
	return installWith(simulator, appPath, func(name string, args ...string) error {
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, bytes.TrimSpace(out))
		}
		return nil
	})
}

func installWith(simulator Simulator, appPath string, run func(name string, args ...string) error) error {
	if simulator.Device {
		return run("xcrun", "devicectl", "device", "install", "app", "--device", simulator.UDID, appPath)
	}
	err := run("xcrun", "simctl", "boot", simulator.UDID)
	if err != nil && !strings.Contains(err.Error(), "current state: Booted") {
		return err
	}
	if err := run("open", "-a", "Simulator"); err != nil {
		return err
	}
	return run("xcrun", "simctl", "install", simulator.UDID, appPath)
}

// mirrorEnv makes a process mirror its os_log messages of level info and above,
// from every framework in it, to stderr in full, the way Xcode's console gets
// them. The unified log cuts a long argument at about a kilobyte and marks the
// cut with <…>; the mirror does not. Debug messages are not mirrored (LogStream).
const mirrorEnv = "OS_ACTIVITY_DT_MODE=YES"

// LaunchMac runs a macOS app's executable directly, so its stdout and stderr stream
// like a simulator console. Cancelling sends SIGTERM, which quits the app.
func LaunchMac(ctx context.Context, settings BuildSettings) (*Stream, error) {
	return start(macCommand(ctx, settings), syscall.SIGTERM)
}

func macCommand(ctx context.Context, settings BuildSettings) *exec.Cmd {
	cmd := exec.CommandContext(ctx, settings.Executable)
	cmd.Env = append(os.Environ(), mirrorEnv)
	return cmd
}

// LogStream follows the app's own debug messages in the unified log, the one
// level the stderr mirror (mirrorEnv) leaves out; frameworks' debug chatter stays
// out. Inside a simulator that is `log stream` under simctl spawn; on the Mac it
// is the host's own. A device has none to reach, so its debug messages are lost.
// Cancelling sends SIGTERM.
func LogStream(ctx context.Context, simulator Simulator, settings BuildSettings) (*Stream, error) {
	if simulator.Device {
		return nil, nil
	}
	args := []string{"log", "stream", "--style", "compact", "--level", "debug", "--predicate", logPredicate(settings)}
	if !simulator.IsMac() {
		args = append([]string{"simctl", "spawn", simulator.UDID}, args...)
	}
	return start(exec.CommandContext(ctx, "xcrun", args...), syscall.SIGTERM)
}

// logPredicate selects debug messages logged by the app's own binary and drops
// the previews stub Xcode builds into the bundle.
func logPredicate(settings BuildSettings) string {
	return fmt.Sprintf(`process == %q AND senderImagePath CONTAINS %q AND messageType == debug AND NOT subsystem BEGINSWITH "com.apple.Previews"`,
		filepath.Base(settings.Executable), "/"+filepath.Base(settings.AppPath)+"/")
}

// logLinePrefix is what `log stream --style compact` puts before each message:
// "2026-09-25 10:23:23.361 Df Puzzle[19921:134cebb] ". The group is the time of day.
var logLinePrefix = regexp.MustCompile(`^\d{4}-\d\d-\d\d (\d\d:\d\d:\d\d\.\d+) \S+\s+\S+\[[^\]]*\] `)

// CleanLogLine cuts a `log stream` line down to its time of day and message and
// reports false for the stream's own header lines and simctl spawn's noise.
func CleanLogLine(line string) (string, bool) {
	for _, noise := range []string{"Filtering the log data", "Timestamp ", "getpwuid_r did not find"} {
		if strings.HasPrefix(line, noise) {
			return "", false
		}
	}
	return logLinePrefix.ReplaceAllString(line, "$1 "), true
}

// mirrorPrefix is what the mirror (mirrorEnv) puts before each os_log message:
// "2026-09-28 10:33:35.230501+0200 Puzzle[9836:34476303] ". The group is the
// time of day to the millisecond.
var mirrorPrefix = regexp.MustCompile(`^\d{4}-\d\d-\d\d (\d\d:\d\d:\d\d\.\d{3})\d*[+-]\d{4} .+?\[\d+:\d+\] `)

// CleanMirroredLine cuts a mirrored os_log line down to its time of day and
// message, and reports false for any other line of the app's output.
func CleanMirroredLine(line string) (string, bool) {
	if !mirrorPrefix.MatchString(line) {
		return line, false
	}
	return mirrorPrefix.ReplaceAllString(line, "$1 "), true
}

// Launch starts the app and streams its console until the app exits or ctx is cancelled.
// simctl and devicectl hand the app the mirror variable (mirrorEnv) from their own
// environment. Cancelling sends SIGINT: simctl then terminates the app, which SIGTERM
// or SIGKILL would leave running; devicectl forwards it to the app.
func Launch(ctx context.Context, simulator Simulator, bundleID string) (*Stream, error) {
	return start(launchCommand(ctx, simulator, bundleID), os.Interrupt)
}

func launchCommand(ctx context.Context, simulator Simulator, bundleID string) *exec.Cmd {
	if simulator.Device {
		cmd := exec.CommandContext(ctx, "xcrun", "devicectl", "device", "process", "launch", "--console", "--terminate-existing", "--device", simulator.UDID, bundleID)
		cmd.Env = append(os.Environ(), "DEVICECTL_CHILD_"+mirrorEnv)
		return cmd
	}
	cmd := exec.CommandContext(ctx, "xcrun", "simctl", "launch", "--console-pty", "--terminate-running-process", simulator.UDID, bundleID)
	cmd.Env = append(os.Environ(), "SIMCTL_CHILD_"+mirrorEnv)
	return cmd
}

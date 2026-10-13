package xcode

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestInstall(t *testing.T) {
	simulator := Simulator{UDID: "ABC-123"}
	allCalls := [][]string{
		{"xcrun", "simctl", "boot", "ABC-123"},
		{"open", "-a", "Simulator"},
		{"xcrun", "simctl", "install", "ABC-123", "/dd/Puzzle.app"},
	}

	tests := []struct {
		name      string
		bootErr   error
		wantCalls [][]string
		wantErr   bool
	}{
		{"shut down simulator", nil, allCalls, false},
		{"already booted", errors.New("Unable to boot device in current state: Booted"), allCalls, false},
		{"boot fails", errors.New("Invalid device"), allCalls[:1], true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls [][]string
			fakeRun := func(name string, args ...string) error {
				calls = append(calls, append([]string{name}, args...))
				if strings.Join(args, " ") == "simctl boot ABC-123" {
					return tt.bootErr
				}
				return nil
			}

			err := installWith(simulator, "/dd/Puzzle.app", fakeRun)
			if (err != nil) != tt.wantErr {
				t.Errorf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(calls, tt.wantCalls) {
				t.Errorf("got calls %v, want %v", calls, tt.wantCalls)
			}
		})
	}
}

func TestInstallOnDevice(t *testing.T) {
	var calls [][]string
	fakeRun := func(name string, args ...string) error {
		calls = append(calls, append([]string{name}, args...))
		return nil
	}
	if err := installWith(Simulator{UDID: "00008140-X", Device: true}, "/dd/Puzzle.app", fakeRun); err != nil {
		t.Fatal(err)
	}
	// Nothing to boot and no Simulator.app.
	want := [][]string{{"xcrun", "devicectl", "device", "install", "app", "--device", "00008140-X", "/dd/Puzzle.app"}}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("got calls %v, want %v", calls, want)
	}
}

func TestNoLogStreamOnDevice(t *testing.T) {
	stream, err := LogStream(context.Background(), Simulator{UDID: "00008140-X", Device: true}, BuildSettings{Executable: "/dd/Puzzle.app/Puzzle"})
	if stream != nil || err != nil {
		t.Errorf("got %v, %v; want no stream and no error", stream, err)
	}
}

func TestLogPredicate(t *testing.T) {
	settings := BuildSettings{AppPath: "/dd/Debug-iphonesimulator/Puzzle.app", Executable: "/dd/Debug-iphonesimulator/Puzzle.app/Puzzle"}
	got := logPredicate(settings)
	for _, want := range []string{`process == "Puzzle"`, `senderImagePath CONTAINS "/Puzzle.app/"`, "messageType == debug", `NOT subsystem BEGINSWITH "com.apple.Previews"`} {
		if !strings.Contains(got, want) {
			t.Errorf("predicate %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "error") {
		t.Errorf("errors and faults reach the console through the mirror, not the log: %q", got)
	}
	mac := BuildSettings{AppPath: "/dd/Debug/ClipNinja.app", Executable: "/dd/Debug/ClipNinja.app/Contents/MacOS/ClipNinja"}
	if got := logPredicate(mac); !strings.Contains(got, `process == "ClipNinja"`) || !strings.Contains(got, `"/ClipNinja.app/"`) {
		t.Errorf("mac predicate %q", got)
	}
}

func TestCleanLogLine(t *testing.T) {
	tests := []struct {
		line   string
		want   string
		wantOK bool
	}{
		{"2026-09-25 10:23:23.361 Df Puzzle[19921:134cebb] [com.apple.UIKit:Application] Deactivation reason added", "10:23:23.361 [com.apple.UIKit:Application] Deactivation reason added", true},
		{"2026-09-25 10:23:23.363 A  Puzzle[19921:134cebb] (CoreFoundation) Loading Preferences", "10:23:23.363 (CoreFoundation) Loading Preferences", true},
		{`Filtering the log data using "process == \"Puzzle\""`, "", false},
		{"Timestamp               Ty Process[PID:TID]", "", false},
		{"getpwuid_r did not find a match for uid 501", "", false},
	}
	for _, tt := range tests {
		got, ok := CleanLogLine(tt.line)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("CleanLogLine(%q) = %q, %v; want %q, %v", tt.line, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestLaunchMirrorsTheLog(t *testing.T) {
	cmd := launchCommand(context.Background(), Simulator{UDID: "ABC-123"}, "com.example.Puzzle")
	want := []string{"xcrun", "simctl", "launch", "--console-pty", "--terminate-running-process", "ABC-123", "com.example.Puzzle"}
	if !slices.Equal(cmd.Args, want) || !slices.Contains(cmd.Env, "SIMCTL_CHILD_OS_ACTIVITY_DT_MODE=YES") {
		t.Errorf("simulator launch: args %v, mirror variable set: %v", cmd.Args, slices.Contains(cmd.Env, "SIMCTL_CHILD_OS_ACTIVITY_DT_MODE=YES"))
	}
	device := launchCommand(context.Background(), Simulator{UDID: "00008140-X", Device: true}, "com.example.Puzzle")
	want = []string{"xcrun", "devicectl", "device", "process", "launch", "--console", "--terminate-existing", "--device", "00008140-X", "com.example.Puzzle"}
	if !slices.Equal(device.Args, want) || !slices.Contains(device.Env, "DEVICECTL_CHILD_OS_ACTIVITY_DT_MODE=YES") {
		t.Errorf("device launch: args %v, mirror variable set: %v", device.Args, slices.Contains(device.Env, "DEVICECTL_CHILD_OS_ACTIVITY_DT_MODE=YES"))
	}
	mac := macCommand(context.Background(), BuildSettings{Executable: "/dd/Debug/ClipNinja.app/Contents/MacOS/ClipNinja"})
	if !slices.Equal(mac.Args, []string{"/dd/Debug/ClipNinja.app/Contents/MacOS/ClipNinja"}) || !slices.Contains(mac.Env, "OS_ACTIVITY_DT_MODE=YES") {
		t.Errorf("mac launch: args %v, mirror variable set: %v", mac.Args, slices.Contains(mac.Env, "OS_ACTIVITY_DT_MODE=YES"))
	}
}

func TestCleanMirroredLine(t *testing.T) {
	tests := []struct {
		line     string
		want     string
		mirrored bool
	}{
		{"2026-09-28 10:33:35.230501+0200 Puzzle[9836:34476303] [Session] Token expired", "10:33:35.230 [Session] Token expired", true},
		{"2026-09-28 10:33:35.230501+0200 My App[9836:34476303] [Application] Deactivation reason added", "10:33:35.230 [Application] Deactivation reason added", true},
		{"hello from stdout", "hello from stdout", false},
		{"2026-09-25 10:23:23.361 Df Puzzle[19921:134cebb] a log stream line", "2026-09-25 10:23:23.361 Df Puzzle[19921:134cebb] a log stream line", false},
	}
	for _, tt := range tests {
		got, ok := CleanMirroredLine(tt.line)
		if got != tt.want || ok != tt.mirrored {
			t.Errorf("CleanMirroredLine(%q) = %q, %v; want %q, %v", tt.line, got, ok, tt.want, tt.mirrored)
		}
	}
}

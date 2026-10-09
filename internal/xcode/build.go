// Package xcode wraps Apple CLI tools for building and running Xcode projects.
package xcode

import (
	"context"
	"os/exec"
	"syscall"
)

// Action is the xcodebuild action to run.
type Action string

const (
	ActionBuild Action = "build"
	ActionClean Action = "clean"
	ActionTest  Action = "test"
)

// Build runs `xcodebuild <action>` for the scheme on the simulator and streams its output.
// Cancel ctx to stop it: SIGTERM makes xcodebuild print "** BUILD INTERRUPTED **" and
// exit, whereas SIGINT makes it dump a backtrace full of "error:" lines.
func Build(ctx context.Context, action Action, project Project, scheme Scheme, simulator Simulator) (*Stream, error) {
	args := append([]string{string(action)}, xcodebuildArgs(project, scheme, simulator)...)
	return start(exec.CommandContext(ctx, "xcodebuild", args...), syscall.SIGTERM)
}

// ResolvePackages runs `xcodebuild -resolvePackageDependencies` for the container and
// streams its output. Cancel ctx to stop it.
func ResolvePackages(ctx context.Context, project Project) (*Stream, error) {
	args := append([]string{"-resolvePackageDependencies"}, project.args()...)
	return start(exec.CommandContext(ctx, "xcodebuild", args...), syscall.SIGTERM)
}

// xcodebuildArgs selects the container, scheme and simulator. Every xcodebuild call that
// resolves build settings needs all three, or it falls back to device paths and defaults.
func xcodebuildArgs(project Project, scheme Scheme, simulator Simulator) []string {
	return append(project.args(),
		"-scheme", scheme.Name,
		"-destination", simulator.Destination(),
	)
}

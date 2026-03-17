package xcode

import (
	"fmt"
	"os/exec"
)

type BuildStatus string

const (
	StatusPending   BuildStatus = "Pending"
	StatusRunning   BuildStatus = "Running"
	StatusSucceeded BuildStatus = "Succeeded"
	StatusFailed    BuildStatus = "Failed"
)

func Build(xcodeproj Project, scheme Scheme, simulator Simulator) (BuildStatus, error) {
	output, err := exec.Command(
		"xcodebuild",
		"build",
		"-project",
		xcodeproj.Name,
		"-scheme",
		scheme.Name,
		"-destination",
		fmt.Sprintf("platform=iOS Simulator,id=%s", simulator.UDID),
	).CombinedOutput()

	if err != nil {
		return StatusFailed, fmt.Errorf("build error: %w: %s\n", err, output)
	}

	return StatusSucceeded, nil
}

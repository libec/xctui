package xcode

import (
	"bufio"
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

func Build(xcodeproj Project, scheme Scheme, simulator Simulator) (<-chan string, error) {
	buildChannel := make(chan string)
	command := exec.Command(
		"xcodebuild",
		"build",
		"-project",
		xcodeproj.Name,
		"-scheme",
		scheme.Name,
		"-destination",
		fmt.Sprintf("platform=iOS Simulator,id=%s", simulator.UDID),
	)

	reader, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}

	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			buildChannel <- scanner.Text()
		}

		if err := scanner.Err(); err != nil {
			buildChannel <- fmt.Sprintf("scanner error: %v", err)
		}
		if err := command.Wait(); err != nil {
			buildChannel <- fmt.Sprintf("command wait error: %v", err)
		}
		close(buildChannel)
	}()

	return buildChannel, nil
}

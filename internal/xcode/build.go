package xcode

import (
	"bufio"
	"fmt"
	"os/exec"
	"sync"
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

	// output reader
	outputReader, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// error reader
	errReader, err := command.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}

	go func() {

		var wg sync.WaitGroup

		// output goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(outputReader)
			for scanner.Scan() {
				buildChannel <- scanner.Text()
			}

			if err := scanner.Err(); err != nil {
				buildChannel <- fmt.Sprintf("output scanner error: %v", err)
			}
		}()

		// error goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(errReader)
			for scanner.Scan() {
				buildChannel <- fmt.Sprintf("❌ %s", scanner.Text())
			}
			if err := scanner.Err(); err != nil {
				buildChannel <- fmt.Sprintf("error scanner error: %v", err)
			}
		}()

		// wait for output and error to finish
		wg.Wait()
		if err := command.Wait(); err != nil {
			buildChannel <- fmt.Sprintf("command wait error: %v", err)
		}
		close(buildChannel)
	}()

	return buildChannel, nil
}

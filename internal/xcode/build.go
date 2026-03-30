// Package xcode wraps Apple CLI tools for building and running Xcode projects.
package xcode

import (
	"bufio"
	"fmt"
	"io"
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
		go scanPipe(outputReader, buildChannel, "", &wg)

		// error goroutine
		wg.Add(1)
		go scanPipe(errReader, buildChannel, "❌ ", &wg)

		// wait for output and error to finish
		wg.Wait()
		if err := command.Wait(); err != nil {
			buildChannel <- fmt.Sprintf("command wait error: %v", err)
		}
		close(buildChannel)
	}()

	return buildChannel, nil
}

func scanPipe(r io.Reader, ch chan<- string, prefix string, wg *sync.WaitGroup) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		ch <- fmt.Sprintf("%s%s", prefix, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		ch <- fmt.Sprintf("scanner error: %v", err)
	}
}

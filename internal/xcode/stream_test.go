package xcode

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestScanLines(t *testing.T) {
	tests := []struct {
		name        string
		inputString string
		want        []string
	}{
		{
			"lines",
			"line one\nline two\n",
			[]string{"line one", "line two"},
		},
		{
			// simctl's console pty ends lines with \r\n
			"carriage returns",
			"line one\r\nline two\r\n",
			[]string{"line one", "line two"},
		},
		{
			"line longer than bufio's 64 KB default",
			strings.Repeat("x", 100_000) + "\nafter\n",
			[]string{strings.Repeat("x", 100_000), "after"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan string, 10)
			scanLines(strings.NewReader(tt.inputString), ch)

			var got []string
			for line := range ch {
				got = append(got, line)
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOutputKeepsStderr(t *testing.T) {
	_, err := output(exec.Command("sh", "-c", "echo the reason >&2; exit 66"))
	if err == nil || !strings.Contains(err.Error(), "the reason") || !strings.Contains(err.Error(), "exit status 66") {
		t.Errorf("got %v, want the exit status and the stderr text", err)
	}
}

func TestStreamExitCode(t *testing.T) {
	stream, err := start(exec.CommandContext(context.Background(), "sh", "-c", "echo out; echo err >&2; exit 3"), os.Interrupt)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for line := range stream.Lines {
		got = append(got, line)
	}
	if want := []string{"out", "err"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	var exitErr *exec.ExitError
	if err := stream.Wait(); !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Errorf("got %v, want exit status 3", err)
	}
}

func TestStreamCancelSignals(t *testing.T) {
	tests := []struct {
		name string
		stop os.Signal
		trap string
	}{
		{"SIGINT for simctl", os.Interrupt, "INT"},
		{"SIGTERM for xcodebuild", syscall.SIGTERM, "TERM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			script := `trap 'echo stopped; exit 130' ` + tt.trap + `; echo ready; while :; do sleep 0.1; done`
			stream, err := start(exec.CommandContext(ctx, "sh", "-c", script), tt.stop)
			if err != nil {
				t.Fatal(err)
			}

			if line := <-stream.Lines; line != "ready" {
				t.Fatalf("got %q, want ready", line)
			}
			began := time.Now()
			cancel()

			var got []string
			for line := range stream.Lines {
				got = append(got, line)
			}
			// The signal ran the trap; a SIGKILL after the grace period would not have.
			if want := []string{"stopped"}; !slices.Equal(got, want) {
				t.Errorf("got %v, want %v", got, want)
			}
			if err := stream.Wait(); err == nil {
				t.Error("got nil, want the exit error")
			}
			if elapsed := time.Since(began); elapsed >= stopGracePeriod {
				t.Errorf("took %v, want less than the %v grace period", elapsed, stopGracePeriod)
			}
		})
	}
}

package xcode

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// Stream is a running command. Its stdout and stderr arrive line by line on Lines.
type Stream struct {
	Lines <-chan string // closed once the command's output ends
	done  chan struct{}
	err   error
}

// Wait blocks until the command has exited and returns nil for exit code 0.
// Lines must be drained, or the command blocks writing its output and never exits.
// Safe to call from several goroutines.
func (s *Stream) Wait() error {
	<-s.done
	return s.err
}

// stopGracePeriod is how long a cancelled command gets to exit after SIGINT before it is killed.
const stopGracePeriod = 5 * time.Second

// start runs cmd and streams its output. Cancelling the command's context sends
// stop (each tool wants a different signal; see Build and Launch), and SIGKILL
// if the process is still there after stopGracePeriod.
func start(cmd *exec.Cmd, stop os.Signal) (*Stream, error) {
	cmd.Cancel = func() error { return cmd.Process.Signal(stop) }
	cmd.WaitDelay = stopGracePeriod

	// One writer for both keeps stdout and stderr lines in the order they were written.
	// It is not an *os.File, so exec copies into it and WaitDelay can cut the copy short
	// if a leftover child process keeps the output open.
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Buffered, so output keeps flowing while the reader is busy and it can take lines in batches.
	lines := make(chan string, 256)
	stream := &Stream{Lines: lines, done: make(chan struct{})}
	go scanLines(pr, lines)
	go func() {
		stream.err = cmd.Wait()
		pw.Close() // scanLines reaches EOF
		close(stream.done)
	}()

	return stream, nil
}

// output is cmd.Output with stderr folded into the error, which Output alone
// drops: "xcodebuild: exit status 66" says nothing, the stderr line does.
func output(cmd *exec.Cmd) ([]byte, error) {
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil, fmt.Errorf("%s: %w\n%s", cmd.Path, err, bytes.TrimSpace(exitErr.Stderr))
	}
	return out, err
}

// maxLineSize fits the longest swiftc invocation xcodebuild echoes; bufio's 64 KB default does not always.
const maxLineSize = 4 << 20

// scanLines sends each line of r to ch and closes ch at EOF.
func scanLines(r io.Reader, ch chan<- string) {
	defer close(ch)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(nil, maxLineSize)

	for scanner.Scan() {
		ch <- scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		ch <- fmt.Sprintf("scanner error: %v", err)
		// Keep reading, or the command blocks writing and never exits.
		io.Copy(io.Discard, r)
	}
}

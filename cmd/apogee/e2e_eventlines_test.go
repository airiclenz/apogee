package main

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
)

// pipeGate is the label the closed-pipe run's only scripted reply waits on, so the test can close
// the reader while the run is still going and be certain the lines that meet the broken pipe are
// written AFTER the close rather than racing it into the pipe's buffer.
const pipeGate = "answer"

// TestE2EEventLinesSurviveAClosedPipe is the SIGPIPE claim end to end, in the only place it can be
// made: a real process with a real pipe on a real file descriptor 1. In the test binary the signal
// is already handled, so nothing in-process can prove the disarm — the Go runtime re-raises SIGPIPE
// with the default disposition only for writes to fd 1 and 2 of a program that has not asked
// otherwise, which is precisely what `apogee headless --format json` asks for (sigpipe_unix.go).
//
// What the run must do when its consumer walks away: keep going, keep its own exit code, and say
// once on stderr that the stream stopped (ADR 0075 decision 9). What it must NOT do is die of a
// signal with its record unsaved because something downstream stopped reading.
func TestE2EEventLinesSurviveAClosedPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGPIPE is a POSIX signal; on Windows a broken pipe is already an ordinary write error")
	}
	if e2eBinary == "" {
		t.Skipf("the binary under test was not built: %v", e2eBuildErr)
	}
	assertNoAmbientApogeeConfig(t)

	// One held reply: the run opens its stream, then waits, so the close below lands mid-run.
	stub := stubllm.New(t, stubllm.Script{
		Model: "stub-model",
		Turns: []stubllm.Turn{{Text: "the answer", Await: pipeGate}},
	})
	home := e2eHome(t, stub)
	// Naming off: a second request would be a second thing to script, and this case is about the
	// stream's writes, not about how many the run makes.
	appendHomeConfig(t, home, "auto-title: false\n")

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("make the stdout pipe: %v", err)
	}
	defer reader.Close()

	cmd := exec.Command(e2eBinary,
		"--config", home, "--workspace", e2eWorkspace(t),
		"headless", "--format", "json", "list the files")
	cmd.Env = ptyEnv()
	cmd.Stdout = writer
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the headless run: %v", err)
	}
	// The child holds the only writer from here, so the read side sees EOF if it ever exits.
	writer.Close()

	// The opening frame is written before the run makes its first request, so it is on the pipe
	// while the reply is still held.
	line, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil {
		t.Fatalf("read the stream's first line: %v\nstderr:\n%s", err, errBuf.String())
	}
	if !strings.Contains(line, `"run_started"`) {
		t.Fatalf("the stream's first line is not the opening frame: %s", line)
	}

	// The consumer walks away. Every line the run writes from now on meets a broken pipe.
	if err := reader.Close(); err != nil {
		t.Fatalf("close the read end: %v", err)
	}
	stub.Release(pipeGate)

	err = cmd.Wait()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			// -1 is what a process killed by a signal reports, which is exactly the death this
			// item exists to prevent.
			t.Fatalf("the run exited with code %d (-1 means a signal killed it: %v); a closed "+
				"reader must break the stream, not the run\nstderr:\n%s",
				exit.ExitCode(), exit.ProcessState, errBuf.String())
		}
		t.Fatalf("wait for the headless run: %v\nstderr:\n%s", err, errBuf.String())
	}

	const stopped = "event lines stopped"
	if n := strings.Count(errBuf.String(), stopped); n != 1 {
		t.Errorf("the stream reported its own failure %d times, want exactly once:\n%s",
			n, errBuf.String())
	}
}

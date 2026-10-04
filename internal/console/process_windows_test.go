//go:build windows

package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
)

// windowsProcessTestTimeout bounds every wait in this file: generous, because a loaded CI host can
// take seconds to start cmd.exe inside a pseudoconsole, and finite, so a wedged Console fails the
// test instead of hanging the suite.
const windowsProcessTestTimeout = 30 * time.Second

// TestProcessRunsCommandsTypedIntoTheConsoleOnWindows drives an interactive cmd.exe the way a
// model does: type a line, read what it printed — with the pseudoconsole's VT painting stripped.
func TestProcessRunsCommandsTypedIntoTheConsoleOnWindows(t *testing.T) {
	t.Parallel()
	process := startWindowsProcess(t, cmdSpec("cmd /q"))

	// %OS% expands only when cmd.exe runs the line, so the echo of the typed text cannot satisfy
	// the wait on its own.
	if _, err := process.Write([]byte("echo apogee-%OS%\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	output := readWindowsUntil(t, process, "apogee-Windows_NT")

	if strings.ContainsRune(output, '\x1b') {
		t.Errorf("output still carries escape sequences: %q", output)
	}
}

// TestProcessRecordsTheExitCodeOnWindows pins the exit code a Console reports once its command has
// ended on its own.
func TestProcessRecordsTheExitCodeOnWindows(t *testing.T) {
	t.Parallel()
	process := startWindowsProcess(t, cmdSpec("cmd /c exit 3"))

	awaitWindows(t, func() bool { return !process.Alive() }, "the process to exit")

	if code := process.ExitCode(); code != 3 {
		t.Errorf("ExitCode() = %d, want 3", code)
	}
}

// TestProcessCloseIsBoundedOnWindows closes a Console whose interactive cmd.exe would run forever:
// Close must release the pseudoconsole, end the process and return well inside closeJoinTimeout,
// with the exit recorded by the time it does.
func TestProcessCloseIsBoundedOnWindows(t *testing.T) {
	t.Parallel()
	process := startWindowsProcess(t, cmdSpec("cmd /q"))

	started := time.Now()
	if err := process.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	elapsed := time.Since(started)

	if elapsed >= closeJoinTimeout {
		t.Errorf("Close took %v, want well inside %v", elapsed, closeJoinTimeout)
	}
	if process.Alive() {
		t.Error("Alive() = true after Close")
	}
	if err := process.Close(); err != nil {
		t.Errorf("second Close: %v, want the first call's nil", err)
	}
}

// TestProcessClosesTheOutputOnlyAfterTheFinalFrameOnWindows pins the reap-time teardown order at
// the seam: once the command has exited, the reaper releases the pseudoconsole but keeps its output
// open until the reader has drained conhost's final flush to the end of output, and only then
// closes it. Closing the output with the release would cancel the read carrying that last frame.
func TestProcessClosesTheOutputOnlyAfterTheFinalFrameOnWindows(t *testing.T) {
	t.Parallel()
	const frame = "apogee-final-frame"
	fake := newFinalFrameConsole(frame)
	process := startFakeProcess(t, fake)

	close(fake.exited)
	awaitClosed(t, fake.released, "the reaper to release the pseudoconsole")
	// The flush is still in flight: an output closed now would cancel the read carrying it.
	select {
	case <-fake.outputClosed:
		t.Fatal("the output was closed before the reader reached the end of output")
	case <-time.After(finalFrameFlushDelay):
	}
	close(fake.flushed)
	awaitClosed(t, process.reaped, "the reaper to finish")

	if got, want := fake.log(), []string{"release", "eof", "close-output"}; !slices.Equal(got, want) {
		t.Errorf("teardown ran %q, want %q", got, want)
	}
	if output, _ := process.Read(0); !strings.Contains(output, frame) {
		t.Errorf("the ring holds %q, want it to contain the final frame %q", output, frame)
	}
}

// TestProcessKeepsTheFinalFrameOfAShortCommandOnWindows is the real-pseudoconsole regression
// guard: a command that prints and exits at once always leaves what it printed in the ring.
func TestProcessKeepsTheFinalFrameOfAShortCommandOnWindows(t *testing.T) {
	t.Parallel()
	const runs = 50
	for i := range runs {
		marker := fmt.Sprintf("apogee-final-frame-%d", i)
		process := startWindowsProcess(t, cmdSpec("cmd /c echo "+marker))

		awaitClosed(t, process.reaped, "the process to be reaped")
		awaitClosed(t, process.readerDone, "the reader to reach the end of output")

		if output, _ := process.Read(0); !strings.Contains(output, marker) {
			t.Fatalf("run %d: the ring holds %q, want it to contain %q", i, output, marker)
		}
	}
}

// TestProcessStartRunsAConfinedSpecOnWindows runs a confined Console under the real Confiner's
// restricted token: a command inside its box runs and writes there, and a write outside the box
// is denied.
func TestProcessStartRunsAConfinedSpecOnWindows(t *testing.T) {
	// Not parallel: the real confiner journals its labels under the real apogee home.
	confiner := newRealTestConfiner(t)
	box, outside := t.TempDir(), t.TempDir()
	confined := func(commandLine string) Spec {
		spec := cmdSpec(commandLine)
		spec.Dir = box
		spec.Confined = true
		spec.Prepare = func(cmd *exec.Cmd) error {
			return confiner.Confine(context.Background(), domain.ConfinementBox{WorkspaceRoot: box}, cmd)
		}
		return spec
	}

	t.Run("prints and writes inside the box", func(t *testing.T) {
		const marker = "apogee-console-confined-marker"
		inside := filepath.Join(box, "inside.txt")
		process := startWindowsProcess(t, confined("cmd /q"))

		if _, err := process.Write([]byte("echo " + marker + "& echo inside>" + inside + "\r")); err != nil {
			t.Fatalf("Write: %v", err)
		}
		readWindowsUntil(t, process, marker)

		awaitWindows(t, func() bool { _, err := os.Stat(inside); return err == nil }, "the write inside the box to land")
	})

	t.Run("is denied a write outside the box", func(t *testing.T) {
		denied := filepath.Join(outside, "denied.txt")
		process := startWindowsProcess(t, confined("cmd /q"))

		if _, err := process.Write([]byte("echo escaped>" + denied + "\r")); err != nil {
			t.Fatalf("Write: %v", err)
		}
		readWindowsUntil(t, process, "Access is denied")

		if _, err := os.Stat(denied); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("stat of the denied write = %v, want it not to exist", err)
		}
	})
}

// TestProcessStartRefusesAConfinedSpecWithoutATokenOnWindows pins the fail-closed backstop: a
// spec reported confined whose Prepare put no restricted token on the command is refused with
// ErrConfinementUnavailable rather than run unfenced.
func TestProcessStartRefusesAConfinedSpecWithoutATokenOnWindows(t *testing.T) {
	t.Parallel()
	spec := cmdSpec("cmd /q")
	spec.Confined = true
	spec.Prepare = func(*exec.Cmd) error { return nil }

	process, err := Start(spec)

	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Fatalf("Start(confined, no token) = %v, want ErrConfinementUnavailable", err)
	}
	if process != nil {
		t.Error("Start returned a Process for a refused spec")
	}
}

// TestProcessPrepareSeesTheRawCommandLineOnWindows pins what the Prepare hook is handed: the
// command with its directory, environment and the caller's verbatim command line already set —
// the line the launcher then runs.
func TestProcessPrepareSeesTheRawCommandLineOnWindows(t *testing.T) {
	t.Parallel()
	errStop := errors.New("stop before launching")
	dir := t.TempDir()
	spec := cmdSpec(`cmd /c echo "quoted"`)
	spec.Dir = dir
	spec.Env = []string{"APOGEE_CONSOLE_TEST=1"}
	var seen *exec.Cmd
	spec.Prepare = func(cmd *exec.Cmd) error {
		seen = cmd
		return errStop
	}

	if _, err := Start(spec); !errors.Is(err, errStop) {
		t.Fatalf("Start = %v, want Prepare's own error", err)
	}

	if seen.SysProcAttr == nil || seen.SysProcAttr.CmdLine != spec.CommandLine {
		t.Errorf("Prepare saw command line %+v, want %q", seen.SysProcAttr, spec.CommandLine)
	}
	if seen.Dir != dir {
		t.Errorf("Prepare saw Dir %q, want %q", seen.Dir, dir)
	}
	if len(seen.Env) != 1 || seen.Env[0] != spec.Env[0] {
		t.Errorf("Prepare saw Env %q, want %q", seen.Env, spec.Env)
	}
}

// cmdSpec returns a Spec that runs commandLine through cmd.exe, verbatim, the way console_open
// hands one over.
func cmdSpec(commandLine string) Spec {
	return Spec{Argv: strings.Fields(commandLine), CommandLine: commandLine}
}

// startWindowsProcess starts spec, closing it when the test ends. It skips on a host without a
// pseudoconsole and fails on any other launch error.
func startWindowsProcess(t *testing.T, spec Spec) *Process {
	t.Helper()
	process, err := Start(spec)
	if errors.Is(err, platform.ErrPseudoConsoleUnavailable) {
		t.Skipf("no pseudoconsole on this host: %v", err)
	}
	if err != nil {
		t.Fatalf("Start(%q): %v", spec.CommandLine, err)
	}
	t.Cleanup(func() { _ = process.Close() })
	return process
}

// readWindowsUntil accumulates a Process's output until it carries want, and returns everything
// read. It fails the test rather than returning short.
func readWindowsUntil(t *testing.T, process *Process, want string) string {
	t.Helper()
	var seen strings.Builder
	deadline := time.Now().Add(windowsProcessTestTimeout)
	for time.Now().Before(deadline) {
		output, _ := process.Read(200 * time.Millisecond)
		seen.WriteString(output)
		if strings.Contains(seen.String(), want) {
			return seen.String()
		}
	}
	t.Fatalf("waited %v for %q; read %q", windowsProcessTestTimeout, want, seen.String())
	return ""
}

// awaitWindows polls condition until it holds, failing the test with what it was waiting for.
func awaitWindows(t *testing.T, condition func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(windowsProcessTestTimeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waited %v for %s", windowsProcessTestTimeout, what)
}

// finalFrameFlushDelay is how long the seam test holds conhost's final flush back after the
// release: far longer than a reaper that closes the output with the release takes to do it.
const finalFrameFlushDelay = 100 * time.Millisecond

// finalFrameConsole stands in for a pseudoconsole whose command has exited but whose last frame is
// still being flushed: Read hands the frame over only once the test lets the flush through, and a
// read cancelled by CloseOutput before then loses it, as a real cancelled pipe read does.
type finalFrameConsole struct {
	frame string

	exited       chan struct{} // closed by the test: the command has ended
	released     chan struct{} // closed by Release
	flushed      chan struct{} // closed by the test: conhost's final flush has arrived
	outputClosed chan struct{} // closed by CloseOutput

	releaseOnce, outputOnce sync.Once
	sentFrame               bool // touched only by the single reader goroutine

	mu    sync.Mutex
	steps []string
}

func newFinalFrameConsole(frame string) *finalFrameConsole {
	return &finalFrameConsole{
		frame:        frame,
		exited:       make(chan struct{}),
		released:     make(chan struct{}),
		flushed:      make(chan struct{}),
		outputClosed: make(chan struct{}),
	}
}

// Read returns the final frame once it has been flushed, then the end of output; a read the output
// is closed under fails instead.
func (c *finalFrameConsole) Read(p []byte) (int, error) {
	if c.sentFrame {
		c.record("eof")
		return 0, io.EOF
	}
	select {
	case <-c.flushed:
	case <-c.outputClosed:
		return 0, os.ErrClosed
	}
	select {
	case <-c.outputClosed:
		return 0, os.ErrClosed
	default:
	}
	c.sentFrame = true
	return copy(p, c.frame), nil
}

func (c *finalFrameConsole) Write(p []byte) (int, error) { return len(p), nil }

func (c *finalFrameConsole) Wait() (int, error) {
	<-c.exited
	return 0, nil
}

func (c *finalFrameConsole) Kill() {}

func (c *finalFrameConsole) Release() error {
	c.releaseOnce.Do(func() {
		c.record("release")
		close(c.released)
	})
	return nil
}

func (c *finalFrameConsole) CloseOutput() error {
	c.outputOnce.Do(func() {
		c.record("close-output")
		close(c.outputClosed)
	})
	return nil
}

// record appends one teardown step to the log.
func (c *finalFrameConsole) record(step string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.steps = append(c.steps, step)
}

// log returns the teardown steps in the order they ran.
func (c *finalFrameConsole) log() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.steps...)
}

// startFakeProcess wires console into a Process exactly as Start wires a real pseudoconsole —
// the reader and the reaper running — and closes it when the test ends.
func startFakeProcess(t *testing.T, console pseudoConsole) *Process {
	t.Helper()
	process := newProcess()
	process.console = console
	go process.collectOutput(console, process.outputSink(false))
	go process.reap()
	t.Cleanup(func() { _ = process.Close() })
	return process
}

// awaitClosed waits for done to close, failing the test with what it was waiting for.
func awaitClosed(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(windowsProcessTestTimeout):
		t.Fatalf("waited %v for %s", windowsProcessTestTimeout, what)
	}
}

//go:build !windows

package console

import (
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// testTimeout bounds every wait in this file: long enough that a loaded CI host does not flake,
// short enough that a genuinely wedged Console fails the test instead of the suite.
const testTimeout = 10 * time.Second

// TestProcessRunsCommandsTypedIntoTheTerminal is the round trip the whole package exists for:
// input written to the terminal reaches the shell, and what it prints comes back out of Read.
func TestProcessRunsCommandsTypedIntoTheTerminal(t *testing.T) {
	t.Parallel()

	process := startShell(t)
	writeInput(t, process, "echo apogee-console-ok\n")

	if output := readUntil(t, process, "apogee-console-ok"); !strings.Contains(output, "apogee-console-ok") {
		t.Fatalf("output %q does not carry the command's result", output)
	}
	if !process.Alive() {
		t.Fatal("shell exited after one command")
	}
	if process.DenialStopped() {
		t.Fatal("an unconfined Console reported a denial stop")
	}
}

// TestProcessRecordsTheExitCode covers the waiter goroutine: a shell told to exit stops being
// alive and reports the code it chose.
func TestProcessRecordsTheExitCode(t *testing.T) {
	t.Parallel()

	process := startShell(t)
	writeInput(t, process, "exit 3\n")

	waitFor(t, func() bool { return !process.Alive() }, "shell to exit")
	if code := process.ExitCode(); code != 3 {
		t.Fatalf("ExitCode() = %d, want 3", code)
	}
}

// TestProcessKillStopsALongRunningCommandPromptly is the escape hatch: a Console parked on
// something that would never finish dies when it is killed, without waiting it out.
func TestProcessKillStopsALongRunningCommandPromptly(t *testing.T) {
	t.Parallel()

	process := startProcess(t, Spec{Argv: []string{"sleep", "60"}})

	start := time.Now()
	process.Kill()
	waitFor(t, func() bool { return !process.Alive() }, "the killed process to be reaped")
	if elapsed := time.Since(start); elapsed > testTimeout {
		t.Fatalf("kill took %v", elapsed)
	}
	if code := process.ExitCode(); code != -1 {
		t.Fatalf("ExitCode() = %d, want -1 for a signalled process", code)
	}
}

// TestProcessConfinedDenialStopsTheConsole covers the kill-on-denial watch (ADR 0056 §2): a
// CONFINED Console whose output carries an OS denial signature is stopped where it was denied,
// instead of running on against a half-done workspace. The signature is fed through the program's
// own output — the watch reads the stream, so no real fence is needed to exercise it.
func TestProcessConfinedDenialStopsTheConsole(t *testing.T) {
	t.Parallel()

	process := startProcess(t, Spec{
		Argv:     []string{"sh", "-c", "echo mkdir: Permission denied; sleep 60"},
		Confined: true,
	})

	waitFor(t, func() bool { return !process.Alive() }, "the denied Console to be stopped")
	if !process.DenialStopped() {
		t.Fatal("DenialStopped() = false after the denial signature was printed")
	}
	if output := readUntil(t, process, "Permission denied"); !strings.Contains(output, "Permission denied") {
		t.Fatalf("output %q lost the denial that killed the Console", output)
	}
}

// TestProcessCloseKillsBackgroundedChildren is the §2.4 group teardown: a child the command
// backgrounded is inside the session's process group, so closing the Console reaps it too rather
// than leaving it running unsupervised.
//
// The command is a `sh -c` script rather than typed into an interactive shell on purpose. An
// INTERACTIVE shell turns job control on, which puts every background job in a process group of
// its own — the documented "descendant that deliberately left the group" residual, which no
// negative-pid kill reaches. What is under test here is the group teardown itself, so the child
// has to still be in the group.
func TestProcessCloseKillsBackgroundedChildren(t *testing.T) {
	t.Parallel()

	process := startProcess(t, Spec{
		Argv: []string{"sh", "-c", "sleep 60 & echo backgrounded=$!; wait"},
	})

	output := readUntil(t, process, "backgrounded=")
	match := regexp.MustCompile(`backgrounded=(\d+)`).FindStringSubmatch(output)
	if match == nil {
		t.Fatalf("output %q never reported the backgrounded pid", output)
	}
	backgrounded, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("parse backgrounded pid %q: %v", match[1], err)
	}
	if !processExists(backgrounded) {
		t.Fatalf("backgrounded pid %d was already gone before the close", backgrounded)
	}

	if err := process.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	waitFor(t, func() bool { return !processExists(backgrounded) },
		"the backgrounded child to be reaped with its group")
}

// TestProcessCloseIsIdempotent pins that a Console can be closed twice — the registry above this
// package closes on both the owner's exit and the engine's.
func TestProcessCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	process := startProcess(t, Spec{Argv: []string{"sleep", "60"}})
	if err := process.Close(); err != nil {
		t.Fatalf("first Close(): %v", err)
	}
	if err := process.Close(); err != nil {
		t.Fatalf("second Close(): %v", err)
	}
	if process.Alive() {
		t.Fatal("process still alive after Close")
	}
}

// TestProcessStartRefusesWhatItCannotRun covers the two ways Start declines before a process
// exists: nothing to run, and a caller's Prepare hook — the seam confinement and refusals arrive
// through — saying no.
func TestProcessStartRefusesWhatItCannotRun(t *testing.T) {
	t.Parallel()

	if _, err := Start(Spec{}); err == nil {
		t.Error("Start with no argv returned no error")
	}

	refused := errors.New("refused by the caller")
	_, err := Start(Spec{
		Argv:    []string{"sh", "-c", "echo should not run"},
		Prepare: func(*exec.Cmd) error { return refused },
	})
	if !errors.Is(err, refused) {
		t.Errorf("Start with a refusing Prepare = %v, want %v", err, refused)
	}
}

// TestProcessPrepareSeesTheAssembledCommand pins what a caller may rely on inside Prepare: the
// working directory and environment are already set, so confinement can read them.
func TestProcessPrepareSeesTheAssembledCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var seenDir string
	var seenEnv []string
	startProcess(t, Spec{
		Argv: []string{"sh", "-c", "exit 0"},
		Dir:  dir,
		Env:  []string{"TERM=dumb"},
		Prepare: func(cmd *exec.Cmd) error {
			seenDir, seenEnv = cmd.Dir, cmd.Env
			return nil
		},
	})

	if seenDir != dir {
		t.Errorf("Prepare saw Dir %q, want %q", seenDir, dir)
	}
	if len(seenEnv) != 1 || seenEnv[0] != "TERM=dumb" {
		t.Errorf("Prepare saw Env %v, want [TERM=dumb]", seenEnv)
	}
}

// startShell starts an interactive shell Console and closes it when the test ends.
func startShell(t *testing.T) *Process {
	t.Helper()
	return startProcess(t, Spec{Argv: []string{"sh"}})
}

// startProcess starts spec and registers the cleanup that stops it.
func startProcess(t *testing.T, spec Spec) *Process {
	t.Helper()
	process, err := Start(spec)
	if err != nil {
		t.Fatalf("Start(%v): %v", spec.Argv, err)
	}
	t.Cleanup(func() { _ = process.Close() })
	return process
}

// writeInput types input into the Console's terminal.
func writeInput(t *testing.T, process *Process, input string) {
	t.Helper()
	if _, err := process.Write([]byte(input)); err != nil {
		t.Fatalf("write %q: %v", input, err)
	}
}

// readUntil accumulates the Console's output until it carries want, and returns everything read.
// It fails the test rather than returning short, so a caller can assert on the text it got.
func readUntil(t *testing.T, process *Process, want string) string {
	t.Helper()
	var seen strings.Builder
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		output, dropped := process.Read(200 * time.Millisecond)
		if dropped != 0 {
			t.Fatalf("the ring dropped %d bytes of a short test's output", dropped)
		}
		seen.WriteString(output)
		if strings.Contains(seen.String(), want) {
			return seen.String()
		}
	}
	t.Fatalf("waited %v for %q; read %q", testTimeout, want, seen.String())
	return ""
}

// waitFor polls condition until it holds, failing the test with what it was waiting for.
func waitFor(t *testing.T, condition func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waited %v for %s", testTimeout, what)
}

// processExists reports whether pid is still a live process. Signal 0 performs the permission and
// existence checks without delivering anything.
func processExists(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

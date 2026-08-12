package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestRunSubprocessNilConfinerFailsClosed pins the §2.2 posture on the one handle shape the
// confine guard used to wave through: a Confinement installed with no Confiner behind it. That
// is broken wiring, not permission to run free — it must surface as ErrConfinementUnavailable,
// which dispatch turns into the truthful demote to Approval, and the command must never run.
func TestRunSubprocessNilConfinerFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell canary; the guard it pins is platform-independent")
	}
	t.Parallel()

	// The canary is a file the command would create: its absence is the proof that nothing
	// ran, which an error alone cannot give.
	canary := filepath.Join(t.TempDir(), "ran")
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: nil,
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})

	_, err := runSubprocess(ctx, subprocessSpec{
		argv: []string{"/bin/sh", "-c", fmt.Sprintf("touch %s", strconv.Quote(canary))},
	})
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Fatalf("runSubprocess err = %v, want ErrConfinementUnavailable (a handle with no Confiner must fail closed)", err)
	}
	if _, statErr := os.Stat(canary); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("stat %s = %v, want not-exist — the command must not have run unconfined", canary, statErr)
	}
}

// TestRunSubprocessReapsTheProcessGroupOnACleanExit pins the half of the §2.4 teardown that
// cmd.Cancel cannot reach: a command that BACKGROUNDS something and then exits normally. Nothing
// is cancelled here, so before the post-Wait reap the descendant simply outlived the call — a
// persistence primitive the tool still reported as a success. The one-shot contract (ADR 0008)
// says the tree goes when the call does.
func TestRunSubprocessReapsTheProcessGroupOnACleanExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process groups; the Windows job-object twin lives in terminal_windows_test.go")
	}
	t.Parallel()

	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	// The shell backgrounds a long sleep, records ITS pid, and exits — no `wait`, so the run
	// completes cleanly in milliseconds. The redirection keeps the sleep off the captured
	// pipes, so Wait returns as soon as the shell does: this test is about the reap, not the
	// drain (TestRunSubprocessReportsAWedgedDrain owns that).
	script := fmt.Sprintf(`sleep 300 >/dev/null 2>&1 & echo $! > %s`, strconv.Quote(pidFile))

	res, err := runSubprocess(context.Background(), subprocessSpec{argv: []string{"/bin/sh", "-c", script}})
	if err != nil {
		t.Fatalf("runSubprocess err = %v, want nil", err)
	}
	if res.exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0 — the shell itself exited cleanly (output %q)", res.exitCode, res.combinedOutput)
	}

	pid := waitForPIDFile(t, pidFile)
	// Whatever the assertion finds, the sleep must not leak into the machine.
	t.Cleanup(func() { killPID(pid) })

	if pidAlive(pid, 3*time.Second) {
		t.Errorf("backgrounded PID %d survived a CLEAN exit; the process group was reaped only on cancellation", pid)
	}
}

// TestRunSubprocessReportsAWedgedDrain pins the second half of the same finding: when something
// the command left running still holds the output pipe, exec cuts the drain off at
// processWaitDelay and returns exec.ErrWaitDelay — which is not an *exec.ExitError, so the exit
// code falls through to the leader's own status. The leader exited 0, so the call used to render
// as a green tick with a silently truncated tail.
func TestRunSubprocessReportsAWedgedDrain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell; the exit-code mapping it pins is platform-independent")
	}
	// processWaitDelay is a package var, so this test cannot run in parallel; shrinking it is
	// what keeps a five-second drain out of the suite.
	prev := processWaitDelay
	processWaitDelay = 250 * time.Millisecond
	t.Cleanup(func() { processWaitDelay = prev })

	// The sleep INHERITS the captured pipes and outlives the shell, so the output copy cannot
	// finish: Wait blocks until the delay expires. The sleep is short enough that a failed
	// reap cannot leave a process around for long.
	res, err := runSubprocess(context.Background(), subprocessSpec{argv: []string{"/bin/sh", "-c", `sleep 10 &`}})
	if err != nil {
		t.Fatalf("runSubprocess err = %v, want nil (a wedged drain is a result, not a Go error)", err)
	}
	if !res.drainWedged {
		t.Fatalf("drainWedged = false, want true — the pipe was still held when the delay expired (exit code %d)", res.exitCode)
	}
	if res.exitCode == 0 {
		t.Errorf("exitCode = 0 for a run whose descendants held the pipe and were killed; the operator would read that as a clean success")
	}
}

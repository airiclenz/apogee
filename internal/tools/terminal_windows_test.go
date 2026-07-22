//go:build windows

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The Windows execution tests the POSIX-shell cases in terminal_test.go skip. They run a
// real cmd.exe: the quoting path is the one that cannot be proven by a table test, because
// what breaks is the boundary between os/exec's argv joining and cmd.exe's parser.

func TestTerminal_WindowsQuotedCommandReachesTheShellIntact(t *testing.T) {
	t.Parallel()

	// A model writes quotes constantly (`git commit -m "..."`). Joined as an argv, the
	// quotes arrive at cmd.exe escaped as \" and are echoed back literally; delivered as
	// the process command line they survive.
	term := NewTerminal(t.TempDir())
	res, err := term.Execute(context.Background(), terminalCall("c1", `echo "hello world"`))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("clean command produced an error result: %q", res.Content)
	}
	if !strings.Contains(res.Content, `"hello world"`) {
		t.Errorf("output = %q, want the quotes intact", res.Content)
	}
	if strings.Contains(res.Content, `\"`) {
		t.Errorf("output = %q, want no backslash-escaped quotes (os/exec's argv joining leaked)", res.Content)
	}
}

func TestTerminal_WindowsRedirectToAQuotedSpacedPath(t *testing.T) {
	t.Parallel()

	// The shape the confinement escape battery uses: a write redirected to a quoted path.
	// %USERPROFILE% routinely contains a space, so this is the ordinary case, not an edge.
	root := t.TempDir()
	dir := filepath.Join(root, "pro be")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	target := filepath.Join(dir, "out.txt")

	term := NewTerminal(root)
	res, err := term.Execute(context.Background(), terminalCall("c1", "echo x> "+shellHost.Quote(target)))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("redirect produced an error result: %q", res.Content)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("redirect to the quoted path %q did not land: %v", target, err)
	}
}

// TestTerminal_WindowsCancelKillsTheProcessTree is the Windows counterpart of
// TestTerminal_CancelKillsChildProcessGroup and the behavioural proof of the §2.4 job-object
// teardown: a ctx-cancelled command must take its whole process tree with it, not just the
// leader apogee holds.
//
// The tree is three deep — cmd.exe (the shell the terminal tool wraps the line in) →
// powershell.exe → a DETACHED cmd.exe running ping. Only the powershell is a child of the
// process apogee can kill, and the ping's parent was started with Start-Process into its own
// console, so nothing but the job object can reap it: a leader-only teardown leaves it
// running for four minutes. It records its own PID, and the test asserts that PID is gone.
func TestTerminal_WindowsCancelKillsTheProcessTree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pidFile := filepath.Join(root, "grandchild.pid")
	scriptPath := filepath.Join(root, "spawn.ps1")

	// A .ps1 file rather than an inline -Command: the script is then opaque to cmd.exe's
	// parser, so the test proves the teardown rather than re-testing quoting (which
	// TestTerminal_WindowsQuotedCommandReachesTheShellIntact owns). Detaching the grandchild
	// also keeps it off the captured pipes, so Wait returns instead of draining for WaitDelay.
	script := "$p = Start-Process -FilePath cmd.exe -ArgumentList '/c','ping -n 240 127.0.0.1' -PassThru -WindowStyle Hidden\r\n" +
		"Set-Content -LiteralPath '" + pidFile + "' -Value $p.Id\r\n" +
		"Start-Sleep -Seconds 240\r\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write %q: %v", scriptPath, err)
	}

	term := NewTerminal(root)
	call := terminalCall("c1", "powershell -NoProfile -ExecutionPolicy Bypass -File "+shellHost.Quote(scriptPath))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = term.Execute(ctx, call)
	}()

	// Wait until the detached grandchild exists and has recorded its PID, then cancel.
	grandchildPID := waitForPIDFile(t, pidFile)
	if syscallKill0(grandchildPID) != nil {
		t.Fatalf("grandchild PID %d was already gone before the cancel; the test proves nothing", grandchildPID)
	}
	cancel()
	<-done

	if pidAlive(grandchildPID, 3*time.Second) {
		t.Errorf("detached grandchild PID %d survived ctx cancel; the job object did not reap the tree", grandchildPID)
	}
}

// TestTerminal_WindowsCleanRunLeavesADetachedProcessAlive pins the other half of the job
// object's contract: the teardown fires on CANCELLATION, not on completion. A command that
// deliberately leaves a process running behind it must keep it, exactly as a backgrounded
// process outlives its POSIX process-group leader — which is why release() clears
// KILL_ON_JOB_CLOSE before closing the handle instead of letting the kernel reap the job.
func TestTerminal_WindowsCleanRunLeavesADetachedProcessAlive(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pidFile := filepath.Join(root, "detached.pid")
	scriptPath := filepath.Join(root, "detach.ps1")

	// The script starts a detached process and then EXITS — the whole command completes
	// cleanly in well under a second, so nothing cancels the run.
	//
	// Two details keep the surviving process from wrecking t.TempDir's RemoveAll, which runs
	// while it is still alive. It is a single ping.exe rather than a cmd.exe wrapping one, so
	// the recorded PID *is* the whole detached tree and the cleanup below reaps all of it; and
	// it is started with its working directory outside the temp dir, because a process's cwd
	// is an open directory handle and Windows refuses to delete a directory anyone holds.
	script := "$p = Start-Process -FilePath ping.exe -ArgumentList '-n','60','127.0.0.1'" +
		" -WorkingDirectory $env:SystemRoot -PassThru -WindowStyle Hidden\r\n" +
		"Set-Content -LiteralPath '" + pidFile + "' -Value $p.Id\r\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write %q: %v", scriptPath, err)
	}

	term := NewTerminal(root)
	call := terminalCall("c1", "powershell -NoProfile -ExecutionPolicy Bypass -File "+shellHost.Quote(scriptPath))
	res, err := term.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Fatalf("clean command produced an error result: %q", res.Content)
	}

	detachedPID := waitForPIDFile(t, pidFile)
	// Whatever the assertion says, this test must not leak a process into the machine.
	t.Cleanup(func() { killPID(detachedPID) })

	if syscallKill0(detachedPID) != nil {
		t.Errorf("detached PID %d died with the completed command; the job object reaped a process the command meant to leave running", detachedPID)
	}
}

func TestTerminal_WindowsNonZeroExitIsErrorResult(t *testing.T) {
	t.Parallel()

	term := NewTerminal(t.TempDir())
	res, err := term.Execute(context.Background(), terminalCall("c1", "exit 3"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil (a non-zero exit is a result, not a Go error)", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "exit code 3") {
		t.Errorf("result = %q, want it to report exit code 3", res.Content)
	}
}

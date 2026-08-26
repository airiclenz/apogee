//go:build !windows

package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// escapeeRunTimeout bounds the runs below so a host on which the rendezvous never completes fails
// the test in seconds instead of hanging until the suite's own deadline.
const escapeeRunTimeout = 30 * time.Second

// runUnderTeardown runs argv through the §2.4 seam exactly as a spawner does — the per-OS
// constructor before Start, RunWithTeardown around Wait — so the two teardown paths this file
// pins are the real ones and not a re-implementation of them. Output is discarded: what these
// tests read is what the kill reached, not what the command printed.
func runUnderTeardown(ctx context.Context, argv ...string) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	return RunWithTeardown(cmd, NewProcessTeardown(cmd))
}

// setsidEscapeScript builds the shell line both halves of TestTeardownDoesNotReachASetsidEscapee
// run, and returns it with the path the leader records the escapee's PID in.
//
// The line backgrounds a descendant that deliberately leaves the run's process group — setsid puts
// it at the head of a new session and group — and then blocks reading a FIFO until that descendant
// reports its own PID. The rendezvous is load-bearing: the descendant can only write after
// setsid(2) has returned, so a leader that got past the read has already been escaped, and the test
// never races the escape it is trying to observe. The descendant's output goes to /dev/null so it
// never holds the captured pipes. tail is what the leader does once the PID is recorded: nothing on
// the clean-exit path, a long sleep on the cancelled one.
func setsidEscapeScript(t *testing.T, tail string) (script, pidFile string) {
	t.Helper()

	dir := t.TempDir()
	fifo := filepath.Join(dir, "escapee.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("Mkfifo(%s) = %v", fifo, err)
	}
	pidFile = filepath.Join(dir, "escapee.pid")

	// The sleep outlives the test by a wide margin but not the suite: if an assertion fails
	// before the PID is known, nothing can kill it by hand, so it must expire on its own.
	script = fmt.Sprintf("setsid /bin/sh -c 'echo $$ > \"$1\"; exec sleep 60' sh %s >/dev/null 2>&1 &\n"+
		"read pid < %s\n"+
		"echo \"$pid\" > %s\n"+tail,
		strconv.Quote(fifo), strconv.Quote(fifo), strconv.Quote(pidFile))
	return script, pidFile
}

// waitForPIDFile returns the PID the leader recorded, waiting for the file to appear.
func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			if pid, perr := strconv.Atoi(strings.TrimSpace(string(b))); perr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("escapee PID file %s never appeared", path)
	return 0
}

// pidAlive reports whether pid is still alive throughout the given window, polling so a
// slightly-late reap does not read as survival. Signal 0 probes existence without delivering
// anything; ESRCH means the process is gone.
func pidAlive(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return syscall.Kill(pid, 0) == nil
}

// killPID terminates pid, best-effort. It exists so a test that leaves a backgrounded process
// behind cannot leak it into the machine, whichever way its assertion goes.
func killPID(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// assertSetsidEscapeeSurvived pins the two halves of the residual on the PID the leader recorded:
// that the descendant really did leave the run's process group (a setsid that silently did nothing
// would leave the survival check proving nothing), and that teardown did not reach it.
func assertSetsidEscapeeSurvived(t *testing.T, pid int) {
	t.Helper()

	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatalf("Getpgid(%d) = %v — the escapee is gone, so teardown reached it after all", pid, err)
	}
	if pgid != pid {
		t.Fatalf("escapee PID %d is in group %d, want its own — setsid did not detach it, so this test would prove nothing", pid, pgid)
	}
	if !pidAlive(pid, 500*time.Millisecond) {
		t.Errorf("setsid-detached descendant PID %d did not survive teardown; the residual documented at NewProcessTeardown (teardown_unix.go) no longer holds — that is the finding, do not re-document it away", pid)
	}
}

// TestTeardownDoesNotReachASetsidEscapee pins the POSIX residual of the §2.4 process-group
// teardown, which was prose-only until this test (teardown_unix.go NewProcessTeardown,
// teardown.go planTreeKill, doc.go): the group holds every descendant that has not
// deliberately left it, and one that calls setsid/setpgid(0,0) leads a NEW group that no
// negative-PID kill aimed at this run can reach. So it survives both teardown paths — the
// clean-exit reap and cmd.Cancel — unsupervised, while the caller sees the leader's own exit
// status. The residual is accepted, not enforced against: deliberately backgrounding a process is
// legitimate terminal use, and the escapee stays inside whatever write-fence the Confiner installed
// (changing process group sheds no landlock ruleset and no seatbelt profile), so what is lost is
// supervision, not confinement. Should this test start failing, the behavior changed and the docs
// are what needs revisiting.
func TestTeardownDoesNotReachASetsidEscapee(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skipf("setsid not on PATH (%v); the escape it pins needs the tool to detach with", err)
	}
	t.Parallel()

	t.Run("the clean-exit reap does not reach it", func(t *testing.T) {
		t.Parallel()
		script, pidFile := setsidEscapeScript(t, "")

		ctx, cancel := context.WithTimeout(context.Background(), escapeeRunTimeout)
		defer cancel()
		if err := runUnderTeardown(ctx, "/bin/sh", "-c", script); err != nil {
			t.Fatalf("RunWithTeardown err = %v, want nil — the leader exits cleanly", err)
		}

		// The leader wrote the file before exiting, so the reap has already run.
		pid := waitForPIDFile(t, pidFile)
		t.Cleanup(func() { killPID(pid) })
		assertSetsidEscapeeSurvived(t, pid)
	})

	t.Run("a cancelled run does not reach it", func(t *testing.T) {
		t.Parallel()
		script, pidFile := setsidEscapeScript(t, "sleep 60\n")

		ctx, cancel := context.WithTimeout(context.Background(), escapeeRunTimeout)
		defer cancel()
		done := make(chan struct{})
		go func() {
			defer close(done)
			// Cancelling a run is a result, not a Go error the assertion needs: what this
			// test reads is what the kill reached, not what the run reported.
			_ = runUnderTeardown(ctx, "/bin/sh", "-c", script)
		}()

		// The PID file appears only after the rendezvous, so by the time cancel fires the
		// escape has already happened and cmd.Cancel gets its fair chance at the descendant.
		pid := waitForPIDFile(t, pidFile)
		t.Cleanup(func() { killPID(pid) })
		cancel()
		<-done

		assertSetsidEscapeeSurvived(t, pid)
	})
}

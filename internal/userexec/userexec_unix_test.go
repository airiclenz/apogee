//go:build !windows

package userexec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRunKillsAWrappersGrandchildOnTimeout is the process-tree half of the deadline: a wrapper — a
// shell that backgrounded the real tool and waits on it — dies at the deadline, and the grandchild
// it left in the process group dies with it rather than surviving as an orphan the run never
// supervised. The wrapper records the grandchild's PID before it waits, so the test can probe the
// grandchild directly once Run has returned; the file lives under the test's own directory, and
// the grandchild is killed by hand whichever way the assertion goes, so a failure leaks nothing
// into the machine. The probe is signal 0, which only asks whether the PID exists, so it lives in
// this !windows file: userexec_test.go carries no build tag and skips per test instead.
func TestRunKillsAWrappersGrandchildOnTimeout(t *testing.T) {
	requireShell(t)
	t.Parallel()

	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	script := fmt.Sprintf("sleep 30 & echo $! > %s; wait", strconv.Quote(pidFile))

	result, err := Run(context.Background(), shell(script), Options{Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatalf("Run past the deadline returned an error rather than a timed-out result: %v", err)
	}
	if !result.TimedOut {
		t.Fatalf("result = %+v, want TimedOut", result)
	}

	pid := readPIDFile(t, pidFile)
	defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()
	if !pidGone(pid, 2*time.Second) {
		t.Errorf("the wrapper's grandchild (pid %d) is still alive after the run timed out; the deadline kills the leader alone", pid)
	}
}

// readPIDFile returns the PID the wrapper recorded, waiting for the file to appear: the wrapper
// wrote it before the deadline could fire, so the wait is for the filesystem, not the shell.
func readPIDFile(t *testing.T, path string) int {
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
	t.Fatalf("grandchild PID file %s never appeared", path)
	return 0
}

// pidGone reports whether pid stops existing within the given window, polling so a reap that lands
// a moment after Run returned does not read as survival. Signal 0 probes existence without
// delivering anything; ESRCH means the process is gone.
func pidGone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return syscall.Kill(pid, 0) != nil
}

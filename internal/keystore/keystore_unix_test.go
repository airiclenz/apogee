//go:build !windows

package keystore

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

	"github.com/airiclenz/apogee/internal/testtime"
)

// A store tool can be wrapper-shaped — a script that starts the real binary and waits on it — and a
// locked store leaves both waiting until the deadline. The deadline has to end the whole tree: a
// grandchild that outlived the write would keep running against the store, holding the secret it
// was fed on stdin, with nothing left to supervise it. The wrapper records the grandchild's PID
// before it waits, so the test probes the grandchild directly once runTool has returned; the
// grandchild is killed by hand whichever way the assertion goes, so a failure leaks nothing into
// the machine. runTool is driven directly, with a short deadline of its own, because Store.Write
// runs under writeTimeout's full minute.
func TestRunToolKillsAWrappersGrandchildWhenTheStoreNeverAnswers(t *testing.T) {
	t.Parallel()

	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	script := fmt.Sprintf("sleep 30 & echo $! > %s; wait", strconv.Quote(pidFile))
	ctx, cancel := context.WithTimeout(context.Background(), testtime.Fixed(200*time.Millisecond))
	defer cancel()

	_, err := runTool(ctx, []string{"sh", "-c", script}, "")

	if err == nil || !strings.Contains(err.Error(), "did not answer in time") {
		t.Fatalf("runTool past the deadline = %v, want the store reported as never answering", err)
	}
	pid := readGrandchildPID(t, pidFile)
	defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()
	if !processGone(pid, testtime.Budget(2*time.Second)) {
		t.Errorf("the wrapper's grandchild (pid %d) is still alive after the store tool timed out; "+
			"the deadline killed the leader alone", pid)
	}
}

// readGrandchildPID returns the PID the wrapper recorded, waiting for the file to appear: the
// wrapper wrote it before the deadline could fire, so the wait is for the filesystem, not the shell.
func readGrandchildPID(t *testing.T, path string) int {
	t.Helper()

	pid := 0
	testtime.Eventually(t, testtime.Budget(20*time.Second), func() bool {
		if data, err := os.ReadFile(path); err == nil {
			if got, parseErr := strconv.Atoi(strings.TrimSpace(string(data))); parseErr == nil && got > 0 {
				pid = got
			}
		}
		return pid > 0
	}, "grandchild PID file "+path+" to appear")
	return pid
}

// processGone reports whether pid stops existing within the given window, polling so a reap that
// lands a moment after runTool returned does not read as survival. Signal 0 probes existence
// without delivering anything; an error means the process is gone.
func processGone(pid int, within time.Duration) bool {
	expired := testtime.Expiry(within)
	for !expired() {
		if err := syscall.Kill(pid, 0); err != nil {
			return true
		}
		time.Sleep(testtime.Fixed(20 * time.Millisecond))
	}
	return syscall.Kill(pid, 0) != nil
}

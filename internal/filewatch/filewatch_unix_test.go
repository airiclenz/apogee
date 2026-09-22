//go:build !windows

package filewatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// The Settle is measured on the clock at the moment a change is observed, not on the tick's
// timestamp. A tick received late carries the time it was due, not the time it was taken, so a poll
// goroutine held off the CPU for most of a Settle — a loaded box under the race detector does this
// routinely — would otherwise compute reportAt from a stale now and report the change the moment it
// first looked: on a real save, the half-written document the delay exists to skip. The stall is
// staged for real rather than faked: the test process is stopped with SIGSTOP, a child rewrites the
// file while it is stopped, and continues it once the pending tick is older than the Settle. A
// second write inside the Settle must then still coalesce into the one report.
func TestWatchSettlesOnTheClockNotTheTick(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	w := startWatcher(t, path)

	// The child waits until this process is stopped, writes, holds it stopped past the Settle, and
	// continues it. `ps -o stat=` reports a stopped process with a leading T on Linux and macOS.
	// The stall holds this process stopped for a whole Settle plus a margin, so the pending tick is
	// unambiguously older than the Settle by the time the SIGCONT lands. The margin covers the
	// scheduling slack on a loaded box between the child's `ps` seeing the T state and its own sleep
	// actually running, and it derives from the Settle so it stays proportional to it.
	stall := testSettle + testSettle/2
	script := fmt.Sprintf(
		"while ! ps -o stat= -p %[1]d | grep -q '^T'; do sleep 0.01; done; "+
			"printf 'auto-title: true\\n' > %[2]q; sleep %[3]s; kill -CONT %[1]d",
		os.Getpid(), path, fmt.Sprintf("%.3f", stall.Seconds()))
	child := exec.Command("sh", "-c", script)
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatalf("start the stalling child: %v", err)
	}
	t.Cleanup(func() { _ = child.Wait() })

	if err := syscall.Kill(os.Getpid(), syscall.SIGSTOP); err != nil {
		t.Fatalf("stop this process: %v", err)
	}

	// Continued. The poll's first tick is now older than the Settle and the child's write is what it
	// observes; this second write lands well inside the Settle measured from that observation.
	time.Sleep(testSettle / 3)
	writeWatchedFile(t, path, "auto-title: true\n# a second line\n")

	awaitChange(t, w, "a write observed on a stale tick")
	expectNoChange(t, w, testQuiet, "the burst had already been reported once")
}

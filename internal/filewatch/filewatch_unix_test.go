//go:build !windows

package filewatch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
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
//
// The second write is step-locked with the poll rather than slept into the Settle: the hook holds the
// poll on the first still tick after it observed the child's write, before that tick's verdict, and
// the second write lands there. A watcher that took its Settle from the stale tick finds it already
// due on that tick and reports the first write alone, then the second — two reports. Should the box
// hold the poll for a whole Settle between the two ticks, a correct watcher rightly settles the first
// write alone too; that attempt is spoiled, its report drained, and a fresh stall starts the next.
func TestWatchSettlesOnTheClockNotTheTick(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	var state atomic.Int32
	// observedAt is when the poll observed the child's write. Only the poll goroutine touches it.
	var observedAt time.Time
	outcomes := make(chan bool)
	release := make(chan struct{})
	freed := make(chan struct{})
	w := startSampledWatcher(t, path, func(changed, pending bool) {
		switch {
		case changed:
			if state.CompareAndSwap(stallWatching, stallObserved) {
				observedAt = time.Now()
			}
		case state.CompareAndSwap(stallObserved, stallIdle):
			spoiled := !pending && time.Since(observedAt) >= testSettle
			select {
			case outcomes <- spoiled:
			case <-freed:
				return
			}
			if spoiled {
				return
			}
			select {
			case <-release:
			case <-freed:
			}
		}
	})
	// Registered after Start's Cleanup, so it runs first and frees a held poll before Stop waits on it.
	t.Cleanup(func() {
		state.Store(stallIdle)
		close(freed)
	})

	for attempt := 1; ; attempt++ {
		state.Store(stallWatching)
		// Each attempt's write is a length no earlier one had, so it is a change on the size witness
		// alone whatever the filesystem's timestamp granularity.
		stopWhileAChildWrites(t, path, "auto-title: true\n"+strings.Repeat("#", attempt)+"\n")

		// Continued. The poll's first tick is now older than the Settle and the child's write is what
		// it observes.
		var spoiled bool
		select {
		case spoiled = <-outcomes:
		case <-time.After(testDeadline):
			t.Fatal("the poll never took a still sample after the write it observed on the stale tick")
		}
		if !spoiled {
			break
		}
		awaitChange(t, w, "a write that settled before its first still tick could be held")
		if attempt == stallAttempts {
			t.Fatalf("all %d attempts held the poll past the %v Settle before its first still tick", stallAttempts, testSettle)
		}
	}

	// The poll is held on the first still tick after the observation, inside the Settle measured
	// from it: the second write lands before that tick's verdict.
	writeWatchedFile(t, path, "auto-title: true\n# a second line\n")
	select {
	case release <- struct{}{}:
	case <-time.After(testDeadline):
		t.Fatal("the held poll never took its release")
	}

	awaitChange(t, w, "a write observed on a stale tick")
	expectNoChange(t, w, testQuiet, "the burst had already been reported once")
}

// stopWhileAChildWrites stops this process with SIGSTOP while a child writes content to path, holds
// it stopped past the Settle, and continues it; it returns once this process runs again.
// `ps -o stat=` reports a stopped process with a leading T on Linux and macOS. The stall holds this
// process stopped for a whole Settle plus a margin, so the pending tick is unambiguously older than
// the Settle by the time the SIGCONT lands. The margin covers the scheduling slack on a loaded box
// between the child's `ps` seeing the T state and its own sleep actually running, and it derives
// from the Settle so it stays proportional to it.
func stopWhileAChildWrites(t *testing.T, path, content string) {
	t.Helper()
	stall := testSettle + testSettle/2
	script := fmt.Sprintf(
		"while ! ps -o stat= -p %[1]d | grep -q '^T'; do sleep 0.01; done; "+
			"printf %%b %[2]q > %[3]q; sleep %[4]s; kill -CONT %[1]d",
		os.Getpid(), content, path, fmt.Sprintf("%.3f", stall.Seconds()))
	child := exec.Command("sh", "-c", script)
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatalf("start the stalling child: %v", err)
	}
	t.Cleanup(func() { _ = child.Wait() })

	if err := syscall.Kill(os.Getpid(), syscall.SIGSTOP); err != nil {
		t.Fatalf("stop this process: %v", err)
	}
}

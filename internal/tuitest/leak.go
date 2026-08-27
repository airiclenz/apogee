package tuitest

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// A driver test starts a real program, a real worker, a real file watch and a real heartbeat. The
// interesting failure is not that one of them crashes — that is loud — but that one of them never
// stops: the test passes, the next test inherits a goroutine still sending into a dead program,
// and the suite becomes flaky somewhere else entirely. [CheckLeaks] makes that the failure of the
// test that caused it.

// leakMarkers are the packages whose goroutines belong to a driver test and must not outlive it.
// Everything else — the testing framework, the runtime, the standard library's own workers — is
// ignored, because they are nobody's leak.
var leakMarkers = []string{
	"internal/tui",
	"bubbletea",
	"internal/tuitest",
	"internal/filewatch",
	"internal/heartbeat",
}

// leakGrace is how long a goroutine gets to notice it was told to stop. Teardown is asynchronous
// by nature — a program returns before its renderer's last write lands — so the check polls rather
// than snapping once.
const leakGrace = 2 * time.Second

// checkerFrame is this package's own inspection frame. The goroutine running the check is walking
// its own stack, which of course names internal/tuitest; without this it would report itself.
const checkerFrame = "tuitest.leakedGoroutines("

// harnessFrames belong to `go test` itself: the goroutine running a test, and the one running the
// suite. A test function that lives in one of the marked packages — every test in THIS package,
// and every driver test in cmd/apogee — names its package in its own stack, so without this the
// check would report the very goroutine it was called from. A leak is never a tRunner: a leaked
// goroutine was started BY a test, and its stack says "created by", not "testing.tRunner".
var harnessFrames = []string{"testing.tRunner(", "testing.(*M).Run(", "testing.runTests("}

// timerFrames are goroutines that are ALREADY over and are only waiting for a clock to say so.
// bubbletea's Tick starts a goroutine that parks on a timer for the whole interval and cannot be
// cancelled by design (commands.go), so every tick a TUI has in flight when it quits outlives the
// test by up to its own period. Reporting those would make this check unusable against any program
// that ticks — which is every TUI — and they hold nothing: no channel a dead program reads, no file,
// no lock. What this check is for is a goroutine that is still WORKING.
var timerFrames = []string{"bubbletea/v2.Tick.func1("}

// CheckLeaks registers a cleanup that fails the test when a goroutine from the driver's own stack
// is still running after it. Call it FIRST in a driver test — before anything is launched — so the
// cleanup runs last, after every other cleanup has had its chance to stop things.
func CheckLeaks(t testing.TB) {
	t.Helper()

	t.Cleanup(func() {
		deadline := time.Now().Add(leakGrace)
		for {
			left := leakedGoroutines()
			if len(left) == 0 {
				return
			}
			if !time.Now().Before(deadline) {
				t.Errorf("%d goroutine(s) outlived the test by %s:\n\n%s",
					len(left), leakGrace, strings.Join(left, "\n\n"))
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
}

// leakedGoroutines returns the stacks of every goroutine naming one of [leakMarkers], excluding
// the one doing the looking.
func leakedGoroutines() []string {
	buf := make([]byte, 1<<16)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	var left []string
	for _, stack := range strings.Split(string(buf), "\n\n") {
		if strings.TrimSpace(stack) == "" || strings.Contains(stack, checkerFrame) ||
			harness(stack) || parkedOnATimer(stack) {
			continue
		}
		for _, marker := range leakMarkers {
			if strings.Contains(stack, marker) {
				left = append(left, strings.TrimSpace(stack))
				break
			}
		}
	}
	return left
}

// parkedOnATimer reports whether a stack is one of [timerFrames] — over, but not yet told.
func parkedOnATimer(stack string) bool {
	for _, frame := range timerFrames {
		if strings.Contains(stack, frame) {
			return true
		}
	}
	return false
}

// harness reports whether a stack belongs to the test runner rather than to the code under test.
func harness(stack string) bool {
	for _, frame := range harnessFrames {
		if strings.Contains(stack, frame) {
			return true
		}
	}
	return false
}

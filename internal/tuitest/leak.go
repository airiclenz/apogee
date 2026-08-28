package tuitest

import (
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// A driver test starts a real program, a real worker, a real file watch and a real heartbeat. The
// interesting failure is not that one of them crashes — that is loud — but that one of them never
// stops: the test passes, the next test inherits a goroutine still sending into a dead program,
// and the suite becomes flaky somewhere else entirely. [CheckLeaks] makes that the failure of the
// test that caused it.
//
// It is the test's OWN goroutines it makes that failure of: a driver test running in parallel with
// others sees their stacks too, and blaming a neighbour's straggler on whichever cleanup happens to
// look next is how a leak check becomes noise nobody reads. So [CheckLeaks] snapshots the goroutines
// already running when it is called and reports only the ids that were not in it. The caveat that
// rests on is id reuse: the runtime hands out goroutine ids from a counter that only ever goes up,
// so a retired id is never seen again and a snapshotted id can only ever mean the same goroutine. If
// that ever changed, a newborn goroutine could inherit a retired id and be forgiven as inherited.

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

// goroutineID is the runtime's identifier for one goroutine, as it appears in the
// "goroutine <id> [<state>]:" header that opens every block of a [runtime.Stack] dump.
type goroutineID string

// CheckLeaks registers a cleanup that fails the test when a goroutine the test itself started, from
// the driver's own stack, is still running after it. Call it FIRST in a driver test — before
// anything is launched — so that the snapshot it takes holds only what the test inherited, and so
// that the cleanup runs last, after every other cleanup has had its chance to stop things.
func CheckLeaks(t testing.TB) {
	t.Helper()

	inherited := leakedGoroutines()
	t.Cleanup(func() {
		deadline := time.Now().Add(leakGrace)
		for {
			left := startedSince(inherited)
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

// startedSince returns the stacks of the leaked goroutines that were not already running when the
// snapshot was taken, in a stable order — the map behind them has none, and a report that reads the
// same way twice is worth the sort.
func startedSince(snapshot map[goroutineID]string) []string {
	var left []string
	for id, stack := range leakedGoroutines() {
		if _, born := snapshot[id]; !born {
			left = append(left, stack)
		}
	}
	slices.Sort(left)
	return left
}

// leakedGoroutines returns the stack of every goroutine naming one of [leakMarkers], keyed by its
// id and excluding the one doing the looking.
func leakedGoroutines() map[goroutineID]string {
	buf := make([]byte, 1<<16)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	left := make(map[goroutineID]string)
	for _, block := range strings.Split(string(buf), "\n\n") {
		if strings.TrimSpace(block) == "" || strings.Contains(block, checkerFrame) ||
			harness(block) || parkedOnATimer(block) {
			continue
		}
		for _, marker := range leakMarkers {
			if strings.Contains(block, marker) {
				stack := strings.TrimSpace(block)
				left[idOf(stack)] = stack
				break
			}
		}
	}
	return left
}

// idOf reads the goroutine id out of a stack block's header line — "goroutine 42 [chan receive]:".
// A block whose header does not parse gets the empty id, which no snapshot taken from the same
// dump format can hold: an unattributable goroutine is reported, never silently forgiven.
func idOf(stack string) goroutineID {
	header, _, _ := strings.Cut(stack, "\n")
	rest, ok := strings.CutPrefix(strings.TrimSpace(header), "goroutine ")
	if !ok {
		return ""
	}
	id, _, _ := strings.Cut(rest, " ")
	return goroutineID(id)
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

package tuitest

import (
	"bytes"
	"context"
	"fmt"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync/atomic"
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
// look next is how a leak check becomes noise nobody reads. So [CheckLeaks] labels the goroutine it
// is called from with a pprof label carrying a per-call id. The runtime copies a goroutine's labels
// to every goroutine it starts, so everything the test starts from then on — the program, its
// renderer, the worker the program starts — inherits the id, and the cleanup reports only the
// goroutines still wearing it. What this rests on: a goroutine that was already running when the
// check was called never carries a fresh id, so an inherited goroutine cannot be blamed; and the
// reach ends where inheritance does — a goroutine a [time.AfterFunc] timer starts is born on the
// runtime's timer goroutine, carries no label, and is outside the check.

// leakLabel is the pprof label key the check attributes with; its value is the per-call id.
const leakLabel = "tuitest.leakcheck"

// leakCheckSeq mints the per-call ids. A counter rather than a test name: one test can call the
// check more than once — a subtest inside a checked test does — and each call must be its own.
var leakCheckSeq atomic.Uint64

// leakMarkers are the packages whose goroutines belong to a driver test and must not outlive it.
// Everything else — the testing framework, the runtime, the standard library's own workers — is
// ignored, because they are nobody's leak.
var leakMarkers = []string{
	"internal/tui",
	"bubbletea",
	"internal/tuitest",
	"internal/filewatch",
	"internal/heartbeat",
	// A Reaction's worker is started by the composition root rather than by the TUI (ADR 0073),
	// so its stack names no other marker here — and a worker that outlives close() is exactly the
	// leak this check exists for.
	"internal/reactions",
}

// leakGrace is how long a goroutine gets to notice it was told to stop. Teardown is asynchronous
// by nature — a program returns before its renderer's last write lands — so the check polls rather
// than snapping once.
const leakGrace = 2 * time.Second

// checkerFrame is this package's own inspection frame. The goroutine reading the profile is in it,
// which of course names internal/tuitest; the cleanup clears its own labels before it reads, so
// this is a second line — it keeps the check from reporting itself should it ever be run from a
// goroutine that still wears the id.
const checkerFrame = "tuitest.goroutineProfile"

// harnessFrames belong to `go test` itself: the goroutine running a test, and the one running the
// suite. A test function that lives in one of the marked packages — every test in THIS package,
// and every driver test in cmd/apogee — names its package in its own stack, and a subtest started
// from a checked test inherits its id; without this the check could report the very goroutine a
// test runs on. A leak is never a tRunner: a leaked goroutine was started BY a test.
var harnessFrames = []string{"testing.tRunner", "testing.(*M).Run", "testing.runTests"}

// timerFrames are goroutines that are ALREADY over and are only waiting for a clock to say so.
// bubbletea's Tick starts a goroutine that parks on a timer for the whole interval and cannot be
// cancelled by design (commands.go), so every tick a TUI has in flight when it quits outlives the
// test by up to its own period. Reporting those would make this check unusable against any program
// that ticks — which is every TUI — and they hold nothing: no channel a dead program reads, no file,
// no lock. What this check is for is a goroutine that is still WORKING.
//
// The frames here, and in [checkerFrame] and [harnessFrames], are spelled the way the debug=1
// goroutine profile prints them — `pkg.func+0x…`, never `pkg.func(…)`.
var timerFrames = []string{"bubbletea/v2.Tick.func1"}

// leak is one entry of the goroutine profile still wearing a check's id. The debug=1 profile folds
// goroutines with an identical stack and identical labels into one entry with a count, so an entry
// can stand for several goroutines.
type leak struct {
	count int
	stack string
}

// CheckLeaks registers a cleanup that fails the test when a goroutine the test itself started, from
// the driver's own stack, is still running after it. Call it FIRST in a driver test — before
// anything is launched — so that everything the test starts inherits its id, and so that the
// cleanup runs last, after every other cleanup has had its chance to stop things.
func CheckLeaks(t testing.TB) {
	t.Helper()

	id := strconv.FormatUint(leakCheckSeq.Add(1), 10)
	pprof.SetGoroutineLabels(pprof.WithLabels(context.Background(), pprof.Labels(leakLabel, id)))
	t.Cleanup(func() {
		// The cleanup runs on the test's own goroutine, which wears the id like everything it
		// started; without this it would count itself.
		pprof.SetGoroutineLabels(context.Background())
		deadline := time.Now().Add(leakGrace)
		for {
			profile, err := goroutineProfile()
			if err != nil {
				t.Errorf("reading the goroutine profile: %v", err)
				return
			}
			left := leaksLabelled(profile, id)
			if len(left) == 0 {
				return
			}
			if !time.Now().Before(deadline) {
				t.Errorf("%d goroutine(s) outlived the test by %s:\n\n%s",
					goroutinesIn(left), leakGrace, stacksOf(left))
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
}

// goroutineProfile is the live goroutine profile in its debug=1 form — the one that prints frames
// as `pkg.func+0x…` lines and, for a labelled goroutine, a `# labels:` line.
func goroutineProfile() ([]byte, error) {
	var buf bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&buf, 1); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// leaksLabelled returns the entries of a debug=1 goroutine profile that wear the check id and name
// one of [leakMarkers], excluding the checker, the test harness and the timer-parked. The profile
// is an argument rather than read in here so this package's own tests can drive the parsing with
// canned profiles.
func leaksLabelled(profile []byte, id string) []leak {
	label := fmt.Sprintf("%q:%q", leakLabel, id)
	var left []leak
	for _, block := range strings.Split(string(profile), "\n\n") {
		entry := strings.TrimSpace(block)
		// The first block starts with the profile's own "goroutine profile: total N" line.
		if _, rest, found := strings.Cut(entry, "\n"); found && strings.HasPrefix(entry, "goroutine profile:") {
			entry = rest
		}
		if entry == "" || !strings.Contains(entry, label) || strings.Contains(entry, checkerFrame) ||
			harness(entry) || parkedOnATimer(entry) {
			continue
		}
		for _, marker := range leakMarkers {
			if strings.Contains(entry, marker) {
				left = append(left, leak{count: countOf(entry), stack: entry})
				break
			}
		}
	}
	return left
}

// countOf reads the goroutine count off an entry's head line — "3 @ 0x… 0x…". An entry whose head
// does not parse still stands for at least the one goroutine it prints.
func countOf(entry string) int {
	head, _, _ := strings.Cut(entry, "\n")
	digits, _, _ := strings.Cut(head, " ")
	if n, err := strconv.Atoi(digits); err == nil && n > 0 {
		return n
	}
	return 1
}

// goroutinesIn is how many goroutines the entries stand for between them.
func goroutinesIn(leaks []leak) int {
	n := 0
	for _, l := range leaks {
		n += l.count
	}
	return n
}

// stacksOf joins the entries for a report, in the order the profile printed them.
func stacksOf(leaks []leak) string {
	stacks := make([]string, 0, len(leaks))
	for _, l := range leaks {
		stacks = append(stacks, l.stack)
	}
	return strings.Join(stacks, "\n\n")
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

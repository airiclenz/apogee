package tuitest

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// parkedForTest blocks in a frame this package owns, which is exactly what a leaked driver
// goroutine looks like from the outside. It reports its own id first, so a test can say WHICH
// goroutine it expects the check to name.
func parkedForTest(started chan<- goroutineID, stop <-chan struct{}) {
	started <- ownGoroutineID()
	<-stop
}

// ownGoroutineID is the calling goroutine's id, read the only way Go offers it: out of the header
// of its own stack. It goes through [idOf], so the parser the check attributes with is the parser
// these tests name their goroutines with.
func ownGoroutineID() goroutineID {
	buf := make([]byte, 1<<10)
	return idOf(string(buf[:runtime.Stack(buf, false)]))
}

// recordingTB stands in for the *testing.T of a test that leaks, so a test can call [CheckLeaks],
// run the cleanup itself and read back what it reported instead of failing on it. testing.TB cannot
// be implemented outside the testing package — it has an unexported method — so the real T is
// embedded and only the two methods CheckLeaks calls are taken over.
type recordingTB struct {
	testing.TB
	cleanups []func()
	reports  []string
}

func (r *recordingTB) Cleanup(f func()) { r.cleanups = append(r.cleanups, f) }

func (r *recordingTB) Errorf(format string, args ...any) {
	r.reports = append(r.reports, fmt.Sprintf(format, args...))
}

// runCleanups runs what was registered, last-registered first, the way testing does.
func (r *recordingTB) runCleanups() {
	for i := len(r.cleanups) - 1; i >= 0; i-- {
		r.cleanups[i]()
	}
}

// TestLeakedGoroutinesSeesAParkedOne — and stops seeing it once it finishes. The two halves are
// one test on purpose: a check that only ever reports a leak is as useless as one that never does.
// It does not run in parallel: it counts goroutines, and a neighbour's screen would be counted too.
func TestLeakedGoroutinesSeesAParkedOne(t *testing.T) {
	before := len(leakedGoroutines())

	started, stop := make(chan goroutineID), make(chan struct{})
	go parkedForTest(started, stop)
	<-started
	if got := len(leakedGoroutines()); got <= before {
		t.Errorf("leakedGoroutines() = %d with a goroutine parked in a tuitest frame, want more than %d", got, before)
	}
	close(stop)

	deadline := time.Now().Add(2 * time.Second)
	for len(leakedGoroutines()) > before {
		if !time.Now().Before(deadline) {
			t.Fatalf("the finished goroutine is still reported as leaked")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestLeakedGoroutinesIgnoresItsOwnChecker: the goroutine walking the stacks is standing in a
// tuitest frame itself, and a check that reported itself would fail every test that used it.
func TestLeakedGoroutinesIgnoresItsOwnChecker(t *testing.T) {
	for _, stack := range leakedGoroutines() {
		if strings.Contains(stack, checkerFrame) {
			t.Errorf("leakedGoroutines() reported the goroutine doing the looking:\n%s", stack)
		}
	}
}

// TestCheckLeaksPassesWhenTheScreenIsClosed is the positive case, run the way a driver test runs
// it: CheckLeaks first, the screen after, and the cleanup order does the rest. If the answer pump
// outlived the subtest this fails.
func TestCheckLeaksPassesWhenTheScreenIsClosed(t *testing.T) {
	t.Run("a screen opened and closed", func(t *testing.T) {
		CheckLeaks(t)
		s := NewScreen(20, 5)
		t.Cleanup(s.Close)
		if _, err := s.Write([]byte("hello")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	})
}

// TestCheckLeaksReportsOnlyWhatTheTestStarted: a goroutine already parked when CheckLeaks is called
// belongs to whoever started it — a parallel neighbour, or the process — and attributing it to this
// test would make the check noise. One started after the call is this test's to answer for. Not
// parallel: it reads a leak report the whole process contributes to.
func TestCheckLeaksReportsOnlyWhatTheTestStarted(t *testing.T) {
	started, stop := make(chan goroutineID), make(chan struct{})
	defer close(stop)

	go parkedForTest(started, stop)
	inherited := <-started

	rec := &recordingTB{TB: t}
	CheckLeaks(rec)

	go parkedForTest(started, stop)
	ours := <-started

	rec.runCleanups()

	if len(rec.reports) != 1 {
		t.Fatalf("CheckLeaks reported %d time(s), want 1: %v", len(rec.reports), rec.reports)
	}
	report := rec.reports[0]
	if !strings.Contains(report, "goroutine "+string(ours)+" ") {
		t.Errorf("the goroutine started after CheckLeaks (%s) is missing from its report:\n%s", ours, report)
	}
	if strings.Contains(report, "goroutine "+string(inherited)+" ") {
		t.Errorf("the goroutine parked before CheckLeaks (%s) was attributed to this test:\n%s", inherited, report)
	}
}

// TestIdOfKeysAnUnparseableBlockOnItsContent: a well-formed header still yields the bare number,
// and a block whose header does not parse yields an id of its own instead of the empty one every
// such block used to share — two of them stay two entries in the snapshot map, and the same block
// keys the same way twice so a snapshot still forgives the goroutine it actually holds.
func TestIdOfKeysAnUnparseableBlockOnItsContent(t *testing.T) {
	t.Parallel()

	if got := idOf("goroutine 42 [chan receive]:\ninternal/tui.wait()"); got != "42" {
		t.Errorf("idOf(a well-formed header) = %q, want %q", got, "42")
	}

	const truncated = "goroutine\ninternal/tui.wait()"
	const headerless = "internal/tui.paint()\n\tlayout.go:12 +0x1"
	first, second := idOf(truncated), idOf(headerless)
	for _, id := range []goroutineID{first, second} {
		if !strings.HasPrefix(string(id), unparsedID) {
			t.Errorf("idOf(an unparseable block) = %q, want the %q prefix a runtime id cannot wear", id, unparsedID)
		}
	}
	if first == second {
		t.Errorf("two different unparseable blocks share the id %q", first)
	}
	if again := idOf(truncated); again != first {
		t.Errorf("idOf(the same unparseable block) = %q then %q, want one stable id", first, again)
	}
}

// TestStartedSinceDoesNotForgiveASecondUnparseableBlock is the leak the empty id caused: one
// unparseable block running when CheckLeaks snapshotted forgave every later one, because they all
// keyed the same. Both maps are built through the real keying, so it is [idOf]'s rule under test
// here and not the test's own bookkeeping.
func TestStartedSinceDoesNotForgiveASecondUnparseableBlock(t *testing.T) {
	t.Parallel()

	const inherited = "goroutine\ninternal/tui.inherited()"
	const leaked = "goroutine\ninternal/tui.leaked()"
	snapshot := map[goroutineID]string{idOf(inherited): inherited}
	current := map[goroutineID]string{idOf(inherited): inherited, idOf(leaked): leaked}

	left := startedSince(snapshot, current)
	if len(left) != 1 || left[0] != leaked {
		t.Fatalf("startedSince(...) = %q, want only the block that was not in the snapshot", left)
	}
}

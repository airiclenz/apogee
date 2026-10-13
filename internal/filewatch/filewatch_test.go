package filewatch

// The file watcher (ADR 0041 decision 3): what counts as a change, how a save's burst of
// writes becomes one report, what Stop guarantees, and that two watchers over two files each answer
// for their own. Every test drives a real watcher over a real file in t.TempDir() at a millisecond
// cadence — the mechanism is a poll and a clock, and a fake of either would test the fake.

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/testtime"
)

// The cadence the tests run the watcher at. The Settle window is generously wider than the Interval
// so that a burst of writes coalesces even on a loaded machine under -race, and the deadline is wide
// enough that a missed report is a failure of the watcher rather than of the box it runs on.
//
// The three windows are margins sized for a loaded box, and the last two derive from the Settle so
// the relation each one asserts survives any future widening. testSettle is the window a FRACTION of
// which still has to be measurable: filewatch_unix_test.go sleeps testSettle/3 to land a second
// write inside a real SIGSTOP-stalled Settle, and at 150ms that fraction was a 50ms sleep a
// descheduled goroutine could overrun. testDeadline is the whole window awaitChange gives one
// report, so that a report merely late on a slow box is not a failure. testQuiet is the window in
// which a SECOND report must not arrive, which only says anything about the Settle when it is at
// least as long as one.
//
// The Interval and the Settle are the watcher's own configuration and the debounce under test, so
// they and every sleep cut from them are fixed (testtime.Fixed); only testDeadline, a wait that ends
// on the report, scales with APOGEE_TEST_TIME_SCALE (testtime.Budget). testQuiet is a window in
// which nothing may happen, so it is fixed too.
const (
	testInterval = 10 * time.Millisecond
	testSettle   = 500 * time.Millisecond
)

var (
	testDeadline = testtime.Budget(10 * testSettle)
	testQuiet    = testtime.Fixed(2 * testSettle)
)

// startWatcher starts a watcher over path at the test cadence and stops it with the test.
func startWatcher(t *testing.T, path string) *Watcher {
	t.Helper()
	w := New(path)
	w.Interval = testInterval
	w.Settle = testSettle
	w.Start()
	t.Cleanup(w.Stop)
	return w
}

// writeWatchedFile writes content to path, failing the test if it cannot.
func writeWatchedFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeBurst writes each content to path in turn, the way an editor's save lands, and skips the test
// when the burst took long enough that the box, not the watcher, decides whether it coalesces: a
// burst spread over a whole Settle is two changes, and two reports are then the right answer.
func writeBurst(t *testing.T, path string, contents ...string) {
	t.Helper()
	start := time.Now()
	for _, content := range contents {
		writeWatchedFile(t, path, content)
	}
	if took := time.Since(start); took >= testSettle/2 {
		t.Skipf("the burst took %v, too close to the %v Settle to be one change", took, testSettle)
	}
}

// awaitChange waits for one report, failing the test with why if none arrives.
func awaitChange(t *testing.T, w *Watcher, why string) {
	t.Helper()
	select {
	case _, ok := <-w.Changes():
		if !ok {
			t.Fatalf("Changes closed before %s was reported", why)
		}
	case <-time.After(testDeadline):
		t.Fatalf("no change reported for %s", why)
	}
}

// expectNoChange asserts that nothing is reported for the given window.
func expectNoChange(t *testing.T, w *Watcher, window time.Duration, why string) {
	t.Helper()
	select {
	case _, ok := <-w.Changes():
		if ok {
			t.Fatalf("a change was reported when %s", why)
		}
		t.Fatalf("Changes was closed when %s", why)
	case <-time.After(window):
	}
}

// One save is one report. The watcher is a poll, so the risk it has to be pinned against is a
// change that keeps being re-reported on every tick after it: the sample the comparison is made
// against must advance when the change is observed, not when it is reported.
func TestWatchReportsAWriteOnce(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	w := startWatcher(t, path)
	writeWatchedFile(t, path, "auto-title: true\n# a second line\n")

	awaitChange(t, w, "a single write")
	expectNoChange(t, w, testQuiet, "the file had already been reported once")
}

// An editor saving is a burst — write, truncate, rename — and each of those is a distinct file state
// a poll can land on. The Settle delay exists so the burst is one apply, not three, the last two of
// which would be against a half-written document.
func TestWatchCoalescesABurstIntoOneReport(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	w := startWatcher(t, path)
	writeBurst(t, path, "", "auto-title: tr", "auto-title: true\n")

	awaitChange(t, w, "a burst of three writes")
	expectNoChange(t, w, testQuiet, "the burst had already been reported once")
}

// stallAttempts bounds how many first writes the stall test spends looking for a still tick inside
// the Settle. An attempt is spoiled only when the box holds the poll past the whole Settle, so the
// first write rightly settles alone; three in a row is a box too loaded to stage the stall at all.
const stallAttempts = 3

// stallResult is what one attempt at the staged stall reports back from the poll goroutine: that it
// was spoiled — the first still tick after the first write already found the Settle due — or the
// outcome of the write it made inside the stall.
type stallResult struct {
	spoiled bool
	err     error
}

// The states the stall hook moves through on one attempt: idle between attempts, watching for the
// attempt's first write to be observed, then waiting for the first still tick after it.
const (
	stallIdle int32 = iota
	stallWatching
	stallObserved
)

// A still sample is quiet only as of the stat that took it. A poll goroutine held off the CPU between
// the stat and its clock read wakes past reportAt, and reporting then would announce a sample the
// rest of the burst has since overtaken — the next tick sees that write and reports the save a second
// time. The stall is staged on the poll goroutine itself, after the stat and before the clock read,
// on the first tick that sees the first write hold still while its Settle is pending, and the burst's
// second write lands inside it. Should the poll be held past the whole Settle before that tick comes,
// the first write settles alone and is reported; the attempt is spoiled, its report drained, and a
// fresh first write starts the next one.
func TestWatchNeverSettlesOnASampleTakenBeforeAStall(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	w := New(path)
	w.Interval = testInterval
	w.Settle = testSettle
	var state atomic.Int32
	outcomes := make(chan stallResult, stallAttempts)
	w.sampled = func(changed, pending bool) {
		switch {
		case changed:
			state.CompareAndSwap(stallWatching, stallObserved)
		case pending:
			if !state.CompareAndSwap(stallObserved, stallIdle) {
				return
			}
			time.Sleep(testtime.Fixed(testSettle + testSettle/2))
			outcomes <- stallResult{err: os.WriteFile(path, []byte("auto-title: true\n# a second line\n"), 0o600)}
		default:
			if state.CompareAndSwap(stallObserved, stallIdle) {
				outcomes <- stallResult{spoiled: true}
			}
		}
	}
	w.Start()
	t.Cleanup(w.Stop)

	for attempt := 1; ; attempt++ {
		state.Store(stallWatching)
		// Each attempt's first write is a length no earlier one had, so it is a change on the size
		// witness alone whatever the filesystem's timestamp granularity.
		writeWatchedFile(t, path, "auto-title: true\n"+strings.Repeat("#", attempt)+"\n")
		var res stallResult
		select {
		case res = <-outcomes:
		case <-time.After(testDeadline):
			t.Fatal("the poll never took a still sample after the first write")
		}
		if res.err != nil {
			t.Fatalf("write inside the stall: %v", res.err)
		}
		if !res.spoiled {
			break
		}
		awaitChange(t, w, "a first write that settled before the stall could be staged")
		if attempt == stallAttempts {
			t.Fatalf("all %d attempts settled the first write before a still tick inside its %v Settle", stallAttempts, testSettle)
		}
	}

	awaitChange(t, w, "a burst whose second write landed inside a stall")
	expectNoChange(t, w, testQuiet, "the burst had already been reported once")
}

// The size half of the change witness, pinned on its own: a rewrite that lands on the same mtime is
// exactly the case ADR 0041 rejected "mtime alone" for. Both files are stamped with a whole-second
// timestamp so the assertion holds on a filesystem of any timestamp granularity, and the new content
// arrives by rename so the watcher can never observe an intermediate state with a moved mtime.
func TestWatchReportsASameMtimeSizeChange(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	staged := filepath.Join(dir, "staged.yaml")
	stamp := time.Now().Add(-time.Hour).Truncate(time.Second)

	writeWatchedFile(t, path, "auto-title: false\n")
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatalf("stamp %s: %v", path, err)
	}

	w := startWatcher(t, path)

	writeWatchedFile(t, staged, "auto-title: false\n# longer\n")
	if err := os.Chtimes(staged, stamp, stamp); err != nil {
		t.Fatalf("stamp %s: %v", staged, err)
	}
	if err := os.Rename(staged, path); err != nil {
		t.Fatalf("rename onto %s: %v", path, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if !info.ModTime().Equal(stamp) {
		t.Fatalf("mtime = %v, want the stamped %v — this test only pins the size witness when the mtime does not move",
			info.ModTime(), stamp)
	}

	awaitChange(t, w, "a same-mtime rewrite of a different length")
}

// Editors that save by renaming leave the watched path missing for an instant. That absence is not a
// change and not a fatal error: the watch survives it, reports nothing for it, and reports the file
// that replaces it.
func TestWatchSurvivesADeleteAndRecreate(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	w := startWatcher(t, path)
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
	expectNoChange(t, w, testQuiet, "the file was merely absent")

	writeWatchedFile(t, path, "auto-title: true\n# recreated\n")
	awaitChange(t, w, "the recreated file")
}

// A watch can be started over a path that holds no file yet — apogee watches its config file from
// startup, and a first run may not have seeded one. Start takes the zero sample as its baseline
// (filewatch.go's Start), which no real file matches, so the file that appears later is the change:
// silence while it is absent, one report when it lands, and silence again after.
func TestWatchReportsAFileThatAppearsAfterStart(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")

	w := startWatcher(t, path)
	expectNoChange(t, w, testQuiet, "no file had ever existed at the watched path")

	writeWatchedFile(t, path, "auto-title: true\n")

	awaitChange(t, w, "a file that first appeared after Start")
	expectNoChange(t, w, testQuiet, "the appearing file had already been reported once")
}

// An idle apogee must stay idle. A poll that reported a file nobody touched would journal markers and
// reconnect MCP servers on its own schedule (ADR 0041's rejection of an unconditional re-apply).
func TestWatchStaysSilentOnAnUnchangedFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	w := startWatcher(t, path)
	expectNoChange(t, w, testQuiet, "nothing had touched the file")
}

// Stop's guarantee is structural rather than statistical: it waits for the poll goroutine to return
// before closing Changes, so once Stop has returned there is no goroutine left to report anything and
// the closed channel says the watch is over. Churning the file afterwards is the leak check — a
// surviving goroutine would answer.
func TestWatchStopEndsTheWatch(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	w := startWatcher(t, path)
	writeWatchedFile(t, path, "auto-title: true\n# changed\n")
	awaitChange(t, w, "a write before Stop")

	w.Stop()
	writeWatchedFile(t, path, "auto-title: false\n# changed again, after Stop\n")
	time.Sleep(testtime.Fixed(testSettle + 5*testInterval))

	select {
	case _, ok := <-w.Changes():
		if ok {
			t.Fatal("a change was reported after Stop returned")
		}
	case <-time.After(testDeadline):
		t.Fatal("Changes must be closed once Stop has returned")
	}
}

// Stop must not depend on anyone draining Changes. The consumer is a select in the composition root,
// and at teardown it has already stopped selecting — a watcher that parked its goroutine on an
// undrained send would wedge the shutdown it is being torn down by.
func TestWatchStopReturnsWithNobodyDraining(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	w := New(path)
	w.Interval = testInterval
	w.Settle = testSettle
	w.Start()

	writeWatchedFile(t, path, "auto-title: true\n# never read\n")
	time.Sleep(testtime.Fixed(testSettle + 5*testInterval))

	stopped := make(chan struct{})
	go func() {
		w.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(testDeadline):
		t.Fatal("Stop did not return with an undrained report pending")
	}

	// A second Stop is what a teardown running its closers twice would do.
	w.Stop()
}

// The reason this is a package rather than a type inside internal/config: the daemon watches
// `schedules.yaml` on the same mechanism (ADR 0034), so two watchers run side by side over two
// files. Each must answer for its own file only — a report that could have come from either would
// make the daemon re-read a file nobody edited, and would re-apply config.yaml on every schedule
// edit.
func TestTwoWatchersReportOnlyTheirOwnFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	schedulesPath := filepath.Join(dir, "schedules.yaml")
	writeWatchedFile(t, configPath, "auto-title: false\n")
	writeWatchedFile(t, schedulesPath, "schedules: []\n")

	configWatch := startWatcher(t, configPath)
	schedulesWatch := startWatcher(t, schedulesPath)

	// A burst on the second file: one report there, and nothing at all on the first.
	writeWatchedFile(t, schedulesPath, "schedules:\n")
	writeWatchedFile(t, schedulesPath, "schedules:\n  - name: nightly\n")
	awaitChange(t, schedulesWatch, "a burst of writes to the second file")
	expectNoChange(t, schedulesWatch, testQuiet, "the burst had already been reported once")
	expectNoChange(t, configWatch, testQuiet, "nothing had touched the first file")

	// And the first file still reports its own single write.
	writeWatchedFile(t, configPath, "auto-title: true\n")
	awaitChange(t, configWatch, "a single write to the first file")
	expectNoChange(t, schedulesWatch, testQuiet, "the second file had not changed again")
}

package filewatch

// The file watcher (ADR 0041 decision 3): what counts as a change, how a save's burst of
// writes becomes one report, what Stop guarantees, and that two watchers over two files each answer
// for their own. Every test drives a real watcher over a real file in t.TempDir() at a millisecond
// cadence — the mechanism is a poll and a clock, and a fake of either would test the fake.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The cadence the tests run the watcher at. The Settle window is generously wider than the Interval
// so that a burst of writes coalesces even on a loaded machine under -race, and the deadline is wide
// enough that a missed report is a failure of the watcher rather than of the box it runs on.
const (
	testInterval = 10 * time.Millisecond
	testSettle   = 150 * time.Millisecond
	testDeadline = 3 * time.Second
	testQuiet    = 400 * time.Millisecond
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
	writeWatchedFile(t, path, "")
	writeWatchedFile(t, path, "auto-title: tr")
	writeWatchedFile(t, path, "auto-title: true\n")

	awaitChange(t, w, "a burst of three writes")
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
	time.Sleep(testSettle + 5*testInterval)

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
	time.Sleep(testSettle + 5*testInterval)

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

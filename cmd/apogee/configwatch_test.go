package main

// The config file watcher (ADR 0041 decision 3): what counts as a change, how a save's burst of
// writes becomes one report, and what Stop guarantees. Every test drives a real watcher over a real
// file in t.TempDir() at a millisecond cadence — the mechanism is a poll and a clock, and a fake of
// either would test the fake.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The cadence the tests run the watcher at. The settle window is generously wider than the interval
// so that a burst of writes coalesces even on a loaded machine under -race, and the deadline is wide
// enough that a missed report is a failure of the watcher rather than of the box it runs on.
const (
	configWatchTestInterval = 10 * time.Millisecond
	configWatchTestSettle   = 150 * time.Millisecond
	configWatchTestDeadline = 3 * time.Second
	configWatchTestQuiet    = 400 * time.Millisecond
)

// startConfigWatcher starts a watcher over path at the test cadence and stops it with the test.
func startConfigWatcher(t *testing.T, path string) *configWatcher {
	t.Helper()
	w := newConfigWatcher(path)
	w.interval = configWatchTestInterval
	w.settle = configWatchTestSettle
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

// awaitConfigChange waits for one report, failing the test with why if none arrives.
func awaitConfigChange(t *testing.T, w *configWatcher, why string) {
	t.Helper()
	select {
	case _, ok := <-w.Changes():
		if !ok {
			t.Fatalf("Changes closed before %s was reported", why)
		}
	case <-time.After(configWatchTestDeadline):
		t.Fatalf("no change reported for %s", why)
	}
}

// expectNoConfigChange asserts that nothing is reported for the given window.
func expectNoConfigChange(t *testing.T, w *configWatcher, window time.Duration, why string) {
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
func TestConfigWatchReportsAWriteOnce(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	w := startConfigWatcher(t, path)
	writeWatchedFile(t, path, "auto-title: true\n# a second line\n")

	awaitConfigChange(t, w, "a single write")
	expectNoConfigChange(t, w, configWatchTestQuiet, "the file had already been reported once")
}

// An editor saving is a burst — write, truncate, rename — and each of those is a distinct file state
// a poll can land on. The settle delay exists so the burst is one apply, not three, the last two of
// which would be against a half-written document.
func TestConfigWatchCoalescesABurstIntoOneReport(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	w := startConfigWatcher(t, path)
	writeWatchedFile(t, path, "")
	writeWatchedFile(t, path, "auto-title: tr")
	writeWatchedFile(t, path, "auto-title: true\n")

	awaitConfigChange(t, w, "a burst of three writes")
	expectNoConfigChange(t, w, configWatchTestQuiet, "the burst had already been reported once")
}

// The size half of the change witness, pinned on its own: a rewrite that lands on the same mtime is
// exactly the case ADR 0041 rejected "mtime alone" for. Both files are stamped with a whole-second
// timestamp so the assertion holds on a filesystem of any timestamp granularity, and the new content
// arrives by rename so the watcher can never observe an intermediate state with a moved mtime.
func TestConfigWatchReportsASameMtimeSizeChange(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	staged := filepath.Join(dir, "staged.yaml")
	stamp := time.Now().Add(-time.Hour).Truncate(time.Second)

	writeWatchedFile(t, path, "auto-title: false\n")
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatalf("stamp %s: %v", path, err)
	}

	w := startConfigWatcher(t, path)

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

	awaitConfigChange(t, w, "a same-mtime rewrite of a different length")
}

// Editors that save by renaming leave the watched path missing for an instant. That absence is not a
// change and not a fatal error: the watch survives it, reports nothing for it, and reports the file
// that replaces it.
func TestConfigWatchSurvivesADeleteAndRecreate(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	w := startConfigWatcher(t, path)
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
	expectNoConfigChange(t, w, configWatchTestQuiet, "the file was merely absent")

	writeWatchedFile(t, path, "auto-title: true\n# recreated\n")
	awaitConfigChange(t, w, "the recreated file")
}

// An idle apogee must stay idle. A poll that reported a file nobody touched would journal markers and
// reconnect MCP servers on its own schedule (ADR 0041's rejection of an unconditional re-apply).
func TestConfigWatchStaysSilentOnAnUnchangedFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	w := startConfigWatcher(t, path)
	expectNoConfigChange(t, w, configWatchTestQuiet, "nothing had touched the file")
}

// Stop's guarantee is structural rather than statistical: it waits for the poll goroutine to return
// before closing Changes, so once Stop has returned there is no goroutine left to report anything and
// the closed channel says the watch is over. Churning the file afterwards is the leak check — a
// surviving goroutine would answer.
func TestConfigWatchStopEndsTheWatch(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	w := startConfigWatcher(t, path)
	writeWatchedFile(t, path, "auto-title: true\n# changed\n")
	awaitConfigChange(t, w, "a write before Stop")

	w.Stop()
	writeWatchedFile(t, path, "auto-title: false\n# changed again, after Stop\n")
	time.Sleep(configWatchTestSettle + 5*configWatchTestInterval)

	select {
	case _, ok := <-w.Changes():
		if ok {
			t.Fatal("a change was reported after Stop returned")
		}
	case <-time.After(configWatchTestDeadline):
		t.Fatal("Changes must be closed once Stop has returned")
	}
}

// Stop must not depend on anyone draining Changes. The consumer is a select in the composition root,
// and at teardown it has already stopped selecting — a watcher that parked its goroutine on an
// undrained send would wedge the shutdown it is being torn down by.
func TestConfigWatchStopReturnsWithNobodyDraining(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	writeWatchedFile(t, path, "auto-title: false\n")

	w := newConfigWatcher(path)
	w.interval = configWatchTestInterval
	w.settle = configWatchTestSettle
	w.Start()

	writeWatchedFile(t, path, "auto-title: true\n# never read\n")
	time.Sleep(configWatchTestSettle + 5*configWatchTestInterval)

	stopped := make(chan struct{})
	go func() {
		w.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(configWatchTestDeadline):
		t.Fatal("Stop did not return with an undrained report pending")
	}

	// A second Stop is what a teardown running its closers twice would do.
	w.Stop()
}

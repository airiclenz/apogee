package tuitest

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// parkedForTest blocks in a frame this package owns, which is exactly what a leaked driver
// goroutine looks like from the outside. It reports that it is parked first, so a test knows the
// goroutine is there to be found before it looks.
func parkedForTest(started chan<- struct{}, stop <-chan struct{}) {
	started <- struct{}{}
	<-stop
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

// The canned entries below are real debug=1 goroutine profile excerpts (Go 1.27, linux/arm64),
// which is the point: the parser matches frames in the form the profile prints them, and a canned
// profile in any other form would pass a parser that fails live.
const (
	profileHeader = "goroutine profile: total 5\n"

	// parkedEntry is a goroutine parked in this package's own frame, wearing check id 7.
	parkedEntry = "1 @ 0x97f38 0x26ff0 0x26b44 0x3e2b54 0x9f504\n" +
		"# labels: {\"tuitest.leakcheck\":\"7\"}\n" +
		"#\t0x3e2b53\tgithub.com/airiclenz/apogee/internal/tuitest.parkedForTest+0x43\t/workspace/repos/apogee/internal/tuitest/leak_test.go:16\n"

	// foldedEntry is the same stack the profile folded: three goroutines, one entry.
	foldedEntry = "3 @ 0x97f38 0x26ff0 0x26b44 0x3e2b54 0x9f504\n" +
		"# labels: {\"tuitest.leakcheck\":\"7\"}\n" +
		"#\t0x3e2b53\tgithub.com/airiclenz/apogee/internal/tuitest.parkedForTest+0x43\t/workspace/repos/apogee/internal/tuitest/leak_test.go:16\n"

	// unlabelledEntry is the same frame on a goroutine no check labelled.
	unlabelledEntry = "1 @ 0x97f38 0x26ff0 0x26b44 0x3e2b54 0x9f504\n" +
		"#\t0x3e2b53\tgithub.com/airiclenz/apogee/internal/tuitest.parkedForTest+0x43\t/workspace/repos/apogee/internal/tuitest/leak_test.go:16\n"

	// neighbourEntry wears another check's id — one whose id happens to contain this one's.
	neighbourEntry = "1 @ 0x97f38 0x26ff0 0x26b44 0x3e2b54 0x9f504\n" +
		"# labels: {\"tuitest.leakcheck\":\"17\"}\n" +
		"#\t0x3e2b53\tgithub.com/airiclenz/apogee/internal/tuitest.parkedForTest+0x43\t/workspace/repos/apogee/internal/tuitest/leak_test.go:16\n"

	// tickEntry is bubbletea's Tick parked on its timer, in the `+0x` form the profile prints.
	tickEntry = "1 @ 0x97f38 0x26ff0 0x26b44 0x22af48 0x9f504\n" +
		"# labels: {\"tuitest.leakcheck\":\"7\"}\n" +
		"#\t0x22af47\tcharm.land/bubbletea/v2.Tick.func1+0x37\t/root/go/pkg/mod/charm.land/bubbletea/v2@v2.0.8/commands.go:157\n"

	// runnerEntry is the goroutine a test in this package runs on — labelled, in-package, and
	// nobody's leak.
	runnerEntry = "1 @ 0x5553c 0x96cc4 0x1b2954 0x1b2600 0x1afe64 0x3e0420 0x12b3c4 0x9f504\n" +
		"# labels: {\"tuitest.leakcheck\":\"7\"}\n" +
		"#\t0x3e041f\tgithub.com/airiclenz/apogee/internal/tuitest.TestSomething+0x21f\t/workspace/repos/apogee/internal/tuitest/some_test.go:26\n" +
		"#\t0x12b3c3\ttesting.tRunner+0xc3\t/usr/local/go/src/testing/testing.go:2193\n"

	// checkerEntry is the goroutine reading the profile, should it still wear the id.
	checkerEntry = "1 @ 0x5553c 0x96cc4 0x1b2954 0x1b2600 0x1afe64 0x3e0420 0x12b3c4 0x9f504\n" +
		"# labels: {\"tuitest.leakcheck\":\"7\"}\n" +
		"#\t0x1afe63\truntime/pprof.(*Profile).WriteTo+0x143\t/usr/local/go/src/runtime/pprof/pprof.go:405\n" +
		"#\t0x3e041f\tgithub.com/airiclenz/apogee/internal/tuitest.goroutineProfile+0x21f\t/workspace/repos/apogee/internal/tuitest/leak.go:120\n"

	// outsideEntry wears the id but names no marked package: the standard library's own worker.
	outsideEntry = "1 @ 0x97f38 0x26ff0 0x26b44 0x2b1234 0x9f504\n" +
		"# labels: {\"tuitest.leakcheck\":\"7\"}\n" +
		"#\t0x2b1233\tnet/http.(*persistConn).readLoop+0x1234\t/usr/local/go/src/net/http/transport.go:2200\n"
)

// TestLeaksLabelledReadsTheProfile drives the parser with canned profiles: what wears the id and
// names a marked package is a leak, everything else is not.
func TestLeaksLabelledReadsTheProfile(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		profile    string
		goroutines int
	}{
		{"labelled stack found", profileHeader + parkedEntry, 1},
		{"a folded entry counts every goroutine in it", profileHeader + foldedEntry, 3},
		{"unlabelled ignored", profileHeader + unlabelledEntry, 0},
		{"another check's id ignored", profileHeader + neighbourEntry, 0},
		{"Tick timer frame ignored", profileHeader + tickEntry, 0},
		{"the test runner ignored", profileHeader + runnerEntry, 0},
		{"the checker ignored", profileHeader + checkerEntry, 0},
		{"an unmarked package ignored", profileHeader + outsideEntry, 0},
		{"the header line does not hide the first entry", profileHeader + parkedEntry + "\n" + unlabelledEntry, 1},
		{"every entry of a full profile is read", profileHeader + runnerEntry + "\n" + tickEntry + "\n" +
			parkedEntry + "\n" + neighbourEntry + "\n" + foldedEntry, 4},
		{"an empty profile", "goroutine profile: total 0\n", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			left := leaksLabelled([]byte(tc.profile), "7")
			if got := goroutinesIn(left); got != tc.goroutines {
				t.Errorf("leaksLabelled(...) stands for %d goroutine(s), want %d:\n%s", got, tc.goroutines, stacksOf(left))
			}
			for _, l := range left {
				if strings.HasPrefix(l.stack, "goroutine profile:") {
					t.Errorf("a reported entry still carries the profile header:\n%s", l.stack)
				}
			}
		})
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

// TestCheckLeaksIgnoresWhatWasRunningBefore: a goroutine already parked when CheckLeaks is called
// belongs to whoever started it — a parallel neighbour, or the process — and carries no id the
// check could blame it by.
func TestCheckLeaksIgnoresWhatWasRunningBefore(t *testing.T) {
	t.Parallel()

	started, stop := make(chan struct{}), make(chan struct{})
	defer close(stop)
	go parkedForTest(started, stop)
	<-started

	rec := &recordingTB{TB: t}
	CheckLeaks(rec)
	rec.runCleanups()

	if len(rec.reports) != 0 {
		t.Errorf("CheckLeaks blamed a goroutine parked before it was called:\n%s", strings.Join(rec.reports, "\n"))
	}
}

// TestCheckLeaksBlamesOnlyTheLeakingSubtest is the attribution the labels buy: two checked
// tests run at once, one leaks, and the report lands on that one alone — the clean neighbour's
// cleanup looks at the same profile and sees nothing of its own.
//
// The two are goroutines of this test's own rather than parallel subtests. A subtest that waits
// on its sibling deadlocks under `-parallel 1` — the bound scripts/test-shards.sh sets on every
// process it launches — because the sibling never gets the one slot the waiter is holding; it
// stalled the sharded suite for the full test timeout. A goroutine wears its labels exactly as a
// subtest's goroutine does (CheckLeaks labels the goroutine it is called on, and what that
// goroutine starts inherits them), so the attribution under test is the same.
func TestCheckLeaksBlamesOnlyTheLeakingSubtest(t *testing.T) {
	t.Parallel()

	stop, leaking := make(chan struct{}), make(chan struct{})
	defer close(stop)

	leaker, clean := &recordingTB{TB: t}, &recordingTB{TB: t}
	var wg sync.WaitGroup
	wg.Add(2)

	go func() { // leaks two goroutines
		defer wg.Done()
		CheckLeaks(leaker)

		started := make(chan struct{})
		go parkedForTest(started, stop)
		<-started
		go parkedForTest(started, stop)
		<-started
		close(leaking)

		leaker.runCleanups()
	}()

	go func() { // leaks nothing
		defer wg.Done()
		CheckLeaks(clean)
		// Look only once the neighbour's goroutines are parked, or there is nothing to misattribute.
		<-leaking
		clean.runCleanups()
	}()

	wg.Wait()

	if len(leaker.reports) != 1 {
		t.Fatalf("CheckLeaks reported %d time(s), want 1:\n%s", len(leaker.reports), strings.Join(leaker.reports, "\n"))
	}
	report := leaker.reports[0]
	if !strings.HasPrefix(report, "2 goroutine(s) outlived the test by "+leakGrace.String()) {
		t.Errorf("the report does not open with the count of what leaked:\n%s", report)
	}
	if !strings.Contains(report, "tuitest.parkedForTest+0x") {
		t.Errorf("the report does not name the parked frame:\n%s", report)
	}
	if len(clean.reports) != 0 {
		t.Errorf("the clean test was blamed for its neighbour's leak:\n%s", strings.Join(clean.reports, "\n"))
	}
}

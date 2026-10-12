package tuitest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/testtime"
)

// TestKitBudgetsAreScaledBudgets: the kit's deadlines are budgets, so they follow
// APOGEE_TEST_TIME_SCALE like every other one, and the leak grace is the wait deadline itself
// rather than a second, separately scaled copy.
func TestKitBudgetsAreScaledBudgets(t *testing.T) {
	t.Parallel()

	if want := testtime.Budget(60 * time.Second); DefaultTimeout != want {
		t.Errorf("DefaultTimeout = %s, want testtime.Budget(60s) = %s", DefaultTimeout, want)
	}
	if want := testtime.Budget(5 * time.Second); teardownTimeout != want {
		t.Errorf("teardownTimeout = %s, want testtime.Budget(5s) = %s", teardownTimeout, want)
	}
	if leakGrace != DefaultTimeout {
		t.Errorf("leakGrace = %s, want DefaultTimeout = %s", leakGrace, DefaultTimeout)
	}
}

// TestWaitForReturnsAsSoonAsTheConditionHolds: the wait is what keeps the e2e budget; it must not
// cost its whole deadline for a condition that lands early.
func TestWaitForReturnsAsSoonAsTheConditionHolds(t *testing.T) {
	t.Parallel()

	start := time.Now()
	landed := testtime.Expiry(testtime.Fixed(60 * time.Millisecond))
	WaitFor(t, landed, Within(testtime.Budget(3*time.Second)), Awaiting("the landing"))
	if elapsed := time.Since(start); elapsed > testtime.Budget(time.Second) {
		t.Errorf("WaitFor took %s for a condition that held after 60ms", elapsed)
	}
}

// TestWaitFailureCarriesTheFrameAndItsStyles: what a timed-out wait says. The plain frame and the
// styled render are both in the message, so a colour bug is visible in the failure output rather
// than only in a rerun with a debugger.
func TestWaitFailureCarriesTheFrameAndItsStyles(t *testing.T) {
	t.Parallel()

	s := NewScreen(12, 2)
	t.Cleanup(s.Close)
	if _, err := s.Write([]byte("\x1b[31mred\x1b[0m ok")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	msg := waiter{screen: s, timeout: 250 * time.Millisecond, what: "the approval pane"}.failure("TestX")
	for _, want := range []string{"timed out after 250ms", "the approval pane", "red ok", "\x1b[31mred"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the failure message does not carry %q:\n%s", want, msg)
		}
	}
}

// TestWaitFailureWithoutAScreenSaysSo: a wait nobody gave a screen to still has to be diagnosable,
// and the diagnosis is "you forgot On(screen)".
func TestWaitFailureWithoutAScreenSaysSo(t *testing.T) {
	t.Parallel()

	msg := waiter{timeout: time.Second, what: "something"}.failure("TestX")
	if !strings.Contains(msg, "tuitest.On(screen)") {
		t.Errorf("the screen-less failure does not say how to get a frame:\n%s", msg)
	}
}

// timeoutRecorder stands in for the *testing.T of a wait that times out, so a test can read back what
// [WaitFor] reported instead of failing on it. The real T is embedded — testing.TB has an unexported
// method — and only Fatalf is taken over; it records and returns, so the caller sees every report.
type timeoutRecorder struct {
	testing.TB
	reports []string
}

func (r *timeoutRecorder) Fatalf(format string, args ...any) {
	r.reports = append(r.reports, fmt.Sprintf(format, args...))
}

// TestWaitForTimeoutReportsThroughTheWaitersFailure: a wait that runs out on a screen fails the test
// once, with the waiter's own failure — the frame plain and styled — not a bare timeout.
func TestWaitForTimeoutReportsThroughTheWaitersFailure(t *testing.T) {
	t.Parallel()

	s := NewScreen(12, 2)
	t.Cleanup(s.Close)
	if _, err := s.Write([]byte("\x1b[31mred\x1b[0m ok")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	timeout := testtime.Fixed(100 * time.Millisecond)
	rec := &timeoutRecorder{TB: t}
	WaitFor(rec, func() bool { return false }, On(s), Within(timeout), Awaiting("the approval pane"))

	if len(rec.reports) != 1 {
		t.Fatalf("WaitFor reported %d failures, want 1: %q", len(rec.reports), rec.reports)
	}
	want := waiter{screen: s, timeout: timeout, what: "the approval pane"}.failure(t.Name())
	if rec.reports[0] != want {
		t.Errorf("the timeout report is not the waiter's failure:\ngot:\n%s\nwant:\n%s", rec.reports[0], want)
	}
	for _, part := range []string{"timed out after 100ms", "red ok", "\x1b[31mred"} {
		if !strings.Contains(rec.reports[0], part) {
			t.Errorf("the timeout report does not carry %q:\n%s", part, rec.reports[0])
		}
	}
}

// TestWaitArtifactsAreWrittenWhenTheDirIsSet:TUITEST_ARTIFACTS is the CI half — the frame lands
// beside the run as two files, plain and styled, named for the test.
func TestWaitArtifactsAreWrittenWhenTheDirIsSet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "artifacts")
	t.Setenv(artifactsEnv, dir)

	s := NewScreen(12, 2)
	t.Cleanup(s.Close)
	if _, err := s.Write([]byte("\x1b[31mred\x1b[0m ok")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	msg := waiter{screen: s, timeout: time.Second, what: "a pane"}.failure("TestSuite/a sub test")
	if !strings.Contains(msg, "frame written to") {
		t.Errorf("the failure does not name the artifacts it wrote:\n%s", msg)
	}
	base := filepath.Join(dir, "TestSuite_a_sub_test")
	plain, err := os.ReadFile(base + ".txt")
	if err != nil {
		t.Fatalf("read the plain artifact: %v", err)
	}
	if got, want := string(plain), "red ok\n"; got != want {
		t.Errorf("plain artifact = %q, want %q", got, want)
	}
	styled, err := os.ReadFile(base + ".ansi")
	if err != nil {
		t.Fatalf("read the styled artifact: %v", err)
	}
	if !strings.Contains(string(styled), "\x1b[31m") {
		t.Errorf("styled artifact = %q, want the SGR sequences intact", styled)
	}
}

// TestWaitTextAndWaitGone: the two shorthands every e2e test is written with.
func TestWaitTextAndWaitGone(t *testing.T) {
	t.Parallel()

	s := NewScreen(20, 2)
	t.Cleanup(s.Close)
	go func() {
		time.Sleep(testtime.Fixed(40 * time.Millisecond))
		_, _ = s.Write([]byte("approve?"))
		time.Sleep(testtime.Fixed(40 * time.Millisecond))
		_, _ = s.Write([]byte("\x1b[2J\x1b[Hdone"))
	}()
	WaitText(t, s, "approve?")
	WaitGone(t, s, "approve?")
	WaitQuiet(t, s, 50*time.Millisecond)
}

// TestFailingWaitPrintsTheStyledFrameOfAnIdleScreen: the polls of a wait that times out build the
// frame once and no styled render; the failure is what builds it, exactly once, and prints it.
func TestFailingWaitPrintsTheStyledFrameOfAnIdleScreen(t *testing.T) {
	t.Parallel()

	s := NewScreen(12, 2)
	t.Cleanup(s.Close)
	if _, err := s.Write([]byte("\x1b[31mred\x1b[0m")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	rec := &timeoutRecorder{TB: t}
	WaitFor(rec, func() bool {
		_, _, ok := s.Snapshot().Find("never")
		return ok
	}, On(s), Within(testtime.Fixed(100*time.Millisecond)), Awaiting("text that never paints"))
	if len(rec.reports) != 1 || !strings.Contains(rec.reports[0], "\x1b[31mred") {
		t.Fatalf("the failing wait reported %q, want one report carrying the styled frame", rec.reports)
	}
	if frames, styled := builds(s); frames != 1 || styled != 1 {
		t.Errorf("a failed wait on an idle screen built %d frames and %d styled renders, want 1 and 1",
			frames, styled)
	}
}

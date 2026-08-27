package tuitest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWaitForReturnsAsSoonAsTheConditionHolds: the wait is what keeps the e2e budget; it must not
// cost its whole deadline for a condition that lands early.
func TestWaitForReturnsAsSoonAsTheConditionHolds(t *testing.T) {
	t.Parallel()

	landed := time.Now().Add(60 * time.Millisecond)
	start := time.Now()
	WaitFor(t, func() bool { return time.Now().After(landed) }, Within(3*time.Second), Awaiting("the landing"))
	if elapsed := time.Since(start); elapsed > time.Second {
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

// TestWaitArtifactsAreWrittenWhenTheDirIsSet: TUITEST_ARTIFACTS is the CI half — the frame lands
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
		time.Sleep(40 * time.Millisecond)
		_, _ = s.Write([]byte("approve?"))
		time.Sleep(40 * time.Millisecond)
		_, _ = s.Write([]byte("\x1b[2J\x1b[Hdone"))
	}()
	WaitText(t, s, "approve?")
	WaitGone(t, s, "approve?")
	WaitQuiet(t, s, 50*time.Millisecond)
}

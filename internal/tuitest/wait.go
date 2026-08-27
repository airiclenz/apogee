package tuitest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A driver test never sleeps. It waits for a condition with a deadline, because the thing it is
// waiting for — a frame, a tool block, a file on disk — arrives when the real composition gets
// there, and a sleep long enough to be reliable on a loaded CI runner is a sleep that costs the
// whole suite its budget.

const (
	// DefaultTimeout is how long [WaitFor] waits before it gives up.
	DefaultTimeout = 5 * time.Second
	// pollInterval is how often the condition is re-asked. Fast enough that a wait costs about
	// what it needs to, slow enough that a busy condition is not a spin loop.
	pollInterval = 20 * time.Millisecond
	// artifactsEnv names a directory failures write their frames into, for a CI run where nobody
	// is watching the output live.
	artifactsEnv = "TUITEST_ARTIFACTS"
)

// Option configures a [WaitFor].
type Option func(*waiter)

// On names the screen whose frame is printed when the wait times out — plain, and again with its
// styles intact. Without it a failure can only say that the wait timed out, which is the least
// useful half of what it knows.
func On(s *Screen) Option { return func(w *waiter) { w.screen = s } }

// Within overrides [DefaultTimeout] for one wait.
func Within(d time.Duration) Option { return func(w *waiter) { w.timeout = d } }

// Awaiting names what is being waited for, in the words the failure should use.
func Awaiting(what string) Option { return func(w *waiter) { w.what = what } }

// waiter is the resolved configuration of one wait.
type waiter struct {
	screen  *Screen
	timeout time.Duration
	what    string
}

// WaitFor polls cond until it holds or the deadline passes, and fails the test when it does not.
// The failure carries the last frame — the whole point of waiting on a screen rather than on a
// channel is that when it goes wrong you get to see what the terminal actually showed.
func WaitFor(t testing.TB, cond func() bool, opts ...Option) {
	t.Helper()

	w := waiter{timeout: DefaultTimeout, what: "the condition to hold"}
	for _, opt := range opts {
		opt(&w)
	}
	deadline := time.Now().Add(w.timeout)
	for {
		if cond() {
			return
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(pollInterval)
	}
	if cond() { // one last look, so a condition that landed during the final sleep still counts
		return
	}
	w.fail(t)
}

// WaitText waits for text to appear anywhere on the screen.
func WaitText(t testing.TB, s *Screen, text string) {
	t.Helper()

	WaitFor(t, func() bool {
		_, _, ok := s.Snapshot().Find(text)
		return ok
	}, On(s), Awaiting(fmt.Sprintf("%q on screen", text)))
}

// WaitGone waits for text to leave the screen — the assertion a pane that must CLOSE needs, and
// the one a test most often forgets to make.
func WaitGone(t testing.TB, s *Screen, text string) {
	t.Helper()

	WaitFor(t, func() bool {
		_, _, ok := s.Snapshot().Find(text)
		return !ok
	}, On(s), Awaiting(fmt.Sprintf("%q to leave the screen", text)))
}

// WaitQuiet waits for the screen to settle: no bytes painted for d. It is what a test does before
// taking a golden, so the frame it pins is a finished one.
func WaitQuiet(t testing.TB, s *Screen, d time.Duration) {
	t.Helper()

	WaitFor(t, func() bool { return s.Quiet(d) }, On(s),
		Awaiting(fmt.Sprintf("the screen to be quiet for %s", d)))
}

// fail ends the test with everything the wait knows.
func (w waiter) fail(t testing.TB) {
	t.Helper()

	t.Fatalf("%s", w.failure(t.Name()))
}

// failure is the whole of what a timed-out wait has to say: what it was waiting for, the last
// frame plain, and the same frame with its styles intact — because half the bugs a driver test
// catches are colour, and a plain frame shows none of them. When TUITEST_ARTIFACTS names a
// directory the two are also written there, for a CI run nobody is watching live.
func (w waiter) failure(name string) string {
	if w.screen == nil {
		return fmt.Sprintf(
			"timed out after %s waiting for %s (no screen was named — pass tuitest.On(screen) to see the frame)",
			w.timeout, w.what)
	}
	frame := w.screen.Snapshot()
	plain, styled := frame.String(), frame.Styled()
	msg := fmt.Sprintf("timed out after %s waiting for %s", w.timeout, w.what)
	if note := writeArtifacts(name, plain, styled); note != "" {
		msg += "\n" + note
	}
	return msg + fmt.Sprintf("\n\n--- frame ---\n%s\n\n--- frame, styled ---\n%s\n", plain, styled)
}

// writeArtifacts saves the frame beside the run when TUITEST_ARTIFACTS names a directory, and
// returns the line the test should log about it (empty when the variable is unset).
func writeArtifacts(name, plain, styled string) string {
	dir := os.Getenv(artifactsEnv)
	if dir == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Sprintf("%s=%s: %v", artifactsEnv, dir, err)
	}
	base := filepath.Join(dir, artifactName(name))
	for _, f := range []struct {
		path string
		text string
	}{{base + ".txt", plain}, {base + ".ansi", styled}} {
		if err := os.WriteFile(f.path, []byte(f.text), 0o644); err != nil {
			return fmt.Sprintf("%s: %v", f.path, err)
		}
	}
	return "frame written to " + base + ".{txt,ansi}"
}

// artifactName turns a test name into one path component; subtests are slash-separated and would
// otherwise ask for directories nobody created.
func artifactName(name string) string {
	return strings.NewReplacer("/", "_", string(filepath.Separator), "_", " ", "_").Replace(name)
}

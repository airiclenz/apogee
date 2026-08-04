package schedule

// The fakes every policy test in this package runs on. Time is faked because the policy is
// about time — a test that waited for a real 30-second cycle would prove the machine's clock
// works, not that a tick landing on a busy Schedule is skipped. The only real durations left
// are synchronisation patience (testWait) and the short windows in which an event must NOT
// appear, which is as real as absence can be observed.

import (
	"sync"
	"testing"
	"time"
)

// testWait is how long a test waits for an Event that should already be on its way. It is
// generous because it bounds a scheduling delay under -race, never a policy interval.
const testWait = 3 * time.Second

// quietWait is how long a test watches for an Event that must NOT arrive.
const quietWait = 200 * time.Millisecond

// fakeClock is a Clock whose ticks a test delivers by hand and whose Now only moves when the
// test moves it.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers []*fakeTicker
}

// newFakeClock starts a fake clock at a fixed instant.
func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)}
}

// Now reports the fake time.
func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// NewTicker registers a ticker the test can fire. Its channel is buffered exactly like
// time.Ticker's, so delivering a tick never blocks the test goroutine.
func (c *fakeClock) NewTicker(d time.Duration) Ticker {
	t := &fakeTicker{ch: make(chan time.Time, 1), every: d}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tickers = append(c.tickers, t)
	return t
}

// advance moves the fake time forward without delivering any tick — the two are separate
// levers, so a test can pin a next-fire time without firing anything.
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// tick delivers one tick to the nth ticker in creation order (which is Add order). It fails
// the test if the previous tick has not been consumed yet, because a test that outruns the
// loop is asserting on the wrong tick.
func (c *fakeClock) tick(t *testing.T, n int) {
	t.Helper()
	c.mu.Lock()
	if n >= len(c.tickers) {
		c.mu.Unlock()
		t.Fatalf("tick(%d): only %d tickers exist", n, len(c.tickers))
	}
	ticker, now := c.tickers[n], c.now
	c.mu.Unlock()

	select {
	case ticker.ch <- now:
	default:
		t.Fatalf("tick(%d): the previous tick was never consumed", n)
	}
}

// tickers reports every ticker created so far.
func (c *fakeClock) allTickers() []*fakeTicker {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*fakeTicker(nil), c.tickers...)
}

// fakeTicker is one Schedule's cadence under the fake clock.
type fakeTicker struct {
	ch    chan time.Time
	every time.Duration

	mu      sync.Mutex
	stopped bool
}

// C is the channel the test delivers ticks on.
func (t *fakeTicker) C() <-chan time.Time { return t.ch }

// Stop records that the loop released its cadence — the proof a loop goroutine exited.
func (t *fakeTicker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
}

// isStopped reports whether Stop has been called.
func (t *fakeTicker) isStopped() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stopped
}

// recorder is the Notify seam: it keeps every Event and hands them out in arrival order, so
// a test asserts on the exact sequence rather than on a set.
type recorder struct {
	ch chan Event

	mu  sync.Mutex
	all []Event
}

// newRecorder returns a recorder whose buffer is far larger than any test's traffic, so
// Notify never blocks a Scheduler goroutine.
func newRecorder() *recorder { return &recorder{ch: make(chan Event, 128)} }

// notify is the seam handed to Config.Notify.
func (r *recorder) notify(e Event) {
	r.mu.Lock()
	r.all = append(r.all, e)
	r.mu.Unlock()
	r.ch <- e
}

// next asserts that the next Event to arrive is of kind want, and returns it. It is
// deliberately strict about order: every wait in these tests is also an ordering assertion.
func (r *recorder) next(t *testing.T, want EventKind) Event {
	t.Helper()
	select {
	case e := <-r.ch:
		if e.Kind != want {
			t.Fatalf("next event = %q (schedule %q), want %q", e.Kind, e.ScheduleID, want)
		}
		return e
	case <-time.After(testWait):
		t.Fatalf("timed out waiting for a %q event", want)
		return Event{}
	}
}

// quiet asserts that no Event arrives for quietWait — the only honest way to observe that
// something did NOT happen.
func (r *recorder) quiet(t *testing.T) {
	t.Helper()
	select {
	case e := <-r.ch:
		t.Fatalf("unexpected %q event for schedule %q", e.Kind, e.ScheduleID)
	case <-time.After(quietWait):
	}
}

// signalled waits for one send on ch, failing the test if it never comes. It is how a test
// observes a seam being ENTERED, which no Event reports (a Gate that is waiting has not
// fired yet, and that is the whole point of the deferral tests).
func signalled(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(testWait):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// eventually polls cond until it holds. It exists for the one state change no Event reports:
// the in-flight flag clears in a deferred call, just after the completed Event goes out, so a
// surface can observe the Event marginally before List agrees.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testWait)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// kinds reports every Event's kind so far, in arrival order.
func (r *recorder) kinds() []EventKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]EventKind, 0, len(r.all))
	for _, e := range r.all {
		out = append(out, e.Kind)
	}
	return out
}

// count reports how many Events of one kind arrived.
func (r *recorder) count(kind EventKind) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.all {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

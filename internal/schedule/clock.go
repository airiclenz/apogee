package schedule

import "time"

// Clock is the Scheduler's whole sense of time: what the time is now, and the cadence a
// Schedule ticks on. Both halves live in one interface on purpose (see the package doc) —
// a test that pins Now must pin the ticks too, or its "next fire" assertions measure the
// machine clock instead of the policy.
type Clock interface {
	// Now reports the current time, the origin every next-fire time is computed from.
	Now() time.Time
	// NewTicker returns a Ticker that delivers a tick every d until it is stopped.
	NewTicker(d time.Duration) Ticker
}

// Ticker is one Schedule's cadence. It is time.Ticker's shape narrowed to what the loop
// uses, so a fake can deliver a tick the instant a test asks for one.
//
// A ticker is the honest model for the skip policy: the cadence is fixed and keeps running
// while a Firing is in flight, which is exactly what makes an overlapping tick a SKIP with
// its own event rather than a silently postponed run.
type Ticker interface {
	// C is the channel ticks arrive on.
	C() <-chan time.Time
	// Stop ends the cadence. It never closes C.
	Stop()
}

// systemClock is the production Clock: the wall clock and real time.Tickers.
type systemClock struct{}

// Now reports the wall-clock time.
func (systemClock) Now() time.Time { return time.Now() }

// NewTicker starts a real time.Ticker.
func (systemClock) NewTicker(d time.Duration) Ticker { return &systemTicker{t: time.NewTicker(d)} }

// systemTicker adapts *time.Ticker to the Ticker seam.
type systemTicker struct{ t *time.Ticker }

// C is the underlying ticker's channel.
func (t *systemTicker) C() <-chan time.Time { return t.t.C }

// Stop ends the underlying ticker.
func (t *systemTicker) Stop() { t.t.Stop() }

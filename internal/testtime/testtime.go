// Package testtime is the single home for timing in tests.
//
// A wall-clock duration in a test is one of two things. Either it is a budget — a deadline or a
// wait that ends on a condition, which a loaded or throttled box needs more of — or it is a
// semantic duration whose length is the claim under test: pacing, a "nothing happens within"
// window, a timeout meant to expire, a fixture's age. The rule is that every timing in a test
// says which it is, by going through [Budget] or [Fixed], and that every poll goes through
// [Eventually] (or, where the poll cannot fail the test itself, through [Expiry]).
//
// [Budget] scales by APOGEE_TEST_TIME_SCALE, read once per process: a number above 0, 1 when
// unset. [Fixed] never scales. Neither do [Eventually] nor [Expiry]: their duration argument is
// an already-budgeted value — a Budget or Fixed call, or an identifier initialised from one such
// as tuitest.DefaultTimeout — so a budget is scaled exactly once, where it is written.
//
// The package imports nothing from the module, so every package's tests can use it.
package testtime

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

const (
	// scaleEnv names the variable that scales every [Budget].
	scaleEnv = "APOGEE_TEST_TIME_SCALE"
	// defaultScale is the scale when the variable is unset or empty.
	defaultScale = 1.0
	// pollInterval is how often [Eventually] re-asks its condition: fast enough that a wait costs
	// about what it needs to, slow enough that a busy condition is not a spin loop.
	pollInterval = 20 * time.Millisecond
)

// scaleOnce guards the single read of [scaleEnv]; scaleValue and scaleErr hold its outcome.
var (
	scaleOnce  sync.Once
	scaleValue float64
	scaleErr   error
)

// Scale reports the factor every [Budget] is multiplied by, read from APOGEE_TEST_TIME_SCALE
// once per process. An unset or empty variable means 1. A value that does not parse to a finite
// number above 0 panics on every call, naming the variable and the value — a misspelt scale is a
// broken test run, not a default.
func Scale() float64 {
	scaleOnce.Do(func() {
		scaleValue, scaleErr = parseScale(os.Getenv(scaleEnv))
	})
	if scaleErr != nil {
		panic(scaleErr)
	}
	return scaleValue
}

// parseScale turns the variable's raw value into a scale.
func parseScale(raw string) (float64, error) {
	if raw == "" {
		return defaultScale, nil
	}
	scale, err := strconv.ParseFloat(raw, 64)
	if err != nil || !(scale > 0) || math.IsInf(scale, 1) {
		return 0, fmt.Errorf("testtime: %s=%q is not a finite number above 0", scaleEnv, raw)
	}
	return scale, nil
}

// Budget returns d scaled by [Scale]. It is for deadlines and waits that end on a condition,
// where a slow box needs more time and a healthy run spends only what it needs.
func Budget(d time.Duration) time.Duration {
	return scaled(d, Scale())
}

// scaled multiplies d by scale.
func scaled(d time.Duration, scale float64) time.Duration {
	return time.Duration(float64(d) * scale)
}

// Fixed returns d unchanged. It marks a semantic duration — pacing, a "nothing happens within"
// window, a timeout meant to expire, a fixture's age — whose length is part of what the test
// claims, so scaling it would change the claim.
func Fixed(d time.Duration) time.Duration {
	return d
}

// Eventually polls cond every 20 ms until it holds, and fails the test with t.Fatalf — "timed out
// after <budget> waiting for <what>" — when it has not held by the time budget has elapsed. The
// budget is used as passed, never scaled (see the package doc). It returns after the failure, so a
// fake testing.TB whose Fatalf does not stop the goroutine sees exactly one report.
func Eventually(t testing.TB, budget time.Duration, cond func() bool, what string) {
	t.Helper()

	expired := Expiry(budget)
	for !cond() {
		if expired() {
			if cond() { // one last look, so a condition that landed during the final sleep counts
				return
			}
			t.Fatalf("timed out after %s waiting for %s", budget, what)
			return
		}
		time.Sleep(pollInterval)
	}
}

// Expiry returns a deadline that does not fail anything: the function it returns reports false
// until budget has elapsed since Expiry was called, and true from then on. It is for polls that
// cannot fail the test themselves — code off the test goroutine, or a helper that reports its own
// failure. Like [Eventually], it uses the budget as passed and never scales it.
func Expiry(budget time.Duration) func() bool {
	deadline := time.Now().Add(budget)
	return func() bool { return !time.Now().Before(deadline) }
}

package testtime

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// recordingTB stands in for the *testing.T of a wait that times out, so a test can read back what
// Eventually reported instead of failing on it. testing.TB cannot be implemented outside the
// testing package — it has an unexported method — so the real T is embedded and only Fatalf is
// taken over.
type recordingTB struct {
	testing.TB
	reports []string
}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.reports = append(r.reports, fmt.Sprintf(format, args...))
}

func TestParseScale(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw     string
		want    float64
		isValid bool
	}{
		{raw: "", want: 1, isValid: true},
		{raw: "2", want: 2, isValid: true},
		{raw: "0.5", want: 0.5, isValid: true},
		{raw: "0", isValid: false},
		{raw: "-1", isValid: false},
		{raw: "x", isValid: false},
		{raw: "NaN", isValid: false},
		{raw: "Inf", isValid: false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.raw), func(t *testing.T) {
			t.Parallel()

			got, err := parseScale(tc.raw)

			if !tc.isValid {
				if err == nil {
					t.Fatalf("parseScale(%q) = %v, want an error", tc.raw, got)
				}
				if msg := err.Error(); !strings.Contains(msg, scaleEnv) || !strings.Contains(msg, fmt.Sprintf("%q", tc.raw)) {
					t.Errorf("parseScale(%q) error %q does not name %s and the value", tc.raw, msg, scaleEnv)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseScale(%q) failed: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("parseScale(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestScaledMultipliesByTheScale(t *testing.T) {
	t.Parallel()

	cases := []struct {
		scale float64
		want  time.Duration
	}{
		{scale: 1, want: time.Second},
		{scale: 2, want: 2 * time.Second},
		{scale: 0.5, want: 500 * time.Millisecond},
	}
	for _, tc := range cases {
		if got := scaled(time.Second, tc.scale); got != tc.want {
			t.Errorf("scaled(1s, %v) = %s, want %s", tc.scale, got, tc.want)
		}
	}
}

// TestBudgetScalesByTheProcessScale holds at any APOGEE_TEST_TIME_SCALE: Scale has no setter, so
// the claim is that Budget applies whatever scale this process runs at.
func TestBudgetScalesByTheProcessScale(t *testing.T) {
	t.Parallel()

	const d = 3 * time.Second

	if got, want := Budget(d), scaled(d, Scale()); got != want {
		t.Errorf("Budget(%s) = %s, want %s at scale %v", d, got, want, Scale())
	}
}

func TestFixedIsTheIdentity(t *testing.T) {
	t.Parallel()

	for _, d := range []time.Duration{0, time.Millisecond, 3 * time.Second, time.Hour} {
		if got := Fixed(d); got != d {
			t.Errorf("Fixed(%s) = %s, want it unchanged", d, got)
		}
	}
}

func TestEventuallyReturnsOnceTheConditionHolds(t *testing.T) {
	t.Parallel()

	rec := &recordingTB{TB: t}
	calls := 0

	Eventually(rec, Budget(5*time.Second), func() bool {
		calls++
		return calls >= 3
	}, "the third call")

	if len(rec.reports) != 0 {
		t.Errorf("Eventually failed on a condition that held: %q", rec.reports)
	}
	if calls < 3 {
		t.Errorf("Eventually returned after %d calls, before the condition held", calls)
	}
}

// TestEventuallyTimesOutNamingTheBudgetAsPassed drives the timeout through a fake TB. The budget is
// named as passed, never scaled: a caller's budget is scaled once, where it is written.
func TestEventuallyTimesOutNamingTheBudgetAsPassed(t *testing.T) {
	t.Parallel()

	rec := &recordingTB{TB: t}
	budget := Fixed(30 * time.Millisecond)

	Eventually(rec, budget, func() bool { return false }, "the flag")

	want := []string{"timed out after 30ms waiting for the flag"}
	if strings.Join(rec.reports, "\n") != strings.Join(want, "\n") {
		t.Errorf("Eventually reported %q, want %q", rec.reports, want)
	}
}

func TestExpiryIsFalseBeforeItsBudget(t *testing.T) {
	t.Parallel()

	expired := Expiry(Fixed(time.Hour))

	if expired() {
		t.Error("Expiry reported true before its budget had elapsed")
	}
}

// TestExpiryIsTrueAfterItsBudgetUnscaled waits until the budget, as passed, has elapsed and then
// reads the expiry: a scaled expiry would still read false at any scale above 1.
func TestExpiryIsTrueAfterItsBudgetUnscaled(t *testing.T) {
	t.Parallel()

	budget := Fixed(50 * time.Millisecond)
	expired := Expiry(budget)
	start := time.Now() // after the call, so start+budget is at or past the expiry's deadline

	Eventually(t, Budget(10*time.Second), func() bool { return time.Since(start) >= budget }, "the budget to elapse")

	if !expired() {
		t.Errorf("Expiry(%s) still reported false once %s had elapsed", budget, budget)
	}
}

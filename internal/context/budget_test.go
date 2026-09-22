package context

import (
	"math"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestAllocate_ReserveHonouredAndPartsSum pins the allocation arithmetic on the UNMEASURED path:
// the response reserve is held back, each standing part keeps the fixed fraction it reserved before
// the Budget could measure, and the four parts sum to the window exactly (no rounding drift, so ≤-window holds).
func TestAllocate_ReserveHonouredAndPartsSum(t *testing.T) {
	cases := []struct {
		name     string
		window   int
		reserve  int
		fraction float64
	}{
		{"explicit reserve", 8192, 2048, 0},
		{"default reserve (zero ⇒ fraction)", 8192, 0, 0},
		{"tiny window", 10, 0, 0},
		{"odd window exercises rounding", 4097, 613, 0},
		{"configured fraction", 8192, 0, 0.35},
		{"configured fraction on an odd window", 4097, 0, 0.35},
		{"explicit reserve alongside a fraction", 8192, 2048, 0.5},
		{"out-of-range fraction falls back", 8192, 0, 1.5},
		{"NaN fraction falls back", 8192, 0, math.NaN()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := Allocate(tc.window, tc.reserve, tc.fraction, Measured{-1, -1})

			if a.Window != tc.window {
				t.Errorf("Window = %d, want %d", a.Window, tc.window)
			}
			if tc.reserve > 0 && a.ResponseReserve != tc.reserve {
				t.Errorf("ResponseReserve = %d, want the explicit %d", a.ResponseReserve, tc.reserve)
			}
			if a.ResponseReserve <= 0 || a.ResponseReserve >= a.Window {
				t.Errorf("ResponseReserve = %d, want in (0, %d) so some working room always remains",
					a.ResponseReserve, a.Window)
			}
			// Every part is non-negative and the four sum to the window exactly.
			if a.SystemPrompt < 0 || a.FileContext < 0 || a.History < 0 {
				t.Errorf("a part is negative: %+v", a)
			}
			sum := a.ResponseReserve + a.SystemPrompt + a.FileContext + a.History
			if sum != tc.window {
				t.Errorf("parts sum = %d, want the window %d exactly (≤-window holds): %+v", sum, tc.window, a)
			}
			// History is the reducers' primary reclaim target, so it takes the largest working share.
			if a.History < a.SystemPrompt || a.History < a.FileContext {
				t.Errorf("History %d is not the largest working part (system %d, file %d)",
					a.History, a.SystemPrompt, a.FileContext)
			}
		})
	}
}

// TestAllocate_UnknownWindowIsZero pins the unbounded signal: a non-positive window has no basis to
// allocate, so every field is zero — a measurement of the standing parts cannot conjure room out
// of a window nobody named — and a consumer treats it as unbounded.
func TestAllocate_UnknownWindowIsZero(t *testing.T) {
	for _, window := range []int{0, -1} {
		for _, measured := range []Measured{{-1, -1}, {0, 0}, {1000, 2000}} {
			if got := Allocate(window, 1024, 0.3, measured); got != (Allocation{}) {
				t.Errorf("Allocate(%d, …, %+v) = %+v, want the zero Allocation", window, measured, got)
			}
		}
	}
}

// TestAllocate_OversizeReserveClamped proves a reserve at/over the window is clamped so at least one
// working token remains rather than leaving a zero (or negative) prompt budget.
func TestAllocate_OversizeReserveClamped(t *testing.T) {
	a := Allocate(1000, 5000, 0, Measured{-1, -1})
	if a.ResponseReserve != 999 {
		t.Errorf("ResponseReserve = %d, want it clamped to window-1 (999)", a.ResponseReserve)
	}
	if a.ResponseReserve+a.SystemPrompt+a.FileContext+a.History != 1000 {
		t.Errorf("parts do not sum to the window after clamping: %+v", a)
	}
}

// TestAllocate_ReservePrecedence pins the three-step precedence the reply reserve follows: explicit
// tokens win over any fraction, a fraction in (0, 1) applies when no tokens are pinned, and a
// fraction outside that range — NaN included, which compares false to every bound — is treated as
// unset so it falls through to the built-in default —
// the defensive floor beneath the config layer's range validation, never a panic or an absurd
// reserve.
func TestAllocate_ReservePrecedence(t *testing.T) {
	const window = 10000
	const builtIn = int(window * defaultReserveFraction) // 2000

	cases := []struct {
		name        string
		reserve     int
		fraction    float64
		wantReserve int
	}{
		{"explicit tokens win over a fraction", 3000, 0.5, 3000},
		{"explicit tokens win over an out-of-range fraction", 3000, 7.5, 3000},
		{"explicit tokens win over a NaN fraction", 3000, math.NaN(), 3000},
		{"fraction applies when no tokens are pinned", 0, 0.5, 5000},
		{"a small fraction applies too", 0, 0.05, 500},
		{"zero fraction is unset", 0, 0, builtIn},
		{"negative fraction is unset", 0, -0.3, builtIn},
		{"a fraction of exactly 1 is unset", 0, 1, builtIn},
		{"a fraction above 1 is unset", 0, 1.5, builtIn},
		{"a NaN fraction is unset", 0, math.NaN(), builtIn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Allocate(window, tc.reserve, tc.fraction, Measured{-1, -1}).ResponseReserve; got != tc.wantReserve {
				t.Errorf("Allocate(%d, %d, %v).ResponseReserve = %d, want %d",
					window, tc.reserve, tc.fraction, got, tc.wantReserve)
			}
		})
	}
}

// TestAllocate_MeasuredPartsReserveWhatTheyMeasure pins the measured path: a part the caller
// measured reserves that measurement plus the headroom, rounded up, and History takes what is
// left — so standing content the session does not carry is never held back from the transcript. A
// negative field still falls back to its fixed fraction, and the two paths mix freely.
func TestAllocate_MeasuredPartsReserveWhatTheyMeasure(t *testing.T) {
	// window 100000, default reserve 20% ⇒ reserve 20000, working room 80000.
	const window = 100000
	const working = 80000

	cases := []struct {
		name       string
		measured   Measured
		wantSystem int
		wantFile   int
	}{
		{"both measured", Measured{SystemPrompt: 5000, FileContext: 12000}, 5500, 13200},
		{"the headroom rounds up", Measured{SystemPrompt: 2001, FileContext: 2001}, 2202, 2202},
		{"system unmeasured keeps its fraction", Measured{SystemPrompt: -1, FileContext: 12000}, 12000, 13200},
		{"file unmeasured keeps its fraction", Measured{SystemPrompt: 5000, FileContext: -1}, 5500, 20000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := Allocate(window, 0, 0, tc.measured)

			if a.SystemPrompt != tc.wantSystem || a.FileContext != tc.wantFile {
				t.Errorf("reservations = {sys %d file %d}, want {sys %d file %d}",
					a.SystemPrompt, a.FileContext, tc.wantSystem, tc.wantFile)
			}
			if want := working - tc.wantSystem - tc.wantFile; a.History != want {
				t.Errorf("History = %d, want the remainder %d", a.History, want)
			}
			if a.SystemPrompt < 0 || a.FileContext < 0 || a.History < 0 || a.ResponseReserve < 0 {
				t.Errorf("a part is negative: %+v", a)
			}
			if sum := a.ResponseReserve + a.SystemPrompt + a.FileContext + a.History; sum != window {
				t.Errorf("parts sum = %d, want the window %d exactly: %+v", sum, window, a)
			}
		})
	}
}

// TestAllocate_MeasuredPartFloorsAtTwoPercent proves a part that measures nothing — or next to
// nothing — still reserves the 2% floor rather than zero, so a session that seeds its first context
// file mid-run has room already held for it.
func TestAllocate_MeasuredPartFloorsAtTwoPercent(t *testing.T) {
	const window = 100000
	const floor = 1600 // 2% of the 80000-token working room

	for _, measured := range []Measured{{SystemPrompt: 0, FileContext: 0}, {SystemPrompt: 100, FileContext: 100}} {
		a := Allocate(window, 0, 0, measured)

		if a.SystemPrompt != floor || a.FileContext != floor {
			t.Errorf("Allocate(…, %+v) reserved {sys %d file %d}, want both floored at %d",
				measured, a.SystemPrompt, a.FileContext, floor)
		}
		if sum := a.ResponseReserve + a.SystemPrompt + a.FileContext + a.History; sum != window {
			t.Errorf("parts sum = %d, want the window %d exactly: %+v", sum, window, a)
		}
	}
}

// TestAllocate_HistoryFloorScalesTheStandingParts proves the History floor holds against standing
// content larger than the working room: History lands on exactly half the working room, the two
// reservations are scaled down TOGETHER in proportion to what each asked for, and the parts still
// sum to the window. Standing content that big is the oversize notice's job to report, never a
// reason to starve the transcript.
func TestAllocate_HistoryFloorScalesTheStandingParts(t *testing.T) {
	const window = 100000
	const working = 80000

	// Reservations of 33000 and 66000 — one part twice the other — against the 40000 of working
	// room above the floor: each is scaled by 40000/99000, so the 1:2 proportion survives.
	a := Allocate(window, 0, 0, Measured{SystemPrompt: 30000, FileContext: 60000})

	if want := working / 2; a.History != want {
		t.Errorf("History = %d, want exactly %d (half the working room)", a.History, want)
	}
	if a.SystemPrompt != 13333 || a.FileContext != 26667 {
		t.Errorf("scaled reservations = {sys %d file %d}, want {sys 13333 file 26667} — scaled together, 1:2 kept",
			a.SystemPrompt, a.FileContext)
	}
	if sum := a.ResponseReserve + a.SystemPrompt + a.FileContext + a.History; sum != window {
		t.Errorf("parts sum = %d, want the window %d exactly: %+v", sum, window, a)
	}
}

// TestAllocate_StandingAdvisoryIsTheFixedShare pins the advisory ceiling the oversize notice reads
// (ADR 0026): 15% of the working room whatever the standing parts measured — a ceiling that moved
// with the measurement is one the notice could never cross.
func TestAllocate_StandingAdvisoryIsTheFixedShare(t *testing.T) {
	const window = 100000
	const want = 12000 // 15% of the 80000-token working room

	for _, measured := range []Measured{
		{SystemPrompt: -1, FileContext: -1},
		{SystemPrompt: 0, FileContext: 0},
		{SystemPrompt: 5000, FileContext: 12000},
		{SystemPrompt: 30000, FileContext: 60000},
	} {
		if got := Allocate(window, 0, 0, measured).StandingAdvisory; got != want {
			t.Errorf("Allocate(…, %+v).StandingAdvisory = %d, want the fixed %d", measured, got, want)
		}
	}
}

// TestTokenEstimator_DefaultsBeforeCalibration pins the uncalibrated state a fresh estimator (and a
// resumed Agent) reports: the default ratio and a zero Used.
func TestTokenEstimator_DefaultsBeforeCalibration(t *testing.T) {
	e := NewTokenEstimator()
	if e.CharsPerToken() != DefaultCharsPerToken {
		t.Errorf("CharsPerToken = %v, want the default %v", e.CharsPerToken(), DefaultCharsPerToken)
	}
	if e.Used() != 0 {
		t.Errorf("Used = %d, want 0 before any usage", e.Used())
	}
}

// TestTokenEstimator_SnapsUsedAndBlendsRatio pins the two calibration effects and the exact EMA
// blend: Used snaps to the reported prompt tokens, and the ratio moves halfway toward each fresh
// sample (calibrationWeight = 0.5).
func TestTokenEstimator_SnapsUsedAndBlendsRatio(t *testing.T) {
	e := NewTokenEstimator()

	// First sample: 600 chars / 100 tokens = 6.0. Blend from the 4.0 seed ⇒ (4+6)/2 = 5.0.
	e.Calibrate(600, 100)
	if e.Used() != 100 {
		t.Errorf("Used = %d, want 100 (snapped to the reported prompt tokens)", e.Used())
	}
	if !approx(e.CharsPerToken(), 5.0) {
		t.Errorf("CharsPerToken = %v, want the 4.0↔6.0 blend 5.0", e.CharsPerToken())
	}

	// Second sample: 200 chars / 100 tokens = 2.0. Blend from 5.0 ⇒ (5+2)/2 = 3.5.
	e.Calibrate(200, 100)
	if !approx(e.CharsPerToken(), 3.5) {
		t.Errorf("CharsPerToken = %v, want the 5.0↔2.0 blend 3.5", e.CharsPerToken())
	}
}

// TestTokenEstimator_ConvergesTowardReportedUsage feeds a stable true ratio across several Turns and
// proves the estimate converges toward it (the acceptance criterion) — the EMA halves the gap each
// Turn — while Used tracks the latest report.
func TestTokenEstimator_ConvergesTowardReportedUsage(t *testing.T) {
	const trueRatio = 6.0
	e := NewTokenEstimator()

	prev := math.Abs(e.CharsPerToken() - trueRatio)
	for turn := 0; turn < 8; turn++ {
		// A consistent server: 600-char prompt reported as 100 tokens ⇒ a 6.0 sample every Turn.
		e.Calibrate(600, 100)
		gap := math.Abs(e.CharsPerToken() - trueRatio)
		if gap > prev {
			t.Errorf("turn %d: gap to true ratio grew (%.4f → %.4f); calibration diverged", turn, prev, gap)
		}
		prev = gap
	}
	// The EMA halves the gap each Turn (2 → 2·0.5⁸ ≈ 0.008), so 8 Turns land well within a small
	// tolerance of the true ratio — asymptotic convergence, not an exact snap.
	if math.Abs(e.CharsPerToken()-trueRatio) > 0.05 {
		t.Errorf("after 8 Turns CharsPerToken = %v, want it converged near %v", e.CharsPerToken(), trueRatio)
	}
	if e.Used() != 100 {
		t.Errorf("Used = %d, want the latest reported 100", e.Used())
	}
}

// TestTokenEstimator_ClampsPathologicalSamples proves a sample outside the sane band cannot drive
// the ratio past the clamp, even fed repeatedly: an all-but-empty prompt (huge tokens) floors at
// minCharsPerToken, a token-starved report ceilings at maxCharsPerToken.
func TestTokenEstimator_ClampsPathologicalSamples(t *testing.T) {
	low := NewTokenEstimator()
	for i := 0; i < 50; i++ {
		low.Calibrate(1, 1000) // ratio 0.001 → clamped to minCharsPerToken
	}
	if low.CharsPerToken() < minCharsPerToken-1e-9 {
		t.Errorf("CharsPerToken = %v, want it floored at %v", low.CharsPerToken(), minCharsPerToken)
	}

	high := NewTokenEstimator()
	for i := 0; i < 50; i++ {
		high.Calibrate(100000, 1) // ratio 100000 → clamped to maxCharsPerToken
	}
	if high.CharsPerToken() > maxCharsPerToken+1e-9 {
		t.Errorf("CharsPerToken = %v, want it ceilinged at %v", high.CharsPerToken(), maxCharsPerToken)
	}
}

// TestTokenEstimator_IgnoresAbsentUsage proves a non-positive token count (a server that omitted
// usage) changes nothing, and a non-positive char count snaps Used but leaves the ratio alone.
func TestTokenEstimator_IgnoresAbsentUsage(t *testing.T) {
	e := NewTokenEstimator()
	e.Calibrate(500, 0) // no token count → no information
	if e.Used() != 0 || e.CharsPerToken() != DefaultCharsPerToken {
		t.Errorf("absent usage changed state: Used=%d ratio=%v", e.Used(), e.CharsPerToken())
	}

	e.Calibrate(0, 120) // tokens but no chars → snap Used, keep the ratio
	if e.Used() != 120 {
		t.Errorf("Used = %d, want 120 snapped from the token count", e.Used())
	}
	if e.CharsPerToken() != DefaultCharsPerToken {
		t.Errorf("CharsPerToken = %v, want it untouched with no char sample", e.CharsPerToken())
	}
}

// TestEstimateTokens_RoundsUp pins the token estimate: characters divided by the calibrated ratio,
// rounded up so a part is never estimated to fit when it is one token over.
func TestEstimateTokens_RoundsUp(t *testing.T) {
	e := NewTokenEstimator() // 4.0
	if got := e.EstimateTokens(401); got != 101 {
		t.Errorf("EstimateTokens(401) = %d, want ceil(401/4) = 101", got)
	}
	if got := e.EstimateTokens(0); got != 0 {
		t.Errorf("EstimateTokens(0) = %d, want 0", got)
	}
}

// TestEstimateTokensMatchesBudget pins the delegation (deepening plan D4): for a grid of
// (chars, ratio) the calibrating estimator and a domain.Budget carrying the same ratio agree
// exactly whenever the ratio is positive — the context path and the domain path cannot drift.
func TestEstimateTokensMatchesBudget(t *testing.T) {
	for _, ratio := range []float64{0.5, 1, 2.5, 3, 4, 7.9} {
		for _, chars := range []int{0, 1, 2, 3, 5, 399, 400, 401, 1000, 12345} {
			e := &TokenEstimator{charsPerToken: ratio}
			want := domain.Budget{CharsPerToken: ratio}.EstimateTokens(chars)
			if got := e.EstimateTokens(chars); got != want {
				t.Errorf("ratio %v, chars %d: TokenEstimator.EstimateTokens = %d, Budget.EstimateTokens = %d",
					ratio, chars, got, want)
			}
		}
	}
}

// approx reports whether two ratios are equal within a small epsilon.
func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

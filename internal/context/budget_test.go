package context

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestAllocate_ReserveHonouredAndPartsSum pins the allocation arithmetic: the response reserve is
// held back, and the four parts sum to the window exactly (no rounding drift, so ≤-window holds).
func TestAllocate_ReserveHonouredAndPartsSum(t *testing.T) {
	cases := []struct {
		name    string
		window  int
		reserve int
	}{
		{"explicit reserve", 8192, 2048},
		{"default reserve (zero ⇒ fraction)", 8192, 0},
		{"tiny window", 10, 0},
		{"odd window exercises rounding", 4097, 613},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := Allocate(tc.window, tc.reserve)

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
// allocate, so every field is zero and a consumer treats it as unbounded.
func TestAllocate_UnknownWindowIsZero(t *testing.T) {
	for _, window := range []int{0, -1} {
		if got := Allocate(window, 1024); got != (Allocation{}) {
			t.Errorf("Allocate(%d, …) = %+v, want the zero Allocation", window, got)
		}
	}
}

// TestAllocate_OversizeReserveClamped proves a reserve at/over the window is clamped so at least one
// working token remains rather than leaving a zero (or negative) prompt budget.
func TestAllocate_OversizeReserveClamped(t *testing.T) {
	a := Allocate(1000, 5000)
	if a.ResponseReserve != 999 {
		t.Errorf("ResponseReserve = %d, want it clamped to window-1 (999)", a.ResponseReserve)
	}
	if a.ResponseReserve+a.SystemPrompt+a.FileContext+a.History != 1000 {
		t.Errorf("parts do not sum to the window after clamping: %+v", a)
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

// TestPromptChars_CountsContentToolArgsAndMenu proves the char measure sums message contents,
// tool-call arguments, and the tool menu's names/descriptions/schemas — the same components on both
// sides of the ratio.
func TestPromptChars_CountsContentToolArgsAndMenu(t *testing.T) {
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "abcde"}, // 5
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
			{Tool: "read", Arguments: json.RawMessage(`{"p":"x"}`)}, // 4 + 9 = 13
		}},
	}
	tools := []domain.ToolDef{
		{Name: "read", Description: "reads", Schema: json.RawMessage(`{}`)}, // 4 + 5 + 2 = 11
	}
	if got := PromptChars(msgs, tools); got != 5+13+11 {
		t.Errorf("PromptChars = %d, want %d", got, 5+13+11)
	}
}

// TestHistoryExceedsAllocation covers the automatic Compaction trigger's decision: it trips only
// once the estimated history tokens pass the History allocation, and never trips on an unknown
// window (a non-positive allocation, the no-basis case).
func TestHistoryExceedsAllocation(t *testing.T) {
	e := NewTokenEstimator() // uncalibrated: 4 chars/token
	// 400 chars ⇒ 100 estimated tokens.
	msgs := []domain.Message{{Role: domain.RoleUser, Content: strings.Repeat("x", 400)}}

	if HistoryExceedsAllocation(0, e, msgs) {
		t.Error("tripped on a zero History allocation (unknown window); want no-basis ⇒ false")
	}
	if HistoryExceedsAllocation(-5, e, msgs) {
		t.Error("tripped on a negative History allocation; want false")
	}
	if HistoryExceedsAllocation(100, e, msgs) {
		t.Error("tripped at exactly the allocation (100 == 100); want strict > only")
	}
	if HistoryExceedsAllocation(200, e, msgs) {
		t.Error("tripped below the allocation (100 < 200); want false")
	}
	if !HistoryExceedsAllocation(99, e, msgs) {
		t.Error("did not trip above the allocation (100 > 99); want true")
	}
	if HistoryExceedsAllocation(50, e, nil) {
		t.Error("tripped on an empty history; want false")
	}
}

// approx reports whether two ratios are equal within a small epsilon.
func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

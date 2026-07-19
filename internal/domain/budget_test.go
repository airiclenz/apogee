package domain

import (
	"strings"
	"testing"
)

// TestBudgetEstimateTokens pins the single chars→token implementation (D4): ceil
// rounding so a part is never estimated to fit when it is one token over, and 0 on a
// non-positive ratio (the zero-value Budget) so token-gated comparisons stay inert
// until the ratio is calibrated.
func TestBudgetEstimateTokens(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		ratio float64
		chars int
		want  int
	}{
		{"zero ratio is inert", 0, 400, 0},
		{"negative ratio is inert", -3, 400, 0},
		{"zero chars", 4, 0, 0},
		{"exact divisor", 4, 400, 100},
		{"one char over rounds up", 4, 401, 101},
		{"one char under stays within the ceil", 4, 399, 100},
		{"single char rounds up to one token", 4, 1, 1},
		{"fractional ratio", 2.5, 6, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Budget{CharsPerToken: tc.ratio}.EstimateTokens(tc.chars)
			if got != tc.want {
				t.Errorf("Budget{CharsPerToken: %v}.EstimateTokens(%d) = %d, want %d",
					tc.ratio, tc.chars, got, tc.want)
			}
		})
	}
}

// TestBudgetHistoryExceedsAllocation transliterates the context-level trigger cases
// onto the domain compare: strict >, a non-positive History allocation never trips
// (the no-basis case), and an uncalibrated ratio keeps the compare inert.
func TestBudgetHistoryExceedsAllocation(t *testing.T) {
	t.Parallel()
	// 400 chars ⇒ 100 estimated tokens at 4 chars/token.
	msgs := []Message{{Role: RoleUser, Content: strings.Repeat("x", 400)}}
	tests := []struct {
		name   string
		budget Budget
		msgs   []Message
		want   bool
	}{
		{"zero History allocation never trips", Budget{CharsPerToken: 4}, msgs, false},
		{"negative History allocation never trips", Budget{CharsPerToken: 4, History: -5}, msgs, false},
		{"at exactly the allocation does not trip (strict >)", Budget{CharsPerToken: 4, History: 100}, msgs, false},
		{"below the allocation does not trip", Budget{CharsPerToken: 4, History: 200}, msgs, false},
		{"above the allocation trips", Budget{CharsPerToken: 4, History: 99}, msgs, true},
		{"empty history never trips", Budget{CharsPerToken: 4, History: 50}, nil, false},
		{"uncalibrated ratio is inert", Budget{History: 50}, msgs, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.budget.HistoryExceedsAllocation(tc.msgs); got != tc.want {
				t.Errorf("%+v.HistoryExceedsAllocation(msgs) = %v, want %v", tc.budget, got, tc.want)
			}
		})
	}
}

package domain

import "testing"

// TestSumAddsEveryCounter pins the roll-up counter by counter: every one of the five is summed,
// none is dropped or transposed, and the empty sum is the zero Usage — the figure a Firing that
// delegated nothing rolls up to.
func TestSumAddsEveryCounter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		readings []Usage
		want     Usage
	}{
		{"no readings sum to zero", nil, Usage{}},
		{
			"one reading is itself",
			[]Usage{{Calls: 2, PromptTokens: 800, CachedPromptTokens: 300, CompletionTokens: 100, TotalTokens: 900}},
			Usage{Calls: 2, PromptTokens: 800, CachedPromptTokens: 300, CompletionTokens: 100, TotalTokens: 900},
		},
		{
			"every counter adds across readings",
			[]Usage{
				{Calls: 1, PromptTokens: 100, CachedPromptTokens: 10, CompletionTokens: 20, TotalTokens: 120},
				{Calls: 2, PromptTokens: 1000, CachedPromptTokens: 0, CompletionTokens: 200, TotalTokens: 1200},
				{Calls: 3, PromptTokens: 10000, CachedPromptTokens: 5000, CompletionTokens: 2000, TotalTokens: 12000},
			},
			Usage{Calls: 6, PromptTokens: 11100, CachedPromptTokens: 5010, CompletionTokens: 2220, TotalTokens: 13320},
		},
		{
			"a zero reading adds nothing",
			[]Usage{{Calls: 1, PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6}, {}},
			Usage{Calls: 1, PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Sum(tc.readings...); got != tc.want {
				t.Errorf("Sum(%+v) = %+v, want %+v", tc.readings, got, tc.want)
			}
		})
	}
}

// TestAdoptLatestWinsOnlyWhenTheReadingCountedACall pins the fold every reader of a cumulative
// stream applies: a reading that counted a call replaces the figure outright (a cumulative
// reading restates, it does not add — so a later SMALLER total still wins), and a zero-Calls
// reading never overwrites what an earlier one established, whatever its other counters say.
func TestAdoptLatestWinsOnlyWhenTheReadingCountedACall(t *testing.T) {
	t.Parallel()
	established := Usage{Calls: 2, PromptTokens: 2500, CachedPromptTokens: 200, CompletionTokens: 200, TotalTokens: 2700}
	tests := []struct {
		name    string
		have    Usage
		reading Usage
		want    Usage
	}{
		{
			"a counted reading replaces the zero figure",
			Usage{},
			established,
			established,
		},
		{
			"a later counted reading replaces the whole figure, smaller counters included",
			established,
			Usage{Calls: 3, PromptTokens: 900, CompletionTokens: 50, TotalTokens: 950},
			Usage{Calls: 3, PromptTokens: 900, CompletionTokens: 50, TotalTokens: 950},
		},
		{
			"a zero-Calls reading never overwrites",
			established,
			Usage{PromptTokens: 99999, CompletionTokens: 99999, TotalTokens: 199998},
			established,
		},
		{
			"a zero-Calls reading leaves the zero figure zero",
			Usage{},
			Usage{TotalTokens: 500},
			Usage{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.have
			got.Adopt(tc.reading)
			if got != tc.want {
				t.Errorf("%+v.Adopt(%+v) = %+v, want %+v", tc.have, tc.reading, got, tc.want)
			}
		})
	}
}

package domain

import "testing"

// TestSumAddsEveryCounter pins the roll-up counter by counter: every one — the five token
// counters and the priced amount with its call split — is summed, none is dropped or
// transposed, and the empty sum is the zero Usage — the figure a Firing that delegated nothing
// rolls up to.
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
			"the priced amount and the call split add across readings",
			[]Usage{
				{Calls: 2, CostMicros: 1_250_000, PricedCalls: 2},
				{Calls: 3, CostMicros: 7, PricedCalls: 1, UnpricedCalls: 2},
				{Calls: 1, UnpricedCalls: 1},
			},
			Usage{Calls: 6, CostMicros: 1_250_007, PricedCalls: 3, UnpricedCalls: 3},
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
	established := Usage{
		Calls: 2, PromptTokens: 2500, CachedPromptTokens: 200, CompletionTokens: 200, TotalTokens: 2700,
		CostMicros: 41_000, PricedCalls: 1, UnpricedCalls: 1,
	}
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
			"a later counted reading replaces the priced amount and call split too",
			established,
			Usage{Calls: 3, PromptTokens: 3000, CostMicros: 52_000, PricedCalls: 2, UnpricedCalls: 1},
			Usage{Calls: 3, PromptTokens: 3000, CostMicros: 52_000, PricedCalls: 2, UnpricedCalls: 1},
		},
		{
			"a zero-Calls reading never overwrites",
			established,
			Usage{PromptTokens: 99999, CompletionTokens: 99999, TotalTokens: 199998},
			established,
		},
		{
			"a zero-Calls reading carrying an amount never overwrites",
			established,
			Usage{CostMicros: 99_999_999, PricedCalls: 9},
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

// TestMinusIsTheInverseOfSum pins the field-wise difference: every counter — the priced amount
// and its call split included — is taken off, so a later reading less an earlier one is exactly
// the spend in between, and adding the base back with Sum restores the later reading.
func TestMinusIsTheInverseOfSum(t *testing.T) {
	t.Parallel()
	base := Usage{
		Calls: 2, PromptTokens: 1000, CachedPromptTokens: 100, CompletionTokens: 50, TotalTokens: 1050,
		CostMicros: 3_000, PricedCalls: 1, UnpricedCalls: 1,
	}
	tests := []struct {
		name   string
		latest Usage
		want   Usage
	}{
		{"a reading less itself is zero", base, Usage{}},
		{
			"every counter is differenced",
			Usage{
				Calls: 5, PromptTokens: 4000, CachedPromptTokens: 700, CompletionTokens: 250, TotalTokens: 4250,
				CostMicros: 10_500, PricedCalls: 3, UnpricedCalls: 2,
			},
			Usage{
				Calls: 3, PromptTokens: 3000, CachedPromptTokens: 600, CompletionTokens: 200, TotalTokens: 3200,
				CostMicros: 7_500, PricedCalls: 2, UnpricedCalls: 1,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.latest.Minus(base)
			if got != tc.want {
				t.Errorf("%+v.Minus(%+v) = %+v, want %+v", tc.latest, base, got, tc.want)
			}
			if back := Sum(got, base); back != tc.latest {
				t.Errorf("Sum(Minus, base) = %+v, want the latest reading %+v", back, tc.latest)
			}
		})
	}
}

// TestUnpricedWithdrawsTheAmount pins the relabel-free carry: the amount drops to zero and every
// priced call joins the unpriced ones, while the call count and every token counter stand — the
// calls happened, only their money is not shown under another label.
func TestUnpricedWithdrawsTheAmount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		reading Usage
		want    Usage
	}{
		{"the zero reading stays zero", Usage{}, Usage{}},
		{
			"priced calls fold into unpriced and the amount goes",
			Usage{
				Calls: 5, PromptTokens: 4000, CachedPromptTokens: 700, CompletionTokens: 250, TotalTokens: 4250,
				CostMicros: 10_500, PricedCalls: 3, UnpricedCalls: 2,
			},
			Usage{
				Calls: 5, PromptTokens: 4000, CachedPromptTokens: 700, CompletionTokens: 250, TotalTokens: 4250,
				UnpricedCalls: 5,
			},
		},
		{
			"an already unpriced reading is unchanged",
			Usage{Calls: 2, PromptTokens: 100, TotalTokens: 120, UnpricedCalls: 2},
			Usage{Calls: 2, PromptTokens: 100, TotalTokens: 120, UnpricedCalls: 2},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tc.reading.Unpriced()

			if got != tc.want {
				t.Errorf("%+v.Unpriced() = %+v, want %+v", tc.reading, got, tc.want)
			}
		})
	}
}

// TestPriceOfPricesEachShareAtItsRate pins one call's amount in millionths of the currency unit:
// the uncached prompt at Input, the cached share at CachedInput, the completion at Output (each
// per 1M tokens), a cached share above the prompt floored rather than priced negative, and the
// single rounding half away from zero.
func TestPriceOfPricesEachShareAtItsRate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                       string
		price                      Price
		prompt, cached, completion int
		want                       int64
	}{
		{"the zero price prices nothing", Price{}, 1000, 200, 300, 0},
		{
			"no cached tokens: prompt at input, completion at output",
			Price{Input: 3, Output: 15, CachedInput: 0.3},
			1_000_000, 0, 100_000, 3_000_000 + 1_500_000,
		},
		{
			"the cached share is priced at cached-input, the rest at input",
			Price{Input: 3, Output: 15, CachedInput: 0.3},
			1_000_000, 400_000, 0, 600_000*3 + 120_000,
		},
		{
			"a cached share above the prompt floors the uncached share at zero",
			Price{Input: 3, Output: 15, CachedInput: 0.5},
			100, 200, 0, 100,
		},
		{"a half rounds away from zero", Price{Input: 2.5}, 1, 0, 0, 3},
		{"just under a half rounds down", Price{Output: 0.49}, 0, 0, 1, 0},
		{"a half from several tokens rounds up", Price{Output: 0.5}, 0, 0, 3, 2},
		{"no tokens prices nothing", Price{Input: 3, Output: 15, CachedInput: 0.3}, 0, 0, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.price.Of(tc.prompt, tc.cached, tc.completion); got != tc.want {
				t.Errorf("%+v.Of(%d, %d, %d) = %d, want %d",
					tc.price, tc.prompt, tc.cached, tc.completion, got, tc.want)
			}
		})
	}
}

// TestFormatCostBoundaries pins the one rendering every surface shows an amount in: two decimals
// rounded half away from zero, then the label as written; a positive amount below a cent reads
// "<0.01", zero reads "0.00", and an empty label leaves the number alone.
func TestFormatCostBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		micros   int64
		currency string
		want     string
	}{
		{"zero is 0.00", 0, "USD", "0.00 USD"},
		{"one millionth is below a cent", 1, "USD", "<0.01 USD"},
		{"just under a cent is below a cent", 9_999, "USD", "<0.01 USD"},
		{"exactly a cent", 10_000, "USD", "0.01 USD"},
		{"a half cent above a cent rounds up", 15_000, "USD", "0.02 USD"},
		{"just under a half cent rounds down", 14_999, "USD", "0.01 USD"},
		{"whole units and cents", 12_345_678, "EUR", "12.35 EUR"},
		{"the label is printed as written", 1_000_000, "credits", "1.00 credits"},
		{"an empty label leaves the number alone", 2_500_000, "", "2.50"},
		{"a negative amount keeps its sign", -1_230_000, "USD", "-1.23 USD"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := FormatCost(tc.micros, tc.currency); got != tc.want {
				t.Errorf("FormatCost(%d, %q) = %q, want %q", tc.micros, tc.currency, got, tc.want)
			}
		})
	}
}

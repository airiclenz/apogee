package domain

import (
	"fmt"
	"math"
)

// Usage is one agent's CUMULATIVE token accounting: the counters every completion it accounted
// for adds to, Compaction folds included — five token counters, and the priced Spend (money)
// beside them (CostMicros with its PricedCalls/UnpricedCalls split, ADR 0093). It is the ONE
// shape those counters travel in — the runner's Result and its sub-agent entries carry it, the
// session record and the headless event frame restate it in their own field order and keys — so
// a Driver reads a spend off one type wherever the reading came from.
//
// It is per-agent as REPORTED: a sub-agent starts from zero and its totals stay its own, so a
// session-wide figure is a Sum taken deliberately, never a counter that folded a delegate's
// spend into its parent's. A reading is READ off the latest event the agent stamped rather than
// summed from the stream (UsageEvent.Cumulative; Adopt is that fold), so it is whole
// even for an observer that joined late. Every counter is zero when nothing accounted for the
// agent at all — an Upstream that reports no usage, or a run that never completed a call — and
// Calls is the counter that says so: a reading with no call behind it is the ABSENCE of
// accounting, not a spend of zero.
//
// The field names and order are the session record's exactly (session.Usage), so the runner's
// conversion onto the record is a struct conversion the compiler checks — a transposed counter
// cannot slip through a positional literal. The wire frames whose key order differs keep a
// field-by-field mapping instead (eventjson).
type Usage struct {
	// Calls is how many completed upstream calls the agent accounted for, Compaction folds
	// included.
	Calls int
	// PromptTokens is the sum of the prompt (context) tokens those calls were charged.
	PromptTokens int
	// CachedPromptTokens is the share of PromptTokens the Upstream answered from its prefix
	// cache, where it reports one (0 on every server that omits the breakdown). It is
	// INFORMATIONAL: it is already counted inside PromptTokens and no bound reads it — a cached
	// prompt token is still context the model reads, only the bill differs.
	CachedPromptTokens int
	// CompletionTokens is the sum of the tokens they generated.
	CompletionTokens int
	// TotalTokens is the sum of the totals the SERVER reported for them, folded as reported
	// rather than recomputed from the two parts above, so it stays consistent with the server's
	// own arithmetic (which may count cached or reasoning tokens the split does not show). A
	// server that reports the parts and omits the sum therefore leaves this at zero.
	TotalTokens int
	// CostMicros is the priced amount of those calls in whole millionths of the currency unit
	// (ADR 0093 decision 5): each call is priced once, when it is recorded, at the Price of the
	// server bound to it (Price.Of), and every later figure is a plain integer sum, so it never
	// drifts across agents, delegates or saves. It covers the PricedCalls only — an unpriced call
	// adds nothing here — and it carries no currency: the label rides beside it wherever it is
	// persisted (session.Meta.Currency) and FormatCost renders the pair.
	CostMicros int64
	// PricedCalls is how many of Calls were priced (their server had a `price:`).
	PricedCalls int
	// UnpricedCalls is how many of Calls were not (their server had no `price:`): counted, never
	// guessed, so a surface can say an amount covers only part of the session (ADR 0093
	// decision 4).
	UnpricedCalls int
}

// Sum adds any number of readings counter by counter into one figure — the roll-up a caller
// takes ON PURPOSE, across agents whose readings are each whole on their own (a Firing's
// delegated runs into the record's DelegateUsage, a session's own and delegated spend into the
// /sessions cell). No readings sum to the zero Usage.
func Sum(readings ...Usage) Usage {
	var total Usage
	for _, r := range readings {
		total.Calls += r.Calls
		total.PromptTokens += r.PromptTokens
		total.CachedPromptTokens += r.CachedPromptTokens
		total.CompletionTokens += r.CompletionTokens
		total.TotalTokens += r.TotalTokens
		total.CostMicros += r.CostMicros
		total.PricedCalls += r.PricedCalls
		total.UnpricedCalls += r.UnpricedCalls
	}
	return total
}

// Minus is u less base counter by counter — the inverse of Sum, for the spend a later reading
// added since an earlier one of the SAME agent (a background workflow's figure since the baseline
// it started from). It is a plain field-wise difference, so a caller never rebuilds a Usage field
// by field and so drops a counter; it does not clamp, because a base that is not an earlier
// reading of the same agent is the caller's mistake, not a figure to hide.
func (u Usage) Minus(base Usage) Usage {
	return Usage{
		Calls:              u.Calls - base.Calls,
		PromptTokens:       u.PromptTokens - base.PromptTokens,
		CachedPromptTokens: u.CachedPromptTokens - base.CachedPromptTokens,
		CompletionTokens:   u.CompletionTokens - base.CompletionTokens,
		TotalTokens:        u.TotalTokens - base.TotalTokens,
		CostMicros:         u.CostMicros - base.CostMicros,
		PricedCalls:        u.PricedCalls - base.PricedCalls,
		UnpricedCalls:      u.UnpricedCalls - base.UnpricedCalls,
	}
}

// Adopt takes reading as u's new value when the reading counted a call, and leaves u alone
// when it did not: the latest reading wins, because a cumulative figure RESTATES the agent's
// running counters rather than adding to them, and a reading that counted nothing is the
// absence of accounting (a pre-feature event stream, an Upstream that reports no usage) rather
// than a fresh zero, so it never overwrites what an earlier reading established.
func (u *Usage) Adopt(reading Usage) {
	if reading.Calls <= 0 {
		return
	}
	*u = reading
}

// Price is one server's configured rate (ADR 0093 decision 2): what one million tokens cost in the
// configured currency — Input for a prompt token the server did not serve from its cache,
// CachedInput for one it did, Output for a completion token. The caller resolves the
// `cached-input:` default (config.Price.CachedInputRate) before building it, so here every rate is
// stated and the zero Price prices every call at nothing; whether a server is priced at all is the
// caller's flag beside it, never a zero rate.
type Price struct {
	Input       float64
	Output      float64
	CachedInput float64
}

// microsPerUnit is how many CostMicros make one unit of the currency.
const microsPerUnit = 1_000_000

// Of prices one call: the uncached share of its prompt at Input, the cached share at CachedInput
// and the completion at Output, each rate per 1M tokens — so the amount in millionths of the
// currency unit is the token-rate products themselves. The uncached share is prompt − cached,
// floored at zero, so a server reporting a cached share above its prompt count never prices a
// negative input. The result is rounded to whole millionths ONCE, here, half away from zero
// (ADR 0093 decision 5); every sum after this is integer arithmetic.
func (p Price) Of(prompt, cached, completion int) int64 {
	uncached := max(prompt-cached, 0)
	micros := float64(uncached)*p.Input + float64(cached)*p.CachedInput + float64(completion)*p.Output
	return int64(math.Round(micros))
}

// microsPerCent is how many CostMicros make one hundredth of the currency unit — the precision
// FormatCost shows.
const microsPerCent = microsPerUnit / 100

// FormatCost renders an amount in millionths of the currency unit for every surface that shows
// Spend (money): two decimals, rounded half away from zero, then the currency label exactly as
// configured ("1.23 USD"). A positive amount too small to show as a cent reads "<0.01 USD" rather
// than a misleading zero, and zero reads "0.00 USD". An empty label leaves the number alone. The
// rounding is the display's own and is never written back (ADR 0093 decision 5).
func FormatCost(micros int64, currency string) string {
	sign := ""
	if micros < 0 {
		sign = "-"
		micros = -micros
	}
	var amount string
	if micros > 0 && micros < microsPerCent {
		amount = "<0.01"
	} else {
		cents := (micros + microsPerCent/2) / microsPerCent
		amount = fmt.Sprintf("%d.%02d", cents/100, cents%100)
	}
	if currency == "" {
		return sign + amount
	}
	return sign + amount + " " + currency
}

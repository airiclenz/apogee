package domain

// Usage is one agent's CUMULATIVE token accounting: the five counters every completion it
// accounted for adds to, Compaction folds included. It is the ONE shape those counters travel
// in — the runner's Result and its sub-agent entries carry it, the session record and the
// headless event frame restate it in their own field order and keys — so a Driver reads a spend
// off one type wherever the reading came from.
//
// It is per-agent as REPORTED: a sub-agent starts from zero and its totals stay its own, so a
// session-wide figure is a Sum taken deliberately, never a counter that folded a delegate's
// spend into its parent's. A reading is READ off the latest event the agent stamped rather than
// summed from the stream (UsageEvent's Cumulative* fields; Adopt is that fold), so it is whole
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
	}
	return total
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

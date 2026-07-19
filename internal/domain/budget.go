package domain

import "math"

// The Budget's pure token arithmetic (ADR 0010: pure logic on a domain type lives
// in domain). The chars→token conversion has exactly ONE implementation — the two
// methods below. The calibrating estimator (internal/context.TokenEstimator) and
// every token-gated Mechanism delegate here, so their estimates cannot drift.

// EstimateTokens converts a character count to a token estimate through the
// calibrated chars→token ratio, rounding up so a part is never estimated to fit
// when it is one token over. A non-positive CharsPerToken — the zero-value
// Budget of an uncalibrated view — yields 0, so a comparison against any
// positive threshold stays false: token-gated behaviour is inert until the
// ratio is known, never fired on an un-measured guess.
func (b Budget) EstimateTokens(chars int) int {
	if b.CharsPerToken <= 0 {
		return 0
	}
	return int(math.Ceil(float64(chars) / b.CharsPerToken))
}

// HistoryExceedsAllocation reports whether the estimated token size of msgs (the
// conversation history the reducers reclaim) has outgrown the Budget's History
// allocation. It is the single compare behind both the engine's automatic
// Compaction trigger and any hook reading the Budget, so the two can never
// disagree. The measure runs the whole conversation through the calibrated
// ratio (PromptChars omits the tool menu — that is not history) and is
// deliberately conservative: comparing the whole conversation against the
// History slice trips slightly before the prompt would overflow. A non-positive
// History (the window is unknown, so nothing was allocated) never trips —
// there is no basis to bound.
func (b Budget) HistoryExceedsAllocation(msgs []Message) bool {
	if b.History <= 0 {
		return false
	}
	return b.EstimateTokens(PromptChars(msgs, nil)) > b.History
}

// PromptChars is a stable character measure of a request's prompt — the message contents and
// tool-call arguments plus the tool menu's names, descriptions, and schemas — used both as the
// estimator's calibration sample (internal/context.TokenEstimator.Calibrate) and as the basis
// for a token estimate (EstimateTokens). It deliberately omits the chat template's own markup,
// which the character count cannot see; the same omission on both sides of the chars→token
// ratio means a systematic offset cancels, so an estimate stays consistent with the calibration
// that produced the ratio.
func PromptChars(msgs []Message, tools []ToolDef) int {
	n := 0
	for i := range msgs {
		n += len(msgs[i].Content)
		for _, tc := range msgs[i].ToolCalls {
			n += len(tc.Tool) + len(tc.Arguments)
		}
	}
	for i := range tools {
		n += len(tools[i].Name) + len(tools[i].Description) + len(tools[i].Schema)
	}
	return n
}

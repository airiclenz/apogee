package domain

import "math"

// The Budget's pure token arithmetic (ADR 0010: pure logic on a domain type lives
// in domain). The chars→token conversion has exactly ONE implementation — the two
// methods below. The calibrating estimator (internal/context.TokenEstimator) and
// every token-gated reader delegate here, so their estimates cannot drift.

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
// there is no basis to bound HERE; the engine's trigger substitutes its own
// conservative ceiling before calling this (internal/agent, ADR 0018), so the
// compare stays the single one while the assumption stays out of the Budget
// view the reactions read.
func (b Budget) HistoryExceedsAllocation(msgs []Message) bool {
	if b.History <= 0 {
		return false
	}
	return b.EstimateTokens(PromptChars(msgs, nil)) > b.History
}

// HistoryExceedsFraction reports whether the estimated token size of msgs has outgrown
// the given fraction of the Budget's History allocation. It is HistoryExceedsAllocation
// with a movable line: the same PromptChars measure and the same calibrated ratio, so a
// reducer that fires at one fraction and stops at another is reading ONE scale, and its
// trigger cannot drift from the per-message sizes it then compares. A non-positive
// History (the window is unknown) or a non-positive fraction never trips, for the same
// reason the sibling stays inert on an un-measured guess.
func (b Budget) HistoryExceedsFraction(msgs []Message, fraction float64) bool {
	if b.History <= 0 || fraction <= 0 {
		return false
	}
	return float64(b.EstimateTokens(PromptChars(msgs, nil))) > float64(b.History)*fraction
}

// HistoryFill reports how far a conversation of the given character size has climbed
// toward the Budget's History allocation, as a fraction of it: EstimateTokens(chars)
// over History, so 1.0 is the automatic Compaction line itself and the value climbs
// past it once the history has outgrown the allocation. It is the scale the
// context-fill notice reports to the model (ADR 0077) and reads exactly what the
// trigger compares — for a conversation of chars = ConversationChars(conv),
// HistoryExceedsAllocation(msgs) == (HistoryFill(chars) > 1.0), so the notice and the
// fold can never disagree on where the line is. A non-positive History (the window is
// unknown, so nothing was allocated) or a non-positive CharsPerToken (the ratio is
// uncalibrated) yields 0: inert on an un-measured guess, with no substitute ceiling,
// for the same reason the sibling compares stay false there.
func (b Budget) HistoryFill(chars int) float64 {
	if b.History <= 0 || b.CharsPerToken <= 0 {
		return 0
	}
	return float64(b.EstimateTokens(chars)) / float64(b.History)
}

// ConversationChars is PromptChars over a ConversationView: the message contents and
// tool-call names and arguments, summed through Range, with no tool menu (the menu is
// not history). It is the same number PromptChars(msgs, nil) yields over the messages
// the view serves, so a reaction reading the view feeds HistoryFill the measure the
// automatic Compaction trigger is estimating from. It is a free function rather than a
// view method because the ConversationView interface gains no methods (ADR 0017 §4).
func ConversationChars(conv ConversationView) int {
	n := 0
	conv.Range(func(_ int, m Message) bool {
		n += len(m.Content)
		for _, tc := range m.ToolCalls {
			n += len(tc.Tool) + len(tc.Arguments)
		}
		return true
	})
	return n
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

package floor

import (
	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
)

// toolResultBudgetFraction is the share of the working context budget a SINGLE tool result may
// occupy before it is capped — apogee-sim's defaultToolResultBudgetPct (`compress.go:28` @pin,
// 0.4). A result over this fraction is trimmed head/tail; one at or under it is left whole.
const toolResultBudgetFraction = 0.4

// CapToolResults is the tool-result cap guard (the `tool-result-cap` key, ADR 0071): every tool
// result from an earlier Turn that has outgrown its fraction of the Budget is trimmed to the shared
// head/tail-plus-marker elision in the PROJECTED REQUEST, leaving the conversation itself untouched.
// It reports how many results it capped, so a caller can stay silent when nothing changed.
//
// It shapes the request without steering the model — the model is told, in the marker, to re-read
// the range it wants back — so it needs no per-model proof and stays on under Bypass. The decision
// logic is apogee-sim's capToolResults (`compress.go:428` @pin), unchanged by the promotion:
//
//   - a zero ceiling (the window is unknown, so the Budget carries no allocation) is a no-op,
//     matching the generative Compaction path;
//   - the most recent tool-call Turn onwards is PROTECTED, so the freshest results reach the model
//     whole;
//   - only a result STRICTLY over the ceiling is trimmed, and only when the trim actually shrinks
//     it — a pathological few-very-long-lines result the head/tail form cannot reduce is left whole
//     rather than grown (the sim replaced unconditionally, `compress.go:459`).
//
// It edits through Request.SetMessageContent, the in-place edit the pre-request seam owns
// (hook-mutation-api §1.4): the message list is never wholesale-replaced, so an untouched request
// books no revision and no firing.
func CapToolResults(req *domain.Request) (capped int) {
	maxChars := capMaxChars(req.View().Budget())
	if maxChars <= 0 {
		return 0
	}
	conv := req.View().Conversation()
	protectedFrom := mostRecentToolCallTurn(conv)
	for i := 0; i < protectedFrom; i++ {
		msg := conv.At(i)
		if msg.Role != domain.RoleTool || len(msg.Content) <= maxChars {
			continue
		}
		trimmed := apogeectx.TruncateToolResult(msg.Content, maxChars)
		if len(trimmed) < len(msg.Content) {
			req.SetMessageContent(i, trimmed)
			capped++
		}
	}
	return capped
}

// capMaxChars is the per-result character ceiling: the working context budget (the window less the
// response reserve — apogee's honest analog of the sim's ContextBudget = contextLimit - contextLimit/4,
// `proxy.go:597` @pin) converted to characters through the calibrated chars→token ratio, times the
// budget fraction (apogee-sim capToolResults `compress.go:438` @pin: budget * charsPerToken * pct).
// It is 0 when the window is unknown (ContextLimit 0 ⇒ a zero Allocation), so capping is inert until
// a window is discovered. ContextLimit is the WORKING ceiling rather than the advertised window
// (domain.Budget), so a session bounded by `working-window:` caps its results against the room it
// actually works in — which is the point of the key on a model advertising a very large window,
// where a cap scaled to the advertisement is no cap at all.
//
// This is the tokens→chars INVERSE of Budget.EstimateTokens — chars = tokens × ratio where the
// estimate is ceil(chars ÷ ratio), computed from the same CharsPerToken — kept as its own
// expression rather than forced through a shared shape (D4).
func capMaxChars(b domain.Budget) int {
	budgetTokens := b.ContextLimit - b.ResponseReserve
	if budgetTokens <= 0 || b.CharsPerToken <= 0 {
		return 0
	}
	return int(float64(budgetTokens) * b.CharsPerToken * toolResultBudgetFraction)
}

// mostRecentToolCallTurn is the index of the last assistant message that issued tool calls;
// everything from it onward is protected from capping so the freshest tool results reach the model
// whole (apogee-sim findMostRecentAssistantTurn `compress.go:466` @pin). With no tool-call Turn in
// the conversation it returns Len, protecting nothing — matching the sim's `return len(msgs)`.
func mostRecentToolCallTurn(conv domain.ConversationView) int {
	for i := conv.Len() - 1; i >= 0; i-- {
		if m := conv.At(i); m.Role == domain.RoleAssistant && len(m.ToolCalls) > 0 {
			return i
		}
	}
	return conv.Len()
}

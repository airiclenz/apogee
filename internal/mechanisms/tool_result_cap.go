package mechanisms

import (
	"context"

	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
)

// tool_result_cap registers the tool-result capping pre-request Mechanism in the catalogue
// constructor table (Phase-4 item 9). Default-off (D1) — the config surface builds it only when
// the `mechanisms:` block enables it. It is the surviving half of apogee-sim's `compress`
// (catalogue C3 SPLIT — the generative Compaction and history-truncation halves live elsewhere,
// D6): a per-tool-result truncation of any single result that has outgrown its fraction of the
// Budget, the most recent Turn always protected.
func init() {
	catalogue[toolResultCapID] = newToolResultCap
	descriptors[toolResultCapID] = toolResultCapDescriptor
}

const toolResultCapID domain.MechanismID = "tool_result_cap"

// toolResultBudgetFraction is the share of the working context budget a SINGLE tool result may
// occupy before it is capped — apogee-sim's defaultToolResultBudgetPct (`compress.go:28` @pin,
// 0.4). A result over this fraction is trimmed head/tail; one at or under it is left whole.
const toolResultBudgetFraction = 0.4

// toolResultCapMechanism is the pre-request Mechanism that caps oversized tool results (catalogue
// Table A `tool_result_cap`; ported from apogee-sim internal/compress/compress.go capToolResults
// `:428` @pin). It walks the request's messages, trims each RoleTool message whose content exceeds
// its Budget fraction to a head/tail-plus-marker form via Request.SetMessageContent (an in-place
// edit — the pre-request hook never wholesale-replaces the message list, hook-mutation-api §1.4),
// and never touches a result from the most recent tool-call Turn. The head/tail rendering itself is
// context.TruncateToolResult, shared with the loop's structural floor on a single oversized result
// so both reducers elide with the same shape and marker. It carries no per-Mechanism state: the
// descriptor's strikes-3 policy routes self-regulation through the loop's per-Session tracker
// (item 3), and the fraction/head/tail are built-in defaults (item 4's `mechanisms:` block is
// enabled-only, so there is no per-Mechanism config surface).
type toolResultCapMechanism struct{}

// newToolResultCap builds the tool_result_cap Mechanism with the built-in defaults. It needs no
// injected Deps (D3): capping reads the Budget and messages off the Request it is handed.
func newToolResultCap(Deps) (domain.Mechanism, error) {
	return toolResultCapMechanism{}, nil
}

// toolResultCapDescriptor identifies tool_result_cap as a strikes-3 proactive-nudge Mechanism
// (catalogue Table A, footnote 2: a context-shaper is neither off-ramp nor response-repair;
// proactive-nudge carries the Bypass semantics — disabled under Bypass, D5 — while the structural
// Budget and Compaction stay on, D6). It is withdrawn by self-regulation after repeated non-help.
var toolResultCapDescriptor = domain.MechanismDescriptor{
	ID:          toolResultCapID,
	Capability:  domain.CapProactiveNudge,
	Suppression: domain.SuppressStrikesThree,
}

// Descriptor returns tool_result_cap's static catalogue descriptor.
func (toolResultCapMechanism) Descriptor() domain.MechanismDescriptor { return toolResultCapDescriptor }

// Ordering declares tool_result_cap After decompose (§Ordering seed, ratified into Table A
// 2026-07-04, review-fixes item 11 / option A): it trims tool results after the other pre-request
// shapers assemble context, so it runs last among them. decompose is the last transform (the nudges
// and library precede toolfilter, which precedes decompose), so an After-decompose edge pushes
// tool_result_cap behind the whole shaper chain; filehint/grammar/read_loop are request-prep
// injectors with no hard order and fall by the D4 ID tiebreak.
func (toolResultCapMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{After: []domain.MechanismID{decomposeID}}
}

// PreRequest caps each oversized tool result in req, protecting the most recent tool-call Turn.
// maxChars is the per-result ceiling derived from the Budget; a zero ceiling (the window is
// unknown, so the Budget carries no allocation) is a no-op, matching the generative Compaction
// path. Only a result STRICTLY over the ceiling is trimmed, and only when the trim actually
// shrinks it (a pathological few-very-long-lines result the head/tail form cannot reduce is left
// whole rather than grown — the sim replaced unconditionally, `compress.go:459`), so an untouched
// request books no fire (the loop keys acted fires on Request.Revision, R4).
func (toolResultCapMechanism) PreRequest(_ context.Context, req *domain.Request) error {
	maxChars := capMaxChars(req.View().Budget())
	if maxChars <= 0 {
		return nil
	}
	conv := req.View().Conversation()
	protectedFrom := mostRecentToolCallTurn(conv)
	for i := 0; i < protectedFrom; i++ {
		msg := conv.At(i)
		if msg.Role != domain.RoleTool || len(msg.Content) <= maxChars {
			continue
		}
		capped := apogeectx.TruncateToolResult(msg.Content, maxChars)
		if len(capped) < len(msg.Content) {
			req.SetMessageContent(i, capped)
		}
	}
	return nil
}

// capMaxChars is the per-result character ceiling: the working context budget (the window less the
// response reserve — apogee's honest analog of the sim's ContextBudget = contextLimit - contextLimit/4,
// `proxy.go:597` @pin) converted to characters through the calibrated chars→token ratio, times the
// budget fraction (apogee-sim capToolResults `compress.go:438` @pin: budget * charsPerToken * pct).
// It is 0 when the window is unknown (ContextLimit 0 ⇒ a zero Allocation), so capping is inert until
// a window is discovered. This is the tokens→chars INVERSE of Budget.EstimateTokens — chars =
// tokens × ratio where the estimate is ceil(chars ÷ ratio), computed from the same CharsPerToken —
// kept as its own expression rather than forced through a shared shape (D4).
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

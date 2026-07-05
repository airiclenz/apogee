package mechanisms

import (
	"context"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// guided_decomposition registers the guided-decomposition Mechanism in the catalogue constructor
// table (ADR 0014). Default-off (D1) — the config surface builds it only when the `mechanisms:`
// block enables it, and it is benched as a stack with tool_result_cap (Requires, below) so the
// bench measures the two together. It steers the PRIMARY call, on an oversized task, to first
// enumerate the work as a numbered list of self-contained subtasks, then (item 4's post-response
// half) serializes the fan-out one sub_agent delegation per Turn. This file is the pre-request
// half: the gate and the enumeration steer. The PostResponse intercept + serialized follow-through
// lands in item 4 on the same struct.
func init() { catalogue[guidedDecompositionID] = newGuidedDecomposition }

const guidedDecompositionID domain.MechanismID = "guided_decomposition"

// guidedDecompositionMaxSubtasks bounds the enumeration the steer asks for (ADR 0014 locked
// decision 5: at most 7 subtasks). Tuning is the bench's job, not code-review taste. The intercept's
// accept window (item 4's 2..12 bound) is a separate, wider tolerance and is declared there.
const guidedDecompositionMaxSubtasks = 7

// Idempotency / no-double-steer markers. Both are guided_decomposition's own vocabulary, embedded
// verbatim in the messages it (and item 4) inject so a single history scan tells the gate to stay
// quiet:
//   - guidedDecompositionSteerMarker rides the enumeration steer this half injects, so a second
//     pass over a request that already carries the steer does not re-ask.
//   - guidedDecompositionDirectiveMarker rides item 4's remaining-items directive, which
//     buildRequest drains and injects BEFORE the pre-request hooks run (loop.go): when a fan-out is
//     already in flight this half sees the directive in the conversation and stays quiet — no
//     double-steer. Item 4 builds its directive around this constant.
const (
	guidedDecompositionSteerMarker     = "Decomposition planning"
	guidedDecompositionDirectiveMarker = "Remaining decomposition subtasks"
)

// guidedDecompositionSteer is the enumeration steer the gate injects on an oversized primary call:
// reply with ONLY a numbered list of at most guidedDecompositionMaxSubtasks self-contained subtasks
// — no other text, no tool calls (ADR 0014 §2). It embeds guidedDecompositionSteerMarker so the
// inject is idempotent (the marker check makes a re-steer a no-op) and so item 4's post-response
// half can recognise an outstanding steer in the request. The exact wording is tuning surface; the
// requirements (list-only, self-contained, bounded, no tool calls) are the ADR's.
var guidedDecompositionSteer = fmt.Sprintf(
	"Decomposition planning: this request is large enough to delegate. Before doing any work, "+
		"reply with ONLY a numbered list of at most %d independent, self-contained subtasks that "+
		"together complete it. Each subtask must stand alone — a sub-agent will run it in a fresh "+
		"conversation and report a single result back. Write nothing else: no preamble, no "+
		"explanation, and do not call any tool in this reply.",
	guidedDecompositionMaxSubtasks,
)

// guidedDecompositionMechanism is the guided-decomposition Mechanism (ADR 0014). It carries no
// per-Mechanism state: the remaining-items queue is re-derived from honest history each
// post-response (item 4, locked decision 1), and its idempotency rides fixed markers in the
// conversation rather than a shared meta map — so it is snapshot/resume-safe for free and
// suppression abandons cleanly.
type guidedDecompositionMechanism struct{}

// newGuidedDecomposition builds the guided_decomposition Mechanism. It needs no injected Deps (D3):
// the gate reads the conversation, tool menu, Budget, and nesting depth off the Request it is handed.
func newGuidedDecomposition(Deps) (domain.Mechanism, error) {
	return guidedDecompositionMechanism{}, nil
}

// Descriptor identifies guided_decomposition as a strikes-3 proactive-nudge Mechanism (ADR 0014 §1):
// disabled under Bypass (D5) and withdrawn by self-regulation after repeated non-help. It is
// IncompatibleWith decompose — the two steer the same "task too big" symptom through different means
// (delegation vs prompt wording) and must not stack (locked decision 2) — and Requires
// tool_result_cap, the peer it is benched as a stack with; enabling it without tool_result_cap is a
// startup error (ValidateRequirements, locked decision 3 / ADR 0014 §4).
func (guidedDecompositionMechanism) Descriptor() domain.MechanismDescriptor {
	return domain.MechanismDescriptor{
		ID:               guidedDecompositionID,
		Capability:       domain.CapProactiveNudge,
		Suppression:      domain.SuppressStrikesThree,
		IncompatibleWith: []domain.MechanismID{decomposeID},
		Requires:         []domain.MechanismID{toolResultCapID},
	}
}

// Ordering declares guided_decomposition After toolfilter: the sub_agent-presence gate must read the
// FINAL tool menu, and toolfilter narrows the menu via SetTools earlier in the pass. There is no
// runtime coupling to encode as an ordering edge here.
func (guidedDecompositionMechanism) Ordering() domain.OrderingConstraints {
	return domain.OrderingConstraints{After: []domain.MechanismID{toolFilterID}}
}

// PreRequest injects the enumeration steer on an oversized primary call so the model first plans the
// work as a numbered list of self-contained subtasks (ADR 0014 §2/§5). It fires only on MEASURED
// signals (no verb heuristics) and only where a fan-out is meaningful and not already under way:
//
//   - the context window must be known (Budget.ContextLimit > 0) — never steer on a guess;
//   - only at the top level (Depth == 0) — guided decomposition steers the primary call, never a
//     nested delegation it itself set up (§5);
//   - sub_agent must be on the (final, post-toolfilter) menu — nothing to delegate toward otherwise;
//   - no steer or fan-out directive may already be outstanding in the conversation — no double-steer;
//   - the task must be oversized by signal A or B (guidedDecompositionOversized).
//
// It books no fire (the loop keys acted fires on Request.Revision, R4) whenever any precondition
// fails — the fail-soft, self-regulated posture of §5. When it does fire it only APPENDS a steer via
// InjectContext; it never trims the user message (honesty, §2).
func (guidedDecompositionMechanism) PreRequest(_ context.Context, req *domain.Request) error {
	view := req.View()
	budget := view.Budget()
	if budget.ContextLimit <= 0 {
		return nil // unknown window — never fire on a guess (ADR 0014 §5)
	}
	if view.Depth() != 0 {
		return nil // steer the primary call only, never a nested delegation (§5)
	}
	if !guidedDecompositionCanDelegate(view.Tools()) {
		return nil // no sub_agent on the final menu — nothing to steer toward
	}
	conv := view.Conversation()
	if guidedDecompositionOutstanding(conv) {
		return nil // a steer or a fan-out directive is already steering — no double-steer
	}
	if !guidedDecompositionOversized(conv, budget) {
		return nil // neither signal A nor B — the task is not large enough to warrant delegation
	}
	req.InjectContext(guidedDecompositionSteer)
	return nil
}

// guidedDecompositionCanDelegate reports whether the sub_agent recursion point is on the final tool
// menu (keyed on the canonical tools.SubAgentToolName). Without it there is nothing to delegate to,
// so the steer would ask for a plan the model cannot execute.
func guidedDecompositionCanDelegate(menu []domain.ToolDef) bool {
	for _, t := range menu {
		if t.Name == tools.SubAgentToolName {
			return true
		}
	}
	return false
}

// guidedDecompositionOutstanding reports whether the conversation already carries this Mechanism's
// steer or item 4's fan-out directive (by their fixed markers). Either means guided decomposition is
// already steering this Exchange, so the gate stays quiet rather than double-steer. The directive is
// injected by buildRequest ahead of the pre-request hooks (the deferred-correction drain), so a
// mid-fan-out Turn sees it here.
func guidedDecompositionOutstanding(conv domain.ConversationView) bool {
	found := false
	conv.Range(func(_ int, m domain.Message) bool {
		if strings.Contains(m.Content, guidedDecompositionSteerMarker) ||
			strings.Contains(m.Content, guidedDecompositionDirectiveMarker) {
			found = true
			return false
		}
		return true
	})
	return found
}

// guidedDecompositionOversized reports whether the task is large enough to warrant delegation, on the
// same measured signals ADR 0014 §5 names — never a verb heuristic. Token estimates use the
// tool_result_cap chars→token idiom (chars / Budget.CharsPerToken); an uncalibrated ratio (<= 0)
// makes every estimate inert, so the gate never fires on an un-measured guess.
func guidedDecompositionOversized(conv domain.ConversationView, budget domain.Budget) bool {
	if budget.CharsPerToken <= 0 {
		return false
	}
	return guidedDecompositionFreshUserOversized(conv, budget) ||
		guidedDecompositionMidExchangeOversized(conv, budget)
}

// guidedDecompositionFreshUserOversized is signal A (the Turn-1 fact): the conversation ends in the
// fresh user message and its estimated tokens exceed the FileContext allocation. The resolved @file
// blocks live inside that message (loop.go), so its size embodies the resolved file context — a big
// opening ask is the primary-call fan-out trigger.
func guidedDecompositionFreshUserOversized(conv domain.ConversationView, budget domain.Budget) bool {
	n := conv.Len()
	if n == 0 {
		return false
	}
	last := conv.At(n - 1)
	if last.Role != domain.RoleUser {
		return false
	}
	return guidedDecompositionEstimateTokens(len(last.Content), budget.CharsPerToken) > budget.FileContext
}

// guidedDecompositionMidExchangeOversized is signal B (the mid-Exchange fact): the estimated history
// tokens exceed the History allocation AND the last assistant message carried tool calls (the model
// is mid-work). It is the auto-compact signal with no mid-Exchange consumer (ADR 0014 §5) — a task
// that has grown too big while the model works is a candidate to re-plan as a fan-out.
func guidedDecompositionMidExchangeOversized(conv domain.ConversationView, budget domain.Budget) bool {
	if !guidedDecompositionLastAssistantCalledTools(conv) {
		return false
	}
	totalChars := 0
	conv.Range(func(_ int, m domain.Message) bool {
		totalChars += len(m.Content)
		return true
	})
	return guidedDecompositionEstimateTokens(totalChars, budget.CharsPerToken) > budget.History
}

// guidedDecompositionLastAssistantCalledTools reports whether the most recent assistant message
// issued tool calls — the "model is mid-work" half of signal B.
func guidedDecompositionLastAssistantCalledTools(conv domain.ConversationView) bool {
	for i := conv.Len() - 1; i >= 0; i-- {
		if m := conv.At(i); m.Role == domain.RoleAssistant {
			return len(m.ToolCalls) > 0
		}
	}
	return false
}

// guidedDecompositionEstimateTokens converts a character count to an estimated token count with the
// calibrated chars→token ratio (the tool_result_cap idiom, inverted). A non-positive ratio yields 0,
// so a comparison against any positive threshold is false — the gate is inert until the estimator
// has calibrated.
func guidedDecompositionEstimateTokens(chars int, charsPerToken float64) int {
	if charsPerToken <= 0 {
		return 0
	}
	return int(float64(chars) / charsPerToken)
}

package mechanisms

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// guided_decomposition registers the guided-decomposition Mechanism in the catalogue constructor
// table (ADR 0014). Default-off (D1) — the config surface builds it only when the `mechanisms:`
// block enables it, and it is benched as a stack with tool_result_cap (Requires, below) so the
// bench measures the two together. It steers the PRIMARY call, on an oversized task, to first
// enumerate the work as a numbered list of self-contained subtasks, then serializes the fan-out one
// sub_agent delegation per Turn. Two halves on one struct: the pre-request gate + enumeration steer,
// and the PostResponse intercept + serialized follow-through (both below).
func init() {
	catalogue[guidedDecompositionID] = newGuidedDecomposition
	descriptors[guidedDecompositionID] = guidedDecompositionDescriptor
}

const guidedDecompositionID domain.MechanismID = "guided_decomposition"

// guidedDecompositionMaxSubtasks bounds the enumeration the steer asks for (ADR 0014 locked
// decision 5: at most 7 subtasks). Tuning is the bench's job, not code-review taste. The intercept's
// accept window (the 2..12 bound below) is a separate, wider tolerance.
const guidedDecompositionMaxSubtasks = 7

// guidedDecompositionMinSubtasks / guidedDecompositionMaxAcceptedSubtasks bound the intercept's
// accept window (ADR 0014 locked decision 5): the post-response half declines the WHOLE enumeration
// — a benign no-op, never a truncation — when the parsed list holds fewer than 2 or more than 12
// items. The upper bound is deliberately wider than the steer's 7-item ask so a model that overshoots
// the ask by a little still fans out; a runaway or degenerate list is declined. Tuning is the bench's.
const (
	guidedDecompositionMinSubtasks         = 2
	guidedDecompositionMaxAcceptedSubtasks = 12
)

// guidedDecompositionReportHygiene is the compact-report ask (ADR 0014 §4) appended to every
// delegated task and named in the follow-through directive. Serialized child reports accumulate in
// one Exchange that no generative reducer can fold mid-Exchange, so each child is asked to report
// tersely to keep the accumulation small — tool_result_cap (the Required peer) caps whatever is left.
const guidedDecompositionReportHygiene = "When done, report back a single compact result — the key " +
	"findings, decisions, and file paths only, not a step-by-step narration of what you did."

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

// guidedDecompositionDescriptor identifies guided_decomposition as a strikes-3 proactive-nudge
// Mechanism (ADR 0014 §1): disabled under Bypass (D5) and withdrawn by self-regulation after
// repeated non-help. It is IncompatibleWith decompose — the two steer the same "task too big"
// symptom through different means (delegation vs prompt wording) and must not stack (locked decision
// 2) — and Requires tool_result_cap, the peer it is benched as a stack with; enabling it without
// tool_result_cap is a startup error (ValidateRequirements, locked decision 3 / ADR 0014 §4).
var guidedDecompositionDescriptor = domain.MechanismDescriptor{
	ID:               guidedDecompositionID,
	Capability:       domain.CapProactiveNudge,
	Suppression:      domain.SuppressStrikesThree,
	IncompatibleWith: []domain.MechanismID{decomposeID},
	Requires:         []domain.MechanismID{toolResultCapID},
}

// Descriptor returns guided_decomposition's static catalogue descriptor.
func (guidedDecompositionMechanism) Descriptor() domain.MechanismDescriptor {
	return guidedDecompositionDescriptor
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
//   - no steer or fan-out directive may already be outstanding in this REQUEST — the same-request
//     double-steer guard (guidedDecompositionOutstanding, keyed on the request-scoped markers);
//   - no fan-out may have begun yet in the current Exchange — the once-per-Exchange guard on COMMITTED
//     evidence (guidedDecompositionFanOutBegun, F1): once any assistant message after the last user ask
//     carries a sub_agent call the gate stays quiet for the rest of the Exchange, so it cannot re-steer
//     on the synthesis Turn after the request-scoped markers have drained;
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
		return nil // a steer or a fan-out directive is already steering this request — no double-steer
	}
	if guidedDecompositionFanOutBegun(conv) {
		return nil // a fan-out has already begun this Exchange (committed sub_agent call) — steer at most once per Exchange (F1)
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

// guidedDecompositionFanOutBegun reports whether a fan-out has already begun in the current Exchange,
// judged from COMMITTED history alone (F1 — the once-per-Exchange rule): true when any assistant
// message after the last RoleUser message carries a sub_agent tool call. That single predicate
// subsumes both of §5's silence conditions — the item-2 enumeration anchor commits as its verbatim
// list PLUS the synthesized first sub_agent call, and a model that delegated unprompted this Exchange
// likewise carries one — so once it holds a steer adds nothing (the model is already delegating) and
// the gate stays quiet for the remainder of the Exchange.
//
// Unlike guidedDecompositionOutstanding this reads committed evidence, not the request-scoped markers:
// the steer rides an InjectContext copy and the directive rides the deferred drain, so both vanish
// from the next request the moment nothing is re-deferred (the synthesis Turn). Committed sub_agent
// calls do not vanish — the child reports grew honest history and mid-Exchange auto-compaction cannot
// run — so this is what stops the gate re-steering on the synthesis Turn once the markers are gone and
// signal B still reads oversized. A new Exchange (a new last RoleUser message) moves the window
// forward and re-arms the gate.
func guidedDecompositionFanOutBegun(conv domain.ConversationView) bool {
	if conv == nil {
		return false
	}
	for i := guidedDecompositionCurrentExchangeStart(conv); i < conv.Len(); i++ {
		if m := conv.At(i); m.Role == domain.RoleAssistant && guidedDecompositionHasSubAgentCall(m.ToolCalls) {
			return true
		}
	}
	return false
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

// PostResponse is the intercept + serialized follow-through half (ADR 0014 §2/§3). Like the gate it
// carries no per-Mechanism state: the remaining-items queue is re-DERIVED from honest history each
// Turn (locked decision 1) — the enumeration is the model's own list+delegation message in the
// current Exchange and the dispatched tasks are the sub_agent calls in that Exchange — so it is
// snapshot/resume-safe and abandons cleanly on suppression. Three cases, evaluated in order against
// resp.View().Conversation():
//
//   - Enumeration response: the pre-request steer is outstanding in the request (its marker is in the
//     conversation), the model replied with ONLY a subtask list and no tool calls, that list is
//     bounded (2..12), AND a strict majority of its lines carried an explicit ordered/bullet marker
//     (F4 — a compliant numbered reply passes; prose, a clarifying question, or a refusal does not).
//     Synthesize the FIRST sub_agent delegation onto the response — text left verbatim (locked
//     decision 4) — and Defer a directive carrying the remaining subtasks for the next Turn.
//   - Fan-out follow-through or off-script tool Turn: a directive is steering (its marker is in the
//     request) and the model called at least one tool this Turn — the requested sub_agent delegation
//     OR some other, off-script tool (F2). Re-derive the remainder from history MINUS every dispatched
//     task (this Turn's calls included) and Defer the shrunken directive. An off-script call carries
//     no dispatched task, so it re-defers the remainder intact — the directive would otherwise drain
//     away this Turn and drop the whole fan-out (the High "one off-script tool call mid-fan-out
//     silently drops the queue"). An empty remainder ends the fan-out with no decision.
//   - Anything else: inspect-only no-op — no marker, an out-of-bounds or minority-marked reply
//     (declined whole — prose is not an enumeration, F4), a directive-steered no-tool final answer
//     (the model closed the fan-out with its own answer — never re-deferred, the accepted §5
//     fail-soft; item 7 expires any queue residue at the Exchange boundary), or an exhausted
//     remainder (ADR 0014 §5 fail-soft; zero revision, zero Action, so the loop books no fire, R4).
//
// Suppression needs no code (locked decision 1): once self-regulation withdraws the Mechanism the
// hook stops being dispatched, the un-consumed directive is never re-derived, and at most one
// already-queued directive still drains via the loop's shared deferred-correction plumbing — that
// trailing inject is loop plumbing every Defer user shares, not a Mechanism fire.
func (guidedDecompositionMechanism) PostResponse(_ context.Context, resp *domain.Response) (domain.PostResponseDecision, error) {
	conv := resp.View().Conversation()
	calls := resp.ToolCalls()

	// Enumeration response: the steer is outstanding and the model answered with a bare subtask
	// list. Synthesize the first delegation and defer the remainder.
	if len(calls) == 0 && guidedDecompositionMarkerPresent(conv, guidedDecompositionSteerMarker) {
		items, marked := guidedDecompositionParseMarkedList(resp.Text())
		if !guidedDecompositionListInBounds(items) || !guidedDecompositionMajorityMarked(items, marked) {
			// Out of bounds, or a minority of marked lines (prose, a clarifying question, a refusal)
			// — decline the whole reply, never a partial truncation (F4 / §5).
			return domain.PostResponseDecision{}, nil
		}
		resp.AppendToolCall(domain.ToolCall{
			ID:        fmt.Sprintf("text_call_%d", resp.View().Turn()), // the loop's synthesized-call style
			Tool:      tools.SubAgentToolName,
			Arguments: guidedDecompositionTaskArgs(items[0]),
		})
		return domain.PostResponseDecision{Action: domain.ActionDefer, Inject: guidedDecompositionDirective(items[1:])}, nil
	}

	// Fan-out follow-through or off-script tool Turn: a directive is steering and the model called at
	// least one tool this Turn — the requested sub_agent delegation OR some other, off-script tool
	// (F2). Either way the drained directive would vanish this Turn, so re-derive the remainder
	// (history + this Turn's calls; an off-script call contributes no task and shrinks nothing) and
	// re-defer the shrunken directive. This keeps the fan-out alive across a single off-script tool
	// call instead of silently dropping the queue. An empty remainder ends the fan-out with no
	// decision; a no-tool final answer never reaches here (F2 — that closes the Exchange).
	if guidedDecompositionMarkerPresent(conv, guidedDecompositionDirectiveMarker) && len(calls) > 0 {
		if remainder := guidedDecompositionRemainder(conv, calls); len(remainder) > 0 {
			return domain.PostResponseDecision{Action: domain.ActionDefer, Inject: guidedDecompositionDirective(remainder)}, nil
		}
	}

	// Anything else: benign inspect-only no-op (§5 fail-soft).
	return domain.PostResponseDecision{}, nil
}

// guidedDecompositionMarkerPresent reports whether any message content carries marker — the
// history scan the intercept keys its two active cases on (the request-scoped steer for the
// enumeration case, the drained directive for the follow-through case). A nil view (the degraded
// no-view Response) carries no marker.
func guidedDecompositionMarkerPresent(conv domain.ConversationView, marker string) bool {
	if conv == nil {
		return false
	}
	found := false
	conv.Range(func(_ int, m domain.Message) bool {
		if strings.Contains(m.Content, marker) {
			found = true
			return false
		}
		return true
	})
	return found
}

// guidedDecompositionHasSubAgentCall reports whether the response carries at least one sub_agent
// delegation the model emitted itself — the follow-through trigger (the model followed the directive).
func guidedDecompositionHasSubAgentCall(calls []domain.ToolCall) bool {
	for _, c := range calls {
		if c.Tool == tools.SubAgentToolName {
			return true
		}
	}
	return false
}

// guidedDecompositionListInBounds reports whether a parsed enumeration is within the intercept's
// accept window (locked decision 5). Out of bounds → the caller declines the whole list, never trims.
func guidedDecompositionListInBounds(items []string) bool {
	return len(items) >= guidedDecompositionMinSubtasks && len(items) <= guidedDecompositionMaxAcceptedSubtasks
}

// guidedDecompositionTaskArgs renders the sub_agent argument shape for one subtask, appending the
// compact-report hygiene ask (ADR 0014 §4). It marshals through tools.SubAgentArgs so the wire shape
// is the exact schema dispatch parses — the args are indistinguishable from a model-emitted call.
func guidedDecompositionTaskArgs(item string) json.RawMessage {
	args, _ := json.Marshal(tools.SubAgentArgs{Task: item + " " + guidedDecompositionReportHygiene})
	return args
}

// guidedDecompositionDirective renders the remaining-items directive deferred into the next request
// (ADR 0014 §3). It embeds guidedDecompositionDirectiveMarker verbatim (so the pre-request gate reads
// a fan-out as in flight and stays quiet, and the follow-through case recognises it), lists the
// remaining subtasks verbatim, asks for exactly ONE delegation this Turn carrying the same hygiene
// ask, and asks the model to synthesize from all reports once none remain.
func guidedDecompositionDirective(remaining []string) string {
	var b strings.Builder
	fmt.Fprintf(&b,
		"%s (%d left): the primary task is being fanned out one delegation per turn. Delegate EXACTLY "+
			"the next subtask now via a single %s call — do not do the work yourself, and do not delegate "+
			"more than one at a time. Give the sub-agent this instruction too: %q. The remaining subtasks, "+
			"in order:\n",
		guidedDecompositionDirectiveMarker, len(remaining), tools.SubAgentToolName, guidedDecompositionReportHygiene)
	for i, item := range remaining {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	b.WriteString("Once no subtasks remain, stop delegating and synthesize a single final answer from all the sub-agent reports.")
	return b.String()
}

// guidedDecompositionRemainder re-derives the outstanding subtasks from honest history (locked
// decision 1): the enumeration is the model's own list+delegation message in the current Exchange,
// and the dispatched tasks are the sub_agent calls recorded in that same Exchange plus this Turn's
// own calls. It reads the CALLS, never the child results, so a report capped by tool_result_cap (the
// Required peer) leaves the cursor's ground truth intact. Consumption is EXACT-MATCH and CONSUME-ONCE
// (item 3): an enumeration item is removed only by a dispatched task equal to the item itself or to
// the item plus the appended hygiene ask, and each dispatched task consumes at most one item
// occurrence — so two identical items need two dispatches, and a longer prefix-nested item never
// absorbs a shorter one's dispatch. A model-authored task that matches no item consumes nothing and
// leaves the remainder intact (off-script, tolerated — §5).
func guidedDecompositionRemainder(conv domain.ConversationView, respCalls []domain.ToolCall) []string {
	items := guidedDecompositionEnumeration(conv)
	if len(items) == 0 {
		return nil
	}
	dispatched := append(guidedDecompositionDispatchedTasks(conv), guidedDecompositionCallTasks(respCalls)...)
	consumed := make([]bool, len(dispatched))
	var remainder []string
	for _, item := range items {
		if idx := guidedDecompositionMatchingDispatch(item, dispatched, consumed); idx >= 0 {
			consumed[idx] = true
			continue
		}
		remainder = append(remainder, item)
	}
	return remainder
}

// guidedDecompositionEnumeration returns the enumeration list from honest history: the FIRST
// assistant message WITHIN THE CURRENT EXCHANGE (the messages after the last RoleUser message — the
// original ask) that BOTH parses as an in-bounds (2..12) subtask list AND carries at least one
// sub_agent tool call. That pair uniquely identifies the real enumeration (F3): the case-1 intercept
// commits it as the verbatim list text PLUS the synthesized first delegation, so no other message
// shares both traits. A prior-Exchange final answer or mid-Exchange narration may parse as a list but
// carries no sub_agent call; a compaction summary is a multi-line RoleAssistant message with no call
// either; and scoping to the current Exchange stops a previous Exchange's fan-out from anchoring this
// one. Both traits are load-bearing — dropping either lets those decoys shadow the true list.
func guidedDecompositionEnumeration(conv domain.ConversationView) []string {
	if conv == nil {
		return nil
	}
	for i := guidedDecompositionCurrentExchangeStart(conv); i < conv.Len(); i++ {
		m := conv.At(i)
		if m.Role != domain.RoleAssistant || !guidedDecompositionHasSubAgentCall(m.ToolCalls) {
			continue
		}
		if parsed := guidedDecompositionParseList(m.Content); guidedDecompositionListInBounds(parsed) {
			return parsed
		}
	}
	return nil
}

// guidedDecompositionDispatchedTasks collects the task text of every sub_agent call recorded on the
// assistant messages of the CURRENT EXCHANGE (after the last RoleUser message) — the dispatched half
// of the honest-history cursor, scoped to this Exchange so a previous Exchange's fan-out cannot
// consume this one's items. It reads the CALLS, never the child results.
func guidedDecompositionDispatchedTasks(conv domain.ConversationView) []string {
	if conv == nil {
		return nil
	}
	var tasks []string
	for i := guidedDecompositionCurrentExchangeStart(conv); i < conv.Len(); i++ {
		if m := conv.At(i); m.Role == domain.RoleAssistant {
			tasks = append(tasks, guidedDecompositionCallTasks(m.ToolCalls)...)
		}
	}
	return tasks
}

// guidedDecompositionCurrentExchangeStart returns the index of the first message of the current
// Exchange — the message just after the last RoleUser message (the current ask). Injected steers and
// the drained directive land before that user message or in the system message, never after it
// (loop.go / Request.InjectContext), so this boundary is stable across injections — the shared-context
// invariant behind F1/F3. With no user message present the whole conversation is the current Exchange.
func guidedDecompositionCurrentExchangeStart(conv domain.ConversationView) int {
	if _, idx, ok := conv.LastUser(); ok {
		return idx + 1
	}
	return 0
}

// guidedDecompositionCallTasks extracts the task string from each sub_agent call in calls, parsing
// the arguments through tools.SubAgentArgs. A non-sub_agent call, unparseable arguments, or an empty
// task contributes nothing.
func guidedDecompositionCallTasks(calls []domain.ToolCall) []string {
	var tasks []string
	for _, c := range calls {
		if c.Tool != tools.SubAgentToolName {
			continue
		}
		var args tools.SubAgentArgs
		if err := json.Unmarshal(c.Arguments, &args); err != nil {
			continue
		}
		if args.Task != "" {
			tasks = append(tasks, args.Task)
		}
	}
	return tasks
}

// guidedDecompositionMatchingDispatch returns the index of the first not-yet-consumed dispatched task
// that EXACTLY matches enumeration item — the task equals the item itself or the item plus the
// appended report-hygiene ask (the two legitimate shapes a dispatched task takes) — or -1 when none
// matches (item 3). Exactness stops a longer prefix-nested item's dispatch from also absorbing a
// shorter item, and the consumed marks make the accounting consume-once: the caller flags the
// returned dispatch so no second item can claim it, so duplicate items each need their own dispatch.
// An off-script model task that matches no item returns -1, leaving the remainder intact (the
// ADR 0014 §5 tolerance).
func guidedDecompositionMatchingDispatch(item string, dispatched []string, consumed []bool) int {
	for i, task := range dispatched {
		if consumed[i] {
			continue
		}
		if task == item || task == item+" "+guidedDecompositionReportHygiene {
			return i
		}
	}
	return -1
}

// guidedDecompositionParseMarkedList parses the model's enumeration into ordered subtask strings and
// reports how many kept lines carried an explicit ordered/bullet marker. It stays lenient about the
// item text (ADR 0014 §2 — the steer asks for a numbered list, but small models emit bulleted, plain,
// or fence-wrapped variants): each non-blank line becomes one item with any leading list marker
// stripped; blank lines and Markdown code-fence delimiters are dropped. Neither the count bounds nor
// the marked-majority test are enforced here — the case-1 intercept applies both (F4 / locked
// decision 5), declining prose (a minority of marked lines) and out-of-bounds counts as a whole.
func guidedDecompositionParseMarkedList(text string) (items []string, marked int) {
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "```") {
			continue // blank line or a code-fence delimiter — noise, not a subtask
		}
		item, hadMarker := guidedDecompositionStripMarker(line)
		if item == "" {
			continue
		}
		items = append(items, item)
		if hadMarker {
			marked++
		}
	}
	return items, marked
}

// guidedDecompositionParseList returns just the ordered subtask strings of an enumeration, discarding
// the marked-line count — the items-only view guidedDecompositionEnumeration re-derives the cursor
// from. The marked count is the case-1 intercept's concern (F4); see guidedDecompositionParseMarkedList.
func guidedDecompositionParseList(text string) []string {
	items, _ := guidedDecompositionParseMarkedList(text)
	return items
}

// guidedDecompositionMajorityMarked reports whether a strict majority of the parsed items carried an
// explicit list marker (F4). The steer asks for a numbered list, so a compliant reply passes
// trivially; a clarifying question, refusal, or multi-line prose answer — where explicit markers are
// a minority (or absent) — does not. An empty list is not a majority.
func guidedDecompositionMajorityMarked(items []string, marked int) bool {
	return marked*2 > len(items)
}

// guidedDecompositionStripMarker removes a leading ordered- or unordered-list marker from a line and
// returns the bare subtask text plus whether an explicit list marker was present. A bullet is a
// single "-", "*", "•", or "+" rune followed by whitespace; an ordered marker is one or more digits
// followed (optionally after spaces) by a ".", ")", "-", or ":" delimiter. A line with no recognised
// marker is returned verbatim with marked=false, so a plain-line item is still kept — but the case-1
// intercept counts the marked lines to tell a compliant enumeration from prose (F4).
func guidedDecompositionStripMarker(line string) (item string, marked bool) {
	r := []rune(line)
	if len(r) >= 2 && strings.ContainsRune("-*•+", r[0]) && unicode.IsSpace(r[1]) {
		return strings.TrimSpace(string(r[1:])), true
	}
	i := 0
	for i < len(r) && unicode.IsDigit(r[i]) {
		i++
	}
	if i == 0 {
		return line, false // no leading number — a plain-line item, kept verbatim, unmarked
	}
	j := i
	for j < len(r) && r[j] == ' ' {
		j++
	}
	if j < len(r) && strings.ContainsRune(".)-:", r[j]) {
		j++
		for j < len(r) && r[j] == ' ' {
			j++
		}
		return strings.TrimSpace(string(r[j:])), true
	}
	return line, false // digits that are not a list marker — keep the line verbatim, unmarked
}

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/airiclenz/apogee/internal/console"
	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
	"github.com/airiclenz/apogee/internal/tasklist"
	"github.com/airiclenz/apogee/internal/tools"
	"github.com/airiclenz/apogee/internal/undo"
)

// dispatchOutcome reports whether a Turn's tool dispatch ran to completion or was cut short
// by a ctx cancellation (which rolls the whole Turn back).
type dispatchOutcome int

const (
	dispatchDone dispatchOutcome = iota
	dispatchCancelled
	// dispatchConfinementUnavailable reports that a Confine subprocess call could not be
	// confined at run time (the Confiner returned ErrConfinementUnavailable). The call did NOT
	// run; the executor follows the verdict's precomputed fallback — a forced Approval gate
	// whose allow-continuation re-runs the call unconfined (Resolution D4;
	// confinement-execution-contract §4).
	dispatchConfinementUnavailable
)

// dispatchTools runs each requested tool call through the pre-tool-exec reactions, the Approval
// gate, execution, and the post-tool-result reactions — appending each result to the
// conversation as a tool message and emitting the observability events. Approval is
// consulted here, AFTER the stream has closed (the §6 #6 resolution: stream fully, then
// gate), so a blocking Approver never holds an open Upstream connection.
//
// A reply's calls are PARTITIONED first (ADR 0039 decision 11): the leaf tools run first, in
// their emitted order and one at a time (dispatchGroup at width 1), and the sub_agent delegations
// run after them as one group, at the width fanOutWidthFor snapshots for that reply. The order is a property of the reply alone, not of
// the bound server's fan-out width, so the same reply produces the same history whether the
// group then runs concurrently or serially — a write a child depends on lands before any child
// starts, and the model maps results back by call ID either way.
//
// It returns dispatchCancelled only if ctx was cancelled while a tool was approving or
// executing; the caller then rolls the Turn back. Every other failure — an unknown tool, a
// denied call, a tool error, a recovered tool panic — becomes an error tool-result the
// model sees on the next Turn, and dispatch continues to the next call (ADR 0007).
func (a *Agent) dispatchTools(ctx context.Context, turn int, calls []domain.ToolCall) dispatchOutcome {
	leaves, delegations := partitionDispatch(calls)
	if outcome := a.dispatchGroup(ctx, turn, 1, leaves); outcome == dispatchCancelled {
		return dispatchCancelled
	}
	return a.dispatchGroup(ctx, turn, a.fanOutWidthFor(delegations), delegations)
}

// partitionDispatch splits a reply's calls into the leaf tools and the sub_agent delegations,
// each group keeping its emitted order (ADR 0039 decision 11). It is a pure function of the
// call list: nothing about the bound server, the depth, or the cap reaches it, so the dispatch
// ORDER a reply produces is fixed even when the fan-out width is not.
func partitionDispatch(calls []domain.ToolCall) (leaves, delegations []domain.ToolCall) {
	for _, call := range calls {
		if isSubAgentCall(call) {
			delegations = append(delegations, call)
			continue
		}
		leaves = append(leaves, call)
	}
	return leaves, delegations
}

// fanOutWidth reports how many of a reply's delegations may run at once: min(cap, group size)
// at depth 0 when the Parallel agents cap (ADR 0039 decision 2) allows more than one, and 1 —
// meaning "run the group serially, exactly as this loop always has" — otherwise. A group of
// one is never worth a pool; everything else is delegationWidth's rule.
//
// It is reached ONCE per reply — through fanOutWidthFor, the seat-aware form dispatchTools calls —
// and the number is handed DOWN as an argument, which is what makes a group's width immutable for
// the life of that group: the Delegation target it may have been resolved from is re-stated on
// every heartbeat of the Sub-agent server (ADR 0045), so a beat landing between the first child and
// the last must not be able to re-size a pool that is already running. One reply, one width,
// however many beats cross it.
func (a *Agent) fanOutWidth(delegations int) int {
	if delegations < 2 {
		return 1
	}
	return min(a.delegationWidth(), delegations)
}

// fanOutWidthFor is fanOutWidth read against the SEATS this particular reply named (ADR 0069):
// with `run_on` on the menu a single reply may put some children on the session server and the rest
// on the Sub-agent server, and the two seats have caps of their own. A reply split across both is
// sized by min(session cap, target cap) — the smaller of the two, because ONE pool runs the whole
// group and a pool wider than either server's cap would overrun that server whichever children
// happened to be in flight. A reply that lands entirely on one seat is not split at all and keeps
// THAT seat's own width: for the Sub-agent server that is fanOutWidth's rule verbatim, and for the
// session seat it is the session server's cap even while a target is latched — ADR 0069 decision
// 7's "a single-seat reply keeps its seat's cap", which fanOutWidth cannot honour on its own
// because delegationCap answers with the latched target's cap whenever a target exists.
//
// The smaller cap is deliberately the whole rule rather than a per-seat accounting: two pools, or
// slots counted per seat, would buy width in the mixed case at the price of a second scheduler in
// the dispatch path — and the mixed case is a reply the model chose to split, not the shape most
// replies take. One reply, one width, however the seats fall (the once-per-reply snapshot rule the
// doc above states, now read for two servers instead of one).
//
// Like fanOutWidth it is called ONCE per reply, and it takes ONE latch snapshot for the whole
// classification: a beat landing between two calls of the same reply must not be able to make the
// group look split when it was not.
func (a *Agent) fanOutWidthFor(calls []domain.ToolCall) int {
	if len(calls) < 2 || a.isDelegate() {
		return 1
	}
	target := a.delegationTarget()
	if !a.seatsAreSplit(calls, target) {
		// ADR 0069 decision 7, the one case fanOutWidth gets wrong: with a target latched
		// delegationCap answers with the TARGET's cap, but a reply whose every call asked for
		// the session seat runs entirely on the session server and must be sized by ITS cap.
		// Gated beside the target on publishesSeatChoice exactly as seatsAreSplit is — under
		// `sub-agents-choice: fixed` run_on is ignored (subagent.go), so an all-session reply
		// still runs on the target and keeps the target's cap.
		if target != nil && publishesSeatChoice(a.tools) && a.allAskedSession(calls) {
			if width := a.parallelAgentsCap(); width > 1 {
				return min(width, len(calls))
			}
			return 1
		}
		return a.fanOutWidth(len(calls))
	}
	// A split reply always has a usable target — a child only lands on the far seat because one is
	// latched — so the cap below is read off a non-nil target by construction.
	width := min(a.parallelAgentsCap(), target.ParallelAgents)
	if width < 2 {
		return 1
	}
	return min(width, len(calls))
}

// seatsAreSplit reports whether calls put children on BOTH Delegation seats, given the target
// latched for the whole group. A call lands on the session server when it ASKED for it and when
// nothing is latched to route it anywhere else; every other call lands on the Sub-agent server. So
// a split needs a latched target and at least one explicit `run_on: "session"` beside at least one
// call that is not one — which is why an unrouted session, a session under `sub-agents-choice:
// fixed`, and every depth below the first can never be split, and never pay for the classification.
//
// The seat is read exactly as runSubAgent reads it, through the same two gates: the roster must
// have PUBLISHED `run_on` for the argument to mean anything, and an unparseable value is not a seat
// at all. It differs only in what it does with a bad one — that call is refused before it spawns
// (subagent.go), so counting it as the configured seat here can only mis-size a group by one slot
// it will never use, where an error would abandon a whole reply's fan-out over one malformed
// argument.
func (a *Agent) seatsAreSplit(calls []domain.ToolCall, target *DelegationTarget) bool {
	if target == nil || !publishesSeatChoice(a.tools) {
		return false
	}
	var onSession, onSubAgentsServer bool
	for _, call := range calls {
		if a.askedSeat(call) == seatSession {
			onSession = true
			continue
		}
		onSubAgentsServer = true
	}
	return onSession && onSubAgentsServer
}

// allAskedSession reports whether EVERY call in the reply EXPLICITLY asked for the session seat.
// The explicitness is the whole point: askedSeat answers seatConfigured for a call that named no
// seat and for one whose argument does not parse, and neither is an ask — both land wherever the
// configured default routes them, which with a target latched is the Sub-agent server. So a reply
// mixing an explicit `run_on: "session"` with an unnamed call is not an all-session reply and is
// sized by the ordinary rule.
func (a *Agent) allAskedSession(calls []domain.ToolCall) bool {
	for _, call := range calls {
		if a.askedSeat(call) != seatSession {
			return false
		}
	}
	return true
}

// askedSeat reports the Delegation seat one call named, seatConfigured for every call that named
// none — which includes a call whose arguments do not parse and one naming a value outside the
// enum, both of which runSubAgent refuses on their own account before a child exists.
func (a *Agent) askedSeat(call domain.ToolCall) delegationSeat {
	var args tools.SubAgentArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return seatConfigured
	}
	seat, err := parseDelegationSeat(args.RunOn)
	if err != nil {
		return seatConfigured
	}
	return seat
}

// fanOutCeiling reports how many sub_agent delegations ONE reply may fan out before the rest are
// refused (refusePastCeiling): `delegate-fanout-rounds` rounds of the width the engine STATES for
// this agent (statedDelegationWidth — the far server's latched cap, else the session server's, 1
// on a delegate), or 0 for no ceiling when rounds is 0. It is the enforced number and the
// announced one at once — the orientation block's delegation bounds read the same function — so
// what the model is told it may fan out is exactly what dispatch lets through.
//
// There is deliberately NO floor and the rule applies at every depth (owner, 2026-09-20): an
// unkeyed, unpinned local server has width 1, so at the default two rounds a reply of three
// delegations there refuses the third, and a delegate's ceiling is `rounds` outright. The ceiling
// is a bound on how much one reply may commit the coordinator to before it reads a single result —
// the 56-dispatch reply this closes had 35 that never started — and a floor would re-open that on
// the narrow servers where it costs the most.
func (a *Agent) fanOutCeiling() int {
	return fanOutCeilingOf(a.cfg.Delegation.FanOutRounds, a.statedDelegationWidth())
}

// fanOutCeilingOf is the one formula behind fanOutCeiling — rounds × width, 0 when rounds is 0 —
// kept apart so the refusal text (fanOutCeilingResult) states the very number it enforces.
func fanOutCeilingOf(rounds, width int) int {
	if rounds <= 0 {
		return 0
	}
	return rounds * width
}

// delegationWidth reports how many sub_agent delegations THIS agent may run at once,
// independent of any particular reply: the resolved Parallel agents cap at depth 0, and 1
// everywhere else.
//
// Depth 0 is the whole eligibility rule (decision 3): a child's own delegations stay serial
// inline, so there is no slot accounting across levels and no way for a nested fan-out to hold
// slots its own children need. It is one rule with two readers — the pool below sizes itself by
// it through fanOutWidthFor, and buildRequest stamps it onto the reaction-facing view
// (LoopView.ParallelAgents) so a Reaction synthesizing delegations batches by the same width the
// engine will honour. That second reader is why such a batch needs nothing of its
// own to follow a routed cap (ADR 0045 §5): its min(cap, remaining) reads the view, the view
// carries this number, and this number already knows which server the children will run on.
//
// It is the DEFAULT seat's width, and stays so with seat choice on the menu (ADR 0069): the view
// is a BATCH HINT, read before any reply exists, and a synthesised delegation names no `run_on` —
// so the seat it will take is precisely the one this number is resolved for. Only a reply the model
// SPLIT across both seats is sized differently, and that is a fact about one reply rather than
// about this agent, which is why it lives in fanOutWidthFor and not here.
func (a *Agent) delegationWidth() int {
	if a.isDelegate() {
		return 1
	}
	if width := a.delegationCap(); width > 1 {
		return width
	}
	return 1
}

// delegationCap reports WHOSE Parallel agents cap governs this agent's delegations. The slots a
// fan-out spends belong to the server the children actually run on, so a latched Delegation
// target answers with ITS cap — the Sub-agent server's pin, else the slot count its own heartbeat
// observed (ADR 0045 §5) — and the bound session server's live cap answers otherwise. The
// otherwise is also the fallback: a target that goes unusable puts the children back on this
// session's Upstream, and the width follows them home on the next dispatch.
//
// A routed cap REPLACES rather than bounds: a grunt box with more slots widens the fan-out past
// the orchestrator's, and a single-slot one narrows it to serial however many the session server
// advertises. Both directions are the same rule — the width follows the work, not the asker.
func (a *Agent) delegationCap() int {
	if target := a.delegationTarget(); target != nil {
		return target.ParallelAgents
	}
	return a.parallelAgentsCap()
}

// ----------------------------------------------------------------------------
// The per-call pipeline (ADR 0039 — Parallel agents)
// ----------------------------------------------------------------------------
//
// Every tool call, leaf or delegation, crosses ONE pipeline of three phases. prepareCall carries a
// call from its ToolCallEvent through the pre-tool-exec Moment, the dispatch facts, the Resolution
// and the gate stage to a verdict — or to a final result when nothing may run. runCall executes
// the verdict: a leaf's run/gate/confine arm, or the child run behind runSubAgent's recover
// boundary. commitCall lands the result: the audit record a delegation earns, the post-tool-result
// Moment, and the append into history. Width is the one parameter (dispatchGroup). At width 1 the
// three phases run per call, in emitted order — a call's result is in history before the next
// call's ToolCallEvent is emitted, the loop this dispatch has always been (ADR 0039 decision 1:
// a cap of 1 reproduces that behaviour exactly; decision 3: a child's own delegations run serially
// inline). Above 1 the group is prepared whole, run through a bounded pool, and committed whole,
// still in emitted order.
//
// The fan-out is deliberately NOT "run the whole pipeline on N goroutines": only the RUN phase is
// concurrent. Everything a call shares with its siblings — the pre-tool-exec Moment, the guardrail
// probe and the Resolution, the audit record, the post-tool-result Moment, and the append into
// history — stays on the dispatching goroutine, in emitted-call order, on either side of the
// pool. That is what keeps the Agent's own state (reactions, guards, conversation) single-goroutine
// while N children run, and what makes the resulting history DETERMINISTIC regardless of which
// child finishes first. A cancellation is answered between the last two phases — every child is
// joined first, then the whole group is discarded unappended, so the parent Turn rolls back with
// no partial delegation in history (ADR 0013 §5, now N-wide). dispatchTools hands the pool
// delegations only — the leaf group always runs at width 1 — but the phases themselves are blind
// to a call's kind, which is what makes a call's disposition a property of the call alone and
// never of the width its group happened to run under.

// dispatchSlot is one call's state as it crosses the pipeline: what prepareCall decided about the
// call before anything ran, what runCall produced, and how it ended. Each slot is written by
// exactly one goroutine at a time — the dispatching one in the prepare and commit phases, one
// pool worker in between — so a group's slice needs no lock.
type dispatchSlot struct {
	call    domain.ToolCall
	tool    domain.Tool
	verdict resolution
	// writeTarget is the call's classified write target — the resolved absolute path of a
	// workspace-scoped writer's target, "" for every other call — carried from the ladder's one
	// resolution to the commit point, so the ToolResultEvent is stamped from the same resolution
	// the ladder decided on. A slot that never resolved (a route before resolve()) carries "".
	writeTarget string
	result      domain.ToolResult
	// run marks a verdict left to execute — a leaf Run, Gate or Confine, or a Delegate whose
	// child still has to run. A refused (or unknown-tool, malformed, or hook-failed) slot already
	// holds its final result. The pool worker that SKIPS a delegation for a pending interjection
	// clears it too, so commitCall treats the skipped slot exactly as a refused one: no audit
	// record for a child that never ran.
	run bool
	// ceilingRefused marks a delegation refused past the reply's fan-out ceiling
	// (refusePastCeiling): it holds its final result like a skipped slot, and it is the one refusal
	// dispatchGroup books a delegate-ledger row for itself — the call never enters runSubAgent, the
	// ledger's own site, and the model's next request must still count it (children.go).
	ceilingRefused bool
	// delegated marks a child that ran to a result: runCall sets it as the child returns, and
	// commitCall books the audit record it earns — on the dispatching goroutine, in call order.
	// A leaf's arms book their own record inside runCall (executeRun, executeGate, executeConfine),
	// per outcome rather than per kind, so commitCall never reads the verdict's kind.
	delegated bool
	// hookFailed marks a pre-tool-exec reaction failure, whose result is appended WITHOUT the
	// productivity signal and the post-tool-result reactions.
	hookFailed bool
	outcome    dispatchOutcome
	// widthNote is the group's delegation-width line (fanOutWidthNote), set on the LAST slot of a
	// group that ran wider than its width with every slot a real child, and empty everywhere else.
	// It is decided after the pool joins — only then is it known that no slot was skipped — and
	// appended at commit, so the audit record keeps the child's own result.
	widthNote string
}

// dispatchGroup runs one group of calls through the pipeline, width at a time, and returns
// dispatchCancelled when ANY call ended on a cancellation: the caller then rolls the Turn back.
//
// Width 1 is the per-call loop: prepare, run and commit each call before the next is looked at,
// so a call's result is in history before its successor's ToolCallEvent — the path every leaf
// group takes, and a delegation group's whenever fanOutWidthFor says 1 (cap < 2, a delegate, or a
// single call). Above 1 the whole group is prepared — the calls within the fan-out ceiling to a
// verdict, the calls past it to their refusal (refusePastCeiling) — then run through a pool of
// width workers, then committed in emitted-call order — a delegation is atomic within the parent
// Turn, so a cancelled group is dropped whole, unappended, after the join (ADR 0013 §5).
//
// A group wider than its width states that width once, on its last committed result
// (fanOutWidthNote) — decided here, after the join, because whether every slot ran is only known
// once the pool has dequeued them all: a slot the pool skipped for a pending interjection clears
// its run flag at dequeue, and such a group carries no width line at all. A group with a slot
// refused past the ceiling carries none either — the refusal already names the width.
//
// A ceiling-refused slot's delegate-ledger row is booked HERE, at either width, because the call
// never reaches runSubAgent, the ledger's own site (children.go): after the width-1 prepare, or
// after the whole pooled group has been prepared and its running slots reserved, so the refused
// rows number behind every slot that runs, in the model's own call order.
func (a *Agent) dispatchGroup(ctx context.Context, turn, width int, calls []domain.ToolCall) dispatchOutcome {
	if width <= 1 {
		for i, call := range calls {
			slot := a.prepareCall(ctx, turn, call, true, i, len(calls))
			a.recordCeilingRefusal(&slot)
			a.runCall(ctx, turn, &slot)
			if slot.outcome == dispatchCancelled {
				return dispatchCancelled
			}
			a.commitCall(ctx, turn, &slot)
		}
		return dispatchDone
	}

	slots := make([]dispatchSlot, len(calls))
	for i, call := range calls {
		slots[i] = a.prepareCall(ctx, turn, call, false, i, len(calls))
		// The delegate ledger numbers a delegation by the order the model issued its calls in
		// (children.go): reserved here, in call order, before any worker can dequeue one — a
		// pool's dequeue order is its own, and runSubAgent would otherwise take the next index
		// as each worker happens to reach it.
		if slots[i].verdict.kind == resolveDelegate {
			a.delegations.reserve(call.ID)
		}
	}
	for i := range slots {
		a.recordCeilingRefusal(&slots[i])
	}

	a.runPool(ctx, turn, width, slots)

	// Join first, decide after: a sibling that reached its boundary with a usable result is
	// still discarded, because the recovery point is the pre-dispatch boundary of the whole Turn.
	for i := range slots {
		if slots[i].outcome == dispatchCancelled {
			return dispatchCancelled
		}
	}
	if everySlotRan(slots) {
		slots[len(slots)-1].widthNote = fanOutWidthNote(len(slots), width)
	}
	for i := range slots {
		a.commitCall(ctx, turn, &slots[i])
	}
	return dispatchDone
}

// prepareCall carries one call as far as it can go WITHOUT running anything: it surfaces the
// ToolCallEvent, fires the pre-tool-exec Moment, answers the dispatch facts, and computes the
// call's Resolution and gate verdict. Everything here touches Agent-wide state (the armed
// Reactions, the guardrails, the loop view), which is why it runs on the dispatching goroutine —
// for every call of a pooled group before any child starts.
//
// One consequence of the pooled shape is deliberate and worth naming: siblings are resolved
// against the SAME guardrail state, so a delegation cannot observe a breaker its sibling tripped.
// Concurrent calls cannot see each other's outcomes by construction — that is what concurrent
// means — and the shared read-only dangerous-action floor still re-fires on every call a child
// actually makes (ADR 0013 D3).
//
// index and group are the call's position within its group and the group's size — for a
// delegation, its index among the reply's delegations in emitted order — which is what the
// fan-out ceiling is keyed on (refusePastCeiling): the check sits right after the pre-tool-exec
// Moment, at both widths, so every call past the ceiling still surfaces its ToolCallEvent and
// fires its Moment, then takes its refusal on its own row in every Driver. It runs BEFORE the
// pre-emption check, so pre-emption decides only among the calls the ceiling let through.
//
// preempt says whether a pending user message pre-empts a delegation HERE — the width-1 rule,
// where a delegation about to be reached is the one about to start — rather than at the pool's
// dequeue (runPool). Either way the check sits after pre-tool-exec and before the lookup, so a
// skipped delegation is never looked up, resolved, gated or audited, and a leaf is never skipped.
//
// The dispatch facts answered before resolve() run in this order: the registry miss (an unknown
// tool is a dispatch fact — short-circuiting it keeps a withheld tool, e.g. sub_agent at the
// depth bound, resolving as an unknown tool, un-audited; Resolution D8), then arguments whose keys
// fold together (collidingArgumentKeysResult), then one key answered twice with different values
// (repeatedArgumentKeysResult). All three produce a final, unaudited slot: the Approver is never
// consulted, no gate key is ever minted, and nothing runs.
//
// The Resolution is computed once (resolve(), resolution.go) from the facts resolutionInput
// gathers — the registry lookup, the always-on guardrails (tightened, for a git_commit, by the
// commit-secrets shadow-index pre-check in secretsguard.go), the effective mode, the caps probe,
// and the one on-disk write-target check — and the gate stage then folds the user's gate
// reactions into it: a deny refuses the call, an ask forces the Approver, an allow leaves the
// ladder's verdict standing (gate.go). This function holds no ladder, guard-tier or demote
// decision of its own: resolve() decides, and a Refuse is carried out here because it has
// nothing to run; every other verdict is runCall's.
func (a *Agent) prepareCall(
	ctx context.Context,
	turn int,
	call domain.ToolCall,
	preempt bool,
	index, group int,
) dispatchSlot {
	a.cfg.Events.Emit(domain.ToolCallEvent{EventBase: a.base(turn), Call: call, ResolvedPath: a.resolvedPath(call)})

	// The pre-tool-exec Moment: reactions reshape the pending call through the shared
	// ToolCallEdit, so their edits compose and the pipeline executes what the cascade left behind.
	if _, err := a.fire(ctx, domain.MomentPreToolExec, domain.NewToolCallEdit(&call)); err != nil {
		// A pre-tool-exec reaction faulted: an error result, nothing run, and no postlude, rather
		// than a call run against a half-applied decision.
		return dispatchSlot{
			call:       call,
			result:     errorToolResult(call.ID, "pre-tool-exec reaction failed"),
			hookFailed: true,
		}
	}

	slot := dispatchSlot{call: call}
	if a.refusePastCeiling(turn, index, group, &slot) {
		return slot
	}
	if preempt && a.preemptDelegation(turn, &slot) {
		return slot
	}

	tool, ok := a.lookupTool(call.Tool)
	if !ok {
		slot.result = a.unknownToolResult(call)
		return slot
	}
	if result, refused := collidingArgumentKeysResult(call); refused {
		slot.result = result
		return slot
	}
	if result, refused := repeatedArgumentKeysResult(call); refused {
		slot.result = result
		return slot
	}

	guard := a.tightenForStagedSecrets(ctx, call, a.guards.PreExecute(call, tool, a.guardExemptions()))
	input := a.resolutionInput(tool, call, guard)
	slot.tool, slot.writeTarget = tool, input.writeTarget
	slot.verdict = a.applyGates(ctx, turn, call, resolve(input))
	if slot.verdict.kind == resolveRefuse {
		// The guard hard-refuse, the depth-bound refusal, a Plan-mode write, a nil-Approver gate,
		// and a gate reaction's deny (gate.go): the one verdict with nothing to run.
		slot.result = a.executeRefuse(turn, call, slot.verdict)
		return slot
	}
	slot.run = true
	return slot
}

// preemptDelegation is the one rule by which a queued user message pre-empts a delegation that
// has not started: when the slot is a sub_agent call and a message is waiting for this Agent's
// boundary (interjectionPending), the slot takes the skip result and its finished phase at once
// (skipDelegation), its run flag is cleared so commitCall books no audit record for a child that
// never ran, and true is returned. A leaf tool is never pre-empted — it runs to its result
// whatever is waiting at the boundary — and the predicate is read ONCE per slot, here, and never
// again: a child that has started is never affected, and a slot skipped stays skipped even if the
// message is withdrawn.
func (a *Agent) preemptDelegation(turn int, slot *dispatchSlot) bool {
	if !isSubAgentCall(slot.call) || !a.interjectionPending() {
		return false
	}
	slot.run = false
	slot.result = a.skipDelegation(turn, slot.call, skippedDelegationResult(slot.call.ID))
	return true
}

// refusePastCeiling is the one rule by which a reply's fan-out is bounded (ADR 0039, amended
// 2026-09-20): when the slot is a sub_agent call sitting at or past the fan-out ceiling in its
// group — index counts from 0, so the first `ceiling` delegations in emitted order run as they
// always have — the slot takes the ceiling refusal and its finished phase at once
// (skipDelegation's path: no started phase, no audit record, the run flag cleared), and true is
// returned. A leaf tool is never counted or refused, whatever its group holds, and a ceiling of 0
// (rounds 0) refuses nothing.
//
// The overflow is REFUSED rather than held back and re-issued by the engine (owner, 2026-09-20):
// the coordinator that asked for more than the ceiling is told so in its own results, in the same
// words every time, and decides what to delegate again once the round it did get has reported.
// The rounds and the stated width are read once, here, so the text names the very ceiling that
// refused the call.
func (a *Agent) refusePastCeiling(turn, index, group int, slot *dispatchSlot) bool {
	if !isSubAgentCall(slot.call) {
		return false
	}
	rounds, width := a.cfg.Delegation.FanOutRounds, a.statedDelegationWidth()
	ceiling := fanOutCeilingOf(rounds, width)
	if ceiling <= 0 || index < ceiling {
		return false
	}
	slot.run = false
	slot.ceilingRefused = true
	slot.result = a.skipDelegation(turn, slot.call, fanOutCeilingResult(slot.call.ID, group, rounds, width))
	return true
}

// recordCeilingRefusal books the delegate-ledger row a ceiling-refused slot is owed (children.go)
// and does nothing for any other slot. It is the ceiling's OWN write, and the ledger's second site
// after runSubAgent's defer (subagent.go): opened and recorded on the dispatching goroutine, so a
// pooled group's refused rows take their indices after every reserved slot. The row reads as any
// refusal does — `refused`, the result's head line as its cause, the call's label as its name.
func (a *Agent) recordCeilingRefusal(slot *dispatchSlot) {
	if !slot.ceilingRefused {
		return
	}
	a.delegations.record(delegationRecord{
		spawnIndex: a.delegations.open(slot.call.ID),
		callID:     slot.call.ID,
		name:       delegationLabel("", slot.call),
		outcome:    delegationRefused,
		cause:      delegationCause(slot.result.Content),
	})
}

// runCall executes one prepared slot's verdict and leaves the result and outcome on the slot; a
// slot that already holds its final result is left untouched. It is the ONE phase a pool runs off
// the dispatching goroutine, so nothing here touches Agent-wide state a sibling could be touching
// at the same time: a leaf's arm records its own audit tail — per outcome, as it always has — and
// dispatchTools hands the pool delegations only. A delegation's record is the commit phase's
// (delegated), because the child ran under a verdict this Agent's guards must be told about in
// call order.
//
// resolve() answers a sub_agent call with Delegate or Refuse and nothing else (its row 2: a
// Tier-2 force is deliberately not applied to a delegation), so the leaf arms below never see one.
func (a *Agent) runCall(ctx context.Context, turn int, slot *dispatchSlot) {
	if !slot.run {
		return
	}
	switch slot.verdict.kind {
	case resolveDelegate:
		slot.result, slot.outcome = a.runDelegation(ctx, turn, slot.call)
		// A cancelled group is discarded unappended and never reaches commitCall; the record is
		// owed only for a child that ran to a result.
		slot.delegated = slot.outcome != dispatchCancelled
	case resolveGate:
		slot.result, slot.outcome = a.executeGate(ctx, turn, slot.tool, slot.call, slot.verdict)
	case resolveConfine:
		slot.result, slot.outcome = a.executeConfine(ctx, turn, slot.tool, slot.call, slot.verdict)
	default: // resolveRun
		slot.result, slot.outcome = a.executeRun(ctx, turn, slot.tool, slot.call, slot.verdict)
	}
}

// commitCall lands one finished slot: the audit record a delegation that ran earns, the
// post-tool-result Moment, and the append into history — the same sequence, in the same order,
// for every call at every width. Running it on the dispatching goroutine, one slot at a time in
// emitted-call order, is what makes a pooled group's history independent of completion order.
//
// A hook-failed slot is appended alone — no productivity signal, no post-tool-result reactions —
// because no decision was ever reached about the call. A refused slot was already recorded by
// executeRefuse in the prepare phase, a leaf by its own arm in the run phase, and a slot the pool
// skipped for a pending interjection never ran and records nothing.
func (a *Agent) commitCall(ctx context.Context, turn int, slot *dispatchSlot) {
	if slot.hookFailed {
		a.appendToolResult(turn, slot.call, slot.result, "", nil)
		return
	}
	if slot.delegated {
		a.recordExecuted(turn, slot.call, slot.verdict.auditDecision, slot.verdict.auditReason, slot.result)
	}
	if slot.widthNote != "" {
		// The group's width line, appended AFTER the audit record so the record keeps the child's
		// own result, and as the last line of the BODY — the SeatFallbackNote precedent — so the
		// user-steered trailer stays the result's final line where both apply (ADR 0063 D3).
		slot.result.Content = withBodyNote(slot.result.Content, slot.widthNote)
	}
	advised := a.firePostToolResult(ctx, slot.call, &slot.result)
	a.appendToolResult(turn, slot.call, slot.result, slot.writeTarget, advised)
}

// fanOutWidthNoteFormat is the ONE structural fact a fan-out states to the parent model about HOW
// its group ran: when a reply asked for more delegations than the width the group ran under, the
// slots past the width — up to the fan-out ceiling, past which a slot is refused instead
// (refusePastCeiling) — waited for a worker, so their results arrived after the others finished,
// and the model reading the burst of results as "all at once" would be reading a fact that is not
// so. It is stated exactly once per group, as the last line of the group's LAST committed result's
// body, and only for a group whose every slot actually ran (fanOutWidthNote's caller): a group with
// a skipped, refused or hook-failed slot did not run N delegations, so the count would be a lie —
// and a group with a ceiling-refused slot has already had its width named by the refusal.
//
// Engine-composed and no steering (ADR 0023 2026-08-25 amendment): the line reports the width the
// group actually ran under — the once-per-reply snapshot fanOutWidthFor took at dispatch — and
// says nothing about what the model should do with it. The orientation block is deliberately not
// where it lives: the width can change per reply (a latched Delegation target, a mixed-seat group)
// while the orientation facts are session-constant (ADR 0069 decisions 4 and 6). The arguments are
// K (the delegations past the width), N (the group's size) and W (the width).
const fanOutWidthNoteFormat = "[%d of this group's %d delegations ran after the others finished — the width is %d]"

// fanOutWidthNote renders the delegation-width line for a group of `group` delegations that ran
// `width` at a time, or "" when the group fit inside the width and no delegation waited.
func fanOutWidthNote(group, width int) string {
	if group <= width {
		return ""
	}
	return fmt.Sprintf(fanOutWidthNoteFormat, group-width, group, width)
}

// everySlotRan reports whether each slot of a group reached the pool and ran a child: no refusal,
// no unknown tool, no hook failure, no interjection skip. Only such a group's width line counts
// real delegations, so it is the gate on stating one at all.
func everySlotRan(slots []dispatchSlot) bool {
	for i := range slots {
		if !slots[i].run || slots[i].hookFailed {
			return false
		}
	}
	return true
}

// runPool drives every slot that still needs running through width worker goroutines, one call
// each, and returns once all of them have reached a boundary. The workers pull indices off one
// channel, so width is a true concurrency bound rather than a goroutine count: a group of nine
// under a cap of three is three children at a time, three times over.
//
// ctx is handed to every child unchanged, so a cancel reaches all of them at once and each
// unwinds at its own next boundary; the join below is what "the pool waits" means. A child's
// failure is ITS result and nothing more — no sibling is cancelled (ADR 0039 decision 4).
//
// The dequeue is where a queued user message PRE-EMPTS a pooled group (preemptDelegation): a
// delegation dequeued while a message waits for the boundary is not run at all — it takes the
// skip result and only a finished phase, at once, so a Driver's row leaves "scheduled"
// immediately — while every child already started runs to its boundary untouched. Dequeue is
// the pooled counterpart of the width-1 rule, where prepareCall pre-empts the delegation the
// instant it is reached: at either width the delegation about to START is the one skipped.
func (a *Agent) runPool(ctx context.Context, turn, width int, slots []dispatchSlot) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < width; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if a.preemptDelegation(turn, &slots[i]) {
					continue
				}
				a.runCall(ctx, turn, &slots[i])
			}
		}()
	}
	for i := range slots {
		if slots[i].run {
			jobs <- i
		}
	}
	close(jobs)
	wg.Wait()
}

// skippedDelegationContent is the whole tool result a delegation pre-empted by a queued user
// message carries. It is a constant because it is the model's only account of a child that never
// ran: the same words every time, so the model can tell a skip from a child's own failure and
// decide whether to delegate again once it has read the message.
const skippedDelegationContent = "sub-agent not started: the user sent a message while this group was running; delegate again if the task is still needed"

// skippedDelegationResult is the error-shaped tool result of a pre-empted delegation — built as
// executeRefuse builds a refusal, so on the wire and in a Driver it reads like one.
func skippedDelegationResult(callID string) domain.ToolResult {
	return errorToolResult(callID, skippedDelegationContent)
}

// fanOutCeilingResultFormat is the whole tool result a delegation refused past the reply's fan-out
// ceiling carries (refusePastCeiling), in the pre-emption result's shape — the same `sub-agent not
// started:` head, so a Driver and the model read the two unstarted kinds alike — and a constant
// format for the pre-emption's reason: it is the model's only account of a child that never ran,
// so it says the same thing every time and names every number the model needs to act on it. The
// arguments are N (the reply's delegations), C (the ceiling), R (the rounds), W (the width the
// ceiling is R rounds of) and C again (how many of the N ran).
const fanOutCeilingResultFormat = "sub-agent not started: this reply fanned out %d delegations and the ceiling is %d (%d rounds × width %d) — the first %d ran; delegate the rest again once their results are in"

// fanOutCeilingResult renders the ceiling refusal for one call of a reply of `group` delegations, the
// ceiling computed by the one formula that refused it (fanOutCeilingOf).
func fanOutCeilingResult(callID string, group, rounds, width int) domain.ToolResult {
	ceiling := fanOutCeilingOf(rounds, width)
	return errorToolResult(callID, fmt.Sprintf(fanOutCeilingResultFormat, group, ceiling, rounds, width, ceiling))
}

// interjectionPending answers whether a user message is waiting for this Agent's next boundary —
// the one predicate that decides a delegation about to start is skipped instead. It is one rule
// for every depth: at the top level it is the host's Config.InterjectionPending seam (nil ⇒ never),
// and on a delegate (isDelegate) it is this child's own mailbox (children.go), because a message queued for a child
// waits on that child's grandchildren exactly as the human's waits on its children.
func (a *Agent) interjectionPending() bool {
	if a.isDelegate() {
		return a.mailbox.hasPending()
	}
	if a.cfg.InterjectionPending == nil {
		return false
	}
	return a.cfg.InterjectionPending()
}

// skipDelegation closes one delegation that has not started with result: it emits the finished
// phase that closes the child's bracket without a started one — carrying that result, not
// Cancelled, since nothing is rolled back — and returns it for the caller to commit in call order.
// Its callers are the two ways a delegation is settled before it starts, preemptDelegation (a
// queued message) and refusePastCeiling (the fan-out ceiling), at either width, so a lone
// delegation is closed exactly as a pooled one.
func (a *Agent) skipDelegation(turn int, call domain.ToolCall, result domain.ToolResult) domain.ToolResult {
	a.emitSubAgentPhase(turn, call, domain.SubAgentPhaseEvent{Phase: domain.SubAgentFinished, Result: result})
	return result
}

// runDelegation drives the sub_agent recursion point (a nested Agent) to its boundary, bracketed
// by the lifecycle phases a Driver reads (domain.SubAgentPhaseEvent): started as the child is
// reached — the instant a pool worker dequeues it, or the instant the width-1 loop arrives at it,
// which is what makes a slot-less delegation observably queued rather than silently pending — and
// finished, carrying the result, as the child returns. A child the human CANCELLED is bracketed
// too: the cancelled delegation is rolled back with the parent Turn and never becomes a result, so
// its finished phase carries none and says so (ADR 0075 decision 12) — an unclosed bracket would
// be a delegation no Driver can see end.
//
// The recover that keeps a child's panic from crossing a pool worker's top frame — which would
// take the process down with it — sits in runSubAgent's own frame, inside every caller's chain,
// so the per-child fault boundary ADR 0007 promises is the child's boundary at every width: a
// recovered child becomes an error tool-result its sibling and the parent Exchange survive (ADR
// 0039 decision 4). runSubAgent keeps its own defensive depth check too — belt-and-braces with the
// resolver's depth-bound row and the withheld-tool floor (ADR 0013 defence in depth) — so the
// bound holds even if the call is reached by another route. The audit record is NOT booked here:
// it is commitCall's, on the dispatching goroutine (dispatchSlot.delegated).
func (a *Agent) runDelegation(ctx context.Context, turn int, call domain.ToolCall) (domain.ToolResult, dispatchOutcome) {
	applied, requested := a.stepCapFor(call)
	a.emitSubAgentPhase(turn, call, domain.SubAgentPhaseEvent{
		Phase:        domain.SubAgentStarted,
		StepCap:      applied,
		CapRequested: requested,
	})
	result, outcome := a.runSubAgent(ctx, call)
	if outcome == dispatchCancelled {
		a.emitSubAgentPhase(turn, call, domain.SubAgentPhaseEvent{Phase: domain.SubAgentFinished, Cancelled: true})
		return result, dispatchCancelled
	}
	a.emitSubAgentPhase(turn, call, domain.SubAgentPhaseEvent{Phase: domain.SubAgentFinished, Result: result})
	return result, dispatchDone
}

// stepCapFor reports the step cap one sub_agent call's child will run under and the ask it clamps
// (resolveStepCap against the configured delegate cap), for the started phase event to carry: the
// same rule runSubAgent applies when it seeds the child, read here BEFORE the child exists so the
// phase that announces it can state the bound. A call whose arguments do not parse asked for
// nothing — runSubAgent refuses it on its own account before a child exists.
func (a *Agent) stepCapFor(call domain.ToolCall) (applied, requested int) {
	var args tools.SubAgentArgs
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return resolveStepCap(a.cfg.Delegation.MaxSteps, 0)
	}
	return resolveStepCap(a.cfg.Delegation.MaxSteps, args.MaxSteps)
}

// emitSubAgentPhase surfaces one delegation lifecycle boundary. The event is stamped with the
// CHILD's identity — one level deeper than this Agent, under the spawning call's id — rather than
// with the emitting parent's, so it carries the same run identity as the events the child itself
// emits and names the tool-call block an observer attaches it to.
//
// runDelegation brackets every child with the started/finished pair whatever width it ran under,
// so nothing that runs is ever left looking queued. A delegation skipped for a pending
// interjection, or refused past the reply's fan-out ceiling, reports a finished phase alone
// (skipDelegation): it never started.
//
// The caller fills what the phase carries — Phase, and Result or Cancelled on a finished one,
// StepCap and CapRequested on a started one (stepCapFor) — and this stamps the identity.
// Cancelled marks a finished phase that closes a ROLLED-BACK delegation rather than a reported one
// (ADR 0075 decision 12). It rides the event so an observer can tell the two apart; a started phase
// is never cancelled.
func (a *Agent) emitSubAgentPhase(turn int, call domain.ToolCall, event domain.SubAgentPhaseEvent) {
	base := a.base(turn)
	base.Depth++
	base.CallID = call.ID
	event.EventBase = base
	a.cfg.Events.Emit(event)
}

// emitSubAgentNamed surfaces the ONE rename a generated delegation name produces (ADR 0068) — and
// the one other rename the engine makes, the name a CONTINUED delegation inherits from the capped
// run it picks up (runSubAgent, plan 2026-09-18 - 00, P6), announced for the new spawn id. It is
// stamped exactly as emitSubAgentPhase stamps a lifecycle boundary — the CHILD's identity, one
// level deeper than this Agent and under the spawning call's id — because a reader applies the
// rename to the run those events opened, which under a fan-out is one member of several. Turn is
// the parent Turn the spawning call belongs to, read on the dispatch goroutine and handed in, so
// the naming goroutine touches no loop state of its own.
//
// It is the naming act's whole wire presence: no usage, no tokens, no Turn of its own. The call
// that produced the name is neither a Reaction nor an Exchange (ADR 0022 addendum), so nothing
// else about it belongs on the stream.
func (a *Agent) emitSubAgentNamed(turn int, callID, name string) {
	base := a.base(turn)
	base.Depth++
	base.CallID = callID
	a.cfg.Events.Emit(domain.SubAgentNamedEvent{EventBase: base, Name: name})
}

// collidingArgumentKeysPrefix and collidingArgumentKeysAdvice are the two halves of the ONE
// wording a call refused for colliding argument keys carries. They are constants because the
// refusal is the model's only signal about what to do differently: it must name the offending
// spellings and prescribe the single fix, in the same words every time, so a retry loop can
// recognise it rather than re-emit the same call.
const (
	collidingArgumentKeysPrefix = "invalid arguments: "
	collidingArgumentKeysAdvice = " name the same parameter — spell each argument once"
)

// collidingArgumentKeysMessage is the error result text for a call whose argument keys fold
// together, listing each colliding group as domain.CollidingArgumentKeys rendered it.
func collidingArgumentKeysMessage(groups []string) string {
	return collidingArgumentKeysPrefix + strings.Join(groups, ", ") + collidingArgumentKeysAdvice
}

// collidingArgumentKeysResult is the refusal itself, in the one shape prepareCall needs: the error
// result for a call whose argument object names one parameter twice under different key cases,
// and false for every ordinary call. It is answered in the prepare phase, before resolve(), so a
// delegation that reaches a pool is refused exactly as a lone call is — the refusal never depends
// on the bound server's Parallel agents cap.
//
// Arguments that do not parse at all are NOT this rule's business: domain.CollidingArgumentKeys
// reports that as an error, and it is left to the tool's own decodeToolArgs, which can name the
// parameters the tool actually has.
func collidingArgumentKeysResult(call domain.ToolCall) (domain.ToolResult, bool) {
	groups, err := domain.CollidingArgumentKeys(call.Arguments)
	if err != nil || len(groups) == 0 {
		return domain.ToolResult{}, false
	}
	return errorToolResult(call.ID, collidingArgumentKeysMessage(groups)), true
}

// repeatedArgumentKeysPrefix and repeatedArgumentKeysAdvice are the two halves of the ONE wording a
// call refused for a repeated argument key carries. Like the colliding pair above they are
// constants because the refusal is the model's only signal about what to do differently: it must
// name the keys it answered twice and prescribe the single fix, in the same words every time, so a
// retry loop can recognise it rather than re-emit the same call.
const (
	repeatedArgumentKeysPrefix = "invalid arguments: repeated with different values: "
	repeatedArgumentKeysAdvice = " — spell each argument once"
)

// repeatedArgumentKeysMessage is the error result text for a call whose argument object answers one
// parameter twice with two different values, listing each repeated key as domain.RepeatedArgumentKeys
// rendered it.
func repeatedArgumentKeysMessage(names []string) string {
	return repeatedArgumentKeysPrefix + strings.Join(names, ", ") + repeatedArgumentKeysAdvice
}

// repeatedArgumentKeysResult is the refusal for the neighbouring malformation: ONE spelling given
// two DIFFERENT answers (`{"task":A,…,"task":B}`), where stdlib JSON's last-wins hands the executor
// one of them and the model wrote both. That is not a call anyone can read one way either — the
// pane, the dangerous-action guard and the allow-for-session digest all take the last value while
// the model meant its first — so it is refused before resolve() in the same shape as
// collidingArgumentKeysResult, and for the same reason: the prepare phase answers it at every
// width, so a disposition never depends on the bound server's Parallel agents cap.
//
// A BYTE-IDENTICAL repeat is deliberately not this rule's business: domain.RepeatedArgumentKeys
// reports no group for it, and last-wins for an exact duplicate stays the pinned contract every
// reader already shares. Arguments that do not parse at all are not either — that is reported as an
// error and left to the tool's own decodeToolArgs, which can name the parameters the tool has.
func repeatedArgumentKeysResult(call domain.ToolCall) (domain.ToolResult, bool) {
	names, err := domain.RepeatedArgumentKeys(call.Arguments)
	if err != nil || len(names) == 0 {
		return domain.ToolResult{}, false
	}
	return errorToolResult(call.ID, repeatedArgumentKeysMessage(names)), true
}

// resolutionInput assembles the facts resolve() decides from for one call: the effective mode,
// the resolved tool, the guardrail verdict, the LIVE confine-to-workspace flag (read through
// ConfineToWorkspace() under its lock, so a /confine toggle from the UI lands on the next call
// exactly as a Shift+Tab mode change does), the backend caps probe, the precomputed on-disk
// write-target check (the one I/O-tainted fact — resolve() does
// none), the sub-agent depth bound, whether an Approver is configured, and the confinement box
// a Confine verdict would run inside. It is dispatch's fact-gathering; the verdict logic lives
// entirely in resolve().
func (a *Agent) resolutionInput(tool domain.Tool, call domain.ToolCall, guard security.PreCheck) resolutionInput {
	target := a.classifyWriteTarget(tool, call)
	return resolutionInput{
		mode:                   a.effectiveMode(),
		call:                   call,
		tool:                   tool,
		guard:                  guard,
		confineToWorkspace:     a.ConfineToWorkspace(),
		fsConfineAvailable:     a.fsConfinementAvailable(),
		writeTargetInWorkspace: target.inFence,
		writeTargetInScratch:   target.inScratch,
		writeEscapeTarget:      target.escape,
		writeTarget:            target.real,
		scratchDir:             a.ScratchDir(),
		atDepthBound:           a.depth >= a.maxDepth(),
		maxDepth:               a.maxDepth(),
		wrapUpOutput:           a.wrapUpOutput(),
		writesWrapUpOutput:     tool.Name() == tools.WriteFileToolName && target.real == a.outputTarget,
		approverPresent:        a.cfg.Approver != nil,
		box:                    a.confinementBox(),
	}
}

// wrapUpOutput is the path resolve's wrap-up row keys on: the delegation's `output_path` as its
// spawning call spelled it, on the wrap-up Turn that kept write_file for it (wrapUpWriter), and ""
// on every other call — an ordinary Turn, a wrap-up whose menu was withdrawn wholesale, or a
// wrap-up whose spawn named no path (Agent.outputPath is "" there) — so the row is inert
// everywhere the narrowing is not in force.
func (a *Agent) wrapUpOutput() string {
	if !a.turns.wrappingUp() {
		return ""
	}
	if _, ok := a.wrapUpWriter(); !ok {
		return ""
	}
	return a.outputPath
}

// confinementBox is the box every per-call consumer builds from: Config.ConfinementBox() — the
// single fold of the Confine* fields — over the LIVE scratch dir rather than the construction
// seed, read through ScratchDir() under its lock. That one substitution is what makes the box
// handed to each tool call carry the CURRENT session's scratch path: the host moves the dir at a
// session boundary (SetScratchDir) and the very next call is fenced to the new session's scratch,
// exactly as a mode or /confine change lands on the next call.
func (a *Agent) confinementBox() domain.ConfinementBox {
	cfg := a.cfg
	cfg.ScratchDir = a.ScratchDir()
	return cfg.ConfinementBox()
}

// errConfinementUnavailable is what syncPermitCtx answers when the operator asked for workspace
// confinement and the host cannot provide it: there is no permit to mint, so the reaction's
// command is never spawned. It is the sync lane's one refusal, and it is deliberately a plain
// sentence — it reaches the user through the Driver's report line, where "the host has no
// confinement backend" is the whole of what they can act on.
var errConfinementUnavailable = errors.New("workspace confinement is unavailable on this host")

// syncPermitCtx returns ctx wrapped with the domain.SubprocessPermit a USER-ORIGIN SYNC reaction —
// class advise or gate — spawns its command under, or the refusal that stops the spawn
// (docs/design/confinement-execution-contract.md §10.4). It is the engine's ONE minting site: no
// other Moment's reactions carry a permit, so every other spawn stays on the contract's refusal
// default (the post-response cascade's Auto-only row was retired 2026-09-15 — no shipped Reaction
// spawns there). Its row:
//
//	| confine-to-workspace | fs caps     | installed                                    |
//	|----------------------|-------------|----------------------------------------------|
//	| off                  | —           | permit, nil Confinement (unfenced)           |
//	| on                   | available   | permit carrying the workspace box, and the   |
//	|                      |             | matching Confinement handle for the funnel   |
//	| on                   | unavailable | nothing — errConfinementUnavailable          |
//
// The MODE is not a term. A user's sync reaction is the user's own configuration rather than
// anything the model chose, so the ladder — which exists to bound what the MODEL may reach — has
// no verdict to give about it, and the reaction fires in Plan exactly as it fires in Auto (ADR 0076
// D8). What is left of the ladder's row is the fence itself: with `confine-to-workspace` on the
// command runs inside the same box a subprocess tool would have been confined to, and a host that
// cannot build that box gets no unfenced fallback.
//
// The Confinement handle is installed BESIDE the permit because the two are read by different
// halves of the funnel: tools.RunHookSubprocess resolves argv[0] against the box on ctx
// (ConfinementFromContext) and internal/subprocess confines the cmd from it, while the permit is
// the authorisation the spawn itself is checked against.
func (a *Agent) syncPermitCtx(ctx context.Context) (context.Context, error) {
	if !a.ConfineToWorkspace() {
		return domain.WithSubprocessPermit(ctx, domain.SubprocessPermit{}), nil
	}
	if !a.fsConfinementAvailable() {
		return nil, errConfinementUnavailable
	}
	conf := domain.Confinement{Confiner: a.cfg.Confiner, Box: a.confinementBox()}
	ctx = domain.WithSubprocessPermit(ctx, domain.SubprocessPermit{Confinement: &conf})
	return domain.WithConfinement(ctx, conf), nil
}

// writeEscapeCtx returns ctx carrying the domain.WriteEscapePermit this verdict authorises, or ctx
// unchanged when it authorises none (ADR 0049). It is the write-time analogue of syncPermitCtx:
// the ladder's answer for a write that lands outside the workspace reaches the shared write funnel
// as a context token, because the funnel is one os.Root-pinned rule that cannot otherwise tell an
// approved escape from an unapproved one.
//
// The permit names ONE resolved absolute path — the writeTarget.Real this call classified as, which
// is the same path the approval pane disclosed — for the duration of this one execution. Dispatch
// invents nothing here: the target rides the verdict, resolve() sets it only on the Run and Gate
// kinds ADR 0049 names, and an empty target installs nothing at all, leaving today's
// workspace-pinned fence governing byte-for-byte.
func writeEscapeCtx(ctx context.Context, verdict resolution) context.Context {
	if verdict.writeEscapeTarget == "" {
		return ctx
	}
	return domain.WithWriteEscapePermit(ctx, domain.WriteEscapePermit{Real: verdict.writeEscapeTarget})
}

// executeRun runs a Run verdict directly — no Approval, no Confine — and records it. It is also
// the shared "run it now" tail for an approved Gate and an approved runtime-demote re-run, both
// of which run unconfined once the human has authorised the call.
//
// Being that one tail is what makes it the single minting point for the write-escape permit
// (writeEscapeCtx): every in-process write that may land outside the workspace — the approved
// gate, the "I am the sandbox" cell, the declared writable path — passes through here, and
// nothing that was refused or denied ever does.
//
// A Run the resolver marked confineChildren also carries the Confinement handle, so a subprocess
// apogee's OWN in-process tool spawns — the workspace-scoped writers' git staging — is fenced by
// the same box a subprocess call would have been Confined in. The box PARAMETER stays nil
// deliberately: it is what executeTool keys the D4 demote translation on, and an unconfinable
// child here is the staging's own best-effort skip, not a call to demote.
func (a *Agent) executeRun(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall, verdict resolution) (domain.ToolResult, dispatchOutcome) {
	ctx = writeEscapeCtx(ctx, verdict)
	if verdict.confineChildren {
		ctx = domain.WithConfinement(ctx, domain.Confinement{Confiner: a.cfg.Confiner, Box: verdict.box})
	}
	result, outcome := a.executeTool(ctx, turn, tool, call, nil /* no confinement box */)
	if outcome == dispatchCancelled {
		return result, dispatchCancelled
	}
	a.recordExecutedTrip(turn, call, verdict, result)
	return result, dispatchDone
}

// executeGate routes a Gate verdict through the Approver and, if allowed, runs it — confined
// when the leaf it upgraded was a Confine.
// The resolver guarantees an Approver is present for a Gate (a gate with none is folded to a
// Refuse — Resolution D5), so nothing runs unapproved here. A forced gate skips the
// allow-for-session cache; a deny (or a nil Approver defensively) refuses the call.
//
// A DENIED forced gate carries the guard rule's Hint into its refusal text, which is the only
// place a Tier-2 rule's way out reaches the model: a forced look that ends in "no" is otherwise
// indistinguishable, to the model, from a human who simply declined this call.
//
// The confineOnAllow branch is a Tier-2 forced look on a call Auto would have Confined: approval
// decides WHETHER it runs, confinement decides WHERE, so the allow executes as the Confine would
// have — box installed, and a run-time ErrConfinementUnavailable following the verdict's own D4
// fallback. That asks the human a second time, by the demote gate, whether to run UNCONFINED; two
// prompts in the rare failure case is the honest shape, because the two questions are different.
func (a *Agent) executeGate(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall, verdict resolution) (domain.ToolResult, dispatchOutcome) {
	allowed, outcome := a.approve(ctx, turn, call, verdict.force, verdict.cacheKey, verdict.reason, verdict.remedy)
	if outcome == dispatchCancelled {
		return domain.ToolResult{}, dispatchCancelled
	}
	if !allowed {
		// A denied gate the guard FORCED answers the model with the rule's way out appended,
		// so a small model reroutes to the sanctioned route instead of re-issuing rewrites of a
		// call the human just said no to (guardRefusalMessage does the same for a Tier-1
		// refusal). Every other gate — and a forced gate whose rule offers no Hint — keeps
		// today's bare sentence.
		denial := "tool call denied by approver"
		if verdict.hint != "" {
			denial += " — " + verdict.hint
		}
		result := errorToolResult(call.ID, denial)
		a.recordBlocked(turn, call, verdict.auditDecision, verdict.auditReason, result)
		return result, dispatchDone
	}
	if verdict.confineOnAllow {
		return a.executeConfine(ctx, turn, tool, call, verdict)
	}
	return a.executeRun(ctx, turn, tool, call, verdict)
}

// executeConfine runs a Confine verdict's subprocess inside the verdict's box. If the box
// cannot be established at run time (the subprocess tool returns ErrConfinementUnavailable
// rather than running unconfined), it follows the verdict's precomputed fallback instead of
// deciding anew — the runtime "confine if you can, gate if you can't" net (Resolution D4).
func (a *Agent) executeConfine(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall, verdict resolution) (domain.ToolResult, dispatchOutcome) {
	result, outcome := a.executeTool(ctx, turn, tool, call, &verdict.box)
	if outcome == dispatchCancelled {
		return result, dispatchCancelled
	}
	if outcome == dispatchConfinementUnavailable {
		return a.executeConfineFallback(ctx, turn, tool, call, verdict)
	}
	a.recordExecutedTrip(turn, call, verdict, result)
	return result, dispatchDone
}

// executeConfineFallback carries out a Confine verdict's precomputed runtime-demote fallback
// (Resolution D4) after the box could not be established: it surfaces the demote event, then
// executes the fallback the resolver already chose — a forced Approval gate whose
// allow-continuation re-runs the call UNCONFINED (Approval is now the bound), or, when no
// Approver is configured, a Refuse. The executor follows the plan; it never decides.
func (a *Agent) executeConfineFallback(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall, verdict resolution) (domain.ToolResult, dispatchOutcome) {
	a.cfg.Events.Emit(domain.ErrorEvent{
		EventBase: a.base(turn),
		Source:    call.Tool,
		Err:       "confinement unavailable at run time: demoting subprocess call to Approval",
	})

	fb := verdict.fallback
	if fb.kind == resolveRefuse {
		// No Approver: the subprocess could not be confined and no human could authorise the
		// unconfined run.
		result := errorToolResult(call.ID, fb.reason)
		a.recordBlocked(turn, call, fb.auditDecision, fb.auditReason, result)
		return result, dispatchDone
	}

	allowed, outcome := a.approve(ctx, turn, call, fb.force, fb.cacheKey, fb.reason, fb.remedy)
	if outcome == dispatchCancelled {
		return domain.ToolResult{}, dispatchCancelled
	}
	if !allowed {
		result := errorToolResult(call.ID, confineDemoteRefuseReason)
		a.recordBlocked(turn, call, fb.auditDecision, fb.auditReason, result)
		return result, dispatchDone
	}
	// Approval granted: re-run with NO confinement handle installed (the call already failed to
	// confine, and Approval is the bound the human granted).
	return a.executeRun(ctx, turn, tool, call, verdict)
}

// executeRefuse carries out a Refuse verdict: an error result plus the exact audit/event trail
// its source produces today (Resolution D8). A guard hard-refuse and a nil-Approver refuse
// carry the guard's pass-through audit decision, so they are recorded and surfaced; an
// unknown-tool (rejected before resolve) and a Plan-mode write refuse carry none, so they are
// neither.
func (a *Agent) executeRefuse(turn int, call domain.ToolCall, verdict resolution) domain.ToolResult {
	result := errorToolResult(call.ID, verdict.reason)
	if verdict.auditDecision != "" {
		a.recordBlocked(turn, call, verdict.auditDecision, verdict.auditReason, result)
		a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: call.Tool, Err: verdict.reason})
	}
	return result
}

// guardRefusalMessage renders the model-facing reason a guardrail refused a call. A rule
// that carries a Hint gets it appended — the way out, so the model reroutes instead of
// looping on rewrites of a reason it cannot satisfy.
func guardRefusalMessage(guard security.PreCheck) string {
	switch guard.Audit {
	case security.AuditCircuitTripped:
		return "circuit-breaker open: this tool call has failed repeatedly with identical arguments and is refused"
	default:
		msg := "refused by the dangerous-action guard: " + guard.Reason
		if guard.Hint != "" {
			msg += " — " + guard.Hint
		}
		return msg
	}
}

// lookupTool resolves a tool name against the resolved registry (nil registry ⇒ not found).
func (a *Agent) lookupTool(name string) (domain.Tool, bool) {
	if a.tools == nil {
		return nil, false
	}
	return a.tools.Lookup(name)
}

// unknownToolResult renders the registry miss prepareCall answers with at every width, so a pooled
// group and a lone call can never word it differently: the former `unknown tool "<name>"`
// sentence, plus a ` — did you mean: <name>` clause when a
// registered name is a near miss of the one the model wrote (tools.ClosestToolName). The clause is
// data for the model's next call, never a re-route: nothing runs here. It matters only with the
// tool-call repair Floor guard off (the guard answers an unknown name before dispatch sees it),
// which is exactly when a model gets no other pointer back to the menu.
func (a *Agent) unknownToolResult(call domain.ToolCall) domain.ToolResult {
	message := fmt.Sprintf("unknown tool %q", call.Tool)
	if a.tools != nil {
		registered := a.tools.All()
		names := make([]string, 0, len(registered))
		for _, tool := range registered {
			names = append(names, tool.Name())
		}
		if closest := tools.ClosestToolName(names, call.Tool); closest != "" {
			message += " — did you mean: " + closest
		}
	}
	return errorToolResult(call.ID, message)
}

// approve consults the Approver for a Gate verdict, returning whether the call may run. It
// honours allow-for-session — remembered for the rest of the Session under the verdict's
// cacheKey — unless force is set: a forced gate (a Tier-2 speed-bump or a runtime demote) is a
// per-call event, not a pre-allowable convenience. reason and remedy feed the Approval prompt —
// the why, and (only where the condition is one the user can lift) the way out; both come off
// the verdict, so dispatch invents neither. It reports dispatchCancelled if ctx is cancelled
// while the human deliberates.
//
// The memory those allows land in is the SESSION's, not this Agent's: it lives on the Approver
// seam every agent in the tree shares (sessionAllows — internal/agent/approvalcache.go), so a
// sub-agent never re-asks for something the human already granted anywhere in the tree. Dispatch
// only READS it, on the silent fast path below — a remembered allow runs the call with no prompt
// and no ApprovalEvent, exactly as it always has. The WRITES belong to the seam, which is also the
// only place that can catch the twin: a duplicate request already queued behind the very prompt
// that allowed its key.
//
// The request names the asking agent's delegated task (empty at depth 0 — a.task) and, when the
// delegation carried one — or the out-of-band namer generated one for it (ADR 0068) — its short
// name, read under the lock through displayName because that rename can land while this very
// prompt is being built. A prompt raised during a fan-out may be one of several queued behind each
// other and "which agent is asking" is otherwise unanswerable
// from the call alone (ADR 0039 decision 12). The queueing
// itself is the Approver seam's (queuedApprovals): from here a gate is one blocking call whatever
// the siblings are doing.
//
// The resolver only produces a Gate when an Approver is configured (a gate with none is a
// Refuse — Resolution D5), so the nil-Approver guard below is defensive: it refuses rather than
// dereferencing a nil Approver, never running unapproved.
func (a *Agent) approve(ctx context.Context, turn int, call domain.ToolCall, force bool, cacheKey, reason, remedy string) (bool, dispatchOutcome) {
	if !force && sessionAllows(a.cfg.Approver).Allowed(cacheKey) {
		return true, dispatchDone
	}
	if a.cfg.Approver == nil {
		return false, dispatchDone
	}

	// The request's CacheKey is what decides whether its answer may ever be remembered, and this
	// single mapping is the whole of that policy: an ordinary gate travels with its key, a forced
	// one travels with NOTHING. An empty key is the seam's "unrememberable decision" signal, so a
	// forced gate stays out of the memory in both directions — the read above and the seam's write.
	// Otherwise one "allow for session" on a Tier-2 speed-bump (or a runtime demote) would silently
	// pre-clear every later ordinary gate under the same key — for an MCP tool, every tool of that
	// server. A forced allow-for-session therefore behaves exactly as a plain ApprovalAllow: it
	// authorises this call only. The resolution carrying a cacheKey alongside force (the demote
	// fallback, resolution.go) is harmless precisely because the emptying happens here.
	sessionKey := cacheKey
	if force {
		sessionKey = ""
	}

	// How far the "allow for this session" answer reaches beyond the call this request paints: an
	// MCP gate's memory is keyed at SERVER grain, so one yes clears every sibling tool of that
	// server (ADR 0012). A gate whose answer is remembered NOWHERE discloses no grant, and the
	// sessionKey emptied above is exactly that condition — a forced allow-for-session behaves as a
	// plain allow, so claiming the server grain on that pane would over-state what the yes does.
	grantAlias, serverGrant := a.mcpServerGrant(call)
	if sessionKey == "" {
		grantAlias, serverGrant = "", false
	}

	areq := domain.ApprovalRequest{
		Tool:         call.Tool,
		Arguments:    call.Arguments,
		Reason:       reason,
		Remedy:       remedy,
		SubAgentTask: a.task,
		SubAgentName: a.displayName(),
		CacheKey:     sessionKey,
		// What that CacheKey's grain means for the human's answer: false on every request whose
		// allow authorises no more than the call above, which is all but an MCP server's
		// (domain.ApprovalRequest.MCPServerGrant).
		MCPServerGrant: serverGrant,
		MCPServerAlias: grantAlias,
		// Where the write really lands, when that is not where the argument says: the one fact
		// this request carries that the model did not write, and the reason the pane can no
		// longer be shown a path the executor will not use (domain.ApprovalRequest.ResolvedPath).
		ResolvedPath: a.resolvedPath(call),
		// What the call reaches beyond what its arguments name, in the TOOL's own line — the
		// second fact on this request the model did not write. Empty for every tool that
		// declares no scope, which is all but one of them (domain.ApprovalRequest.Scope).
		Scope: a.approvalScope(call),
	}
	// The gate is announced BEFORE the Approver is consulted, because the consultation blocks for
	// as long as the human takes: an observer that only saw the decided phase would learn about
	// the wait only once it was over. Both phases carry the same request (domain.ApprovalPhase).
	a.cfg.Events.Emit(domain.ApprovalEvent{EventBase: a.base(turn), Phase: domain.ApprovalRequested, Request: areq})

	decision, err := a.cfg.Approver.Approve(ctx, areq)
	if err != nil {
		if ctx.Err() != nil {
			return false, dispatchCancelled
		}
		a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: "loop", Err: "approver: " + err.Error()})
		return false, dispatchDone
	}

	a.cfg.Events.Emit(domain.ApprovalEvent{EventBase: a.base(turn), Phase: domain.ApprovalDecided, Request: areq, Decision: decision})
	switch decision {
	// The two allows are one branch here: whether this verdict is also REMEMBERED was settled by
	// the CacheKey above and acted on by the seam, so all dispatch has left to read from either is
	// "the call may run".
	case domain.ApprovalAllowForSession, domain.ApprovalAllow:
		return true, dispatchDone
	default: // ApprovalDeny or any unknown verdict — refuse
		return false, dispatchDone
	}
}

// executeTool runs one tool under a recover boundary (ADR 0007): a panic becomes an ErrorEvent
// and an error tool-result so the loop survives; a ctx cancellation propagates as
// dispatchCancelled; any other Execute error is surfaced to the model as an error result rather
// than failing the Turn (a tool returns a Go error only for cancellation).
//
// When box is non-nil the call is a Confine verdict: the Confinement handle (Confiner + box) is
// installed in its context, so a subprocess tool confines the *exec.Cmd it builds
// (confinement-execution-contract §2.2). A subprocess tool that cannot establish the box at run
// time returns ErrConfinementUnavailable rather than running unconfined; executeTool surfaces
// that as dispatchConfinementUnavailable so the caller follows the verdict's demote fallback.
// That translation happens ONLY when box is non-nil: with no box no confinement was asked for,
// no caller has a demote to follow, and a tool claiming otherwise (a third-party or
// host-registered one) is treated as any other erroring tool — an ErrorEvent and an error
// result — so the claim is never swallowed into an empty result.
// An ExternalEffectTool routes through the injected ExternalEffects boundary (ADR 0008) when
// the host supplied one; else it runs live.
func (a *Agent) executeTool(ctx context.Context, turn int, tool domain.Tool, call domain.ToolCall, box *domain.ConfinementBox) (result domain.ToolResult, outcome dispatchOutcome) {
	outcome = dispatchDone
	defer func() {
		if r := recover(); r != nil {
			a.cfg.Events.Emit(domain.ErrorEvent{
				EventBase: a.base(turn),
				Source:    call.Tool,
				Err:       fmt.Sprintf("panic: %v", r),
			})
			result = errorToolResult(call.ID, fmt.Sprintf("tool %q panicked", call.Tool))
			outcome = dispatchDone
		}
	}()

	// Install this Agent's run identity — its nesting depth and the id of the sub_agent call that
	// spawned it — for EVERY call, the top-level agent's included: depth 0 and an empty spawn id
	// are the honest identity of the outermost run, not a missing value, so a tool that builds its
	// own host request (present_document, ask_user) reads a number it can trust rather than one it
	// must guess at. Depth places such a request at the right level, the spawn id inside the right
	// run when a depth-0 fan-out has siblings running at once (ADR 0039). A non-zero pair on
	// either request is no longer reachable (2026-09-15, plan 2026-09-14 - 03, item 5): neither
	// tool is on any sub-agent's roster, so the two host requests are only ever built at depth 0;
	// the carriers stay because the identity is the run's, not the two tools'.
	ctx = domain.WithSubAgentDepth(ctx, a.depth)
	ctx = domain.WithSpawnCallID(ctx, a.callID)
	// And beside them the Console PRIVILEGE key, which the spawn call id must not double as: this
	// one is minted by the registry that compares it, so no two runs can share it however the
	// model numbers its calls (ADR 0059 §6). Installed unconditionally too — "" is the top-level
	// agent, whose Consoles no delegation's end may reap.
	ctx = domain.WithConsoleOwner(ctx, a.consoleOwner)

	if a.isDelegate() {
		// Install this Agent's delegated task so a tool that puts a QUESTION to the human can name
		// the agent asking it (domain.AskRequest.SubAgentTask), the way an ApprovalRequest already
		// names it. The Approval path needs no carrier — the loop builds that request itself
		// (approve) — but ask_user builds its own, one interface boundary away from the Agent that
		// knows the task, so the identity rides the call's context (ADR 0039 decision 12).
		// Nothing is installed on a top-level Agent (isDelegate false): there, it is the only thing
		// that could be asking. No AskRequest is built under this value any more (2026-09-15, plan
		// 2026-09-14 - 03, item 5: ask_user is withheld from every sub-agent); it is still
		// installed because it is the child's identity on the ctx, not the question's.
		ctx = domain.WithSubAgentTask(ctx, a.task)
		// The delegation's short name rides beside it, installed even when EMPTY: an unnamed child
		// must report its own namelessness rather than let an outer value stand in for it, and ""
		// is exactly the "fall back to the task" signal the prompt reads.
		ctx = domain.WithSubAgentName(ctx, a.displayName())
	}

	// Install the undo journal for EVERY call (ADR 0051), the same way the box and the permit
	// reach a tool: the shared write funnel is one os.Root-pinned rule with no engine of its
	// own, so the thing it records into has to ride the execution context. Installing it
	// unconditionally is what makes the coverage boundary the FUNNEL rather than a list kept
	// here — a tool that writes through it is journalled, and a tool that reaches the
	// filesystem some other way (a subprocess, an MCP server, a third-party tool) records
	// nothing HERE precisely because it never asks — those writes are reached instead by the
	// whole-tree images the journal takes around the Exchange (ADR 0074), which is what puts
	// them back within `/undo`'s reach without widening what rides this context. A nil journal
	// installs nothing.
	ctx = undo.WithJournal(ctx, a.journal)

	// Install the console registry for EVERY call too (ADR 0059), and for the same reason the
	// journal rides here rather than sitting on a tool: SwapTools rebuilds tool instances
	// mid-session, so a registry held by a console tool would be a set of running processes
	// nothing could reach to close. The engine owns it and the call context carries it. Beside it
	// the dispatch already carries the spawn call id (WithSpawnCallID, above), which is what a
	// console tool stamps on the Consoles it opens — so a delegation's end can close its own.
	ctx = console.WithRegistry(ctx, a.consoles)

	// And the task list on EVERY call as well (ADR 0072), third for the same structural reason:
	// the list is the ENGINE's session state, so a task_list tool instance that held it would
	// lose the checklist the moment SwapTools rebuilt the roster. Installing it unconditionally
	// is also what keeps the tool stateless (ADR 0008) — it reads the list out of the call it
	// was given and writes back through it, and an execution that carries none (a tool driven
	// outside an engine) finds nil and says so rather than writing into a list nothing renders.
	ctx = tasklist.WithList(ctx, a.tasks)

	// The floor's own context, stripped of any Confinement handle — executeRun installs one
	// for confineChildren Runs before this point, so taking ctx as it stands is not enough:
	// apogee's bookkeeping git (`MarkPre`, `tree.beforeCall`, `mutationWarning`) is never the
	// model's command and must never run inside the call's box. Inside it, the snapshot
	// store's own index (`GIT_INDEX_FILE=<store>/index`, outside the box) is unwritable
	// (`index.lock: Permission denied` on every confined Auto write — apogee-y72), a confined
	// snapshot would pay the re-exec
	// wrapper twice per call — and two extra token label walks per call on Windows
	// (ADR 0020) — for a read that changes nothing, and a backend that could not establish
	// the box would turn the floor's silent skip into the D4 demote signal, gating a call on
	// apogee's own bookkeeping. Stripping the handle also narrows the bookkeeping git's exec
	// fence (gitexec.Resolve → security.ResolveProgram) from the box to the workspace root
	// alone — identical to what the Confine (box != nil) path below has always done, so the
	// narrowing is intended, not a regression. Cancellation still reaches it: floorCtx is the
	// same ctx chain, so a cancelled Turn skips the check, per contract.
	floorCtx := domain.WithoutConfinement(ctx)

	if box != nil {
		// Install the Confinement handle so the subprocess tool confines the command it
		// launches. resolve() chose Confine only after confirming caps (§4), so the Confiner is
		// non-nil and fs-confinement-capable here.
		ctx = domain.WithConfinement(ctx, domain.Confinement{
			Confiner: a.cfg.Confiner,
			Box:      *box,
		})
	}

	// The undo journal's PRE image, taken lazily on the first write-capable call of the Exchange
	// and skipped entirely for a read (ADR 0074 decision 3): an Exchange that only looks at the
	// workspace costs no git and leaves no step the human has to walk past. The journal itself
	// takes the image once per open group, so the test here is only "could this call write",
	// never "has one written yet".
	//
	// No depth gate, unlike the group's opening and closing: a delegated child shares the parent's
	// journal (newChildAgent), and its writes belong inside the parent's Exchange pair, so a
	// child's first write is exactly when the pair's pre image is owed. It rides floorCtx for the
	// reason the tree floor below does — apogee's own bookkeeping git is not the model's command
	// and must never run inside the call's box.
	//
	// A capture that fails is reported and nothing else: the group stays funnel-only, which is the
	// coverage ADR 0051 shipped, and the call proceeds. Failing the tool call over apogee's own
	// bookkeeping is the one thing this must never do.
	if a.journal != nil && !domain.IsReadOnly(tool) {
		if err := a.journal.MarkPre(floorCtx); err != nil {
			a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: "undo", Err: err.Error()})
		}
	}

	// Tracked-file mutation floor (treesnapshot.go — structural, every mode including
	// Bypass, ADR 0006 class): snapshot the git tree around a subprocess run so the
	// result can name the workspace files the command changed. Best-effort by contract:
	// a non-repo workspace, a git error or a timeout skips the check for this call
	// silently, and the floor never turns a clean result into an error.
	preTree, watchTree := "", false
	if domain.IsSubprocessTool(tool) {
		preTree, watchTree = a.tree.beforeCall(floorCtx)
	}

	res, err := a.runTool(ctx, tool, call)
	if err != nil {
		if ctx.Err() != nil {
			return domain.ToolResult{}, dispatchCancelled
		}
		// A subprocess tool that could not confine its command (the backend returned
		// ErrConfinementUnavailable when asked to wrap the cmd) reports it as a Go error rather
		// than running unconfined. Surface it as the demote signal so the caller follows the
		// verdict's fallback (Resolution D4). The box test is what MAKES that true rather than
		// assuming it: only a Confine call installs a handle, so only a Confine call can have a
		// demote to fall back to. Outside one, no caller reads the outcome, so translating there
		// would swallow the claim into an empty result — the sentinel takes the ordinary
		// tool-error branch below instead, reaching the human and the model.
		if box != nil && errors.Is(err, domain.ErrConfinementUnavailable) {
			return domain.ToolResult{}, dispatchConfinementUnavailable
		}
		a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: call.Tool, Err: err.Error()})
		errResult := errorToolResult(call.ID, err.Error())
		if watchTree {
			// The error result carries the warning too: a command that failed may
			// still have written before it failed — the incident's exact shape.
			appendTreeMutationWarning(&errResult, a.tree.mutationWarning(floorCtx, preTree))
		}
		return errResult, dispatchDone
	}
	if watchTree {
		appendTreeMutationWarning(&res, a.tree.mutationWarning(floorCtx, preTree))
	}
	return res, dispatchDone
}

// runTool routes the call to the injected ExternalEffects boundary for an external-effect
// tool when one is configured (ADR 0008 — the single non-forkable-effect seam, both network
// and MCP kinds), otherwise to the tool's live Execute. The gating decision keyed on the
// effect KIND (the Resolution); routing here is the SEPARATE concern of where the effect
// actually runs, so the two stay distinct (confinement-execution-contract §8 / task P3.4).
func (a *Agent) runTool(ctx context.Context, tool domain.Tool, call domain.ToolCall) (domain.ToolResult, error) {
	if _, isExternal := tool.(domain.ExternalEffectTool); isExternal && a.cfg.ExternalEffects != nil {
		return a.cfg.ExternalEffects.Do(ctx, call)
	}
	return tool.Execute(ctx, call)
}

// effectiveMode is the autonomy mode the per-call Resolution runs under. For a top-level Agent
// it is simply the Agent's own live mode. For a sub-agent (liveMode != nil) it is the TIGHTER of
// the child's spawn mode and the parent's EFFECTIVE mode (ADR 0013), so a parent tightening
// mid-delegation (Shift+Tab from Auto down to Plan) gates/refuses the still-running child's next
// call, while a parent loosening never loosens the child. Composing on the parent's effective
// mode rather than its own makes the rule transitive: a depth-2 grandchild folds in the top-level
// agent's live mode through its parent, so no descendant can ever run looser than an ancestor.
// The recursion terminates at the top-level agent, whose liveMode is nil. Every mode is read
// under the modeMu of the agent that owns it (Mode(), reached through the captured accessors), so
// a concurrent SetMode anywhere on the chain is observed race-free.
func (a *Agent) effectiveMode() domain.Mode {
	own := a.Mode()
	if a.liveMode == nil {
		return own
	}
	return domain.TighterMode(own, a.liveMode())
}

// writeTargetClass is what classifyWriteTarget answers about a workspace-scoped writer's target:
// the three facts the ladder and the executor read, all taken from ONE path resolution. Each
// field is defined by the matching bullet on classifyWriteTarget.
type writeTargetClass struct {
	inFence   bool   // inside the ladder's fence (workspace root ∪ declared writable paths)
	inScratch bool   // inside the LIVE session scratch dir (false when none is set)
	escape    string // the resolved path a permit must name, "" for an in-root write
	real      string // the resolved path itself, "" when the call has no inspectable target
}

// classifyWriteTarget answers ALL the facts a workspace-scoped writer's target decides, from the
// ONE resolution that discovers them (EvalRealPath touches disk — this is the single I/O-tainted
// fact dispatch precomputes for the hermetically pure resolve(), and resolving twice to answer
// twice would invite the two answers to describe different paths):
//
//   - inFence — whether the target lands inside the FENCE the ladder classifies against, which is
//     the workspace root UNION the box's declared writable paths (ADR 0049 Q3). A call with no
//     inspectable target (ok==false) is in-bounds, exactly as before: the Resolution runs it and
//     path-safety bounds it at Execute. A tool that is not a workspace-scoped writer is never
//     in-workspace by this seam.
//   - inScratch — whether the target lands inside the session's own scratch dir, read live so a
//     SetScratchDir move lands on the next call. It is decided BEFORE the workspace check so a
//     scratch dir that happens to sit under the workspace root still classifies as scratch; a
//     call with no inspectable target is never in-scratch (Plan runs nothing on its account).
//   - escape — the resolved path a permit must name for the write to LAND, set whenever the
//     target is outside the workspace ROOT. That is deliberately wider than !inFence: a writable
//     path outside the workspace is in-fence for the ladder (it gates nothing) and still needs the
//     permit at Execute, because the fence itself keeps one rule — the workspace root, plus
//     whatever single target the context's permit names.
//   - real — the resolved path itself, wherever it lands, so a consumer that compares the target
//     against another reading of the same resolver (the wrap-up's output path, Agent.outputTarget)
//     compares two outputs of one resolution rather than resolving again.
func (a *Agent) classifyWriteTarget(tool domain.Tool, call domain.ToolCall) writeTargetClass {
	abs, ok := tools.WorkspaceWriteTarget(tool, call)
	if !ok {
		return writeTargetClass{inFence: true} // nothing inspectable to classify ⇒ in-bounds (Execute path-bounds it)
	}
	inScratch := pathWithin(abs, a.ScratchDir()) // pathWithin answers false for an unset ("") dir
	if pathWithin(abs, a.cfg.WorkspaceDir) {
		return writeTargetClass{inFence: true, inScratch: inScratch, real: abs}
	}
	// The union is read off the LIVE box — the same fold every per-call consumer builds from —
	// rather than the raw ConfineWritablePaths slice, so the session's scratch dir (folded in by
	// ConfinementBox and moved by SetScratchDir) is in-fence for the native writers exactly as the
	// orientation's `Scratch dir: … — writable` line announces it. Reading the config slice alone
	// left that dir gating in Allow-Edits/Auto and refused by a Firing's denier.
	for _, writable := range a.confinementBox().WritablePaths {
		if pathWithin(abs, writable) {
			return writeTargetClass{inFence: true, inScratch: inScratch, escape: abs, real: abs}
		}
	}
	return writeTargetClass{inScratch: inScratch, escape: abs, real: abs}
}

// resolvedPath is the DISCLOSURE twin of classifyWriteTarget: the same resolved target,
// surfaced as a path instead of consumed as a bool, and only when it differs from the path the
// model's argument names (tools.ResolvedWriteTarget). It rides the ToolCallEvent and the
// ApprovalRequest so a Driver can say where a write really goes; it is "" for every ordinary
// call, which is what keeps an unremarkable prompt unremarkable.
//
// It looks the tool up itself because the two seams that need it stand on either side of the
// registry lookup — the ToolCallEvent is emitted before the call is resolved, the Approval
// after — and an unknown tool simply discloses nothing, exactly as it classifies as nothing.
// The resolution is the same disk-touching one dispatch already performs for the ladder, so a
// gated write costs one more EvalRealPath and a non-writer costs a type assertion.
func (a *Agent) resolvedPath(call domain.ToolCall) string {
	tool, ok := a.lookupTool(call.Tool)
	if !ok {
		return ""
	}
	return tools.ResolvedWriteTarget(tool, call)
}

// approvalScope is the ApprovalRequest's other tool-derived fact (domain.ApprovalScoper): the
// one line a tool states about what THIS call reaches beyond what its arguments name — go vet's
// package directory around the file the call named. It looks the tool up itself for the same
// reason resolvedPath does, and an unknown tool declares nothing, exactly as it classifies as
// nothing. It rides the Approval only: the gate is the surface where the widening is decided,
// while a tool that runs ungated states the same scope on its own result string.
func (a *Agent) approvalScope(call domain.ToolCall) string {
	tool, ok := a.lookupTool(call.Tool)
	if !ok {
		return ""
	}
	return domain.ApprovalScopeOf(tool, call)
}

// mcpServerGrant is the ApprovalRequest's third tool-derived fact: whether an allow-for-session on
// this call would be remembered at MCP SERVER grain, and the alias of the server such an answer
// would clear (domain.ApprovalRequest.MCPServerGrant / .MCPServerAlias). It reads the very marker
// the cache key is minted from (mcpServerAlias, resolution.go), so what the pane discloses and what
// the memory keys on are one fact rather than two readings of it.
//
// The alias is deliberately NOT recovered from the key by stripping its prefix: a forced gate
// travels with an EMPTY CacheKey (approve), so a key-derived alias would read as the unnamed server
// exactly where nothing is remembered at all. It looks the tool up itself for the same reason
// resolvedPath and approvalScope do, and an unknown tool discloses nothing, exactly as it
// classifies as nothing.
func (a *Agent) mcpServerGrant(call domain.ToolCall) (alias string, serverGrant bool) {
	tool, ok := a.lookupTool(call.Tool)
	if !ok {
		return "", false
	}
	return mcpServerAlias(tool)
}

// fsConfinementAvailable reports whether the injected Confiner can enforce filesystem
// confinement on this host — the caps gate the Resolution checks before choosing to confine a
// subprocess tool (confinement-execution-contract §4/§5).
func (a *Agent) fsConfinementAvailable() bool {
	return a.cfg.Confiner != nil && a.cfg.Confiner.Capabilities().FSWrite
}

// pathWithin reports whether abs (an already-resolved real path) is the workspace root or lives
// beneath it, resolving the root through symlinks the same way the write tool's target resolver
// does so the two agree (e.g. macOS /tmp). An empty root cannot contain anything, so a write is
// treated as out-of-workspace — the safe default that gates.
func pathWithin(abs, root string) bool {
	if root == "" {
		return false
	}
	realRoot := security.EvalRealPath(filepath.Clean(root))
	if abs == realRoot {
		return true
	}
	return strings.HasPrefix(abs, realRoot+string(filepath.Separator))
}

// appendToolResult commits a tool result to the conversation as a tool message (linked to
// its call by ID) and emits the ToolResultEvent observers see, after clamping a pathologically
// oversized result to the structural floor (clampToolResult). The clamp lands here, at the ONE
// seam every tool result crosses on its way into history, so no route — a plain call, a Confine
// verdict's, an approved gate's, a sub-agent delegation's, an error result — can bypass it.
//
// The same one-seam property is why the IsError flag is projected onto the committed message
// here (domain.ToolOutcomeOf): the flag is the only authority on whether a call failed, and it
// used to die at this line, leaving a history-scanning Reaction to guess from the result text —
// which for a successful read IS a file body, error strings and all. Every route committing a
// result gets the marker, so the guess is now only ever a legacy-record fallback.
//
// call is the call the result answers, as the pre-tool-exec Moment left it: its Tool is the
// resolved name the ToolResultEvent carries. writeTarget is the call's classified write target —
// the resolved path prepareCall carried from the ladder's one resolution, "" for a call
// that is not a write or never resolved one — stamped onto the event as it is, whatever the
// result's fate (domain.ToolResultEvent).
//
// advised is the post-tool-result cascade's advise slot (reactions.go) — the spans this result's
// message carries as a fenced trailer, nil for the two routes that commit a result no cascade ran
// on (a pre-tool-exec fault, a hook-failed delegation slot).
//
// The step-budget notice (stepnotice.go) lands here too, AFTER the advice and under the engine's
// own fence (Message.WithEngineNote): it is structural — consulted for every result on every
// route, under Bypass too, booking no firing — and the fence header is what tells the model it
// is the engine speaking and not a Reaction, which a delegate reading a source file full of
// reaction prose has mistaken an advice fence for. Its token-budget twin (tokenBudgetNotice)
// follows it, so the fence order on a closing result is fixed: tool output, advice, step note,
// token note — and the wrap-up directive, stamped on the request tail at send time
// (Request.NoteOnTail), always last.
func (a *Agent) appendToolResult(
	turn int,
	call domain.ToolCall,
	result domain.ToolResult,
	writeTarget string,
	advised []advice,
) {
	result.Content = a.clampToolResult(result.Content)
	msg := domain.Message{
		Role:        domain.RoleTool,
		Content:     result.Content,
		ToolCallID:  result.CallID,
		ToolOutcome: domain.ToolOutcomeOf(result.IsError),
	}
	// The advise slot the post-tool-result cascade filled, rendered in ladder order AFTER the
	// clamp: a trailer is guidance for the next Step, so it must not be what the structural floor
	// elides, and its offset must measure the content as committed. WithAdvice stamps each span's
	// Offset from the message as it stands — past the clamp and past every earlier fence — so the
	// ledger keeps pointing at its own fence however many spans land.
	for _, adv := range advised {
		msg = msg.WithAdvice(adv.span, adv.text)
	}
	if text, fired := a.stepBudgetNotice(); fired {
		msg = msg.WithEngineNote(stepNoticeTopic, text)
	}
	if text, fired := a.tokenBudgetNotice(); fired {
		msg = msg.WithEngineNote(tokenNoticeTopic, text)
	}
	a.conv.Append(msg)
	// The event carries the tool's own result, never the trailer: advice is a model-facing
	// injection, so what an observer records, the transcript shows and the session record keeps is
	// the output the tool actually produced.
	a.cfg.Events.Emit(domain.ToolResultEvent{
		EventBase:   a.base(turn),
		Result:      result,
		Tool:        call.Tool,
		WriteTarget: writeTarget,
	})
}

// structuralFloor is the BOUND behind both structural clamps: the whole History allocation — the
// most any single body could occupy and still leave the conversation renderable. Content past it
// can never survive ANY reducer, so committing it whole buys nothing and can doom the Turn
// outright: the emergency fold's own summary call keeps the most recent message unconditionally
// (renderBudgetedTranscript), so a fresh giant body IS that message and overflows the fold that was
// supposed to rescue the Turn.
//
// With an unknown window (a zero History allocation — Allocate had no basis to allocate) the floor
// measures against compactUnknownWindowTranscriptTokens instead of standing down — the substitution
// growthBounds.historyFloor makes at the one site every growth bound shares (deriveGrowthBounds,
// compact.go), so this clamp and the boundary trigger bound against one number. Being inert there
// was not the conservative choice it looked like: the fold's transcript render keeps the most recent
// message UNCONDITIONALLY, so an unclamped giant body becomes the one message the emergency fold
// cannot shed and re-wedges the session bounding the fold was meant to un-wedge (audit 2026-08-01,
// follow-up B). Keying the floor to the fold's own unknown-window budget makes the two meet exactly:
// content that survives the clamp is, by construction, content the fold can still render.
//
// The threshold sits deliberately far above the tool-result-cap Floor guard's: the whole History
// allocation (~60% of the working room, ~48% of the window at the default reserve), chosen because
// it sits BELOW the emergency fold's own transcript budget at every window an agent can
// realistically run in, which is the property that keeps the fold survivable — while the
// guard's tighter 40%-of-working-room cap shapes the ordinary case. That ordering is
// arithmetic, not an invariant: the fold budgets its transcript at window - compactMaxTokens -
// compactPromptOverheadTokens (= window - 4608), so the floor stays under it only while
// 0.6*(window - reserve) < window - 4608 — windows above ~8.9k tokens at the default reserve.
// Smaller windows invert the two and lose the property; they sit far under the ~32k target window
// and are too small to run a coding Turn in (ADR 0018 §8 states the same condition).
func (a *Agent) structuralFloor() int {
	return deriveGrowthBounds(a.budget()).historyFloor
}

// clampToBound is the RENDERING both structural clamps share: content whose estimated tokens exceed
// bound is replaced by the head/tail-plus-marker elision (context.TruncateToolResult — the same
// shape the tool-result-cap guard renders), so the model reads ONE "the middle was dropped, re-read the
// range" idiom whichever seam produced it. Content within bound is returned untouched.
//
// It never GROWS content: a pathological few-very-long-lines body the head/tail form cannot shrink
// is left whole (the same check the tool-result-cap guard applies).
func (a *Agent) clampToBound(content string, bound int) string {
	b := a.budget()
	if b.EstimateTokens(len(content)) <= bound {
		return content
	}
	clamped := apogeectx.TruncateToolResult(content, int(float64(bound)*b.CharsPerToken))
	if len(clamped) >= len(content) {
		return content
	}
	return clamped
}

// clampToolResult is the STRUCTURAL floor on a single tool result: a result whose estimated tokens
// exceed the ENTIRE History allocation is committed to the conversation as the shared elision
// instead of whole.
//
// It is structural, not a Reaction (ADR 0006's floor): it consults no config and is never
// disabled under Bypass. The tool-result-cap Floor guard is the
// tighter working cap above it and cannot substitute for it — the guard caps only the turns BEFORE
// the most recent tool call, so the freshly appended result (the one that overflows) is exactly the
// one it never touches. Both are structural now, and the difference is WHAT each edits: the clamp
// edits the conversation, the guard the projected request; only the guard has a key
// (`tool-result-cap`) a user can switch off.
//
// Unlike the guard, which edits only the projected request, this clamp edits the conversation
// itself: the raw result never reaches history, and so never reaches a snapshot or the rendered
// transcript. That is the price of a floor that must hold for every later reducer — and the model
// is told, in the marker, to re-read the omitted range.
//
// A tool result is not the only body with this floor: resolveFileRefs and resolveSkillRefs
// (loop.go) clamp every @file block and every attached skill body against the same bound, divided
// across ALL the references of one message (refBound), so an assembled block of references can no
// more outgrow the allocation than a result can. The seams share structuralFloor and clampToBound
// because they have one reason to change — the fold's arithmetic.
func (a *Agent) clampToolResult(content string) string {
	return a.clampToBound(content, a.structuralFloor())
}

// errorToolResult builds a tool-level failure result surfaced to the model (IsError) rather
// than returned as a Go error, which the loop reserves for ctx cancellation (ADR 0007).
func errorToolResult(callID, message string) domain.ToolResult {
	return domain.ToolResult{CallID: callID, Content: message, IsError: true}
}

// recordExecutedTrip records an executed call's audit + circuit-breaker outcome and surfaces the
// single ErrorEvent on the breaker's trip edge (so a runaway identical-failure loop is halted,
// not crashed). It is the shared post-execution tail of a Run and a Confine verdict.
func (a *Agent) recordExecutedTrip(turn int, call domain.ToolCall, verdict resolution, result domain.ToolResult) {
	if tripped := a.recordExecuted(turn, call, verdict.auditDecision, verdict.auditReason, result); tripped {
		a.cfg.Events.Emit(domain.ErrorEvent{
			EventBase: a.base(turn),
			Source:    call.Tool,
			Err: fmt.Sprintf("circuit-breaker tripped: tool %q failed %d times with identical arguments; "+
				"further identical calls will be refused", call.Tool, a.guards.Breaker.Threshold()),
		})
	}
}

// recordExecuted appends the executed call's audit record (feeding the circuit-breaker) AND
// emits an AuditEvent so the trail is observable, not only held in the in-process ring
// (security-review M1). It returns whether the breaker tripped on this call. A sub-agent
// records through its own guards but emits through the SAME EventSink at Depth > 0, so a
// delegated call's audit reaches the parent's observer instead of vanishing with the child.
func (a *Agent) recordExecuted(turn int, call domain.ToolCall, decision security.AuditDecision, reason string, result domain.ToolResult) (tripped bool) {
	tripped = a.guards.RecordExecution(call, decision, reason, result)
	a.emitAudit(turn, call, decision, reason, result)
	return tripped
}

// recordBlocked appends a blocked/diverted call's audit record AND emits the matching
// AuditEvent (security-review M1), so a refused/denied call is observable, not silently
// dropped into a ring no observer reads.
func (a *Agent) recordBlocked(turn int, call domain.ToolCall, decision security.AuditDecision, reason string, result domain.ToolResult) {
	a.guards.RecordBlocked(call, decision, reason, result)
	a.emitAudit(turn, call, decision, reason, result)
}

// emitAudit surfaces one audit record to the EventSink as a domain.AuditEvent (M1). It is
// the single bridge from the security audit record onto the observable event stream; the
// agent layer constructs the domain-only event so domain keeps its no-upward-dependency
// property (ADR 0010).
func (a *Agent) emitAudit(turn int, call domain.ToolCall, decision security.AuditDecision, reason string, result domain.ToolResult) {
	a.cfg.Events.Emit(domain.AuditEvent{
		EventBase: a.base(turn),
		Tool:      call.Tool,
		CallID:    call.ID,
		Decision:  string(decision),
		Reason:    reason,
		IsError:   result.IsError,
	})
}

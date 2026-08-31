package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/airiclenz/apogee/internal/console"
	apogeectx "github.com/airiclenz/apogee/internal/context"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
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

// dispatchTools runs each requested tool call through the pre-tool-exec hooks, the Approval
// gate, execution, and the post-tool-result hooks — appending each result to the
// conversation as a tool message and emitting the observability events. Approval is
// consulted here, AFTER the stream has closed (the §6 #6 resolution: stream fully, then
// gate), so a blocking Approver never holds an open Upstream connection.
//
// A reply's calls are PARTITIONED first (ADR 0039 decision 11): the leaf tools run first, in
// their emitted order and through exactly the path they have always taken, and the sub_agent
// delegations run after them as one group. The order is a property of the reply alone, not of
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
	if outcome := a.dispatchSerially(ctx, turn, leaves); outcome == dispatchCancelled {
		return dispatchCancelled
	}
	if width := a.fanOutWidth(len(delegations)); width > 1 {
		return a.dispatchFanOut(ctx, turn, width, delegations)
	}
	return a.dispatchSerially(ctx, turn, delegations)
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
// dispatchTools calls it ONCE per reply and hands the number DOWN as an argument, which is what
// makes a group's width immutable for the life of that group: the Delegation target it may have
// been resolved from is re-stated on every heartbeat of the Sub-agent server (ADR 0045), so a
// beat landing between the first child and the last must not be able to re-size a pool that is
// already running. One reply, one width, however many beats cross it.
func (a *Agent) fanOutWidth(delegations int) int {
	if delegations < 2 {
		return 1
	}
	return min(a.delegationWidth(), delegations)
}

// delegationWidth reports how many sub_agent delegations THIS agent may run at once,
// independent of any particular reply: the resolved Parallel agents cap at depth 0, and 1
// everywhere else.
//
// Depth 0 is the whole eligibility rule (decision 3): a child's own delegations stay serial
// inline, so there is no slot accounting across levels and no way for a nested fan-out to hold
// slots its own children need. It is one rule with two readers — the pool below sizes itself by
// it, and buildRequest stamps it onto the hook-facing view (LoopView.ParallelAgents) so a
// Mechanism synthesizing delegations batches by the same width the engine will honour. That
// second reader is why guided decomposition's batch needs nothing of its own to follow a routed
// cap (ADR 0045 §5): its min(cap, remaining) reads the view, the view carries this number, and
// this number already knows which server the children will run on.
func (a *Agent) delegationWidth() int {
	if a.depth != 0 {
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

// dispatchSerially is the loop this dispatch has always been: one call at a time, each carried
// from its ToolCallEvent through the hooks, the Resolution, execution, and into history before
// the next call is looked at. It is the path every leaf tool takes, and the path a delegation
// group takes whenever the fan-out width is 1 (cap < 2, depth > 0, or a single call), so those
// cases keep today's behavior exactly.
func (a *Agent) dispatchSerially(ctx context.Context, turn int, calls []domain.ToolCall) dispatchOutcome {
	for _, call := range calls {
		a.cfg.Events.Emit(domain.ToolCallEvent{EventBase: a.base(turn), Call: call, ResolvedPath: a.resolvedPath(call)})

		if err := a.runPreToolExecHooks(ctx, turn, &call); err != nil {
			// A pre-tool-exec hook panicked (recovered into an ErrorEvent): skip the call
			// with an error result rather than running it against a half-applied decision.
			a.appendToolResult(turn, errorToolResult(call.ID, "pre-tool-exec hook failed"))
			continue
		}

		result, outcome := a.resolveAndExecute(ctx, turn, call)
		if outcome == dispatchCancelled {
			return dispatchCancelled
		}

		// Feed this call's outcome to self-regulation's proxy signals (R3): a novel read or a
		// successful write is the productive signal, an error result the harmful one. Ordering
		// relative to the post-tool-result hooks is immaterial to their judgment — fires are
		// judged by the NEXT Turn's outcome (next-Turn judgment), not this one's.
		a.noteToolProductivity(call, result)
		a.runPostToolResultHooks(ctx, turn, call, &result)
		a.appendToolResult(turn, result)
	}
	return dispatchDone
}

// ----------------------------------------------------------------------------
// Depth-0 fan-out (ADR 0039 — Parallel agents)
// ----------------------------------------------------------------------------
//
// A reply that asks for several delegations at once gets them at once, up to the bound server's
// Parallel agents cap. The fan-out is deliberately NOT "run the whole per-call pipeline on N
// goroutines": only the CHILD RUN is concurrent. Everything a delegation shares with its
// siblings — the pre-tool-exec hooks, the guardrail probe and the Resolution, the audit record,
// the self-regulation signals, the post-tool-result hooks, and the append into history — stays
// on the dispatching goroutine, in emitted-call order, on either side of the pool. That is what
// keeps the Agent's own state (registry, guards, tracker, conversation) single-goroutine while
// N children run, and what makes the resulting history DETERMINISTIC regardless of which child
// finishes first.
//
// The three phases are: prepare each call (serial), run the Delegate verdicts through a bounded
// pool (concurrent), commit each call's result (serial). A cancellation is answered between the
// last two — every child is joined first, then the whole group is discarded unappended, so the
// parent Turn rolls back with no partial delegation in history (ADR 0013 §5, now N-wide).

// fanOutSlot is one delegation's state as it crosses the pool: what was decided about the call
// before any child ran, what the child produced, and how it ended. Each slot is written by
// exactly one goroutine at a time — the dispatching one in the prepare and commit phases, one
// worker in between — so the slice needs no lock.
type fanOutSlot struct {
	call    domain.ToolCall
	verdict resolution
	result  domain.ToolResult
	// run marks a Delegate verdict: this slot's child still has to run through the pool. A
	// refused (or unknown-tool, or hook-failed) slot already holds its final result.
	run bool
	// hookFailed marks a pre-tool-exec hook failure, whose result is appended WITHOUT the
	// productivity signal and the post-tool-result hooks — the serial path's `continue`.
	hookFailed bool
	outcome    dispatchOutcome
}

// dispatchFanOut runs a reply's delegation group concurrently, width children at a time, and
// commits their results in emitted-call order. It returns dispatchCancelled when ANY child ended
// on a cancellation: the whole group is then dropped unappended, because a delegation is atomic
// within the parent Turn and the Turn is about to roll back wholesale (ADR 0013 §5).
func (a *Agent) dispatchFanOut(ctx context.Context, turn, width int, calls []domain.ToolCall) dispatchOutcome {
	slots := make([]fanOutSlot, len(calls))
	for i, call := range calls {
		slots[i] = a.prepareDelegation(ctx, turn, call)
	}

	a.runDelegationPool(ctx, turn, width, slots)

	// Join first, decide after: a sibling that reached its boundary with a usable result is
	// still discarded, because the recovery point is the pre-dispatch boundary of the whole Turn.
	for i := range slots {
		if slots[i].outcome == dispatchCancelled {
			return dispatchCancelled
		}
	}
	for i := range slots {
		a.commitDelegation(ctx, turn, &slots[i])
	}
	return dispatchDone
}

// prepareDelegation carries one delegation as far as it can go WITHOUT running a child: it
// surfaces the ToolCallEvent, runs the pre-tool-exec hooks, and computes the call's Resolution.
// Everything here touches Agent-wide state (the Mechanism registry, the guardrails, the
// self-regulation view), which is why it runs on the dispatching goroutine for every call in the
// group before any child starts.
//
// One consequence is deliberate and worth naming: siblings are resolved against the SAME
// guardrail state, so a delegation cannot observe a breaker its sibling tripped. Concurrent
// calls cannot see each other's outcomes by construction — that is what concurrent means — and
// the shared read-only dangerous-action floor still re-fires on every call a child actually
// makes (ADR 0013 D3).
//
// The two dispatch facts resolveAndExecute answers before resolve() are answered here too, and in
// its order: the registry miss, then arguments whose keys fold together
// (collidingArgumentKeysResult). Both produce a final, unaudited slot with no child — the Approver
// is never consulted and nothing runs — so a call's disposition never depends on whether the
// reply that carried it happened to fan out.
func (a *Agent) prepareDelegation(ctx context.Context, turn int, call domain.ToolCall) fanOutSlot {
	a.cfg.Events.Emit(domain.ToolCallEvent{EventBase: a.base(turn), Call: call, ResolvedPath: a.resolvedPath(call)})

	if err := a.runPreToolExecHooks(ctx, turn, &call); err != nil {
		// Same disposition as the serial path: an error result, no child, and no postlude.
		return fanOutSlot{
			call:       call,
			result:     errorToolResult(call.ID, "pre-tool-exec hook failed"),
			hookFailed: true,
		}
	}

	tool, ok := a.lookupTool(call.Tool)
	if !ok {
		// The recursion point is not in this Agent's registry (e.g. withheld): the registry miss
		// is a dispatch fact answered before resolve(), exactly as resolveAndExecute answers it.
		return fanOutSlot{call: call, result: errorToolResult(call.ID, fmt.Sprintf("unknown tool %q", call.Tool))}
	}

	if result, refused := collidingArgumentKeysResult(call); refused {
		return fanOutSlot{call: call, result: result}
	}

	verdict := resolve(a.resolutionInput(tool, call, a.guards.PreExecute(call, tool, a.guardExemptions())))
	if verdict.kind != resolveDelegate {
		// resolve() answers a sub_agent call with Delegate or Refuse and nothing else (its row 2:
		// a Tier-2 force is deliberately not applied to a delegation, so no Gate or Confine can
		// reach here) — so this is the guard hard-refuse and the depth-bound refusal.
		return fanOutSlot{call: call, verdict: verdict, result: a.executeRefuse(turn, call, verdict)}
	}
	return fanOutSlot{call: call, verdict: verdict, run: true}
}

// runDelegationPool drives every slot that still needs a child through width worker goroutines,
// one call each, and returns once all of them have reached a boundary. The workers pull indices
// off one channel, so width is a true concurrency bound rather than a goroutine count: a group of
// nine under a cap of three is three children at a time, three times over.
//
// ctx is handed to every child unchanged, so a cancel reaches all of them at once and each
// unwinds at its own next boundary; the join below is what "the pool waits" means. A child's
// failure is ITS result and nothing more — no sibling is cancelled (ADR 0039 decision 4).
//
// The worker brackets each child with its lifecycle phases (domain.SubAgentPhaseEvent): started as
// the job is DEQUEUED — which is what makes a slot-less delegation observably queued rather than
// silently pending — and finished, carrying the result, as the child returns. They are the group's
// only per-child timing: the results themselves still burst after the join, in call order.
func (a *Agent) runDelegationPool(ctx context.Context, turn, width int, slots []fanOutSlot) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < width; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				a.emitSubAgentPhase(turn, slots[i].call, domain.SubAgentStarted, domain.ToolResult{})
				slots[i].result, slots[i].outcome = a.runDelegation(ctx, turn, slots[i].call)
				if slots[i].outcome != dispatchCancelled {
					// A cancelled group is discarded unappended, so its children never finish into
					// a result: claiming one here would report a delegation the parent Turn is
					// about to roll back.
					a.emitSubAgentPhase(turn, slots[i].call, domain.SubAgentFinished, slots[i].result)
				}
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

// runDelegation is one worker's whole job: drive this call's nested Agent to its boundary. The
// recover sits HERE, inside the goroutine, because that is the only place it can be: a panic
// crossing a goroutine's top frame takes the process down with it, so the per-child fault
// boundary ADR 0007 promises has to be the child's own goroutine. A recovered child becomes an
// error tool-result its sibling and the parent Exchange survive.
//
// The serial path deliberately keeps its existing shape here — a delegation panic there still
// unwinds to the driver, exactly as before this fan-out existed.
func (a *Agent) runDelegation(ctx context.Context, turn int, call domain.ToolCall) (result domain.ToolResult, outcome dispatchOutcome) {
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
	return a.runSubAgent(ctx, call)
}

// emitSubAgentPhase surfaces one delegation lifecycle boundary. The event is stamped with the
// CHILD's identity — one level deeper than this Agent, under the spawning call's id — rather than
// with the emitting parent's, so it carries the same run identity as the events the child itself
// emits and names the tool-call block an observer attaches it to.
//
// Both delegation paths call it, so a lone (serial) delegation reports the same started/finished
// pair a pooled one does: nothing that runs is ever left looking queued.
func (a *Agent) emitSubAgentPhase(turn int, call domain.ToolCall, phase domain.SubAgentPhase, result domain.ToolResult) {
	base := a.base(turn)
	base.Depth++
	base.CallID = call.ID
	a.cfg.Events.Emit(domain.SubAgentPhaseEvent{EventBase: base, Phase: phase, Result: result})
}

// commitDelegation lands one finished delegation: the audit record its verdict earns, the
// self-regulation signal, the post-tool-result hooks, and the append into history — the same
// sequence, in the same order, the serial path runs inline for every call. Running it here, one
// slot at a time in emitted-call order, is what makes the fan-out's history independent of
// completion order.
func (a *Agent) commitDelegation(ctx context.Context, turn int, slot *fanOutSlot) {
	if slot.hookFailed {
		a.appendToolResult(turn, slot.result)
		return
	}
	if slot.run {
		// executeDelegate's tail: a delegation that actually ran is audit-recorded under its
		// verdict. A refused slot was already recorded by executeRefuse in the prepare phase.
		a.recordExecuted(turn, slot.call, slot.verdict.auditDecision, slot.verdict.auditReason, slot.result)
	}
	a.noteToolProductivity(slot.call, slot.result)
	a.runPostToolResultHooks(ctx, turn, slot.call, &slot.result)
	a.appendToolResult(turn, slot.result)
}

// resolveAndExecute gathers the facts one tool call is decided from — the registry lookup, the
// always-on guardrails, the effective mode, the caps probe, and the one on-disk write-target
// check — computes the call's complete Resolution once (resolve(), resolution.go), and then
// EXECUTES that verdict mechanically. It holds no ladder, guard-tier, or demote decision of its
// own: resolve() decides, the switch below carries it out (Resolution D6;
// confinement-execution-contract §4). It returns the tool result (or an error result) and
// whether ctx was cancelled mid-flight.
//
// An unknown tool is rejected here, before resolve(): the registry miss is a dispatch fact, and
// short-circuiting it keeps a withheld tool (e.g. sub_agent at the depth bound) resolving as an
// unknown tool exactly as before, un-audited (Resolution D8). resolve() has a matching
// unknown-tool row for its own test, but dispatch never reaches it.
//
// Its neighbour row is arguments that name one parameter twice under different key cases
// (domain.CollidingArgumentKeys): the executor's decode folds them and runs ONE value, so every
// other reader of the call — the pane a human decides on, the dangerous-action guard, the
// allow-for-session digest — is at risk of describing the value the tool discards. There is no
// spelling of such a call that all of them agree on, so it is refused here, before resolve():
// the Approver is never asked about it, no gate key is ever minted for it, and the tool never
// runs. Arguments that do not DECODE are left alone — the tool's own decodeToolArgs reports
// those, with the parameter names the tool actually has. The refusal itself lives in
// collidingArgumentKeysResult, because prepareDelegation owes a fanned-out call the same answer.
func (a *Agent) resolveAndExecute(ctx context.Context, turn int, call domain.ToolCall) (domain.ToolResult, dispatchOutcome) {
	tool, ok := a.lookupTool(call.Tool)
	if !ok {
		return errorToolResult(call.ID, fmt.Sprintf("unknown tool %q", call.Tool)), dispatchDone
	}
	if result, refused := collidingArgumentKeysResult(call); refused {
		return result, dispatchDone
	}

	verdict := resolve(a.resolutionInput(tool, call, a.guards.PreExecute(call, tool, a.guardExemptions())))

	switch verdict.kind {
	case resolveRefuse:
		return a.executeRefuse(turn, call, verdict), dispatchDone
	case resolveDelegate:
		return a.executeDelegate(ctx, turn, call, verdict)
	case resolveGate:
		return a.executeGate(ctx, turn, tool, call, verdict)
	case resolveConfine:
		return a.executeConfine(ctx, turn, tool, call, verdict)
	default: // resolveRun
		return a.executeRun(ctx, turn, tool, call, verdict)
	}
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

// collidingArgumentKeysResult is the refusal itself, in the one shape both dispatch paths need:
// the error result for a call whose argument object names one parameter twice under different key
// cases, and false for every ordinary call. It exists as a function because the serial path and
// the fan-out path must answer such a call IDENTICALLY — a delegation that reaches a pool is
// still a call whose arguments no two readers agree on, and a check living in only one of the two
// would make the refusal depend on the bound server's Parallel agents cap.
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

// resolutionInput assembles the facts resolve() decides from for one call: the effective mode,
// the resolved tool, the guardrail verdict, the LIVE confine-to-workspace flag (read through
// ConfineToWorkspace() under its lock, so a /confine toggle from the UI lands on the next call
// exactly as a Shift+Tab mode change does), the backend caps probe, the precomputed on-disk
// write-target check (the one I/O-tainted fact — resolve() does
// none), the sub-agent depth bound, whether an Approver is configured, and the confinement box
// a Confine verdict would run inside. It is dispatch's fact-gathering; the verdict logic lives
// entirely in resolve().
func (a *Agent) resolutionInput(tool domain.Tool, call domain.ToolCall, guard security.PreCheck) resolutionInput {
	inFence, escape := a.classifyWriteTarget(tool, call)
	return resolutionInput{
		mode:                   a.effectiveMode(),
		call:                   call,
		tool:                   tool,
		guard:                  guard,
		confineToWorkspace:     a.ConfineToWorkspace(),
		fsConfineAvailable:     a.fsConfinementAvailable(),
		writeTargetInWorkspace: inFence,
		writeEscapeTarget:      escape,
		atDepthBound:           a.depth >= maxSubAgentDepth,
		approverPresent:        a.cfg.Approver != nil,
		box:                    a.confinementBox(),
	}
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

// hookExecutionCtx returns ctx wrapped with the domain.SubprocessPermit a hook may spawn a
// subprocess under, or ctx unchanged when it may not (confinement-execution-contract §10). It is
// the hook-time analogue of resolveLadderAuto's subprocess row — a hook runs outside the per-call
// Resolution, so the ladder's answer reaches it as a context token instead of a verdict:
//
//	| effective mode | confine-to-workspace | fs caps    | installed                          |
//	|----------------|----------------------|------------|------------------------------------|
//	| not Auto       | —                    | —          | nothing (Plan refuses; Ask-Before / |
//	|                |                      |            | Allow-Edits need an Approval a hook |
//	|                |                      |            | cannot open)                        |
//	| Auto           | off                  | —          | permit, nil Confinement (unfenced)  |
//	| Auto           | on                   | available   | permit carrying the workspace box   |
//	| Auto           | on                   | unavailable | nothing (the ladder gates the       |
//	|                |                      |            | subprocess surface here)            |
//
// The mode is read through effectiveMode(), never Mode(), so a sub-agent whose parent has
// tightened mid-delegation loses the permit exactly as it loses the matching tool verdict
// (ADR 0013). The box comes from confinementBox(), the same live-scratch fold resolutionInput
// uses, so a hook-spawned process is fenced identically to a subprocess tool's.
func (a *Agent) hookExecutionCtx(ctx context.Context) context.Context {
	if a.effectiveMode() != domain.ModeAuto {
		return ctx
	}
	if !a.ConfineToWorkspace() {
		return domain.WithSubprocessPermit(ctx, domain.SubprocessPermit{})
	}
	if !a.fsConfinementAvailable() {
		return ctx
	}
	return domain.WithSubprocessPermit(ctx, domain.SubprocessPermit{
		Confinement: &domain.Confinement{
			Confiner: a.cfg.Confiner,
			Box:      a.confinementBox(),
		},
	})
}

// writeEscapeCtx returns ctx carrying the domain.WriteEscapePermit this verdict authorises, or ctx
// unchanged when it authorises none (ADR 0049). It is the write-time analogue of hookExecutionCtx:
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

// executeDelegate drives the sub_agent recursion point (a nested Agent) and records the
// delegation. runSubAgent keeps its own defensive depth check — belt-and-braces with the
// resolver's depth-bound row and the withheld-tool floor (ADR 0013 defence in depth) — so the
// bound holds even if the call is reached by another route.
//
// It is the SERIAL path's delegation, so it brackets the child with the same lifecycle phases the
// pool emits: a delegation that runs alone starts the instant it is reached and finishes with its
// result, exactly as a pooled sibling does.
func (a *Agent) executeDelegate(ctx context.Context, turn int, call domain.ToolCall, verdict resolution) (domain.ToolResult, dispatchOutcome) {
	a.emitSubAgentPhase(turn, call, domain.SubAgentStarted, domain.ToolResult{})
	result, outcome := a.runSubAgent(ctx, call)
	if outcome == dispatchCancelled {
		return result, dispatchCancelled
	}
	a.emitSubAgentPhase(turn, call, domain.SubAgentFinished, result)
	a.recordExecuted(turn, call, verdict.auditDecision, verdict.auditReason, result)
	return result, dispatchDone
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
// delegation carried one, its short name (a.name), because a prompt raised during a fan-out may be
// one of several queued behind each other and "which agent is asking" is otherwise unanswerable
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
		SubAgentName: a.name,
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
	decision, err := a.cfg.Approver.Approve(ctx, areq)
	if err != nil {
		if ctx.Err() != nil {
			return false, dispatchCancelled
		}
		a.cfg.Events.Emit(domain.ErrorEvent{EventBase: a.base(turn), Source: "loop", Err: "approver: " + err.Error()})
		return false, dispatchDone
	}

	a.cfg.Events.Emit(domain.ApprovalEvent{EventBase: a.base(turn), Request: areq, Decision: decision})
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
	// run when a depth-0 fan-out has siblings running at once (ADR 0039).
	ctx = domain.WithSubAgentDepth(ctx, a.depth)
	ctx = domain.WithSpawnCallID(ctx, a.callID)
	// And beside them the Console PRIVILEGE key, which the spawn call id must not double as: this
	// one is minted by the registry that compares it, so no two runs can share it however the
	// model numbers its calls (ADR 0059 §6). Installed unconditionally too — "" is the top-level
	// agent, whose Consoles no delegation's end may reap.
	ctx = domain.WithConsoleOwner(ctx, a.consoleOwner)

	if a.task != "" {
		// Install this Agent's delegated task so a tool that puts a QUESTION to the human can name
		// the agent asking it (domain.AskRequest.SubAgentTask), the way an ApprovalRequest already
		// names it. The Approval path needs no carrier — the loop builds that request itself
		// (approve) — but ask_user builds its own, one interface boundary away from the Agent that
		// knows the task, so the identity rides the call's context (ADR 0039 decision 12).
		// Nothing is installed at depth 0: there, the top-level agent is the only thing that could
		// be asking.
		ctx = domain.WithSubAgentTask(ctx, a.task)
		// The delegation's short name rides beside it, installed even when EMPTY: an unnamed child
		// must report its own namelessness rather than let an outer value stand in for it, and ""
		// is exactly the "fall back to the task" signal the prompt reads.
		ctx = domain.WithSubAgentName(ctx, a.name)
	}

	// Install the undo journal for EVERY call (ADR 0051), the same way the box and the permit
	// reach a tool: the shared write funnel is one os.Root-pinned rule with no engine of its
	// own, so the thing it records into has to ride the execution context. Installing it
	// unconditionally is what makes the coverage boundary the FUNNEL rather than a list kept
	// here — a tool that writes through it is journalled, and a tool that reaches the
	// filesystem some other way (a subprocess, an MCP server, a third-party tool) records
	// nothing precisely because it never asks. A nil journal installs nothing.
	ctx = undo.WithJournal(ctx, a.journal)

	// Install the console registry for EVERY call too (ADR 0059), and for the same reason the
	// journal rides here rather than sitting on a tool: SwapTools rebuilds tool instances
	// mid-session, so a registry held by a console tool would be a set of running processes
	// nothing could reach to close. The engine owns it and the call context carries it. Beside it
	// the dispatch already carries the spawn call id (WithSpawnCallID, above), which is what a
	// console tool stamps on the Consoles it opens — so a delegation's end can close its own.
	ctx = console.WithRegistry(ctx, a.consoles)

	// The floor's own context, taken BEFORE the Confinement handle goes on: apogee's
	// bookkeeping git is not the model's command and must never run inside the call's box. A
	// confined snapshot would pay the re-exec wrapper twice per call — and two extra token
	// label walks per call on Windows (ADR 0020) — for a read that changes nothing, and a
	// backend that could not establish the box would turn the floor's silent skip into the D4
	// demote signal, gating a call on apogee's own bookkeeping. Cancellation still reaches it:
	// floorCtx is the same ctx chain, so a cancelled Turn skips the check, per contract.
	floorCtx := ctx

	if box != nil {
		// Install the Confinement handle so the subprocess tool confines the command it
		// launches. resolve() chose Confine only after confirming caps (§4), so the Confiner is
		// non-nil and fs-confinement-capable here.
		ctx = domain.WithConfinement(ctx, domain.Confinement{
			Confiner: a.cfg.Confiner,
			Box:      *box,
		})
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

// classifyWriteTarget answers BOTH facts a workspace-scoped writer's target decides, from the ONE
// resolution that discovers them (EvalRealPath touches disk — this is the single I/O-tainted fact
// dispatch precomputes for the hermetically pure resolve(), and resolving twice to answer twice
// would invite the two answers to describe different paths):
//
//   - inFence — whether the target lands inside the FENCE the ladder classifies against, which is
//     the workspace root UNION the box's declared writable paths (ADR 0049 Q3). A call with no
//     inspectable target (ok==false) is in-bounds, exactly as before: the Resolution runs it and
//     path-safety bounds it at Execute. A tool that is not a workspace-scoped writer is never
//     in-workspace by this seam.
//   - escapeTarget — the resolved path a permit must name for the write to LAND, set whenever the
//     target is outside the workspace ROOT. That is deliberately wider than !inFence: a writable
//     path outside the workspace is in-fence for the ladder (it gates nothing) and still needs the
//     permit at Execute, because the fence itself keeps one rule — the workspace root, plus
//     whatever single target the context's permit names.
func (a *Agent) classifyWriteTarget(tool domain.Tool, call domain.ToolCall) (inFence bool, escapeTarget string) {
	abs, ok := tools.WorkspaceWriteTarget(tool, call)
	if !ok {
		return true, "" // nothing inspectable to classify ⇒ in-bounds (Execute path-bounds it)
	}
	if pathWithin(abs, a.cfg.WorkspaceDir) {
		return true, ""
	}
	for _, writable := range a.cfg.ConfineWritablePaths {
		if pathWithin(abs, writable) {
			return true, abs
		}
	}
	return false, abs
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
// used to die at this line, leaving a history-scanning Mechanism to guess from the result text —
// which for a successful read IS a file body, error strings and all. Every route committing a
// result gets the marker, so the guess is now only ever a legacy-record fallback.
func (a *Agent) appendToolResult(turn int, result domain.ToolResult) {
	result.Content = a.clampToolResult(result.Content)
	a.conv.Append(domain.Message{
		Role:        domain.RoleTool,
		Content:     result.Content,
		ToolCallID:  result.CallID,
		ToolOutcome: domain.ToolOutcomeOf(result.IsError),
	})
	a.cfg.Events.Emit(domain.ToolResultEvent{EventBase: a.base(turn), Result: result})
}

// structuralFloor is the BOUND behind both structural clamps: the whole History allocation — the
// most any single body could occupy and still leave the conversation renderable. Content past it
// can never survive ANY reducer, so committing it whole buys nothing and can doom the Turn
// outright: the emergency fold's own summary call keeps the most recent message unconditionally
// (renderBudgetedTranscript), so a fresh giant body IS that message and overflows the fold that was
// supposed to rescue the Turn.
//
// With an unknown window (a zero History allocation — Allocate had no basis to allocate) the floor
// measures against compactUnknownWindowTranscriptTokens instead of standing down. Being inert there
// was not the conservative choice it looked like: the fold's transcript render keeps the most recent
// message UNCONDITIONALLY, so an unclamped giant body becomes the one message the emergency fold
// cannot shed and re-wedges the session bounding the fold was meant to un-wedge (audit 2026-08-01,
// follow-up B). Keying the floor to the fold's own unknown-window budget makes the two meet exactly:
// content that survives the clamp is, by construction, content the fold can still render.
//
// The threshold sits deliberately far above the `tool_result_cap` Mechanism's: the whole History
// allocation (~60% of the working room, ~48% of the window at the default reserve), chosen because
// it sits BELOW the emergency fold's own transcript budget at every window an agent can
// realistically run in, which is the property that keeps the fold survivable — while the
// Mechanism's tighter 40%-of-working-room nudge shapes the ordinary case. That ordering is
// arithmetic, not an invariant: the fold budgets its transcript at window - compactMaxTokens -
// compactPromptOverheadTokens (= window - 4608), so the floor stays under it only while
// 0.6*(window - reserve) < window - 4608 — windows above ~8.9k tokens at the default reserve.
// Smaller windows invert the two and lose the property; they sit far under the ~32k target window
// and are too small to run a coding Turn in (ADR 0018 §8 states the same condition).
func (a *Agent) structuralFloor() int {
	if history := a.budget().History; history > 0 {
		return history
	}
	return compactUnknownWindowTranscriptTokens
}

// clampToBound is the RENDERING both structural clamps share: content whose estimated tokens exceed
// bound is replaced by the head/tail-plus-marker elision (context.TruncateToolResult — the same
// shape `tool_result_cap` renders), so the model reads ONE "the middle was dropped, re-read the
// range" idiom whichever seam produced it. Content within bound is returned untouched.
//
// It never GROWS content: a pathological few-very-long-lines body the head/tail form cannot shrink
// is left whole (the same guard tool_result_cap applies).
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
// It is structural, not a Mechanism (ADR 0006's floor): it consults no config, is never disabled
// under Bypass, and self-regulation cannot withdraw it. The `tool_result_cap` Mechanism stays the
// A/B-able tuning valve above it and cannot substitute for it — it is default-off, bypass-disabled,
// withdrawable, and caps only the turns BEFORE the most recent tool call, so the freshly appended
// result (the one that overflows) is exactly the one it never touches.
//
// Unlike the Mechanism, which edits only the projected request, this clamp edits the conversation
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

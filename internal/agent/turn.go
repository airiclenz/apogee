package agent

import (
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// turnLifecycle owns the loop's Turn/Exchange lifecycle state WHOLE — where the loop stands
// between quiescent boundaries (ADR 0007), the input queued for the next Exchange, and the
// per-Exchange latches the loop's structural reducers set and the Exchange's end or replacement
// clears — and the verbs that mutate it; nothing outside this type writes a field of it. The
// collaborator the exits touch is the conversation (rollback, deferred queue). Same-package,
// unexported: the Turn is the loop's concept, not a public seam.
type turnLifecycle struct {
	conv *domain.Conversation

	index         int  // 0-based index of the next Turn (was Agent.turnIndex)
	inExchange    bool // true between Submit and the Step that completes the Exchange
	exchangeStart int  // cached rollback boundary of the open Exchange (ADR 0017 §2's recorded fallback)

	// exchangeTurns counts the Turns COMPLETED in the open Exchange — the quantity the delegate
	// step cap bounds (Agent.stepCap). It is written by Run, the one place the cap is enforced,
	// and reset by openExchange so the bound is per-Exchange rather than per-Agent. It is
	// deliberately not serialized: a snapshot is taken at a quiescent boundary, and a resumed
	// Exchange's cap starts counting again from there.
	exchangeTurns int

	// pendingInput is the user input Submit queued for the next Exchange, consumed by open() on
	// the Turn that opens it. Non-nil only between a Submit and the Step that consumes it: submit
	// refuses a second input while one is queued or an Exchange is open, so a continuation Turn
	// never finds one. Serialized (snapshot/restore): a session snapshotted between Submit and
	// Step resumes with its input still queued.
	pendingInput *domain.UserInput

	// wrapUp latches the ONE closing Turn a delegate stopped at its step cap is given (capped;
	// Agent.finishAtStepCap, subagent.go's wrapUpDirectiveFormat). While it is set, three seams
	// change together and only for the request they compose: toolMenu returns no tools, buildRequest
	// stamps the directive that says why they are gone and what to write instead, and step() takes
	// the final-answer exit even if the reply asks for a tool anyway (loop.go). The Turn is
	// tool-less with ONE exception: write_file, once (wrapUpWriter), so a capped child can still
	// save its report or partial output — and a delegation spawned with an `output_path` keeps it
	// for exactly that file (Agent.outputPath), so the output it was asked for still lands; every
	// other call is still dropped, and with a path named a write elsewhere is refused (resolve,
	// resolution.go). It is latched for exactly ONE request and released before the
	// capped Exchange returns, so it never outlives the Exchange that raised it. Transient like
	// exchangeTurns: neither configured nor serialized, because a resumed session resumes at a
	// boundary, never mid-wrap-up. Structural (ADR 0006), not a Reaction: no config key, and it
	// holds under Bypass.
	wrapUp bool

	// compactSat is the saturation latch (S2): a prior automatic fold could not bring the history
	// under its allocation (an oversized protected prefix), so further automatic folds stand down
	// until the estimate drops back under it (foldSaturated / autoFoldArmed, compact.go).
	compactSat bool

	// compactFailed is the stand-down latch: an automatic fold FAULTED, so the estimate-driven
	// trigger stands down for the rest of THIS Exchange rather than re-running the identical
	// failing summary call at every Turn boundary (foldFaulted, compact.go). openExchange clears
	// it; the emergency fold and the on-demand /compact ignore it.
	compactFailed bool

	// fillRung is the context-fill notice's ladder position (fillnotice.go): the highest rung fired
	// on the current climb toward the compaction line, 0 = none. Every path that shrinks or
	// replaces the conversation behind the model's back — a fold, /clear, a restored snapshot, an
	// aborted Exchange, a cancelled Turn's rollback — re-arms the whole ladder (rearmFill), so the
	// next climb fires from its first reached rung (ADR 0077 D4).
	fillRung int

	// lastFault is the text of the most recent loop-level fault this Agent surfaced as an
	// ErrorEvent — the very sentence the human already read (noteFault; Agent.emitLoopFault). It
	// exists for the PARENT of a delegation: runSubAgent turns a faulted child Exchange into an
	// error tool result, and without this the result could only point at "the preceding error",
	// which the parent MODEL never sees. A fault ends the Exchange, so the last one recorded is
	// always the one that abandoned it; an Exchange abandoned with no ErrorEvent at all (a recovered
	// hook panic) leaves this empty and the caller falls back to wording that names no cause.
	// Written on the Agent's own loop goroutine, read by the parent only after Run has returned.
	lastFault string

	// observer is told the two MOMENTS this type owns that mean something outside it: an Exchange
	// ENDING (exchangeClosed, fired by closeExchange) and a cancelled Turn's ROLLBACK
	// (turnRolledBack, fired by end()'s endCancelled row). It is an interface rather than an Agent
	// for the same reason conv is a pointer: this type owns the moments and knows nothing of what
	// an Agent wants to do about them — the undo journal's closing capture hangs off the first
	// (Agent.closeUndoGroup, agent.go), the context-fill ladder's re-arm and the step-budget
	// note's latch off the second (Agent.rearmNotices, stepnotice.go) — and neither fire site carries a context
	// or an Agent to hand one. nil is inert, never an error — a bare lifecycle in a unit test has
	// no Agent behind it, and an engine that records nothing simply hangs nothing here.
	observer exchangeObserver
}

// exchangeObserver is what turnLifecycle notifies at the two moments it owns that reach past it
// (turnLifecycle.observer); the Agent is its one implementation (construct.go).
//
// exchangeClosed fires on every row that ends an Exchange and on none that leaves one open, which
// is exactly closeExchange's own contract: endCancelled is not a caller, so a Turn that will be
// re-attempted never closes the group its re-attempt writes into.
//
// turnRolledBack fires once per Turn ROLLBACK, after the conversation is dropped back to the
// Turn's boundary. The rollback drops the Turn's committed tool results, including the one a
// notice rode on, so what tracks against the dropped conversation ends here exactly as it does
// on AbortExchange. A Step-driven host may re-attempt the Turn and cancel again, so an
// implementation must be idempotent (the re-arm is).
type exchangeObserver interface {
	exchangeClosed()
	turnRolledBack()
}

// turnRun is the working state of one Turn attempt — the values step() used to thread as five
// positional locals (and cancelTurn took as five positional parameters). It is created at the
// top of step() and dies at the Turn's exit; nothing in it is serialized.
type turnRun struct {
	turn          int             // this Turn's index (StepResult.TurnIndex)
	start         time.Time       // Turn start (StepResult.Elapsed)
	rollback      int             // conversation boundary a cancel restores to
	req           *domain.Request // the request under attempt this Turn
	deferred      []string        // corrections buildRequest drained; a cancel re-queues them
	deferredFloor int             // queue length after the drain — the cancel truncation floor (F6)

	// foldSpent is the one-fold-per-Turn latch: it flips once this Turn has folded its history and
	// re-sent a request the model's context window rejected, and the predictive and reactive
	// overflow paths share it (refold latches it on a fold that ran; the respond phase gives up
	// once it is set). A fold is a lossy rewrite of the user's history, so a second overflow means
	// folding is not the answer here — the protected prefix alone is over the window, or the
	// server rejects even a minimal prompt — and the Turn gives up exactly as it did before
	// recovery existed: the same sanitized ErrorEvent, the same abandoned Exchange.
	foldSpent bool

	// restreamSpent is the one-re-stream-per-Turn latch: it flips once this Turn has re-sent its
	// request after a TRANSIENT Upstream fault — one whose class the provider would have retried
	// at the HTTP layer (429, 5xx, an aggregator's provider_unavailable) but which arrived in-band
	// on a 200 mid-stream, past every retry the client could make. One re-send costs a momentary
	// blip a stutter instead of the whole exchange; a second fault of any class is not a blip, so
	// it surfaces exactly as it did before the re-stream existed. The latch is deliberately its own
	// budget: maxPostResponseRetries bounds a Reaction-driven Outcome{Retry} and foldSpent bounds the
	// overflow fold — different remedies for different failures, none of them spending another's.
	restreamSpent bool
}

// turnEnd names the five ways a Turn exits. One row per exit; end() is the whole table.
type turnEnd int

const (
	endTurnDone     turnEnd = iota // advance · Exchange stays open   · StatusTurnComplete
	endExchangeDone                // advance · Exchange closes       · StatusExchangeComplete
	endAbandoned                   // advance · Exchange closes       · StatusExchangeComplete + Faulted
	endCancelled                   // roll back + restore deferred · no advance · Exchange stays open · StatusCancelled
	endStepCapped                  // step-cap fallback · no advance · Exchange closes · StatusExchangeComplete + StepCapped
)

// end exits the Turn t on the row how names — the single table that replaced the three exit
// helpers (completeTurn / abandonTurn / cancelTurn). Each dimension is expressed once: whether
// the Exchange closes, whether the Turn counter advances, and whether the Turn FAULTED. It
// returns the boundary StepResult (ADR 0007).
func (l *turnLifecycle) end(t *turnRun, how turnEnd) domain.StepResult {
	var status domain.StepStatus
	var faulted bool
	var stepCapped bool
	switch how {
	case endTurnDone, endExchangeDone:
		// A tool-call Turn (endTurnDone) leaves the Exchange OPEN — StatusTurnComplete, the next
		// Step calls the Upstream again with the tool results in context. A final no-tool reply
		// (endExchangeDone) ends it — StatusExchangeComplete, awaiting the next Submit.
		if how == endExchangeDone {
			// In practice the deferred queue is already empty here — a no-tool final answer ends
			// the Exchange and F2 never re-defers there — so closeExchange's clear is the F6 backstop.
			l.closeExchange()
			status = domain.StatusExchangeComplete
		} else {
			status = domain.StatusTurnComplete
		}
		l.index++
	case endAbandoned:
		// A faulted Turn (an Upstream fault or a recovered reaction panic) produced no usable
		// outcome. The Exchange ends (there is nothing to continue from) and the counter advances
		// so resume does not re-run the failed Turn.
		// A deferral is a decision about the SAME flow's next request — closeExchange expires it
		// with the faulted Exchange (F6), so any correction drained this Turn dies here.
		l.closeExchange()
		l.index++
		// The Exchange closed with NOTHING to show for it, yet the status it closes on is the
		// same StatusExchangeComplete a real final answer returns (a closed Exchange is a closed
		// Exchange, and a resuming host must treat both the same). Mark the result faulted so a
		// reader that reports the Exchange's OUTCOME onward — the sub-agent orchestrator
		// answering its parent — can tell a fault from an answer instead of passing off a
		// placeholder, or stale mid-task text, as the delegated result.
		status = domain.StatusExchangeComplete
		faulted = true
	case endCancelled:
		// The Turn is rolled back and re-attempted on resume.
		// Roll the conversation back to the boundary the Turn began at (dropping this Turn's
		// assistant message and any tool results). Truncate the queue back to its pre-hooks floor
		// before restoring: the cancelled Turn's own post-response deferrals (e.g. a shrunken
		// directive built from a delegation that is now rolled back) die with the Turn, so
		// restoreDeferred re-queues the drained injections exactly once and a re-attempt or
		// snapshot never carries two contradictory directives (F6).
		l.conv.DropRange(t.rollback, l.conv.Len())
		l.conv.TruncateDeferred(t.deferredFloor)
		l.restoreDeferred(t.deferred)
		// The dropped tool results may include the one a context-fill notice rode on: let the
		// Agent end the ladder's climb (observer.turnRolledBack → rearmNotices), as abort does.
		if l.observer != nil {
			l.observer.turnRolledBack()
		}
		// inExchange is deliberately left untouched (NOT cleared) and the counter is NOT advanced,
		// so the snapshot taken here resumes and re-attempts the Turn from serializable state: a
		// cancelled Turn does not END the Exchange — the user input / tool results committed so far
		// are still mid-flight — so the flag must keep reflecting an open Exchange. On resume that
		// makes the next Step re-attempt the Turn and, crucially, makes Submit reject a new user
		// message that would otherwise interleave into the open Exchange (two consecutive user
		// messages, or a user message wedged after a tool result — both of which a strict chat
		// template rejects). Clearing it here contradicted the un-advanced index (which says
		// "re-attempt"), opening that exact hole.
		status = domain.StatusCancelled
	case endStepCapped:
		// The delegate step cap's FALLBACK exit. The cap no longer ends the Exchange on this row in
		// the ordinary case: finishAtStepCap (agent.go) spends one further tool-less Turn (bar
		// write_file to a spawn-named `output_path`) on the
		// child's closing report, and THAT Turn ends through endExchangeDone — counter
		// advanced, Exchange closed — so a capped child now ends at cap+1 Turns and the boundary
		// the parent reads is the wrap-up's own with StepCapped forced on. This row is what is
		// left for the wrap-up that produced no boundary of its own: a loop-level error that gave
		// up without closing the Exchange. Nothing completed here, so there is nothing to advance
		// past — the counter is left alone so the index keeps naming the next Turn rather than one
		// beyond it, an off-by-one a Snapshot stores and a resume reads back (state.go). What is left is the Exchange half: close it (F6 — the
		// deferred queue dies with it) and report the same StatusExchangeComplete a real final
		// answer does. NOT Faulted: nothing failed — the work up to the cap stands and the parent
		// receives it — so StepCapped is the flag that tells a capped Exchange from a finished one.
		l.closeExchange()
		status = domain.StatusExchangeComplete
		stepCapped = true
	}
	return domain.StepResult{
		Status:     status,
		TurnIndex:  t.turn,
		Elapsed:    time.Since(t.start),
		Faulted:    faulted,
		StepCapped: stepCapped,
	}
}

// closeExchange ends the open Exchange — the ONE engine-side owner of Exchange end (ADR 0017
// §3). It flips inExchange (re-opening Submit) and clears the deferred Response-Action queue,
// owning the F6 invariant: a deferral dies with its Exchange — a directive deferred for this
// flow's next request must never ride into a different Exchange's. Its callers are the three
// Exchange ends: end()'s endExchangeDone row (a final no-tool reply), its endAbandoned row (a
// faulted Turn), and AbortExchange (the host scrapping the Exchange). endCancelled is
// deliberately NOT one — a cancelled Turn leaves the Exchange open for the resume re-attempt
// and truncates-then-restores the deferred queue instead (F6(b)). Being the one owner is what
// lets the undo journal hang its closing capture here through the observer: four Exchange ends, one
// capture point, and the row that does not end an Exchange does not take an image either.
func (l *turnLifecycle) closeExchange() {
	l.inExchange = false
	l.conv.ClearDeferred()
	// And the one thing an Exchange's END means outside this type: the undo journal's closing
	// capture (ADR 0074 decision 3). It runs AFTER the state flips so an observer reached from it
	// sees a closed Exchange, and it is the last word here for the same reason — whatever it does,
	// the Exchange is already over.
	if l.observer != nil {
		l.observer.exchangeClosed()
	}
}

// restoreDeferred re-queues deferred corrections drained by buildRequest when the Turn did not
// commit (cancelled), so a best-effort correction is consumed only when a request is actually
// sent and processed to a committed boundary — never silently lost.
func (l *turnLifecycle) restoreDeferred(deferred []string) {
	for _, inject := range deferred {
		l.conv.Defer(inject)
	}
}

// openExchange marks the boundary a new Exchange opens at — the conversation length BEFORE its
// first user message is appended — and flips inExchange, so AbortExchange can roll a cancelled
// Exchange all the way back to a clean, submittable boundary. It is called once per Exchange:
// pendingInput is non-nil only on the opening Turn (Submit is refused mid-Exchange), so a
// continuation Turn never resets the boundary. The boundary is a CACHED value (ADR 0017 §2's
// recorded fallback) precisely because a later mid-Exchange fold can drop the opening user
// message, leaving nothing to re-derive it from — readers go through Agent.exchangeBoundary.
func (l *turnLifecycle) openExchange() {
	l.exchangeStart = l.conv.Len()
	l.inExchange = true
	// A new Exchange is a new step-cap budget: the cap bounds the Turns of ONE Exchange, so the
	// count starts over here rather than accumulating across a delegation's life (Agent.Run).
	l.exchangeTurns = 0
	// And a new automatic-fold budget, for the same reason: a fold that faulted stood the
	// estimate-driven trigger down for the Exchange it faulted in (compactFailed, compact.go), not
	// forever. The main agent therefore re-arms at every opening; a CHILD never reaches here a
	// second time — its whole life is one Exchange — so its stand-down lasts the delegation, which
	// is the retry runaway this closes.
	l.compactFailed = false
}

// submit queues in as the input the next Step opens an Exchange with (Agent.Submit's engine
// half). Submitting while an input is already queued or an Exchange is open is refused with
// domain.ErrInputPending: a second user message would interleave into the open Exchange (two
// consecutive user messages, or one wedged after a tool result — both of which a strict chat
// template rejects).
func (l *turnLifecycle) submit(in domain.UserInput) error {
	if l.pendingInput != nil || l.inExchange {
		return domain.ErrInputPending
	}
	l.pendingInput = &in
	return nil
}

// open consumes the queued input and opens the Exchange it begins (openExchange), returning the
// input for step() to append as the Exchange's first user message. It returns nil — and opens
// nothing — when no input is queued: a continuation Turn of an open Exchange, whose boundary must
// not be reset. The input is taken BEFORE the boundary flips, so a reader reached from the
// opening never sees an input queued behind an open Exchange.
func (l *turnLifecycle) open() *domain.UserInput {
	in := l.pendingInput
	if in == nil {
		return nil
	}
	l.pendingInput = nil
	l.openExchange()
	return in
}

// abort scraps the open Exchange (Agent.AbortExchange's engine half): it rolls the conversation
// back to the boundary the Exchange began at — dropping the un-answered user message and any tool
// Turns committed so far — re-arms the context-fill ladder, closes the Exchange and drops any
// input queued behind it. No-op when no Exchange is open.
//
// The ladder re-arms because the tool results the scrapped Exchange committed go with it —
// including the one that carried a context-fill notice — so the climb ends here as it does after
// a fold: the next result at or above a rung must be told again, not left silent until the rung
// above. closeExchange runs after the rollback so it expires any deferred Response Action with
// the Exchange (F6) — a mid-fan-out abort must not leave a stale remaining-items directive queued
// for the next Exchange's request.
func (l *turnLifecycle) abort() {
	if !l.inExchange {
		return
	}
	l.conv.DropRange(l.exchangeStart, l.conv.Len())
	l.rearmFill()
	l.closeExchange()
	l.pendingInput = nil
}

// capped latches the wrap-up Turn (wrapUp) a delegate stopped at its step cap is given and returns
// the func that releases it — `defer l.capped()()` brackets exactly the one step() the latch is
// for (Agent.finishAtStepCap), so it never outlives the capped Exchange.
func (l *turnLifecycle) capped() (release func()) {
	l.wrapUp = true
	return func() { l.wrapUp = false }
}

// wrappingUp reports whether the current Turn is the capped wrap-up Turn (capped) — the read the
// tool menu, the request builder, the reply-salvage and step()'s exits share.
func (l *turnLifecycle) wrappingUp() bool { return l.wrapUp }

// noteFault records the text of a loop-level fault as the reason the Exchange it ends was
// ABANDONED (lastFault) — read back by fault for the parent of a faulted delegation, and by
// Agent.LastFault for a Driver.
func (l *turnLifecycle) noteFault(text string) { l.lastFault = text }

// fault returns the text of the most recent loop-level fault, or "" when none has been surfaced.
func (l *turnLifecycle) fault() string { return l.lastFault }

// foldFaulted latches the automatic-fold stand-down (compactFailed): a fold FAULTED against this
// Exchange's history, and retrying the identical summary call at every Turn boundary is the
// 2026-08-29 runaway. openExchange clears it.
func (l *turnLifecycle) foldFaulted() { l.compactFailed = true }

// foldSaturated latches the saturation stand-down (compactSat): a fold ran and could not bring the
// history under its allocation, so growth alone must not re-trigger one. autoFoldArmed clears it
// once the estimate drops back under the allocation.
func (l *turnLifecycle) foldSaturated() { l.compactSat = true }

// autoFoldArmed is the latch half of the automatic-fold trigger (Agent.shouldAutoCompact): whether
// the two stand-down latches let an estimate-driven fold run, given exceedsAllocation, the
// history-versus-allocation compare. The stand-down (compactFailed) is consulted BEFORE the compare
// deliberately: the compare is also where compactSat clears, and a stand-down must not double as a
// reason to leave that saturation latch stale. Under the allocation the saturation latch clears —
// a later overflow may fold afresh — and nothing fires; over it, a fold fires unless a prior fold
// already proved it cannot help (compactSat — an oversized protected prefix). Only dropping back
// under the allocation re-arms a saturated trigger.
func (l *turnLifecycle) autoFoldArmed(exceedsAllocation func() bool) bool {
	if l.compactFailed {
		return false
	}
	if !exceedsAllocation() {
		l.compactSat = false
		return false
	}
	return !l.compactSat
}

// resetFoldLatches clears both fold latches together (Rebind, SwitchUpstream): each recorded a
// verdict on a fold against the server and model just departed — that it faulted, or that it
// could not bring the history under THAT window's allocation — which says nothing about the pair
// now bound. The Exchange-scoped clear (openExchange) would reach the stand-down at the next
// Exchange anyway — a rebind is a quiescent boundary — so this keeps the two latches moving together.
func (l *turnLifecycle) resetFoldLatches() {
	l.compactSat = false
	l.compactFailed = false
}

// noteFill records the rung a context-fill reading reached on the current climb (fillRung) and
// reports whether the notice fires: only a NEW high does. A reading UNDER a fired rung is the
// estimate moving — a usage report recalibrated the chars→token ratio — not a shorter history
// (every path that shrinks the conversation re-arms the whole ladder through rearmFill), so the
// rungs the reading fell under re-arm silently: the model was told the higher figure already, and
// a "50" at 74% would only repeat it lower. A reading AT the fired rung is the same climb and fires
// nothing.
func (l *turnLifecycle) noteFill(reached int) (fires bool) {
	fires = reached > l.fillRung
	l.fillRung = reached
	return fires
}

// rearmFill ends the current context-fill climb: every rung is armed again, so the next result the
// notice measures fires whichever rung its fill reaches, as the first result of a session does.
// Idempotent — a Step-driven host may roll the same Turn back twice.
func (l *turnLifecycle) rearmFill() { l.fillRung = 0 }

// turnSnapshot is the serializable half of the lifecycle — the fields a Session snapshot carries
// (state.go's agentState) and restore puts back whole. The latches (wrapUp, compactSat,
// compactFailed, fillRung, lastFault) and exchangeTurns are deliberately absent: a snapshot is
// taken at a quiescent boundary, and what they recorded belongs to the conversation the snapshot
// replaces.
type turnSnapshot struct {
	index         int
	inExchange    bool
	exchangeStart int
	pendingInput  *domain.UserInput
}

// snapshot returns the serializable half of the lifecycle for Agent.encodeState.
func (l *turnLifecycle) snapshot() turnSnapshot {
	return turnSnapshot{
		index:         l.index,
		inExchange:    l.inExchange,
		exchangeStart: l.exchangeStart,
		pendingInput:  l.pendingInput,
	}
}

// restore puts a snapshot's lifecycle state back whole (Agent.restoreState — Resume onto a fresh
// Agent and RestoreSession onto a live one) and clears the latches that judged the conversation
// the snapshot replaces: a fold that faulted or saturated against the outgoing history says
// nothing about the incoming one, and the context-fill ladder climbed the conversation just
// swapped out, not this one. RestoreSession left the two fold latches standing before this verb
// owned the reset, so a session restored over a stood-down Agent stayed stood down.
func (l *turnLifecycle) restore(s turnSnapshot) {
	l.index = s.index
	l.inExchange = s.inExchange
	l.exchangeStart = s.exchangeStart
	l.pendingInput = s.pendingInput
	l.compactSat = false
	l.compactFailed = false
	l.rearmFill()
}

// reanchorAfterShrink repairs the cached Exchange boundary (S2) after a mid-Exchange history
// rewrite (a lab `HistoryRewriter`) dropped `dropped` messages, shifting the current Exchange's messages
// down: it shifts exchangeStart down by the delta, clamped to [conv.PrefixEnd()+1, conv.Len()], so
// AbortExchange still rolls back to this Exchange's boundary rather than over-dropping into the
// protected prefix or leaving orphaned tool results. The floor is just past the protected prefix +
// gap note (PrefixEnd()+1): after a truncation everything from there to Len is current-Exchange
// tail, so exchangeStart validly sits anywhere in that span. Only a shrink is repaired — a grow (no
// registered rewrite does this) would mis-shift, and on an Exchange-opening Turn a zero-drop clamp
// could wrongly push exchangeStart past the just-appended user message — so a non-positive `dropped`
// or an out-of-Exchange rewrite is a no-op. The cache + this repair are deliberate (ADR 0017 §2's
// recorded fallback): this very rewrite can drop the open Exchange's opening user message, so the
// boundary cannot be re-derived from the conversation — readers go through Agent.exchangeBoundary.
func (l *turnLifecycle) reanchorAfterShrink(dropped int) {
	if dropped <= 0 || !l.inExchange {
		return
	}
	l.exchangeStart = min(max(l.exchangeStart-dropped, l.conv.PrefixEnd()+1), l.conv.Len())
}

// anchorAtBridge re-anchors the cached Exchange boundary to the just-appended overflow bridge after
// a mid-Exchange emergency fold (ADR 0018), so AbortExchange rolls back to the folded prefix +
// summary rather than into the protected prefix. That repair is required, not optional: the boundary
// is a CACHED value (ADR 0017 §2's recorded fallback) precisely because a rewrite like the fold can
// drop the Exchange's opening user message, leaving nothing to re-derive it from. It mirrors
// reanchorAfterShrink, the S2 repair step() performs after a mid-Exchange history-rewrite shrink.
// No-op outside an Exchange.
func (l *turnLifecycle) anchorAtBridge() {
	if l.inExchange {
		l.exchangeStart = l.conv.Len() - 1
	}
}

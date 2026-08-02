package agent

import (
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// turnLifecycle owns the loop's Turn/Exchange lifecycle state — where the loop stands
// between quiescent boundaries (ADR 0007) — and the exits that mutate it. It coordinates the
// two collaborators the exits touch together: the conversation (rollback, deferred queue) and
// the self-regulator (judge vs discard). Same-package, unexported: the Turn is the loop's
// concept, not a public seam.
type turnLifecycle struct {
	conv    *domain.Conversation
	tracker *selfRegulator

	index         int  // 0-based index of the next Turn (was Agent.turnIndex)
	inExchange    bool // true between Submit and the Step that completes the Exchange
	exchangeStart int  // cached rollback boundary of the open Exchange (ADR 0017 §2's recorded fallback)
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
}

// turnEnd names the four ways a Turn exits. One row per exit; end() is the whole table.
type turnEnd int

const (
	endTurnDone     turnEnd = iota // judged · advance · Exchange stays open   · StatusTurnComplete
	endExchangeDone                // judged · advance · Exchange closes       · StatusExchangeComplete
	endAbandoned                   // discarded · advance · Exchange closes    · StatusExchangeComplete + Faulted
	endCancelled                   // discarded · roll back + restore deferred · no advance · Exchange stays open · StatusCancelled
)

// end exits the Turn t on the row how names — the single table that replaced the three exit
// helpers (completeTurn / abandonTurn / cancelTurn). Each dimension is expressed once: judge
// (tracker.endTurn) vs discard (tracker.discardTurn), whether the Exchange closes, whether
// the Turn counter advances, and whether the Turn FAULTED. It returns the boundary StepResult
// (ADR 0007).
func (l *turnLifecycle) end(t *turnRun, how turnEnd) domain.StepResult {
	var status domain.StepStatus
	var faulted bool
	switch how {
	case endTurnDone, endExchangeDone:
		// Resolve the completed Turn for self-regulation (R3, next-Turn judgment): this Turn's
		// outcome judges the PREVIOUS Turn's fires — striking, freezing, or clearing — and this
		// Turn's fires shift into the pending set the next Turn's outcome will judge. A tool-call
		// Turn (endTurnDone) leaves the Exchange OPEN — StatusTurnComplete, the next Step calls the
		// Upstream again with the tool results in context. A final no-tool reply (endExchangeDone)
		// ends it — StatusExchangeComplete, awaiting the next Submit.
		l.tracker.endTurn()
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
		// A faulted Turn (an Upstream fault or a recovered hook panic) produced no usable outcome,
		// so self-regulation discards it WITHOUT judging — an infra fault neither strikes a
		// Mechanism nor advances the Turn Budget, and this Turn's fires do not bleed into the next
		// Turn's judgment. The pending set (the previous Turn's fires) stays in place for the next
		// completed Turn to judge (R3). The Exchange ends (there is nothing to continue from) and
		// the counter advances so resume does not re-run the failed Turn.
		l.tracker.discardTurn()
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
		// The Turn is rolled back and re-attempted on resume, so self-regulation discards it
		// WITHOUT judging — the re-attempt repopulates the fired-this-Turn set and the proxy
		// signals from scratch. The discard also rolls this Turn's novel-read keys back out of
		// seenReads, so the mandated re-attempt regains its novelty credit; the pending set (the
		// previous Turn's fires) stays in place for the re-attempt's outcome to judge (R3).
		l.tracker.discardTurn()
		// Roll the conversation back to the boundary the Turn began at (dropping this Turn's
		// assistant message and any tool results). Truncate the queue back to its pre-hooks floor
		// before restoring: the cancelled Turn's own post-response deferrals (e.g. a shrunken
		// directive built from a delegation that is now rolled back) die with the Turn, so
		// restoreDeferred re-queues the drained injections exactly once and a re-attempt or
		// snapshot never carries two contradictory directives (F6).
		l.conv.DropRange(t.rollback, l.conv.Len())
		l.conv.TruncateDeferred(t.deferredFloor)
		l.restoreDeferred(t.deferred)
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
	}
	return domain.StepResult{Status: status, TurnIndex: t.turn, Elapsed: time.Since(t.start), Faulted: faulted}
}

// closeExchange ends the open Exchange — the ONE engine-side owner of Exchange end (ADR 0017
// §3). It flips inExchange (re-opening Submit) and clears the deferred Response-Action queue,
// owning the F6 invariant: a deferral dies with its Exchange — a directive deferred for this
// flow's next request must never ride into a different Exchange's. Its callers are the three
// Exchange ends: end()'s endExchangeDone row (a final no-tool reply), its endAbandoned row (a
// faulted Turn), and AbortExchange (the host scrapping the Exchange). endCancelled is
// deliberately NOT one — a cancelled Turn leaves the Exchange open for the resume re-attempt
// and truncates-then-restores the deferred queue instead (F6(b)).
func (l *turnLifecycle) closeExchange() {
	l.inExchange = false
	l.conv.ClearDeferred()
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
}

// reanchorAfterShrink repairs the cached Exchange boundary (S2) after a mid-Exchange history
// rewrite (truncate_history) dropped `dropped` messages, shifting the current Exchange's messages
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
// reanchorAfterShrink, the S2 repair step() performs after a mid-Exchange truncate_history shrink.
// No-op outside an Exchange.
func (l *turnLifecycle) anchorAtBridge() {
	if l.inExchange {
		l.exchangeStart = l.conv.Len() - 1
	}
}

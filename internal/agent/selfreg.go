package agent

import "github.com/airiclenz/apogee/internal/domain"

// Self-regulation (CONTEXT: Self-regulation; plan item 3) is the per-Session safety net that
// keeps a Mechanism from hurting the model — the proxy-signal heuristic half of the hard
// constraint, deliberately WEAKER than the bench's A/B gate (decision 12). It is entirely
// per-Session and resets on Resume: newAgent builds a fresh selfRegulator and restoreState does
// not restore it (agentState carries no tracker), so a resumed Session starts with a clean slate.
// That is the accepted v1 posture — fresh suppression state can only cause a withdrawn Mechanism
// to be re-tried, never to be wrongly withheld, so the failure mode is benign (the tracker is not
// serialized, so it cannot drift out of sync with a snapshot).
//
// Two withdrawal rules ride on one input — whether each Turn was PRODUCTIVE (a new file read or a
// file written, CONTEXT: Turn Budget). Adaptive Suppression is per-Mechanism: a Mechanism judged
// not-helpful adaptiveSuppressStrikes consecutive Turns is withdrawn for the rest of the Session,
// with a clear-path that re-opens it on a productive Turn. The Turn Budget is global: after
// turnBudgetLimit consecutive non-productive Turns every non-exempt Mechanism is withdrawn until
// productive activity resumes. An exempt (off-ramp) Mechanism bypasses both — suppressing it would
// leave a failed Turn with no way out (CONTEXT: Off-ramp).

const (
	// adaptiveSuppressStrikes is how many consecutive non-productive Turns a Mechanism may fire
	// through before Adaptive Suppression withdraws it for the rest of the Session — apogee-sim's
	// shipped default (docs/design/mechanism-catalogue.md §"Self-regulation constants").
	adaptiveSuppressStrikes = 3
	// turnBudgetLimit is how many consecutive non-productive Turns trip the global Turn Budget,
	// withdrawing every non-exempt Mechanism until productive activity resumes — apogee-sim's
	// shipped default (docs/design/mechanism-catalogue.md §"Self-regulation constants").
	turnBudgetLimit = 8
)

// selfRegulator holds the per-Session effectiveness tracking, Adaptive Suppression, and Turn
// Budget state (plan item 3). One Agent owns one selfRegulator; it is driven from the single
// Step goroutine, so it needs no locking.
type selfRegulator struct {
	// fireCounts is the lifetime fire count per Mechanism this Session — the ledger
	// LoopView.Fired answers from. It is shared BY REFERENCE into each Request's view
	// (buildRequest / loopView pass it to domain.NewRequest), so a Mechanism querying Fired sees
	// a peer's fire from earlier in the same hook pass (the decompose↔read_loop coupling seam,
	// D2). It is only read through the view; only recordFire writes it.
	fireCounts map[domain.MechanismID]int

	// firedThisTurn is the set of Mechanisms that fired in the Turn currently in flight. The
	// Turn's productivity judges exactly this set at the quiescent boundary, then it is cleared.
	firedThisTurn map[domain.MechanismID]bool

	// strikes counts a Mechanism's consecutive not-helped fires; suppressed records the ones
	// Adaptive Suppression has withdrawn for the Session. A productive Turn clears both.
	strikes    map[domain.MechanismID]int
	suppressed map[domain.MechanismID]bool

	// seenReads keys the read-only tool calls already made this Session (tool name + arguments),
	// so a re-read of the same target is NOT counted as a new-file-read productivity signal — the
	// wasted-read shape a read-loop produces (grounded on apogee-sim's identical-args signal).
	seenReads map[string]bool

	// nonProductiveStreak counts consecutive non-productive Turns; budgetTripped is the global
	// Turn-Budget withdrawal it raises. A productive Turn resets both.
	nonProductiveStreak int
	budgetTripped       bool

	// productive records whether the Turn in flight has shown a productivity signal (a novel
	// read-only call, or any successful mutating call). Reset each Turn by endTurn/discardTurn.
	productive bool
}

// newSelfRegulator builds an empty per-Session tracker. newAgent calls it, so a fresh Agent —
// including one rebuilt by Resume — starts with clean self-regulation state.
func newSelfRegulator() *selfRegulator {
	return &selfRegulator{
		fireCounts:    make(map[domain.MechanismID]int),
		firedThisTurn: make(map[domain.MechanismID]bool),
		strikes:       make(map[domain.MechanismID]int),
		suppressed:    make(map[domain.MechanismID]bool),
		seenReads:     make(map[string]bool),
	}
}

// recordFire books one successful fire: it bumps the Session ledger LoopView.Fired reads and
// marks the Mechanism as fired this Turn, so the Turn's productivity judges it. It is called for
// every dispatched hook, catalogued or experimental — an experimental hook's synthetic ID accrues
// counts harmlessly (it is never consulted for suppression).
func (r *selfRegulator) recordFire(id domain.MechanismID) {
	r.fireCounts[id]++
	r.firedThisTurn[id] = true
}

// suppress reports whether self-regulation has withdrawn m from this Turn's dispatch: a non-exempt
// Mechanism struck out by Adaptive Suppression, or ANY non-exempt Mechanism while the global Turn
// Budget is tripped. An exempt (off-ramp) Mechanism is never withdrawn — SuppressionPolicy ==
// exempt bypasses both rules (CONTEXT: Off-ramp).
func (r *selfRegulator) suppress(m domain.Mechanism) bool {
	if m.Descriptor().Suppression == domain.SuppressExempt {
		return false
	}
	return r.budgetTripped || r.suppressed[m.Descriptor().ID]
}

// noteRead books a successful read-only tool call as a productivity signal when its target is
// novel this Session (a NEW file read). A repeat read of the same tool + arguments is the
// wasted-read shape and contributes nothing.
func (r *selfRegulator) noteRead(call domain.ToolCall) {
	key := string(call.Tool) + "\x00" + string(call.Arguments)
	if r.seenReads[key] {
		return
	}
	r.seenReads[key] = true
	r.productive = true
}

// noteWrite books a successful mutating tool call as a productivity signal (a file written / an
// action taken).
func (r *selfRegulator) noteWrite() { r.productive = true }

// endTurn resolves the Turn in flight against its productivity, advances the self-regulation
// state, then clears the per-Turn scratch. A productive Turn (a new read or a write) is the
// clear-path: it zeroes every strike, re-opens every suppressed Mechanism, and lifts the Turn
// Budget. A non-productive Turn strikes each Mechanism that fired — withdrawing one at the strike
// limit — and advances the Turn-Budget streak, tripping it at the limit.
func (r *selfRegulator) endTurn() {
	if r.productive {
		r.strikes = make(map[domain.MechanismID]int)
		r.suppressed = make(map[domain.MechanismID]bool)
		r.nonProductiveStreak = 0
		r.budgetTripped = false
	} else {
		for id := range r.firedThisTurn {
			r.strikes[id]++
			if r.strikes[id] >= adaptiveSuppressStrikes {
				r.suppressed[id] = true
			}
		}
		r.nonProductiveStreak++
		if r.nonProductiveStreak >= turnBudgetLimit {
			r.budgetTripped = true
		}
	}
	r.discardTurn()
}

// discardTurn clears the per-Turn scratch (the fired-this-Turn set and the productivity flag)
// WITHOUT judging — the path a cancelled or abandoned Turn takes, so a rolled-back or faulted Turn
// neither strikes a Mechanism nor advances the Turn Budget, and its fires do not bleed into the
// next Turn's judgment. The Session ledgers (fireCounts, seenReads, strikes, suppression) are
// untouched.
func (r *selfRegulator) discardTurn() {
	r.firedThisTurn = make(map[domain.MechanismID]bool)
	r.productive = false
}

// skipMechanism reports whether a catalogued Mechanism is NOT dispatched this Turn: either the
// Bypass gate switches it off (D5, skipUnderBypass), or self-regulation has withdrawn it (Adaptive
// Suppression / the Turn Budget, D2). It governs only catalogued Mechanisms — an experimental hook
// is the bench's own instrument and consults neither gate.
func (a *Agent) skipMechanism(m domain.Mechanism) bool {
	return a.skipUnderBypass(m) || a.tracker.suppress(m)
}

// noteToolProductivity feeds one executed tool call's outcome to the Turn's productivity signal
// (the effectiveness-tracking / Turn-Budget input). A tool error contributes nothing; a successful
// read-only call is a new-file read when its target is novel; any other successful call is a
// write/action. An unknown tool (already surfaced as an error result) is ignored.
func (a *Agent) noteToolProductivity(call domain.ToolCall, result domain.ToolResult) {
	if result.IsError {
		return
	}
	tool, ok := a.lookupTool(call.Tool)
	if !ok {
		return
	}
	if domain.IsReadOnly(tool) {
		a.tracker.noteRead(call)
		return
	}
	a.tracker.noteWrite()
}

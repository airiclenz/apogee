package agent

import "github.com/airiclenz/apogee/internal/domain"

// Self-regulation (CONTEXT: Self-regulation; plan item 3, amended by phase-4-review-fixes
// item 4 — R3/R4) is the per-Session safety net that keeps a Mechanism from hurting the model —
// the proxy-signal heuristic half of the hard constraint, deliberately WEAKER than the bench's
// A/B gate (decision 12). It is entirely per-Session and resets on Resume: newAgent builds a
// fresh selfRegulator and restoreState does not restore it (agentState carries no tracker), so a
// resumed Session starts with a clean slate. That is the accepted v1 posture — fresh suppression
// state can only cause a withdrawn Mechanism to be re-tried, never to be wrongly withheld, so the
// failure mode is benign (the tracker is not serialized, so it cannot drift out of sync with a
// snapshot).
//
// Judgment is NEXT-Turn (R3): fires recorded in Turn N are judged by Turn N+1's outcome — a
// Mechanism's intervention can only show up in what the model does next. Each completed Turn is
// classified three-way on the four proxy signals: PRODUCTIVE (a novel file read, or a successful
// write/action), HARMFUL (a tool-result error, or an empty final response), NEUTRAL (neither —
// e.g. a substantive text-only answer); productive wins when signals mix. Strikes and the
// Turn-Budget streak advance ONLY on a harmful Turn; a neutral Turn freezes both (no strike, no
// streak advance, no clear); a productive Turn is the global clear-path. Consequence: a pure Q&A
// session neither strikes Mechanisms nor trips the Turn Budget.
//
// Fired means ACTED (R4): recordFire is reached only when an invocation actually intervened —
// it mutated its working value or returned a non-zero post-response Action (hookrun.go brackets
// each catalogued fire) — so fireCounts / LoopView.Fired count interventions, not invocations
// (apogee-sim's FiredCounts). An experimental hook's synthetic ID keeps the always-booked
// behaviour (bench observability); it is never consulted for suppression.
//
// Two withdrawal rules ride on the judgment. Adaptive Suppression is per-Mechanism: a Mechanism
// whose fires are judged harmful adaptiveSuppressStrikes consecutive times is withdrawn for the
// rest of the Session, with a clear-path that re-opens it on a productive Turn. The Turn Budget
// is global: after turnBudgetLimit harmful Turns with no productive Turn in between, every
// non-exempt Mechanism is withdrawn until productive activity resumes. An exempt (off-ramp)
// Mechanism bypasses both — suppressing it would leave a failed Turn with no way out
// (CONTEXT: Off-ramp).

const (
	// adaptiveSuppressStrikes is how many consecutive harmful judgments a Mechanism's fires may
	// accrue before Adaptive Suppression withdraws it for the rest of the Session — apogee-sim's
	// shipped default (docs/design/mechanism-catalogue.md §"Self-regulation constants").
	adaptiveSuppressStrikes = 3
	// turnBudgetLimit is how many harmful Turns (with no productive Turn in between) trip the
	// global Turn Budget, withdrawing every non-exempt Mechanism until productive activity
	// resumes — apogee-sim's shipped default (docs/design/mechanism-catalogue.md
	// §"Self-regulation constants").
	turnBudgetLimit = 8
)

// turnJudgment is the three-way classification of a completed Turn on the four proxy
// signals (R3). Productive wins when signals mix.
type turnJudgment int

const (
	judgedNeutral turnJudgment = iota
	judgedProductive
	judgedHarmful
)

// selfRegulator holds the per-Session effectiveness tracking, Adaptive Suppression, and Turn
// Budget state (plan item 3, R3/R4). One Agent owns one selfRegulator; it is driven from the
// single Step goroutine, so it needs no locking.
type selfRegulator struct {
	// fireCounts is the lifetime ACTED-fire count per Mechanism this Session — the ledger
	// LoopView.Fired answers from (R4: interventions, not invocations). It is shared BY
	// REFERENCE into each Request's view (buildRequest / loopView pass it to domain.NewRequest),
	// so a Mechanism querying Fired sees a peer's fire from earlier in the same hook pass (the
	// decompose↔read_loop coupling seam, D2). It is only read through the view; only recordFire
	// writes it.
	fireCounts map[domain.MechanismID]int

	// firedThisTurn is the set of Mechanisms that acted in the Turn currently in flight. At
	// endTurn it shifts into pendingJudgment: this Turn's fires are judged by the NEXT Turn's
	// outcome (R3).
	firedThisTurn map[domain.MechanismID]bool

	// pendingJudgment is the PREVIOUS Turn's fired set awaiting this Turn's outcome — the
	// next-Turn judgment seam (R3). endTurn judges it, then replaces it with firedThisTurn.
	// discardTurn (cancel/abandon) leaves it in place, so the re-attempt's outcome judges it.
	pendingJudgment map[domain.MechanismID]bool

	// strikes counts a Mechanism's consecutive harmful judgments; suppressed records the ones
	// Adaptive Suppression has withdrawn for the Session. A productive Turn clears both; a
	// neutral Turn freezes them.
	strikes    map[domain.MechanismID]int
	suppressed map[domain.MechanismID]bool

	// seenReads keys the read-only tool calls already made this Session (tool name + arguments),
	// so a re-read of the same target is NOT counted as a new-file-read productivity signal — the
	// wasted-read shape a read-loop produces (grounded on apogee-sim's identical-args signal).
	// turnReads holds the keys booked THIS Turn, so a cancelled Turn's rollback (discardTurn)
	// can restore the re-attempt's novelty credit.
	seenReads map[string]bool
	turnReads map[string]bool

	// harmfulStreak counts harmful Turns since the last productive Turn (neutral Turns freeze
	// it); budgetTripped is the global Turn-Budget withdrawal it raises at turnBudgetLimit. A
	// productive Turn resets both.
	harmfulStreak int
	budgetTripped bool

	// sawProductive / sawHarmful are the Turn-in-flight's proxy signals: a novel read or a
	// successful write/action sets sawProductive; a tool-result error or an empty final response
	// sets sawHarmful. Productive wins when both are set. Reset each Turn.
	sawProductive bool
	sawHarmful    bool
}

// newSelfRegulator builds an empty per-Session tracker. newAgent calls it, so a fresh Agent —
// including one rebuilt by Resume — starts with clean self-regulation state.
func newSelfRegulator() *selfRegulator {
	return &selfRegulator{
		fireCounts:      make(map[domain.MechanismID]int),
		firedThisTurn:   make(map[domain.MechanismID]bool),
		pendingJudgment: make(map[domain.MechanismID]bool),
		strikes:         make(map[domain.MechanismID]int),
		suppressed:      make(map[domain.MechanismID]bool),
		seenReads:       make(map[string]bool),
		turnReads:       make(map[string]bool),
	}
}

// recordFire books one ACTED fire (R4 — hookrun calls it only when the invocation mutated its
// working value or returned a non-zero post-response Action): it bumps the Session ledger
// LoopView.Fired reads and marks the Mechanism as fired this Turn, so the NEXT Turn's outcome
// judges it (R3). An experimental hook's synthetic ID accrues counts on every invocation
// (bench observability — it is never consulted for suppression).
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

// noteRead books a successful read-only tool call as a productive signal when its target is
// novel this Session (a NEW file read). A repeat read of the same tool + arguments is the
// wasted-read shape and contributes nothing. The key is also remembered per-Turn so a cancelled
// Turn's discard restores the novelty credit for the mandated re-attempt.
func (r *selfRegulator) noteRead(call domain.ToolCall) {
	key := string(call.Tool) + "\x00" + string(call.Arguments)
	if r.seenReads[key] {
		return
	}
	r.seenReads[key] = true
	r.turnReads[key] = true
	r.sawProductive = true
}

// noteWrite books a successful mutating tool call as a productive signal (a file written / an
// action taken).
func (r *selfRegulator) noteWrite() { r.sawProductive = true }

// noteToolError books a tool-result error as this Turn's harmful signal (R3).
func (r *selfRegulator) noteToolError() { r.sawHarmful = true }

// noteEmptyResponse books an empty final response (whitespace-only text, no tool calls) as
// this Turn's harmful signal (R3).
func (r *selfRegulator) noteEmptyResponse() { r.sawHarmful = true }

// judgment classifies the Turn in flight three-way on the recorded proxy signals (R3):
// productive wins when signals mix, harmful needs a harmful signal with no productive one,
// and a Turn with neither is neutral.
func (r *selfRegulator) judgment() turnJudgment {
	switch {
	case r.sawProductive:
		return judgedProductive
	case r.sawHarmful:
		return judgedHarmful
	default:
		return judgedNeutral
	}
}

// endTurn resolves a COMPLETED Turn: it first judges the pending set — the PREVIOUS Turn's
// fires — against THIS Turn's outcome (R3, next-Turn judgment), then shifts this Turn's fires
// into pending and clears the per-Turn scratch. A productive Turn is the clear-path: it zeroes
// every strike, re-opens every suppressed Mechanism, and lifts the Turn Budget. A harmful Turn
// strikes each pending Mechanism — withdrawing one at the strike limit — and advances the
// Turn-Budget streak, tripping it at the limit. A neutral Turn freezes both: no strike, no
// streak advance, no clear.
func (r *selfRegulator) endTurn() {
	switch r.judgment() {
	case judgedProductive:
		r.strikes = make(map[domain.MechanismID]int)
		r.suppressed = make(map[domain.MechanismID]bool)
		r.harmfulStreak = 0
		r.budgetTripped = false
	case judgedHarmful:
		for id := range r.pendingJudgment {
			r.strikes[id]++
			if r.strikes[id] >= adaptiveSuppressStrikes {
				r.suppressed[id] = true
			}
		}
		r.harmfulStreak++
		if r.harmfulStreak >= turnBudgetLimit {
			r.budgetTripped = true
		}
	case judgedNeutral:
		// Freeze: no strike, no streak advance, no clear. The pending set still
		// rotates below — a fire gets exactly one judgment window (the next completed
		// Turn), and a neutral window yields no evidence either way.
	}
	r.pendingJudgment = r.firedThisTurn
	r.resetTurnScratch()
}

// discardTurn clears the per-Turn scratch WITHOUT judging — the path a cancelled or abandoned
// Turn takes, so a rolled-back or faulted Turn neither strikes a Mechanism nor advances the Turn
// Budget, and its fires do not bleed into the next Turn's judgment. The pending set is left IN
// PLACE: the previous Turn's fires are still awaiting an outcome, and the re-attempt's outcome
// judges them (R3). This Turn's novel-read keys are rolled back out of seenReads, so the
// mandated re-attempt regains its novelty credit. The Session ledgers (fireCounts, strikes,
// suppression) are untouched.
func (r *selfRegulator) discardTurn() {
	for key := range r.turnReads {
		delete(r.seenReads, key)
	}
	r.resetTurnScratch()
}

// resetTurnScratch clears the per-Turn scratch: the fired-this-Turn set, this Turn's
// novel-read keys, and the proxy-signal flags.
func (r *selfRegulator) resetTurnScratch() {
	r.firedThisTurn = make(map[domain.MechanismID]bool)
	r.turnReads = make(map[string]bool)
	r.sawProductive = false
	r.sawHarmful = false
}

// skipMechanism reports whether a catalogued Mechanism is NOT dispatched this Turn: either the
// Bypass gate switches it off (D5, skipUnderBypass), or self-regulation has withdrawn it (Adaptive
// Suppression / the Turn Budget, D2). It governs only catalogued Mechanisms — an experimental hook
// is the bench's own instrument and consults neither gate.
func (a *Agent) skipMechanism(m domain.Mechanism) bool {
	return a.skipUnderBypass(m) || a.tracker.suppress(m)
}

// noteToolProductivity feeds one executed tool call's outcome to the Turn's proxy signals (R3).
// A tool-result error is the harmful signal; a successful read-only call is the productive
// new-file-read signal when its target is novel; any other successful call is a write/action.
// A successful result from an unknown tool contributes nothing (unreachable today — the
// registry miss already produced an error result, caught above).
func (a *Agent) noteToolProductivity(call domain.ToolCall, result domain.ToolResult) {
	if result.IsError {
		a.tracker.noteToolError()
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

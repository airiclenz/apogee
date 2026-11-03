package agent

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// delegateBound names which of a delegate's three bounds ended its Exchange (Agent.capHit). The
// zero value is the step cap — the bound every capped result was read as before the token and
// time bounds existed, so an Agent built with a stepCap alone still words its markers as it did.
// It indexes boundRows, the one table every per-bound fact is read from.
type delegateBound int

const (
	boundSteps delegateBound = iota
	boundTokens
	boundTime
)

// boundRow is everything one delegate bound is known by, so a fact about a bound is read off its
// row rather than switched on in each place that states it. The formats are the package constants
// that carry each surface's pinned text (stepCapErrFormat, stepCapResultFormat, …): a row only
// names which of them belongs to its bound.
type boundRow struct {
	// word is the bound as the session snapshot saves it (delegateBound.word) — the bound's own
	// name, as the capped result heads spell the resource that ran out.
	word string
	// delegation is the host-facing name of the bound, the one a capped result's typed outcome
	// (domain.DelegationOutcome) reports beside the head line.
	delegation domain.DelegationBound
	// errFormat is the ErrorEvent text finishAtStepCap emits (boundErrText).
	errFormat string
	// reportHeadFormat and nonReportHeadFormat are the capped result's head line for a closing
	// report and for a closing text floor.ClosingShapeOf judged not to be one (capResultHead).
	reportHeadFormat    string
	nonReportHeadFormat string
	// wrapUpFormat is the closing-report directive the capped delegate is handed (wrapUpDirective).
	wrapUpFormat string
	// finishCauseFormat is the bound phrase a capped workflow item's finish directive opens with
	// (wrapUpFinishDirective).
	finishCauseFormat string
	// reached reports whether the Turn just completed has reached this bound; a bound at 0 is off.
	reached func(a *Agent) bool
	// marker is the applied bound as every format above quotes it in its bound slot: the raw count
	// for steps and tokens, boundDurationText for time — so a token marker reads
	// `(20000000 tokens)` where the token notice's own render reads `20.0M`.
	marker func(a *Agent) any
	// notice is the bound's ¾ budget notice (budgetNotices), nil for a bound that has none.
	notice *boundNotice
}

// boundNotice is a bound's budget notice: the engine note that tells a delegate, once, that it has
// spent share of the bound (stepnotice.go).
type boundNotice struct {
	// topic is the engine-note topic the notice is fenced under and its latch is read on.
	topic string
	// share is the share of the bound the notice fires at: spent reaching ceil(share × limit).
	share float64
	// spent is what the delegate has used of the bound at the result about to be committed.
	spent func(a *Agent) int
	// limit is the bound applied; 0 is unbounded, and an unbounded delegation is never told.
	limit func(a *Agent) int
	// render is the notice's fact line for spent of limit, in the notice's own spelling.
	render func(spent, limit int) string
}

// boundRows is the table: one row per delegateBound, in the order delegateBoundHit checks them —
// steps first, then tokens, then time, so where two trip at once the cheaper story wins — and the
// order budgetNotices fences their notices in.
var boundRows = [...]boundRow{
	boundSteps: {
		word:                "steps",
		delegation:          domain.DelegationStepCap,
		errFormat:           stepCapErrFormat,
		reportHeadFormat:    stepCapResultFormat,
		nonReportHeadFormat: stepCapNonReportFormat,
		wrapUpFormat:        wrapUpDirectiveFormat,
		finishCauseFormat:   wrapUpFinishStepCause,
		reached:             func(a *Agent) bool { return a.stepCap > 0 && a.turns.exchangeTurns >= a.stepCap },
		marker:              func(a *Agent) any { return a.stepCap },
		notice: &boundNotice{
			topic: stepNoticeTopic,
			share: stepNoticeShare,
			// turns.exchangeTurns is advanced by Run AFTER step() returns, so at the commit of
			// Turn N it reads N-1 and the Turn under way is one more.
			spent: func(a *Agent) int { return a.turns.exchangeTurns + 1 },
			limit: func(a *Agent) int { return a.stepCap },
			render: func(used, stepCap int) string {
				return fmt.Sprintf(stepNoticeLine, used, stepCap, stepCap-used)
			},
		},
	},
	boundTokens: {
		word:                "tokens",
		delegation:          domain.DelegationTokenBudget,
		errFormat:           tokenCapErrFormat,
		reportHeadFormat:    tokenCapResultFormat,
		nonReportHeadFormat: tokenCapNonReportFormat,
		wrapUpFormat:        wrapUpTokenDirectiveFormat,
		finishCauseFormat:   wrapUpFinishTokenCause,
		// The token bound reads this Agent's own usage tally: what the child spent, nothing a
		// parent spent.
		reached: func(a *Agent) bool { return a.tokenCap > 0 && a.usage.prompt >= a.tokenCap },
		marker:  func(a *Agent) any { return a.tokenCap },
		notice: &boundNotice{
			topic: tokenNoticeTopic,
			share: tokenNoticeShare,
			// The Turn under way has already booked its usage: the server's report lands on the
			// stream before the tool runs (loop.go, DeltaDone).
			spent: func(a *Agent) int { return a.usage.prompt },
			limit: func(a *Agent) int { return a.tokenCap },
			// The tokens-left figure floors at 0: the result closing the Turn that crossed the cap
			// commits before the boundary trips the bound, and "0 left" is what the wrap-up states.
			render: func(spent, tokenCap int) string {
				left := max(tokenCap-spent, 0)
				return fmt.Sprintf(tokenNoticeLine, formatTokens(spent), formatTokens(tokenCap), formatTokens(left))
			},
		},
	},
	boundTime: {
		word:                "time",
		delegation:          domain.DelegationTimeLimit,
		errFormat:           timeCapErrFormat,
		reportHeadFormat:    timeCapResultFormat,
		nonReportHeadFormat: timeCapNonReportFormat,
		wrapUpFormat:        wrapUpTimeDirectiveFormat,
		finishCauseFormat:   wrapUpFinishTimeCause,
		// The time bound reads the clock through a.now against capStart, which Run set at its head.
		reached: func(a *Agent) bool {
			return a.timeCap > 0 && !a.capStart.IsZero() && a.now().Sub(a.capStart) >= a.timeCap
		},
		marker: func(a *Agent) any { return boundDurationText(a.timeCap) },
	},
}

// row is b's row in boundRows.
func (b delegateBound) row() boundRow { return boundRows[b] }

// word is the word the session snapshot saves b as.
func (b delegateBound) word() string { return b.row().word }

// delegationBound is the host-facing name of this bound (domain.DelegationBound), the one a capped
// result's typed outcome reports beside the head line capResultHead writes for the same bound.
func (b delegateBound) delegationBound() domain.DelegationBound { return b.row().delegation }

// parseDelegateBound reads a saved bound word back; false for a word this build never writes.
func parseDelegateBound(word string) (delegateBound, bool) {
	for b, row := range boundRows {
		if row.word == word {
			return delegateBound(b), true
		}
	}
	return 0, false
}

// boundDurationText spells a time bound the way the markers quote it: time.Duration's own text
// with a trailing `0s` dropped where minutes precede it, so the default reads `2h0m` rather than
// `2h0m0s` and a bound of `90s` still reads as `1m30s`.
func boundDurationText(d time.Duration) string {
	text := d.String()
	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}
	return text
}

// delegateBoundHit reports which bound, if any, the Turn just completed has reached, in row order.
func (a *Agent) delegateBoundHit() (delegateBound, bool) {
	for b, row := range boundRows {
		if row.reached(a) {
			return delegateBound(b), true
		}
	}
	return boundSteps, false
}

// capMarker renders format — one of the capHit row's formats — with that bound's applied value in
// its bound slot, followed by any further arguments the format takes.
func (a *Agent) capMarker(format string, rest ...any) string {
	return fmt.Sprintf(format, append([]any{a.capHit.row().marker(a)}, rest...)...)
}

// boundErrText words the ErrorEvent finishAtStepCap emits for the bound capHit names.
func (a *Agent) boundErrText() string {
	return a.capMarker(a.capHit.row().errFormat)
}

// threshold is the spend the notice fires at for limit: ceil(share × limit).
func (n *boundNotice) threshold(limit int) int {
	return int(math.Ceil(n.share * float64(limit)))
}

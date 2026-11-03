package agent

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// delegateBound names which of a delegate's three bounds ended its Exchange (delegateLimits.hit).
// The zero value is the step cap — the bound every capped result was read as before the token and
// time bounds existed, so an Agent bounded by steps alone still words its markers as it did.
// It indexes boundRows, the one table every per-bound fact is read from.
type delegateBound int

const (
	boundSteps delegateBound = iota
	boundTokens
	boundTime
)

// delegateLimits is a delegate's bounds, whole: the three caps it runs under, the clock reading the
// time cap counts from, the bound that ended its Exchange and the max_steps ask its step cap
// clamped. It is seeded once from the delegation (newChildAgentOn reads the caps from
// Config.Delegation at SPAWN, delegation.seed copies them verbatim) and is zero on a top-level
// Agent, whose main loop is the human's to stop where a delegate's is nobody's — so a zero cap is
// off, and the zero value bounds nothing. Structural (ADR 0006), not a Reaction: on under Bypass.
// Run is the ONE enforcement site (tripped).
type delegateLimits struct {
	// steps bounds the Turns of ONE Exchange (Config.Delegation.MaxSteps, `delegate-max-steps`);
	// a sub_agent call's max_steps may lower it for that one delegation (clampSteps).
	steps int
	// tokens bounds the cumulative PROMPT tokens of this Agent's own usage tally
	// (Config.Delegation.MaxTokens, `delegate-max-tokens`).
	tokens int
	// time bounds the wall clock of the one Exchange from its first request
	// (Config.Delegation.Timeout, `delegate-timeout`).
	time time.Duration
	// start is the clock reading the time cap counts from, taken once at the head of the child's
	// Run (startClock) through the Agent's injected clock, so a test can pin it.
	start time.Time
	// hit records WHICH bound ended the Exchange: the one fact the markers a capped run writes
	// (finishAtStepCap's ErrorEvent, loop.go's wrap-up directive, subagent.go's result head) need in
	// common. Its zero value is the step cap, so a capped boundary that never named a bound reads
	// exactly as every capped boundary read before the other two existed.
	hit delegateBound
	// requested is the `max_steps` this delegation's spawning call asked for when it was ABOVE the
	// configured step cap and was applied as the cap instead (clampSteps): delegationResult appends
	// the clamp note that says so. 0 for every other spawn — no ask, a lower ask that bound, an ask
	// against an unbounded cap. Step-only, so it is no row of boundRows.
	requested int
}

// tripped reports which bound, if any, a delegate that has taken turns Turns of its Exchange and
// spent prompt tokens has reached, in row order (steps, then tokens, then time). A cap at 0 is
// off, and a zero start never trips the time cap. now is read only when the time cap is live.
func (l delegateLimits) tripped(turns, prompt int, now func() time.Time) (delegateBound, bool) {
	for b, row := range boundRows {
		if row.reached(l, turns, prompt, now) {
			return delegateBound(b), true
		}
	}
	return boundSteps, false
}

// startClock takes the reading the time cap counts from, once: a resumed loop never restarts the
// clock, and an unbounded delegation never reads it.
func (l *delegateLimits) startClock(now func() time.Time) {
	if l.time > 0 && l.start.IsZero() {
		l.start = now()
	}
}

// clampSteps applies a sub_agent call's optional max_steps to the step cap (resolveStepCap),
// recording the ask it clamped in requested. Outside the table: no other bound takes an ask.
func (l *delegateLimits) clampSteps(asked int) {
	l.steps, l.requested = resolveStepCap(l.steps, asked)
}

// capMarker renders format — one of the hit row's formats — with that bound's applied value in its
// bound slot, followed by any further arguments the format takes.
func (l delegateLimits) capMarker(format string, rest ...any) string {
	return fmt.Sprintf(format, append([]any{l.hit.row().marker(l)}, rest...)...)
}

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
	// reached reports whether a delegate under limits that has taken turns Turns and spent prompt
	// tokens has reached this bound; a bound at 0 is off.
	reached func(limits delegateLimits, turns, prompt int, now func() time.Time) bool
	// marker is the applied bound as every format above quotes it in its bound slot: the raw count
	// for steps and tokens, boundDurationText for time — so a token marker reads
	// `(20000000 tokens)` where the token notice's own render reads `20.0M`.
	marker func(limits delegateLimits) any
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
	limit func(limits delegateLimits) int
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
		reached: func(l delegateLimits, turns, _ int, _ func() time.Time) bool {
			return l.steps > 0 && turns >= l.steps
		},
		marker: func(l delegateLimits) any { return l.steps },
		notice: &boundNotice{
			topic: stepNoticeTopic,
			share: stepNoticeShare,
			// turns.exchangeTurns is advanced by Run AFTER step() returns, so at the commit of
			// Turn N it reads N-1 and the Turn under way is one more.
			spent: func(a *Agent) int { return a.turns.exchangeTurns + 1 },
			limit: func(l delegateLimits) int { return l.steps },
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
		reached: func(l delegateLimits, _, prompt int, _ func() time.Time) bool {
			return l.tokens > 0 && prompt >= l.tokens
		},
		marker: func(l delegateLimits) any { return l.tokens },
		notice: &boundNotice{
			topic: tokenNoticeTopic,
			share: tokenNoticeShare,
			// The Turn under way has already booked its usage: the server's report lands on the
			// stream before the tool runs (loop.go, DeltaDone).
			spent: func(a *Agent) int { return a.usage.prompt },
			limit: func(l delegateLimits) int { return l.tokens },
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
		// The time bound reads the clock against start, which Run set at its head (startClock).
		reached: func(l delegateLimits, _, _ int, now func() time.Time) bool {
			return l.time > 0 && !l.start.IsZero() && now().Sub(l.start) >= l.time
		},
		marker: func(l delegateLimits) any { return boundDurationText(l.time) },
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
	return a.limits.tripped(a.turns.exchangeTurns, a.usage.prompt, a.now)
}

// boundErrText words the ErrorEvent finishAtStepCap emits for the bound limits.hit names.
func (a *Agent) boundErrText() string {
	return a.limits.capMarker(a.limits.hit.row().errFormat)
}

// threshold is the spend the notice fires at for limit: ceil(share × limit).
func (n *boundNotice) threshold(limit int) int {
	return int(math.Ceil(n.share * float64(limit)))
}

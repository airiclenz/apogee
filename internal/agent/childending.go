package agent

import (
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/floor"
	"github.com/airiclenz/apogee/internal/workflow"
)

// childEnding is how ONE delegation ended, decided once: a child's run read by the one run switch
// (runEnding, then withReport over the result the parent reads), a sub_agent call no child was
// ever started for (refusedEnding), or a delegation settled before its child started
// (unstartedSkipped, unstartedPastCeiling, unstartedCancelled, unstartedStopped). Every word a
// delegation's ending is spelled in elsewhere is a projection of it: the ledger row's outcome and
// cause (ledgerRecord), the workflow Ending (workflowEnding), why mail left in the child's mailbox
// never landed (undelivered), the typed half of the rendered result (summary) and the result an
// unstarted ending answers its call with (unstartedResult).
//
// It is a return value, never a field on Agent (ADR 0083 part 3): the path that decided it hands
// it on to each reader.
type childEnding struct {
	// kind is the ledger's word for the ending — one of the seven delegationOutcome words.
	kind delegationOutcome
	// hasStarted is an ending whose child reached its Run; a refusal and every unstarted ending
	// never did.
	hasStarted bool
	// content is the whole tool result an unstarted ending answers its call with — the model's only
	// account of a child that never ran — and "" for every other ending, whose result is rendered
	// from the run (or the refusal) by its own path.
	content string
	// unstarted is why an unstarted ending's child never ran — the typed outcome its result carries
	// (unstartedResult) — and domain.DelegationRan for every other ending, a refusal included.
	unstarted domain.DelegationUnstarted
}

// runEnding is the ONE run switch both delegation paths read a finished child's Run through — the
// sub_agent path and the workflow spawner alike: a loop-level Run error is a fault, then the stop
// verdict (runChild's stopped: the human's stop, or the cancel of the whole Turn that found the
// child running) outranks every state the stop left the run in, then a run cancelled with neither
// behind it, an abandoned Exchange, the engine's bound, and otherwise the child's own completion.
// A Run error never comes with a stop (childRunStopped reads err == nil), and finishAtStepCap
// clears Faulted on the capped run it returns, so no reachable input sets two of these at once.
func runEnding(res domain.StepResult, err error, stopped bool) childEnding {
	ending := childEnding{hasStarted: true}
	switch {
	case err != nil:
		ending.kind = delegationFaulted
	case stopped:
		ending.kind = delegationStopped
	case res.Status == domain.StatusCancelled:
		ending.kind = delegationCancelled
	case res.Faulted:
		ending.kind = delegationFaulted
	case res.StepCapped:
		ending.kind = delegationCapped
	default:
		ending.kind = delegationCompleted
	}
	return ending
}

// withReport is the ending once the result the parent reads is known: a completed run whose reply
// the engine would not hand over as a report — completedResult's error shapes — or whose result
// rendering panicked is a fault. Every other ending already says what its result says.
func (e childEnding) withReport(result domain.ToolResult) childEnding {
	if e.kind == delegationCompleted && result.IsError {
		e.kind = delegationFaulted
	}
	return e
}

// refusedEnding is a sub_agent call no child was ever started for: the depth bound, bad arguments,
// an unknown `continue`, a bad seat or roster, a construction or Submit failure, or a panic before
// the child's Run was reached.
func refusedEnding() childEnding {
	return childEnding{kind: delegationRefused}
}

// unstartedSkipped is a delegation a queued user message pre-empted before its child started
// (preemptDelegation).
func unstartedSkipped() childEnding {
	return childEnding{kind: delegationSkipped, content: skippedDelegationContent, unstarted: domain.DelegationSkipped}
}

// unstartedPastCeiling is a delegation refused past the reply's fan-out ceiling (refusePastCeiling).
// Its result is rendered per call — the reply's size, the rounds and width that set the ceiling and
// whether fan_out is on the roster (fanOutCeilingResult) — so the ending carries the rendered one.
func unstartedPastCeiling(result domain.ToolResult) childEnding {
	return childEnding{kind: delegationRefused, content: result.Content, unstarted: domain.DelegationRefused}
}

// unstartedCancelled is a delegation the cancel of the whole Turn reached before its child started
// (settleCancelledDelegation).
func unstartedCancelled() childEnding {
	return childEnding{kind: delegationCancelled, content: cancelledQueuedDelegationContent, unstarted: domain.DelegationCancelled}
}

// unstartedStopped is a pooled delegation the human stopped before a worker took it
// (stopQueuedDelegation).
func unstartedStopped() childEnding {
	return childEnding{kind: delegationStopped, content: stoppedQueuedDelegationContent, unstarted: domain.DelegationStoppedQueued}
}

// ledgerRecord completes row — the delegation's spawn index, call, label and output target, which
// the ending does not know — with the ending's outcome word and, for a refusal or a fault, the head
// line of resultContent, the result the parent read (an unstarted ending's own content).
func (e childEnding) ledgerRecord(row delegationRecord, resultContent string) delegationRecord {
	row.outcome = e.kind
	row.cause = ""
	if e.kind == delegationRefused || e.kind == delegationFaulted {
		row.cause = delegationCause(resultContent)
	}
	return row
}

// workflowEnding is the workflow Ending the ending reads as: a cancel or a stop is stopped, a
// refusal or a skip a fault.
func (e childEnding) workflowEnding() workflow.Ending {
	switch e.kind {
	case delegationCompleted:
		return workflow.EndCompleted
	case delegationCapped:
		return workflow.EndCapped
	case delegationCancelled, delegationStopped:
		return workflow.EndStopped
	default:
		return workflow.EndFaulted
	}
}

// undelivered is why the messages a delegation's mailbox still held never landed. A child that
// never ran did not take them, whichever way it was settled, so a refusal and every unstarted
// ending read as refused (owner, 2026-11-21) — on the sub_agent and the workflow path alike.
func (e childEnding) undelivered() domain.UndeliveredReason {
	if !e.hasStarted {
		return domain.UndeliveredRefused
	}
	switch e.kind {
	case delegationCapped:
		return domain.UndeliveredCapped
	case delegationFaulted:
		return domain.UndeliveredFaulted
	case delegationCancelled:
		return domain.UndeliveredCancelled
	case delegationStopped:
		return domain.UndeliveredStopped
	default:
		return domain.UndeliveredCompleted
	}
}

// summary is the typed half of a started child's rendered result (domain.DelegationOutcome):
// steered is how many messages the human sent into the run, bound the engine bound a capped run
// hit, and report the result's content with the absolute cap applied and no body note appended
// yet — the child's closing text, which the no-report reading judges. A stop outranks a bound, and
// only a completed run can have no report; a fault carries the steered count alone. An ending that
// never ran carries why it never ran alone (unstarted) — DelegationRan, the zero value, for a
// refusal, whose result is rendered by its own path and carries no summary.
func (e childEnding) summary(steered int, bound domain.DelegationBound, report string) domain.DelegationOutcome {
	if !e.hasStarted {
		return domain.DelegationOutcome{Unstarted: e.unstarted}
	}
	summary := domain.DelegationOutcome{SteeredMessages: steered}
	switch e.kind {
	case delegationStopped:
		summary.IsStoppedByUser = true
	case delegationCapped:
		summary.Bound = bound
	case delegationCompleted:
		summary.HasNoReport = strings.TrimSpace(report) == noReportMarker || floor.IsNonReport(report)
	}
	return summary
}

// unstartedResult is the whole tool result an unstarted ending answers call callID with: its content,
// error-shaped, carrying its typed outcome (summary) — why the child never ran — so a host reads
// the ending off the value, never back out of the content.
func (e childEnding) unstartedResult(callID string) domain.ToolResult {
	result := errorToolResult(callID, e.content)
	result.Summary = e.summary(0, domain.DelegationUnbounded, "")
	return result
}

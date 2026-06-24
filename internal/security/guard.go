package security

import "github.com/airiclenz/apogee/internal/domain"

// ----------------------------------------------------------------------------
// Guards — the executor-facing guardrail bundle the tool executor threads (D6)
// ----------------------------------------------------------------------------

// Guards bundles the always-on, mode-independent guardrails the tool executor consults
// for every tool call, in every mode: the dangerous-action floor, the circuit-breaker,
// and the audit log (D6). Path-safety and url-safety are tool-local guards (the file and
// network tools call them directly), so they are not part of this executor bundle — this
// is the set the dispatcher itself runs around each call.
//
// A Guards value threads into a sub-agent verbatim (D2), so the sub-agent inherits the
// same floor for free. A zero Guards is inert (every field nil): PreExecute returns
// GuardProceed and RecordResult is a no-op, so an executor can hold a Guards
// unconditionally without nil-checking each field.
type Guards struct {
	// Dangerous is the dangerous-action guard (footgun floor). nil ⇒ no dangerous-action
	// inspection.
	Dangerous *DangerousActionGuard
	// Breaker halts a runaway identical-failing-call loop. nil ⇒ no circuit-breaking.
	Breaker *CircuitBreaker
	// Audit is the append-only tool-call log. nil ⇒ no auditing.
	Audit *AuditLog
}

// NewDefaultGuards returns the production guardrail bundle: the default dangerous-action
// ruleset, a default-threshold circuit-breaker, and a fresh audit log. The executor wires
// this when the host does not supply its own.
func NewDefaultGuards() Guards {
	return Guards{
		Dangerous: DefaultDangerousActionGuard(),
		Breaker:   NewCircuitBreaker(DefaultCircuitBreakerThreshold),
		Audit:     NewAuditLog(),
	}
}

// GuardOutcome is what PreExecute tells the executor to do with a call before the mode
// disposition runs. The guardrails are tighten-only (ADR 0012): an outcome can only make
// a call stricter than its mode would, never looser.
type GuardOutcome int

const (
	// GuardProceed: no guardrail fired; the executor continues to the mode disposition.
	GuardProceed GuardOutcome = iota
	// GuardRefuse: the call must be refused outright with a clear error result, in every
	// mode, with no override (a Tier-1 dangerous action, or a tripped circuit-breaker).
	GuardRefuse
	// GuardForceApproval: the call must route through the Approver even in Auto (a Tier-2
	// dangerous action). A nil Approver downstream ⇒ refuse (the caller enforces that).
	GuardForceApproval
)

// PreCheck is PreExecute's verdict: the outcome plus the human-facing reason and the
// audit decision the executor records.
type PreCheck struct {
	Outcome GuardOutcome
	Reason  string        // why the guard fired ("" when GuardProceed)
	Audit   AuditDecision // the decision to record for a fired guard
}

// PreExecute runs the always-on pre-execution guardrails for call, in order: the
// circuit-breaker (a tripped signature short-circuits before anything else, so a runaway
// loop cannot keep re-triggering the dangerous-action guard), then the dangerous-action
// guard (tighten-only, before the mode disposition). It does not execute the call and
// does not consult the Approver — it only reports what the executor must do.
func (g Guards) PreExecute(call domain.ToolCall) PreCheck {
	if g.Breaker != nil && g.Breaker.Tripped(call) {
		return PreCheck{
			Outcome: GuardRefuse,
			Reason:  "circuit-breaker open: identical tool call has failed repeatedly",
			Audit:   AuditCircuitTripped,
		}
	}

	if g.Dangerous != nil {
		if d := g.Dangerous.Inspect(call); d.Triggered() {
			switch d.Tier {
			case TierHardRefuse:
				return PreCheck{Outcome: GuardRefuse, Reason: d.Reason, Audit: AuditDangerousRefused}
			case TierForceApproval:
				return PreCheck{Outcome: GuardForceApproval, Reason: d.Reason, Audit: AuditDangerousForceApproval}
			}
		}
	}

	return PreCheck{Outcome: GuardProceed, Audit: AuditAllowed}
}

// RecordExecution updates the post-execution guardrails after a call ran: it feeds the
// circuit-breaker the call's failure outcome (returning true on the trip edge so the
// executor surfaces a single ErrorEvent) and appends an audit record. decision is the
// audit decision for the call (typically AuditAllowed for an executed call).
func (g Guards) RecordExecution(call domain.ToolCall, decision AuditDecision, reason string, result domain.ToolResult) (tripped bool) {
	if g.Breaker != nil {
		tripped = g.Breaker.Record(call, result.IsError)
	}
	if g.Audit != nil {
		g.Audit.RecordCall(call, decision, reason, result)
	}
	return tripped
}

// RecordBlocked appends an audit record for a call the guardrails refused or diverted
// before execution (so the trail captures blocked calls, not just executed ones).
func (g Guards) RecordBlocked(call domain.ToolCall, decision AuditDecision, reason string, result domain.ToolResult) {
	if g.Audit != nil {
		g.Audit.RecordCall(call, decision, reason, result)
	}
}

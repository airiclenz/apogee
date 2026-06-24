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
// LIVE STATE. Breaker and Audit hold MUTABLE pointer-backed state (the breaker's failure
// streaks, the audit ring). A Guards value-copy therefore ALIASES that live state through
// the shared pointers — copying the struct does NOT copy the breaker or the log. Dangerous,
// by contrast, is read-only after construction (Inspect/Rules only), so sharing its pointer
// is safe and intended. The split matters when handing Guards to a sub-agent: a verbatim
// copy would make the sub-agent and parent share one breaker and one audit trail. Use
// ForSubAgent to ISOLATE the live state (fresh breaker + fresh audit) while keeping the
// dangerous-action floor SHARED read-only — the floor a sub-agent must never be able to
// re-derive or loosen (ADR 0013).
//
// A zero Guards is inert (every field nil): PreExecute returns GuardProceed and
// RecordExecution is a no-op, so an executor can hold a Guards unconditionally without
// nil-checking each field.
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

// ForSubAgent returns a Guards for a delegated sub-agent that ISOLATES the live state but
// SHARES the dangerous-action floor read-only (ADR 0013). The breaker and the audit log are
// fresh (a sub-agent's runaway tool-loop trips its own breaker, not the parent's, and its
// audit trail is its own), so the two loops cannot interfere through the aliased pointers a
// verbatim copy would share. The Dangerous guard is shared by POINTER: it is read-only after
// construction (Inspect/Rules expose no mutator), so a sub-agent inherits the exact same
// floor and has NO seam to re-derive, replace, or loosen it — the floor cannot be lowered one
// level down. A nil Breaker/Audit on the parent stays nil (isolation of "no guard" is still
// "no guard"); the breaker keeps the parent's configured threshold.
func (g Guards) ForSubAgent() Guards {
	sub := Guards{Dangerous: g.Dangerous} // shared read-only floor
	if g.Breaker != nil {
		sub.Breaker = NewCircuitBreaker(g.Breaker.Threshold())
	}
	if g.Audit != nil {
		sub.Audit = NewAuditLog()
	}
	return sub
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

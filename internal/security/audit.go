package security

import (
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Audit record (append-only tool-call log: call / decision / result — D6)
// ----------------------------------------------------------------------------

// AuditDecision is the guardrail/dispatch decision recorded for a tool call alongside its
// result, so the audit trail shows not just what ran but how it was gated.
type AuditDecision string

const (
	// AuditAllowed: the call cleared the guardrails and ran (gating, if any, is recorded
	// separately by the dispatch disposition — the breaker/dangerous-action floor let it
	// through).
	AuditAllowed AuditDecision = "allowed"
	// AuditDangerousRefused: the dangerous-action guard hard-refused the call (Tier 1).
	AuditDangerousRefused AuditDecision = "dangerous-refused"
	// AuditDangerousForceApproval: the dangerous-action guard forced approval (Tier 2).
	AuditDangerousForceApproval AuditDecision = "dangerous-force-approval"
	// AuditCircuitTripped: the circuit-breaker short-circuited the call (runaway loop).
	AuditCircuitTripped AuditDecision = "circuit-tripped"
)

// AuditRecord is one append-only entry in the tool-call audit log: the call, the
// guardrail decision, and the result. It is a value (no live handles) so the log can be
// snapshotted or shipped to an observer cheaply.
type AuditRecord struct {
	Time     time.Time
	Tool     string
	CallID   string
	Decision AuditDecision
	Reason   string // the guardrail reason (e.g. the dangerous-action rule), if any
	IsError  bool   // whether the recorded result was a tool-level error
	Result   string // the result content (truncated to maxAuditResultBytes)
}

// maxAuditResultBytes bounds the result text stored per record so a large tool output
// cannot make the in-memory log unbounded.
const maxAuditResultBytes = 4096

// AuditLog is an append-only, in-memory record of tool calls and their disposition. It is
// safe for concurrent append/read. The log is the guardrail observability spine — it
// records every call, its decision, and its result, in order. (Persisting it to disk is a
// later concern; v1 keeps it in-memory, snapshot-shippable.)
type AuditLog struct {
	mu      sync.Mutex
	records []AuditRecord
	now     func() time.Time // injectable clock for deterministic tests
}

// NewAuditLog returns an empty audit log using the wall clock.
func NewAuditLog() *AuditLog {
	return &AuditLog{now: time.Now}
}

// Append records one entry. It is the only mutator — the log is append-only (no edit, no
// delete), which is what makes it a trustworthy trail.
func (l *AuditLog) Append(rec AuditRecord) {
	if rec.Time.IsZero() {
		rec.Time = l.clock()
	}
	if len(rec.Result) > maxAuditResultBytes {
		rec.Result = rec.Result[:maxAuditResultBytes]
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, rec)
}

// RecordCall is the executor-facing convenience that builds and appends a record from a
// call, its decision, and its result in one step.
func (l *AuditLog) RecordCall(call domain.ToolCall, decision AuditDecision, reason string, result domain.ToolResult) {
	l.Append(AuditRecord{
		Tool:     call.Tool,
		CallID:   call.ID,
		Decision: decision,
		Reason:   reason,
		IsError:  result.IsError,
		Result:   result.Content,
	})
}

// Records returns a copy of the log in append order, so a caller can read the trail
// without racing the next append or mutating internal storage.
func (l *AuditLog) Records() []AuditRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]AuditRecord, len(l.records))
	copy(out, l.records)
	return out
}

// Len reports how many records the log holds.
func (l *AuditLog) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.records)
}

// clock returns the current time via the injected clock (wall clock by default).
func (l *AuditLog) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

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

// DefaultAuditCap is the default number of records an AuditLog retains. A long-running
// session emits one record per tool call, so an uncapped log would grow without bound;
// the cap keeps memory bounded while retaining the most recent window of the trail. When
// the cap is exceeded the OLDEST records are dropped (a ring buffer) and a dropped-count
// is incremented so the overflow is observable rather than silent.
const DefaultAuditCap = 10000

// AuditLog is a bounded, in-memory record of tool calls and their disposition. It is safe
// for concurrent append/read. The log is the guardrail observability spine — it records
// every call, its decision, and its result, in order. It retains at most cap records:
// once full, each new append evicts the oldest (a ring buffer) and bumps Dropped, so the
// trail stays the most-recent window and the eviction is observable. (Persisting the full
// trail to disk is a later concern; v1 keeps a bounded window in-memory, snapshot-shippable.)
type AuditLog struct {
	mu      sync.Mutex
	records []AuditRecord    // ring buffer of up to cap entries, oldest-first via head
	head    int              // index of the oldest record when the ring is full
	full    bool             // whether the ring has wrapped (len(records)==cap)
	dropped uint64           // count of records evicted by the cap (observability)
	cap     int              // maximum retained records
	now     func() time.Time // injectable clock for deterministic tests
}

// NewAuditLog returns an empty audit log using the wall clock, retaining the default cap
// of records.
func NewAuditLog() *AuditLog {
	return NewAuditLogWithCap(DefaultAuditCap)
}

// NewAuditLogWithCap returns an empty audit log retaining at most cap records (a
// non-positive cap falls back to DefaultAuditCap). Exposed so tests can exercise the cap
// + dropped-count on a small ring without emitting DefaultAuditCap records.
func NewAuditLogWithCap(cap int) *AuditLog {
	if cap <= 0 {
		cap = DefaultAuditCap
	}
	return &AuditLog{now: time.Now, cap: cap}
}

// Append records one entry. The log is append-only in spirit (no edit, no per-record
// delete) but BOUNDED: once cap records are held, appending evicts the oldest and
// increments Dropped, so memory stays bounded and the eviction is observable.
func (l *AuditLog) Append(rec AuditRecord) {
	if rec.Time.IsZero() {
		rec.Time = l.clock()
	}
	if len(rec.Result) > maxAuditResultBytes {
		rec.Result = rec.Result[:maxAuditResultBytes]
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.full {
		l.records = append(l.records, rec)
		if len(l.records) == l.cap {
			l.full = true // ring is now full; subsequent appends overwrite oldest-first
		}
		return
	}
	// Ring is full: overwrite the oldest slot, advance head, and count the eviction.
	l.records[l.head] = rec
	l.head = (l.head + 1) % l.cap
	l.dropped++
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

// Records returns a copy of the retained trail in append (oldest-to-newest) order, so a
// caller can read it without racing the next append or mutating internal storage. Once the
// cap has been exceeded this is the most-recent window only; Dropped reports how many older
// records were evicted.
func (l *AuditLog) Records() []AuditRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]AuditRecord, len(l.records))
	if !l.full {
		copy(out, l.records)
		return out
	}
	// Unroll the ring from the oldest slot (head) so the copy is in append order.
	n := copy(out, l.records[l.head:])
	copy(out[n:], l.records[:l.head])
	return out
}

// Len reports how many records the log currently retains (at most cap).
func (l *AuditLog) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.records)
}

// Dropped reports how many records the cap has evicted (overflowed past the ring). It is
// the observability signal that the trail is a most-recent window, not the full history.
func (l *AuditLog) Dropped() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.dropped
}

// clock returns the current time via the injected clock (wall clock by default).
func (l *AuditLog) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

package domain

// ----------------------------------------------------------------------------
// Events (ADR 0001 — consumed as Go values in-process)
// ----------------------------------------------------------------------------

// EventSink receives typed Events as the loop produces them, including *inside* a
// Step (streaming). The TUI adapts these to Bubble Tea messages; the bench consumes
// them as Go values. Emit must not block the loop for long — fan out if needed.
type EventSink interface {
	Emit(Event)
}

// Event is the sealed sum type of everything the loop reports. It is sealed (an
// unexported marker) so the variant set stays owned by Apogee and additively
// versioned; external code switches on the concrete types but cannot add variants.
type Event interface {
	eventDepth() int // sealing marker; also carries sub-agent nesting depth
}

// EventBase is embedded in every Event variant. Depth is the sub-agent nesting
// level (0 = top-level agent); a sub-agent's events nest into the parent's stream
// with Depth > 0 (ADR 0005). Turn is the Turn index the event belongs to.
//
// It is exported so the engine and other internal subsystems can construct Event
// variants (setting Turn/Depth), but it is deliberately NOT re-exported by the root
// facade: the sealing method eventDepth() stays unexported in this package, so no
// package outside internal/* can satisfy Event — the variant set remains closed.
type EventBase struct {
	Depth int
	Turn  int
}

func (b EventBase) eventDepth() int { return b.Depth }

// TokenEvent is one streamed chunk of assistant text. The tokens streamed for a Turn may be
// superseded by a StreamResetEvent (the loop re-streamed the Turn on an ActionRetry):
// accumulate TokenEvents per Turn and discard the accumulation when a reset arrives.
type TokenEvent struct {
	EventBase
	Text string
}

// StreamResetEvent signals that the assistant tokens streamed for the current Turn since the
// last boundary are superseded and must be discarded — the loop is re-streaming the Turn
// because an ActionRetry post-response decision re-called the Upstream. A streaming observer
// (the TUI) clears its in-progress token buffer for the Turn on this event; the MessageEvent
// that ends the Turn carries the final, accepted text.
type StreamResetEvent struct {
	EventBase
}

// MessageEvent is a completed assistant message (the no-tool turn ends an Exchange).
type MessageEvent struct {
	EventBase
	Text string
}

// ToolCallEvent reports that the model requested a tool call (post-parse).
type ToolCallEvent struct {
	EventBase
	Call ToolCall
}

// ToolResultEvent reports a tool's result after execution (and after any
// post-tool-result Mechanisms have acted on it).
type ToolResultEvent struct {
	EventBase
	Result ToolResult
}

// ApprovalEvent reports that an Approval was requested/decided for a tool call.
// (The decision is obtained synchronously via the Approver; this event is for
// observers — TUI display, bench accounting.)
type ApprovalEvent struct {
	EventBase
	Request  ApprovalRequest
	Decision ApprovalDecision
}

// MechanismFiredEvent reports that a Mechanism (or experimental hook) fired at a
// hook point — the observability spine for self-regulation and bench attribution.
type MechanismFiredEvent struct {
	EventBase
	Mechanism MechanismID
	Hook      HookPoint
	Action    string // e.g. the PostResponseDecision taken, or "suppressed"
}

// ErrorEvent reports a localised, recovered fault — a tool or Mechanism panic
// caught at the extension boundary, or a tool execution error (ADR 0007). It does
// not imply the loop stopped.
type ErrorEvent struct {
	EventBase
	Source string // tool name / mechanism ID / "loop"
	Err    string
}

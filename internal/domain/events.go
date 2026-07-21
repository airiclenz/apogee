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

// ReasoningEvent is one newly-revealed chunk of the model's reasoning channel — the
// observability seam for "the model is thinking", which the visible TokenEvent stream by
// design never shows. It is emitted for BOTH reasoning paths: the provider's native
// channel (reasoning_content) and an inline <think>/harmony span held off the visible
// stream. Chunks arrive in order and concatenate to the reasoning the Turn's assistant
// message preserves; a Turn that reasons without emitting visible text produces
// ReasoningEvents and no TokenEvents.
//
// It is OBSERVATION ONLY: it never changes history or what the model receives. The
// reasoning channel is already preserved on the committed assistant message
// (reasoning_content), so an observer that ignores this event loses nothing but liveness.
// Arrival alone is a usable signal — a UI may render "thinking" from the event and never
// read Text at all.
//
// Text is untrusted model output. Any consumer that DISPLAYS it must escape-strip it
// exactly as the TUI's token path (transcript.appendToken) does before it reaches a
// terminal; the raw chunk may carry ESC bytes.
type ReasoningEvent struct {
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

// UsageEvent reports the token accounting an Upstream reply carried — the prompt
// (context) tokens, the generated completion tokens, and their total — once a Turn's
// stream reaches its terminal Done. A server that omits usage emits no UsageEvent, so an
// observer that never sees one simply has no token counts (the zero state). It is the
// observability spine for the live context-usage gauge and a tokens/sec readout: an
// observer reads the latest Depth-0 UsageEvent for the current context fill and times the
// completion against its own clock for throughput. Like every variant it nests by Depth, so
// a sub-agent's usage reaches the parent's observer at its nesting level.
type UsageEvent struct {
	EventBase
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// AuditEvent surfaces one append-only audit record — a tool call, the guardrail
// decision it cleared/was blocked by, and whether its result errored — to the
// EventSink as it is recorded, so the audit trail is OBSERVABLE (and snapshot- or
// log-shippable) rather than living only in a volatile in-process ring no observer
// reads (security-review M1). Because a sub-agent emits through the parent's EventSink
// at Depth > 0, a delegated call's audit record reaches the same observer at its nesting
// depth instead of vanishing with the discarded child Agent.
//
// The payload mirrors security.AuditRecord but is expressed in domain-only types: the
// agent layer (which imports both domain and security) constructs it, so domain keeps
// its no-upward-dependency property (ADR 0010). Decision is the audit decision as a
// string (e.g. "allowed", "dangerous-refused", "circuit-tripped").
type AuditEvent struct {
	EventBase
	Tool     string
	CallID   string
	Decision string
	Reason   string // the guardrail reason, if any
	IsError  bool   // whether the recorded result was a tool-level error
}

package domain

// ----------------------------------------------------------------------------
// Events (ADR 0001 — consumed as Go values in-process)
// ----------------------------------------------------------------------------

// EventSink receives typed Events as the loop produces them, including *inside* a
// Step (streaming). The TUI adapts these to Bubble Tea messages; the bench consumes
// them as Go values. Emit must not block the loop for long — fan out if needed.
//
// A sink NEVER has to be safe for concurrent use: the engine serializes emission on its side,
// so an implementation sees one Emit at a time, each happening-after the last. That guarantee
// survives a concurrent depth-0 sub-agent fan-out (ADR 0039), where several children emit at
// once — the engine funnels them through one seam and the sink still receives a LINEAR stream.
// What a driver must not assume is that a linear stream is a serial one: concurrent emitters
// interleave in an unspecified order, so events belonging to one agent are recognised by their
// identity (EventBase.Depth and EventBase.CallID), never by contiguity.
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
// variants (setting Turn/Depth/CallID), but it is deliberately NOT re-exported by the
// root facade: the sealing method eventDepth() stays unexported in this package, so no
// package outside internal/* can satisfy Event — the variant set remains closed.
type EventBase struct {
	Depth int
	Turn  int
	// CallID is the RUN IDENTITY of the agent that emitted the event: the id of the
	// sub_agent tool call that spawned it. It is stamped once, at that agent's
	// construction, and every event the agent emits carries it — including the events of
	// the tools it runs — so an observer can attribute a delegated event to the delegation
	// that asked for it. It is empty at Depth 0: the top-level agent was spawned by no call.
	//
	// Depth alone cannot do this once children run CONCURRENTLY (ADR 0039): two siblings
	// spawned by one reply share a depth, so a depth-keyed observer would braid their
	// streams together. The call id is unique per spawning call, so it separates them, and
	// it also identifies the tool call whose result the run will become — the same id the
	// parent's ToolCallEvent and ToolResultEvent carry for that delegation.
	//
	// It names the SPAWNING call and never the event's own subject. A variant that also
	// reports a call of its OWN — AuditEvent's audited call, ToolResultEvent's completed
	// call — carries that one in its own member; AuditEvent's is named CallID too and
	// therefore SHADOWS this field, so the spawning id is reached there as
	// ev.EventBase.CallID.
	CallID string
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
// ReasoningEvents and no TokenEvents. The concatenation is a liveness view, not a
// byte-exact copy: on the inline path a channel token split across deltas is revealed as
// span text while it accumulates, so the chunks can carry a partial closer (e.g.
// "secret</thi") that the completed token later removes from the preserved reasoning.
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

// SubAgentPhase names the point in a delegation's life that a SubAgentPhaseEvent reports.
type SubAgentPhase string

const (
	// SubAgentStarted reports that the delegation's child agent has BEGUN RUNNING — the instant a
	// pool worker dequeued it, or, on the serial path, the instant the recursion point was entered.
	// A delegation whose ToolCallEvent has arrived but whose start has not is queued behind the
	// Parallel agents cap: it holds no slot yet and has produced nothing.
	SubAgentStarted SubAgentPhase = "started"
	// SubAgentFinished reports that the child reached its boundary and its result is known. The
	// result rides the event, so an observer can show THAT delegation's report the moment it lands
	// instead of waiting for the group's trailing result burst.
	SubAgentFinished SubAgentPhase = "finished"
)

// SubAgentPhaseEvent reports one delegation crossing a lifecycle boundary: its child starting, and
// its child finishing with the result the parent will eventually see. It exists because the
// ToolResultEvents of a delegation GROUP are deliberately not a liveness signal — they burst
// together, in emitted-call order, after every child has joined (ADR 0039 decision 4) — so an
// observer reading them alone cannot tell a queued delegation from a running one, and cannot show
// an early-finishing member as done while its siblings still run. This variant carries exactly
// that missing timing, and carries it ADDITIVELY: the ToolCallEvent burst, the commit order, and
// the resulting history are untouched.
//
// It is OBSERVATION ONLY. An observer that ignores it loses liveness and nothing else: the result
// still arrives on the delegation's own ToolResultEvent, which remains the authoritative one — so a
// consumer that applies the payload here must tolerate seeing the same result again.
//
// Its EventBase is the CHILD RUN's identity rather than the emitting parent's: Depth is the child's
// nesting level and CallID the id of the sub_agent call that spawned it — the same stamp every
// event the child itself emits carries, and the id of the parent's tool-call block the phase is
// about.
//
// Result is the child's ToolResult on SubAgentFinished and the zero value on SubAgentStarted. A
// delegation the human CANCELLED emits no finished phase at all: the cancelled group is dropped
// unappended and never becomes a result, so its phase pair stays open exactly as its tool call does.
type SubAgentPhaseEvent struct {
	EventBase
	Phase  SubAgentPhase
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
//
// The Cumulative* fields carry the EMITTING agent's running totals for the whole session —
// every completion it has accounted for, itself included — so a Driver reports session usage
// by keeping the LATEST event per agent instead of summing a stream it may have joined late
// or partially. They obey the same latest-wins rule as the fill fields above. They are
// per-emitting-agent: a sub-agent counts only its OWN calls (it starts from zero and its
// totals are never folded into the parent's), so an observer groups them by the Depth and
// CallID stamps every event already carries and sums the agents it wants.
//
// Maintenance marks an accounting event that is NOT a Turn's completion — today the
// Compaction call, whose tokens are real but whose prompt/completion counts describe the
// summarizer's own request rather than the conversation's current fill. A reader of the
// live gauge or a tokens/sec clock MUST skip a Maintenance event; a reader of the
// cumulative totals accepts it, which is what keeps session usage honest across a fold.
//
// Model is the model the EMITTING agent is bound to — the id it puts on the wire, not a
// display spelling — so a reading says which model produced it as well as how big it was. It
// rides here rather than on a lifecycle event because it is a property OF the reading: a
// delegation routed to the Sub-agent server (ADR 0045) fills a window on a model of its own,
// and a Driver that paints the child's fill can name that model beside it without tracking a
// second stream. An agent bound late (before the first heartbeat) leaves it empty, which is
// the same absence a Driver already tolerates in its own footer.
//
// ContextWindow is the window that fill sits in — the EMITTING agent's own bound window, on
// Model's terms exactly and for the same reason: a routed sub-agent works against the Delegation
// target's window (ADR 0045), which may be a fraction of the session's, so a fill painted against
// the session's limit would be a wrong number rather than a missing one. 0 means the emitting
// agent knows no window (none discovered, none pinned), and a Driver falls back to its own —
// which is what every unrouted reading amounts to, the child inheriting the parent's window
// verbatim.
type UsageEvent struct {
	EventBase
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Model            string
	ContextWindow    int

	CumulativePromptTokens     int
	CumulativeCompletionTokens int
	CumulativeTotalTokens      int
	CumulativeCalls            int

	Maintenance bool
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
	Tool string
	// CallID is the AUDITED call's id — the tool call this record is about. It shadows
	// EventBase.CallID, which is a different fact (the sub_agent call that spawned the
	// emitting agent, empty at Depth 0): both travel, and the spawning one is reached as
	// ev.EventBase.CallID. Pinned by TestAuditEventCallIDShadowsTheSpawningCall.
	CallID   string
	Decision string
	Reason   string // the guardrail reason, if any
	IsError  bool   // whether the recorded result was a tool-level error
}

package domain

import "encoding/json"

// ----------------------------------------------------------------------------
// Hook working values & shared substrate (docs/design/hook-mutation-api.md)
// ----------------------------------------------------------------------------
//
// Request, Response, and Conversation are the loop's working values exposed to
// hooks. They stay opaque structs with method-only surfaces so the internal
// representation and the variant set remain Apogee-owned and additively versioned
// (ADR 0001): a hook reads Message snapshots and mutates by index against the owning
// container, never touching the backing storage. The operation set is scoped from
// apogee-sim's real Transform / Injector / Intervention footprint — not speculation
// (TDD §6.2). The bodies are panic stubs until the hook-mutation API lands (P1.5).

// Role is a conversation message's role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a read-only snapshot of one conversation message handed to hooks. A hook
// reads Messages and mutates by index against the owning container (Request /
// Conversation); it never holds the loop's backing storage.
type Message struct {
	Role       Role
	Content    string
	ToolCalls  []ToolCall // RoleAssistant only
	ToolCallID string     // RoleTool only — links the result to its ToolCall.ID
}

// Extra reports a preserved unknown wire field on the message (reasoning_content,
// tool_choice, thinking, …). Round-trip preservation of these is load-bearing for
// snapshot/resume and the bench's fork, so they survive a history rewrite.
func (m Message) Extra(key string) (json.RawMessage, bool) { panic("sketch: not implemented") }

// ToolDef is one entry of the tool menu the model sees.
type ToolDef struct {
	Name        string
	Description string
	Schema      json.RawMessage // JSON-schema of arguments
}

// Budget is the read-only context-budget view a hook reads to gate token-sensitive
// behaviour (e.g. Library injection backs off as the window fills).
type Budget struct {
	ContextLimit  int
	Used          int     // estimated tokens used so far
	CharsPerToken float64 // model-specific estimate
}

// LoopView is the read-only window every hook has onto loop state beyond its own
// mutable value — the conversation so far, the tool menu, the budget, the Turn index,
// and a self-regulation query. It is the home of all cross-Turn reads: most
// Mechanisms decide by aggregating across Turns, so the primary mutable value (a
// *Response, *ToolCall, *ToolResult) is never sufficient alone. Request and Response
// expose it via their View method; the tool-stage hooks receive it as an argument.
type LoopView interface {
	Conversation() ConversationView
	Tools() []ToolDef
	Budget() Budget
	Turn() int
	// Fired reports how many times a Mechanism has fired this Session — the seam for
	// cross-Mechanism coupling (e.g. decompose muting itself once a read-loop
	// Mechanism has fired) without a shared mutable meta map.
	Fired(id MechanismID) int
}

// ConversationView is read-only history with the tool-call/result pairing helpers
// every history-inspecting Mechanism needs: the tool name and arguments live only on
// the originating ToolCall, never on the tool-result message, so resolving a result
// back to its call is mandatory for error-handling Mechanisms.
type ConversationView interface {
	Len() int
	At(i int) Message
	Range(fn func(i int, m Message) bool)
	// LastUser returns the most recent user message and its index.
	LastUser() (msg Message, index int, ok bool)
	// CallByID resolves a tool result to its originating call (for the name/args).
	CallByID(id string) (call ToolCall, index int, ok bool)
	// ResultFor resolves a tool call to its result message.
	ResultFor(callID string) (msg Message, index int, ok bool)
}

// Request is the outgoing Upstream request a pre-request hook may shape. Reads go
// through View; mutations are the characterised operation set from apogee-sim's
// pre-request Mechanisms.
type Request struct {
	// unexported: messages, tools menu, budgeted context, params, extras
}

// View exposes the read-only conversation/tools/budget window.
func (r *Request) View() LoopView { panic("sketch: not implemented") }

// Model is the target model id (the Library keys its lookup on this).
func (r *Request) Model() string { panic("sketch: not implemented") }

// Extra reports a preserved unknown request field (e.g. a grammar Mechanism checks
// for an existing response_format before setting one).
func (r *Request) Extra(key string) (json.RawMessage, bool) { panic("sketch: not implemented") }

// AppendToSystem appends text to the first system message (creating one if absent),
// but is a no-op if marker already occurs there — the idempotent inject the nudge
// Mechanisms (library, cot, decompose) share. Reports whether it injected.
func (r *Request) AppendToSystem(marker, text string) (injected bool) {
	panic("sketch: not implemented")
}

// InjectContext inserts a user message at the role-safe position: before the last
// user message, or appended to the system prompt if the conversation ends in a tool
// result (a user message after a tool result breaks strict chat templates).
func (r *Request) InjectContext(text string) { panic("sketch: not implemented") }

// SetMessageContent edits one message's content in place by index — tool-result
// capping and history-collapse of older messages. An out-of-range index is a no-op.
func (r *Request) SetMessageContent(index int, content string) { panic("sketch: not implemented") }

// SetTools replaces and reorders the tool menu (the tool-filter Mechanism).
func (r *Request) SetTools(tools []ToolDef) { panic("sketch: not implemented") }

// SetExtra sets an unknown request field, allocating the carrier if needed (e.g. a
// grammar constraint sets response_format).
func (r *Request) SetExtra(key string, v json.RawMessage) { panic("sketch: not implemented") }

// SetSampling overrides sampling parameters. Forward-looking — no current Mechanism
// mutates these; included so the surface need not change to add one.
func (r *Request) SetSampling(p SamplingParams) { panic("sketch: not implemented") }

// SamplingParams are the optional sampling overrides a pre-request hook may set; a
// nil field leaves the loop's value untouched.
type SamplingParams struct {
	Temperature *float64
	MaxTokens   *int
}

// Response is the model response a post-response hook inspects and may intercept.
type Response struct {
	// unexported: raw text, parsed tool calls, finish reason, thinking channel
}

// View exposes the read-only conversation/tools/budget window — response-repair
// Mechanisms validate tool calls against the menu; loop detection reads history.
func (r *Response) View() LoopView { panic("sketch: not implemented") }

// Text is the assistant's raw text content.
func (r *Response) Text() string { panic("sketch: not implemented") }

// ToolCalls are the parsed tool calls the model requested.
func (r *Response) ToolCalls() []ToolCall { panic("sketch: not implemented") }

// FinishReason is the model's stop reason.
func (r *Response) FinishReason() FinishReason { panic("sketch: not implemented") }

// Thinking is the harmony/thinking channel content when the model and parser expose
// it (ok == false when there is none).
func (r *Response) Thinking() (text string, ok bool) { panic("sketch: not implemented") }

// SetText replaces the assistant text — the intercept path (ActionIntercept).
func (r *Response) SetText(s string) { panic("sketch: not implemented") }

// SetToolCallArguments rewrites one tool call's arguments in place — the auto-fix
// Mechanism writing back repaired/formatted content (ActionIntercept).
func (r *Response) SetToolCallArguments(index int, args json.RawMessage) {
	panic("sketch: not implemented")
}

// FinishReason is the model's stop reason; the set is open (treat unknown values
// defensively).
type FinishReason string

const (
	FinishStop      FinishReason = "stop"
	FinishLength    FinishReason = "length"
	FinishToolCalls FinishReason = "tool_calls"
)

// Conversation is the serializable conversation state a history-rewrite hook edits.
// It is a cleanly copyable value with no live handles (ADR 0001) — what lets the
// bench fork by deep-copying it and the user resume from a snapshot. Summaries are
// not a separate structure: they are ordinary messages produced by generative
// Compaction (context/) and written back via Replace. A deferred Response Action
// (ActionDefer) is held here so it survives a snapshot/resume boundary.
type Conversation struct {
	// unexported: messages, deferred response actions
}

// Len reports the number of messages.
func (c *Conversation) Len() int { panic("sketch: not implemented") }

// At returns the message at index i.
func (c *Conversation) At(i int) Message { panic("sketch: not implemented") }

// Range iterates messages until fn returns false.
func (c *Conversation) Range(fn func(i int, m Message) bool) { panic("sketch: not implemented") }

// PrefixEnd is the index past the leading system messages and the first user message
// — the protected prefix a truncation must keep.
func (c *Conversation) PrefixEnd() int { panic("sketch: not implemented") }

// AssistantBoundaries are the indices of assistant messages — the only safe cut
// points, because a tool result must stay adjacent to the assistant call that
// produced it (strict chat templates).
func (c *Conversation) AssistantBoundaries() []int { panic("sketch: not implemented") }

// SetMessageContent edits one message's content in place by index.
func (c *Conversation) SetMessageContent(i int, content string) { panic("sketch: not implemented") }

// DropRange drops messages in [start, end) — history truncation drops the middle,
// keeping the prefix and a recent tail.
func (c *Conversation) DropRange(start, end int) { panic("sketch: not implemented") }

// Insert places a message at index i — e.g. a static gap note at a truncation cut.
func (c *Conversation) Insert(i int, m Message) { panic("sketch: not implemented") }

// Replace swaps the entire message list — generative Compaction writes its
// summarised history back through here.
func (c *Conversation) Replace(msgs []Message) { panic("sketch: not implemented") }

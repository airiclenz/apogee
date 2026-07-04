package domain

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

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
// (TDD §6.2).
//
// The exported constructors and the State / Messages drains here (NewRequest,
// Request.State, NewResponse, NewConversation, Conversation.Messages, Defer /
// TakeDeferred) are the ENGINE SEAM: internal/agent builds these values from loop
// state and reads the post-hook result back through them. They are exported only
// because the engine lives in a sibling package; they are deliberately NOT re-exported
// by the root facade, so they carry no public-API promise and a hook never needs them.

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

	// extra carries preserved unknown wire fields (reasoning_content, tool_choice,
	// thinking, …) read through Extra. It is populated by Message's own JSON decoder
	// (UnmarshalJSON collects unknown siblings) and by WithExtra; a Message built as a
	// plain literal carries none.
	extra map[string]json.RawMessage
}

// Extra reports a preserved unknown wire field on the message (reasoning_content,
// tool_choice, thinking, …). Round-trip preservation of these is load-bearing for
// snapshot/resume and the bench's fork, so they survive a history rewrite.
func (m Message) Extra(key string) (json.RawMessage, bool) {
	v, ok := m.extra[key]
	return v, ok
}

// WithExtra returns a copy of m carrying an additional preserved wire field under key.
// The engine attaches the model's reasoning channel (reasoning_content) to a committed
// assistant message this way, so it survives snapshot/resume; an empty key or value is a
// no-op. It copies the extra set, so a caller already holding the original Message is
// unaffected.
func (m Message) WithExtra(key string, v json.RawMessage) Message {
	if key == "" || len(v) == 0 {
		return m
	}
	next := make(map[string]json.RawMessage, len(m.extra)+1)
	for k, val := range m.extra {
		next[k] = val
	}
	next[key] = v
	m.extra = next
	return m
}

// messageJSON is the canonical on-wire shape of a Message's known fields. The unknown
// sibling fields in extra are flattened alongside these at the top level (not nested), so
// a serialized message matches the OpenAI chat shape a provider emits and a future field
// round-trips untouched.
type messageJSON struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// messageKnownKeys are the top-level JSON keys messageJSON owns; UnmarshalJSON strips them
// so only genuinely-unknown siblings land in extra. Kept in sync with messageJSON's tags.
var messageKnownKeys = []string{"role", "content", "tool_calls", "tool_call_id"}

// isKnownMessageKey reports whether key is one messageJSON owns (so a same-named extra entry
// is skipped on encode — the known field always wins a collision).
func isKnownMessageKey(key string) bool {
	for _, k := range messageKnownKeys {
		if k == key {
			return true
		}
	}
	return false
}

// MarshalJSON serializes the Message as its known wire fields with any preserved Extra
// fields flattened alongside them. Known fields win on a key collision, so a stale extra
// entry can never shadow a real field. A Message with no extras takes the fast path and
// marshals straight from messageJSON.
//
// The preserved siblings are spliced on in sorted key order rather than via a map marshal,
// so the wire bytes are deterministic regardless of Go's map iteration order — snapshots
// containing reasoning_content (or any other Extra) are byte-reproducible, which a later
// snapshot diff/hash relies on.
func (m Message) MarshalJSON() ([]byte, error) {
	known, err := json.Marshal(messageJSON{
		Role:       m.Role,
		Content:    m.Content,
		ToolCalls:  m.ToolCalls,
		ToolCallID: m.ToolCallID,
	})
	if err != nil {
		return nil, err
	}
	if len(m.extra) == 0 {
		return known, nil
	}

	// Collect the genuinely-unknown, non-empty siblings and sort for a stable key order.
	keys := make([]string, 0, len(m.extra))
	for k, v := range m.extra {
		if !isKnownMessageKey(k) && len(v) > 0 {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return known, nil
	}
	sort.Strings(keys)

	// Splice the siblings onto the known object. messageJSON always emits at least "role"
	// (no omitempty), so known is never "{}" and dropping its closing brace then appending
	// ",key:value" pairs is always well-formed.
	var buf bytes.Buffer
	buf.Write(known[:len(known)-1]) // drop the closing '}'
	for _, k := range keys {
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.WriteByte(',')
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(m.extra[k])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON restores a Message, decoding the known fields and collecting any unknown
// sibling fields into the preserved Extra set so they survive a snapshot round-trip.
func (m *Message) UnmarshalJSON(data []byte) error {
	var known messageJSON
	if err := json.Unmarshal(data, &known); err != nil {
		return err
	}
	m.Role = known.Role
	m.Content = known.Content
	m.ToolCalls = known.ToolCalls
	m.ToolCallID = known.ToolCallID

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	for _, k := range messageKnownKeys {
		delete(all, k)
	}
	if len(all) > 0 {
		m.extra = all
	} else {
		m.extra = nil
	}
	return nil
}

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
	// Fired reports how many times a Mechanism has ACTED this Session (R4): an
	// invocation is booked only when it mutated its working value or returned a
	// non-zero post-response Action — an inspect-and-do-nothing invocation is not a
	// fire. It is the seam for cross-Mechanism coupling (e.g. decompose muting itself
	// once a read-loop Mechanism has fired) without a shared mutable meta map. An
	// experimental hook's synthetic ID keeps counting every invocation (bench
	// observability).
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

// ----------------------------------------------------------------------------
// Request — the pre-request hook's working value
// ----------------------------------------------------------------------------

// Request is the outgoing Upstream request a pre-request hook may shape. Reads go
// through View; mutations are the characterised operation set from apogee-sim's
// pre-request Mechanisms. The loop builds one with NewRequest, hands it to every
// pre-request hook (their mutations compose), then drains it with State to project
// onto the provider wire shape.
type Request struct {
	model    string
	messages []Message
	tools    []ToolDef
	budget   Budget
	turn     int
	fired    map[MechanismID]int
	sampling SamplingParams
	extras   map[string]json.RawMessage
	revision int // bumped by each mutator — the acted-fire probe (R4), read via Revision
}

// NewRequest builds the pre-request working value from loop state (engine seam). The
// messages and tools slices are copied, so a hook mutating the Request never reaches
// back into the loop's conversation storage. fired, in contrast, is shared BY REFERENCE:
// it is the loop's live per-Session fire ledger LoopView.Fired reads, so a Mechanism can
// see a peer's fire from earlier in the same hook pass (the decompose↔read_loop coupling
// seam). It is only ever read through the view — no view operation mutates it — so the
// shared reference is safe. nil is fine (Fired then reports 0 for every Mechanism).
func NewRequest(model string, messages []Message, tools []ToolDef, budget Budget, turn int, fired map[MechanismID]int) *Request {
	return &Request{
		model:    model,
		messages: append([]Message(nil), messages...),
		tools:    append([]ToolDef(nil), tools...),
		budget:   budget,
		turn:     turn,
		fired:    fired,
	}
}

// RequestState is the post-hook state of a Request the loop reads to build the
// provider request (engine seam). Hooks shape the Request through its mutators and
// never call State.
type RequestState struct {
	Model    string
	Messages []Message
	Tools    []ToolDef
	Sampling SamplingParams
	Extras   map[string]json.RawMessage
}

// State returns the Request's current state after any hook mutations (engine seam).
// The slices and the extras map are copies, so the loop's projection cannot disturb
// the Request and a later hook (none run after the drain today) would still see a
// faithful value.
func (r *Request) State() RequestState {
	return RequestState{
		Model:    r.model,
		Messages: append([]Message(nil), r.messages...),
		Tools:    append([]ToolDef(nil), r.tools...),
		Sampling: r.sampling,
		Extras:   cloneRawMap(r.extras),
	}
}

// View exposes the read-only conversation/tools/budget window.
func (r *Request) View() LoopView {
	return loopView{messages: r.messages, tools: r.tools, budget: r.budget, turn: r.turn, fired: r.fired}
}

// Model is the target model id (the Library keys its lookup on this).
func (r *Request) Model() string { return r.model }

// Revision reports how many mutations have been applied to the Request — the loop's
// acted-fire probe (R4, engine seam): hookrun snapshots it around each catalogued fire
// and books the fire only when the counter moved. A hook never needs it.
func (r *Request) Revision() int { return r.revision }

// Extra reports a preserved unknown request field (e.g. a grammar Mechanism checks
// for an existing response_format before setting one).
func (r *Request) Extra(key string) (json.RawMessage, bool) {
	v, ok := r.extras[key]
	return v, ok
}

// AppendToSystem appends text to the first system message (creating one if absent),
// but is a no-op if marker already occurs there — the idempotent inject the nudge
// Mechanisms (library, cot, decompose) share. Reports whether it injected. The caller
// embeds marker within text so a second call with the same marker is a no-op.
func (r *Request) AppendToSystem(marker, text string) (injected bool) {
	if i := firstIndex(r.messages, RoleSystem); i >= 0 && strings.Contains(r.messages[i].Content, marker) {
		return false
	}
	r.appendOrCreateSystem(text)
	r.revision++
	return true
}

// InjectContext inserts a user message at the role-safe position: appended to the
// system prompt if the conversation ends in a tool result (a user message after a
// tool result breaks strict chat templates); appended at the end if it ends in an
// assistant message (the retry-exchange shape — the correction answers the superseded
// assistant message it follows, R1); otherwise inserted before the last user message.
// With no user message present it appends at the end.
func (r *Request) InjectContext(text string) {
	r.revision++
	if n := len(r.messages); n > 0 && r.messages[n-1].Role == RoleTool {
		r.appendOrCreateSystem(text)
		return
	}
	msg := Message{Role: RoleUser, Content: text}
	if n := len(r.messages); n > 0 && r.messages[n-1].Role == RoleAssistant {
		r.messages = append(r.messages, msg)
		return
	}
	idx := lastIndex(r.messages, RoleUser)
	if idx < 0 {
		r.messages = append(r.messages, msg)
		return
	}
	r.messages = insertMessage(r.messages, idx, msg)
}

// AppendSupersededAssistant appends a superseded assistant message (text + tool calls)
// to the end of the request — the loop's retry-exchange seam (engine seam, R1), NOT a
// hook-mutation primitive: on an ActionRetry correction the loop appends the response
// it is retrying, then the correction via InjectContext, so the re-streamed request
// carries the exchange the sim's retry builders carried. The append is request-scoped
// — it is never committed to history. A wholly empty superseded response (empty text,
// no calls) appends nothing. calls is copied, so the caller's slice stays independent.
func (r *Request) AppendSupersededAssistant(text string, calls []ToolCall) {
	if text == "" && len(calls) == 0 {
		return
	}
	r.messages = append(r.messages, Message{
		Role:      RoleAssistant,
		Content:   text,
		ToolCalls: append([]ToolCall(nil), calls...),
	})
}

// SetMessageContent edits one message's content in place by index — tool-result
// capping and history-collapse of older messages. An out-of-range index is a no-op.
func (r *Request) SetMessageContent(index int, content string) {
	if index < 0 || index >= len(r.messages) {
		return
	}
	r.messages[index].Content = content
	r.revision++
}

// SetTools replaces and reorders the tool menu (the tool-filter Mechanism). The slice
// is copied so the caller cannot mutate the menu after the call.
func (r *Request) SetTools(tools []ToolDef) {
	r.tools = append([]ToolDef(nil), tools...)
	r.revision++
}

// SetExtra sets an unknown request field, allocating the carrier if needed (e.g. a
// grammar constraint sets response_format).
func (r *Request) SetExtra(key string, v json.RawMessage) {
	if r.extras == nil {
		r.extras = make(map[string]json.RawMessage)
	}
	r.extras[key] = v
	r.revision++
}

// SetSampling overrides sampling parameters. Forward-looking — no current Mechanism
// mutates these; included so the surface need not change to add one.
func (r *Request) SetSampling(p SamplingParams) {
	r.sampling = p
	r.revision++
}

// appendOrCreateSystem appends text to the first system message, creating one at the
// front of the conversation if none exists.
func (r *Request) appendOrCreateSystem(text string) {
	if i := firstIndex(r.messages, RoleSystem); i >= 0 {
		if r.messages[i].Content == "" {
			r.messages[i].Content = text
		} else {
			r.messages[i].Content += "\n\n" + text
		}
		return
	}
	r.messages = append([]Message{{Role: RoleSystem, Content: text}}, r.messages...)
}

// SamplingParams are the optional sampling overrides a pre-request hook may set; a
// nil field leaves the loop's value untouched.
type SamplingParams struct {
	Temperature *float64
	MaxTokens   *int
}

// ----------------------------------------------------------------------------
// Response — the post-response hook's working value
// ----------------------------------------------------------------------------

// Response is the model response a post-response hook inspects and may intercept. The
// loop builds one with NewResponse from the parsed Upstream reply; reads go through
// the accessors, and ActionIntercept is expressed by mutating in place.
type Response struct {
	text         string
	thinking     string
	toolCalls    []ToolCall
	finishReason FinishReason
	view         LoopView
	revision     int // bumped by each mutator — the acted-fire probe (R4), read via Revision
}

// NewResponse builds the post-response working value from the parsed reply (engine
// seam). view is the read window onto the conversation+tools+budget the response was
// produced against; a nil view degrades to an empty one so View never returns nil.
func NewResponse(text, thinking string, toolCalls []ToolCall, finish FinishReason, view LoopView) *Response {
	if view == nil {
		view = loopView{}
	}
	return &Response{
		text:         text,
		thinking:     thinking,
		toolCalls:    append([]ToolCall(nil), toolCalls...),
		finishReason: finish,
		view:         view,
	}
}

// View exposes the read-only conversation/tools/budget window — response-repair
// Mechanisms validate tool calls against the menu; loop detection reads history.
func (r *Response) View() LoopView { return r.view }

// Text is the assistant's raw text content.
func (r *Response) Text() string { return r.text }

// ToolCalls are the parsed tool calls the model requested (a copy; mutate via
// SetToolCallArguments).
func (r *Response) ToolCalls() []ToolCall { return append([]ToolCall(nil), r.toolCalls...) }

// FinishReason is the model's stop reason.
func (r *Response) FinishReason() FinishReason { return r.finishReason }

// Thinking is the harmony/thinking channel content when the model and parser expose
// it (ok == false when there is none).
func (r *Response) Thinking() (text string, ok bool) { return r.thinking, r.thinking != "" }

// SetText replaces the assistant text — the intercept path (ActionIntercept).
func (r *Response) SetText(s string) {
	r.text = s
	r.revision++
}

// SetToolCallArguments rewrites one tool call's arguments in place — the auto-fix
// Mechanism writing back repaired/formatted content (ActionIntercept). An out-of-range
// index is a no-op.
func (r *Response) SetToolCallArguments(index int, args json.RawMessage) {
	if index < 0 || index >= len(r.toolCalls) {
		return
	}
	r.toolCalls[index].Arguments = args
	r.revision++
}

// Revision reports how many mutations have been applied to the Response — the loop's
// acted-fire probe (R4, engine seam): hookrun snapshots it around each catalogued fire
// and books the fire only when the counter moved or a non-zero Action was returned. A
// hook never needs it.
func (r *Response) Revision() int { return r.revision }

// FinishReason is the model's stop reason; the set is open (treat unknown values
// defensively).
type FinishReason string

const (
	FinishStop      FinishReason = "stop"
	FinishLength    FinishReason = "length"
	FinishToolCalls FinishReason = "tool_calls"
)

// ----------------------------------------------------------------------------
// Conversation — the history-rewrite hook's working value
// ----------------------------------------------------------------------------

// Conversation is the serializable conversation state a history-rewrite hook edits.
// It is a cleanly copyable value with no live handles (ADR 0001) — what lets the
// bench fork by deep-copying it and the user resume from a snapshot. Summaries are
// not a separate structure: they are ordinary messages produced by generative
// Compaction (context/) and written back via Replace. A deferred Response Action
// (ActionDefer) is held here (Defer / TakeDeferred) so it survives a snapshot/resume
// boundary.
//
// MarshalJSON / UnmarshalJSON keep the type opaque while persisting it; the v1 wire
// schema persists the message list (with per-message Extra preservation, P1.6) and the
// pending deferred corrections. The engine wraps this payload in its session-state
// envelope (internal/agent/state.go), which adds the loop counters.
type Conversation struct {
	messages []Message
	deferred []string // pending ActionDefer injections, FIFO
	// revision is bumped by each mutator — the acted-fire probe (R4), read via
	// Revision. Runtime-only: it is deliberately NOT serialized (it carries no
	// history, only "did a hook just mutate me").
	revision int
}

// NewConversation builds a Conversation over a copy of messages (engine seam).
func NewConversation(messages []Message) *Conversation {
	return &Conversation{messages: append([]Message(nil), messages...)}
}

// Len reports the number of messages.
func (c *Conversation) Len() int { return len(c.messages) }

// At returns the message at index i (panics on an out-of-range index, like a slice).
func (c *Conversation) At(i int) Message { return c.messages[i] }

// Range iterates messages until fn returns false.
func (c *Conversation) Range(fn func(i int, m Message) bool) {
	for i := range c.messages {
		if !fn(i, c.messages[i]) {
			return
		}
	}
}

// Messages returns a copy of the message list (engine seam — the loop projects it
// onto the provider wire shape).
func (c *Conversation) Messages() []Message { return append([]Message(nil), c.messages...) }

// PrefixEnd is the index past the leading system messages and the first user message
// — the protected prefix a truncation must keep.
func (c *Conversation) PrefixEnd() int {
	i := 0
	for i < len(c.messages) && c.messages[i].Role == RoleSystem {
		i++
	}
	if i < len(c.messages) && c.messages[i].Role == RoleUser {
		i++
	}
	return i
}

// AssistantBoundaries are the indices of assistant messages — the only safe cut
// points, because a tool result must stay adjacent to the assistant call that
// produced it (strict chat templates).
func (c *Conversation) AssistantBoundaries() []int {
	var b []int
	for i := range c.messages {
		if c.messages[i].Role == RoleAssistant {
			b = append(b, i)
		}
	}
	return b
}

// SetMessageContent edits one message's content in place by index. An out-of-range
// index is a no-op.
func (c *Conversation) SetMessageContent(i int, content string) {
	if i < 0 || i >= len(c.messages) {
		return
	}
	c.messages[i].Content = content
	c.revision++
}

// DropRange drops messages in [start, end) — history truncation drops the middle,
// keeping the prefix and a recent tail. Bounds are clamped; an empty range is a no-op.
func (c *Conversation) DropRange(start, end int) {
	if start < 0 {
		start = 0
	}
	if end > len(c.messages) {
		end = len(c.messages)
	}
	if start >= end {
		return
	}
	c.messages = append(c.messages[:start:start], c.messages[end:]...)
	c.revision++
}

// Insert places a message at index i — e.g. a static gap note at a truncation cut.
// i is clamped to [0, Len].
func (c *Conversation) Insert(i int, m Message) {
	c.messages = insertMessage(c.messages, i, m)
	c.revision++
}

// Append adds m to the end of the history — the engine's per-Turn commit of a user,
// assistant, or tool-result message, and the natural primitive a history-rewrite hook
// uses to grow the conversation (a summary, a gap note). It is Insert at Len with a
// name that reads at the call site.
func (c *Conversation) Append(m Message) {
	c.messages = append(c.messages, m)
	c.revision++
}

// Replace swaps the entire message list — generative Compaction writes its
// summarised history back through here. The slice is copied.
func (c *Conversation) Replace(msgs []Message) {
	c.messages = append([]Message(nil), msgs...)
	c.revision++
}

// Defer records a deferred correction (the Inject payload of an ActionDefer
// PostResponseDecision) to be injected, role-safe, into the next request. It is held
// in conversation state so it survives a snapshot/resume boundary — the streaming
// feed-forward path (design §4.1).
func (c *Conversation) Defer(inject string) {
	c.deferred = append(c.deferred, inject)
	c.revision++
}

// Revision reports how many mutations have been applied to the Conversation — the
// loop's acted-fire probe (R4, engine seam): hookrun snapshots it around each
// catalogued history-rewrite fire and books the fire only when the counter moved. A
// hook never needs it, and it does not survive a snapshot round-trip.
func (c *Conversation) Revision() int { return c.revision }

// TakeDeferred removes and returns the pending deferred corrections in FIFO order —
// the loop drains them when building the next request and InjectContexts each. ok is
// false when none are pending.
func (c *Conversation) TakeDeferred() (injects []string, ok bool) {
	if len(c.deferred) == 0 {
		return nil, false
	}
	out := c.deferred
	c.deferred = nil
	return out, true
}

// conversationJSON is the on-disk shape of a Conversation. Per-message Extra fields
// round-trip through Message's own MarshalJSON/UnmarshalJSON (the unknown wire siblings
// are flattened alongside the known fields), so reasoning_content and the like survive.
type conversationJSON struct {
	Messages []Message `json:"messages"`
	Deferred []string  `json:"deferred,omitempty"`
}

// MarshalJSON serializes the Conversation (messages + pending deferred corrections).
func (c *Conversation) MarshalJSON() ([]byte, error) {
	return json.Marshal(conversationJSON{Messages: c.messages, Deferred: c.deferred})
}

// UnmarshalJSON restores a Conversation from its serialized form.
func (c *Conversation) UnmarshalJSON(data []byte) error {
	var j conversationJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	c.messages = j.Messages
	c.deferred = j.Deferred
	return nil
}

// ----------------------------------------------------------------------------
// Shared message-slice helpers (used by Request and Conversation)
// ----------------------------------------------------------------------------

// firstIndex returns the index of the first message with role, or -1.
func firstIndex(msgs []Message, role Role) int {
	for i := range msgs {
		if msgs[i].Role == role {
			return i
		}
	}
	return -1
}

// lastIndex returns the index of the last message with role, or -1.
func lastIndex(msgs []Message, role Role) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == role {
			return i
		}
	}
	return -1
}

// insertMessage returns msgs with m inserted at i, clamping i to [0, len(msgs)].
func insertMessage(msgs []Message, i int, m Message) []Message {
	if i < 0 {
		i = 0
	}
	if i > len(msgs) {
		i = len(msgs)
	}
	msgs = append(msgs, Message{})
	copy(msgs[i+1:], msgs[i:])
	msgs[i] = m
	return msgs
}

// cloneRawMap returns an independent copy of a raw-JSON map (nil stays nil).
func cloneRawMap(m map[string]json.RawMessage) map[string]json.RawMessage {
	if m == nil {
		return nil
	}
	c := make(map[string]json.RawMessage, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

package domain

import (
	"bytes"
	"encoding/json"
	"sort"
)

// ----------------------------------------------------------------------------
// Hook types & shared substrate (docs/design/hook-mutation-api.md)
// ----------------------------------------------------------------------------
//
// Request, Response, and Conversation are the loop's working values exposed to
// hooks; each lives in its own file (request.go, response.go, conversation.go), and
// this file holds the hook types they share — Message, Role, ToolDef, Budget, the
// LoopView / ConversationView interfaces — and the message-slice helpers. The three
// stay opaque structs with method-only surfaces so the internal
// representation and the variant set remain Apogee-owned and additively versioned
// (ADR 0001): a hook reads Message snapshots and mutates by index against the owning
// container, never touching the backing storage. The operation set is scoped from
// apogee-sim's real Transform / Injector / Intervention footprint — not speculation
// (TDD §6.2).
//
// The exported constructors and the State / Messages drains (NewRequest,
// Request.State, NewResponse, NewConversation, Conversation.Messages, Defer /
// TakeDeferred) are the ENGINE SEAM: internal/agent builds these values from loop
// state and reads the post-hook result back through them. They are exported only
// because the engine lives in a sibling package, and a hook never needs them.
//
// The types themselves are NOT hidden from the public API: the root facade aliases
// Request, Response and Conversation, and a Go type alias carries the full method set —
// so Request.State, Conversation.Messages, Conversation.Defer and
// Conversation.TakeDeferred are reachable through the facade too. What the facade does
// not re-export is the package-level constructors — NewRequest, NewResponse,
// NewConversation — so a facade user can receive these values but never mint one.

// Role is a conversation message's role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolOutcome is the committed record of whether the tool call a RoleTool message answers
// succeeded or failed — the persisted half of ToolResult.IsError. The flag itself lives only
// on the live result the tool stage passes around; once the result is committed to history all
// that survives is its Content, and a successful read_file's Content IS a file body. So a
// reaction asking "did that earlier call fail?" had nothing but the text to sniff, and file
// bodies are full of error strings. This marker is that missing fact.
//
// It is a tri-state on purpose: Unrecorded is distinct from Succeeded, so a reader can tell a
// message that was never marked (a snapshot written before the marker existed) from one marked
// as a success, and fall back to text sniffing only for the former.
type ToolOutcome string

const (
	// ToolOutcomeUnrecorded is the zero value — no outcome was committed with this message.
	// Every non-tool message carries it, as does a tool-result message restored from a
	// snapshot written before the marker existed.
	ToolOutcomeUnrecorded ToolOutcome = ""
	// ToolOutcomeSucceeded records a tool result whose IsError was false.
	ToolOutcomeSucceeded ToolOutcome = "ok"
	// ToolOutcomeFailed records a tool result whose IsError was true.
	ToolOutcomeFailed ToolOutcome = "error"
)

// ToolOutcomeOf projects a live ToolResult.IsError flag onto the marker committed beside the
// result's text. It never returns Unrecorded: a result crossing the commit seam always knows
// which way it went.
func ToolOutcomeOf(isError bool) ToolOutcome {
	if isError {
		return ToolOutcomeFailed
	}
	return ToolOutcomeSucceeded
}

// Message is a read-only snapshot of one conversation message handed to hooks. A hook
// reads Messages and mutates by index against the owning container (Request /
// Conversation); it never holds the loop's backing storage.
type Message struct {
	Role       Role
	Content    string
	ToolCalls  []ToolCall // RoleAssistant only
	ToolCallID string     // RoleTool only — links the result to its ToolCall.ID

	// Images are the image parts that ride beside Content (ADR 0001, amendment 2 note of
	// 2026-10-03): an additive field, so Content stays a plain string and every reader that
	// only knows text keeps working. Only a RoleUser message carries them. Their bytes are
	// never rendered as text — a compaction transcript shows a `[image: <name>]` placeholder —
	// and the token estimate charges ImageChars per image rather than reading Data.
	Images []Image

	// ToolOutcome records whether the tool call this RoleTool message answers failed. It is
	// stamped at the ONE seam every tool result crosses into history (internal/agent
	// appendToolResult) from the result's IsError, so no route — a plain call, a refusal, a
	// gate denial, a sub-agent delegation — commits an unmarked result.
	//
	// Like Interjected it is Apogee-owned and process-local: it rides the session snapshot,
	// and the wire projection maps fields explicitly, so the marker never reaches a provider
	// request.
	ToolOutcome ToolOutcome

	// Interjected marks a RoleUser message the human interjected INTO a running Exchange
	// rather than one that opens an Exchange. It is set ONLY by Agent.Interject; every
	// other user message leaves it false. The derived Exchange opening skips it
	// (CurrentExchange, exchange.go), so a mid-Exchange message never moves the boundary
	// the Reactions read. It is process-local: the wire projection maps fields
	// explicitly, so the marker never reaches a provider request.
	Interjected bool

	// Advice is the provenance ledger of the advice spans injected into Content — one row
	// per advise Reaction that fenced text onto this message, in the order they landed
	// (advice.go). It is runtime-only and deliberately NOT serialized: MarshalJSON writes
	// Content up to the first span's fence, so a session record carries no advice and a
	// resume has nothing to drop (ADR 0076 D6).
	Advice []AdviceSpan `json:"-"`

	// extra carries preserved unknown wire fields (reasoning_content, tool_choice,
	// thinking, …) read through Extra. It is populated by Message's own JSON decoder
	// (UnmarshalJSON collects unknown siblings) and by WithExtra; a Message built as a
	// plain literal carries none.
	extra map[string]json.RawMessage
}

// Image is one image part of a user message: the file or paste name it was attached under, its
// IANA media type ("image/png", "image/jpeg", …) and the encoded image bytes, exactly as read.
// The producers (the `@ref` expansion and the TUI's paste/attach) bound Data by MaxImageBytes
// and a message's images together by MaxMessageImageBytes before an Image is built; the
// session snapshot carries Data base64-encoded under the message's "images" key.
type Image struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Data      []byte `json:"data"`
}

// The image caps the producers enforce. They live here rather than beside the engine because the
// TUI attaches images too and cannot import internal/agent (ADR 0010); every producer reads the
// same two numbers.
const (
	// MaxImageBytes bounds one image's Data.
	MaxImageBytes = 5 * 1024 * 1024
	// MaxMessageImageBytes bounds the summed Data of every image one message carries.
	MaxMessageImageBytes = 5 * 1024 * 1024
)

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
//
// Content is the RECORD content, not necessarily the live one: MarshalJSON fills it from
// Message.recordContent, which cuts at the first advice span's fence. The Advice ledger has
// no field here at all — spans are ephemeral by construction (advice.go).
type messageJSON struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`

	// Images rides the session snapshot under an Apogee-owned key — the provider wire never
	// reads this encoding; each dialect projects images explicitly. omitempty keeps it off
	// every text-only message, so no SessionVersion bump is needed: an older snapshot lacks the
	// key and decodes no images, and an older binary round-trips it as an unknown sibling.
	Images []Image `json:"images,omitempty"`

	// Interjected is Apogee-owned, not an OpenAI wire field: it rides the session snapshot
	// (the marker must survive save/restore) and never reaches a provider. omitempty keeps
	// it absent from every ordinary message, so no SessionVersion bump is needed — an older
	// snapshot simply lacks the key (decoding false) and an older binary round-trips it as
	// an unknown sibling.
	Interjected bool `json:"interjected,omitempty"`

	// ToolOutcome is Apogee-owned on the same terms as Interjected, and needs no
	// SessionVersion bump for the same reason: omitempty keeps it off every non-tool message,
	// an older snapshot lacks the key (decoding ToolOutcomeUnrecorded, which routes a reader
	// to its legacy text fallback), and an older binary round-trips it as an unknown sibling.
	ToolOutcome ToolOutcome `json:"tool_outcome,omitempty"`
}

// messageKnownKeys are the top-level JSON keys messageJSON owns; UnmarshalJSON strips them
// so only genuinely-unknown siblings land in extra. Kept in sync with messageJSON's tags.
var messageKnownKeys = []string{
	"role", "content", "tool_calls", "tool_call_id", "images", "interjected", "tool_outcome",
}

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
// Advice spans are stripped here, at the one encoder every persisted Message crosses (a
// Conversation marshals its messages through this method): the Content written is
// recordContent — everything before the first fence — so no session record carries advice
// and a resume never re-reads it. A message with no spans marshals byte-identically to one
// written before the ledger existed.
//
// The preserved siblings are spliced on in sorted key order rather than via a map marshal,
// so the wire bytes are deterministic regardless of Go's map iteration order — snapshots
// containing reasoning_content (or any other Extra) are byte-reproducible, which a later
// snapshot diff/hash relies on.
func (m Message) MarshalJSON() ([]byte, error) {
	known, err := json.Marshal(messageJSON{
		Role:        m.Role,
		Content:     m.recordContent(),
		ToolCalls:   m.ToolCalls,
		ToolCallID:  m.ToolCallID,
		Images:      m.Images,
		Interjected: m.Interjected,
		ToolOutcome: m.ToolOutcome,
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
	m.Images = known.Images
	m.Interjected = known.Interjected
	m.ToolOutcome = known.ToolOutcome

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
	// ReadOnly is IsReadOnly of the tool this entry describes, stamped where the loop builds its
	// menu, so a hook reading the LoopView can tell a read-only call from a write-capable one
	// without holding the Tool itself. A tool that makes no ReadOnlyTool declaration (every MCP
	// tool) reads false — the safe default.
	ReadOnly bool
}

// Budget is the read-only context-budget view a hook reads to gate token-sensitive
// behaviour (e.g. Library injection backs off as the window fills, tool-result capping trims
// a result to its fraction). It is the CONTEXT "Budget": the single authority on how much
// room each part of a request gets. Its token accounting is calibrated against server-reported
// usage (internal/context.TokenEstimator), so Used and CharsPerToken are honest measures rather
// than a fixed guess.
type Budget struct {
	// Window is the model's ADVERTISED context window (n_ctx tokens) — the hard wall the server
	// itself enforces; 0 when unknown. Overflow detection measures against it, because whether a
	// request WILL NOT FIT is the server's question rather than a question about how much room the
	// session chose to use. Nothing else reads it: every reducer, guard and derived ceiling reads
	// the working ContextLimit below.
	Window int
	// ContextLimit is the WORKING ceiling: how much of Window this session actually works in. It is
	// min(Window, ContextConfig.WorkingWindow) when a working window is configured and Window itself
	// otherwise, so it equals Window on every session that configures none. It is what the
	// allocation below is computed from and what every reducer and guard reads — 0 only when
	// neither a window nor a working window is known, which leaves nothing to allocate.
	ContextLimit  int
	Used          int     // tokens the last server usage reported the prompt occupied; 0 until the first UsageEvent
	CharsPerToken float64 // the chars→token ratio, calibrated against reported usage

	// The window allocation (internal/context.Allocate): how many tokens of ContextLimit each
	// part of a request may claim. ResponseReserve is held back for the reply; SystemPrompt and
	// FileContext are what this session's standing content MEASURED plus headroom, so a session
	// that seeds no workspace context files reserves almost nothing for them; History is the rest
	// of the working room, capped at what the emergency fold can still render (internal/agent's
	// HistoryCap), so the three no longer sum to ContextLimit - ResponseReserve — on a known
	// window the cap is what the surplus falls out of. Every field is 0 when the window is
	// unknown. It is ADVISORY: the context reducers (tool-result capping, automatic Compaction)
	// read it; nothing in the request path is reshaped by it here.
	ResponseReserve int
	SystemPrompt    int
	FileContext     int
	History         int

	// StandingAdvisory is the FIXED 15%-of-working-room share the oversize notice measures the
	// whole standing system content against (ADR 0026: oversize is advisory). It is deliberately
	// not SystemPrompt: a measured reservation grows with the content it measures, so reading the
	// reservation as the ceiling would mean the notice could never fire.
	StandingAdvisory int
}

// LoopView is the read-only window every reaction has onto loop state beyond its own
// mutable value — the conversation so far, the tool menu, the budget and the Turn index.
// It is the home of all cross-Turn reads: most reactions
// decide by aggregating across Turns, so the primary mutable value (a
// *Response, a *ToolCallEdit, a *ToolResultEdit) is never sufficient alone. Request
// and Response expose it via their View method; the tool-stage hooks receive it as an
// argument.
type LoopView interface {
	Conversation() ConversationView
	Tools() []ToolDef
	Budget() Budget
	Turn() int
	// Depth reports the sub-agent nesting level the reaction is firing at: 0 for a top-level
	// Agent, parent+1 for a sub-agent (ADR 0013). It is the seam a gate keyed on "only at
	// the top level" needs — a reaction that opens a fan-out shapes only the primary call,
	// never a nested delegation it itself set up. A view built without a depth (a test
	// fake, the degraded no-view Response) reports 0, the top-level default.
	Depth() int
	// ParallelAgents reports how many sub_agent delegations the agent this reaction is firing
	// inside may run AT ONCE — the bound server's Parallel agents cap (ADR 0039 decision 2,
	// pin-else-discover-else-floor, the floor 4 for a keyed server and 1 otherwise) at Depth 0,
	// and 1 at any deeper level, where a child's own delegations stay serial inline
	// (decision 3). It is the width a reaction that
	// synthesizes delegations batches by, dispatching min(cap, remaining) per Turn (ADR 0039
	// decision 2). A view built without one (a test
	// fake, the degraded no-view Response) reports 0, which reads the same as 1 — strictly
	// serial, the ratified floor.
	ParallelAgents() int
}

// ConversationView is read-only history with the tool-call/result pairing helpers
// every history-inspecting reaction needs: the tool name and arguments live only on
// the originating ToolCall, never on the tool-result message, so resolving a result
// back to its call is mandatory for error-handling reactions.
type ConversationView interface {
	Len() int
	At(i int) Message
	Range(fn func(i int, m Message) bool)
	// LastUser returns the most recent user message and its index — an Interjection
	// included. It is deliberately NOT the Exchange opening (CurrentExchange skips
	// interjections): a reaction asking "what did the human last say" wants the
	// remark, while one scoping itself to the Exchange derives the boundary instead.
	LastUser() (msg Message, index int, ok bool)
	// CallByID resolves a tool result to its originating call (for the name/args).
	CallByID(id string) (call ToolCall, index int, ok bool)
	// ResultFor resolves a tool call to its result message.
	ResultFor(callID string) (msg Message, index int, ok bool)
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

// lastIndex returns the index of the last message with role, or -1. It routes
// through lastRoleIndex, the one boundary-derivation core (exchange.go).
func lastIndex(msgs []Message, role Role) int {
	return lastRoleIndex(messageSlice(msgs), role)
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

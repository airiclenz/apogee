package domain

import (
	"encoding/json"
	"strings"
)

// ----------------------------------------------------------------------------
// Request — the pre-request hook's working value
// ----------------------------------------------------------------------------

// Request is the outgoing Upstream request a pre-request reaction may shape. Reads go
// through View; mutations are the characterised operation set from apogee-sim's
// pre-request lab rows. The loop builds one with NewRequest, hands it to every
// pre-request reaction (their mutations compose), then drains it with State to project
// onto the provider wire shape.
type Request struct {
	model    string
	messages []Message
	tools    []ToolDef
	budget   Budget
	turn     int
	sampling SamplingParams
	extras   map[string]json.RawMessage
	revision int // bumped by each mutator — the acted-fire probe (R4), read via Revision
	depth    int // sub-agent nesting level surfaced through View().Depth() (0 = top-level; ADR 0013/0014)
	// parallelAgents is the delegation width surfaced through View().ParallelAgents(): how many
	// sub_agent calls this agent may run at once (ADR 0039). 0 = unstamped, read as the serial floor.
	parallelAgents int

	// committedLen bounds the history View() exposes to the post-response scanners: it is
	// frozen at the first retry-in-place append (AppendSupersededAssistant), so the
	// request-scoped superseded attempt + correction never masquerade as committed history
	// to a history-scanning lab hook or to the tool-loop breaker Floor guard (item 10, sim parity — the sim's
	// retry builders left the detector's request unmutated). -1 means no retry appendage has been recorded,
	// so View() exposes the whole request. State() (the model-facing projection) is never
	// bounded — the appendage still reaches the Upstream.
	//
	// Once frozen it is MAINTAINED, not static (F2): a below-boundary structural mutation
	// (InjectContext inserting before the Exchange opening, or appendOrCreateSystem
	// prepending a system message) shifts committed indices, so committedLen advances to keep
	// View() pinned to the same logical committed history. This matters for the
	// empty-superseded retry, where nothing is appended so the correction lands below the
	// boundary rather than after it.
	committedLen int
}

// NewRequest builds the pre-request working value from loop state (engine seam). The
// messages and tools slices are copied, so a reaction mutating the Request never reaches
// back into the loop's conversation storage.
func NewRequest(model string, messages []Message, tools []ToolDef, budget Budget, turn int) *Request {
	return &Request{
		model:        model,
		messages:     append([]Message(nil), messages...),
		tools:        append([]ToolDef(nil), tools...),
		budget:       budget,
		turn:         turn,
		committedLen: -1, // no retry appendage yet — View() exposes the whole request
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

// View exposes the read-only conversation/tools/budget window. The conversation is bounded
// to committedLen once a retry-in-place has appended a superseded exchange (item 10): the
// post-response scanners then see only committed history + the response under review, never
// the request-scoped superseded attempt/correction — matching the sim, whose retry builders
// ran their detectors against the unmutated request. The tool menu and budget are unbounded.
func (r *Request) View() LoopView {
	messages := r.messages
	if r.committedLen >= 0 && r.committedLen <= len(r.messages) {
		messages = r.messages[:r.committedLen]
	}
	return loopView{
		messages:       messages,
		tools:          r.tools,
		budget:         r.budget,
		turn:           r.turn,
		depth:          r.depth,
		parallelAgents: r.parallelAgents,
	}
}

// Model is the target model id (the Library keys its lookup on this).
func (r *Request) Model() string { return r.model }

// Revision reports how many mutations have been applied to the Request — the loop's
// acted-fire probe (R4, engine seam): hookrun snapshots it around each catalogued fire
// and books the fire only when the counter moved. A hook never needs it.
func (r *Request) Revision() int { return r.revision }

// SetDepth records the sub-agent nesting level this request runs at (engine seam, ADR
// 0013/0014): the loop stamps it from Agent.depth so a pre-request hook reading
// req.View().Depth() can tell a top-level (0) from a nested request. It is loop setup, not a
// hook mutation — it carries no acted-fire meaning and so does NOT bump the revision. A
// Request built without it reports Depth 0, the top-level default.
func (r *Request) SetDepth(depth int) { r.depth = depth }

// SetParallelAgents records how many sub_agent delegations the agent building this request may
// run at once (engine seam, ADR 0039 — the SetDepth sibling): the loop stamps the bound server's
// resolved Parallel agents cap at Depth 0 and 1 deeper down, so a post-response hook reading
// req.View().ParallelAgents() knows how wide a batch of delegations it may synthesize. Like
// SetDepth it is loop setup rather than a hook mutation, so it does NOT bump the revision. A
// Request built without it reports 0 — read as 1, the serial floor.
func (r *Request) SetParallelAgents(width int) { r.parallelAgents = width }

// Extra reports a preserved unknown request field — the read half of SetExtra (a
// pre-request reaction checks for an existing response_format before setting one).
func (r *Request) Extra(key string) (json.RawMessage, bool) {
	v, ok := r.extras[key]
	return v, ok
}

// AppendToSystem appends text to the first system message (creating one if absent),
// but is a no-op if marker already occurs there — the idempotent inject the nudge
// reactions share. Reports whether it injected. The caller
// embeds marker within text so a second call with the same marker is a no-op.
func (r *Request) AppendToSystem(marker, text string) (injected bool) {
	if i := firstIndex(r.messages, RoleSystem); i >= 0 && strings.Contains(r.messages[i].Content, marker) {
		return false
	}
	r.appendOrCreateSystem(text)
	r.revision++
	return true
}

// NoteOnTail fences text onto the request's last message as an engine note on topic
// (Message.WithEngineNote) when that message is a tool result, and reports whether the note is
// on the tail. It is the engine's own placement for a per-request structural instruction that
// must sit where the model reads it — after the closing tool result of the Turn, not at the far
// end of the system prompt — and it stays role-safe: the note rides the tool message, and a
// wire that offers no tools degrades that message to user role whole, fence included, so no new
// message is inserted after a tool result. A tail that is not a tool result (a user message, or
// an assistant message on a faulted-then-retried shape) is left alone and false is returned, so
// the caller can fall back to AppendToSystem. Idempotent on topic: a tail already carrying a
// note on topic is not noted twice and the revision is not bumped, yet true is still returned —
// the note IS on the tail. A fresh note bumps the revision like every other mutation.
func (r *Request) NoteOnTail(topic, text string) bool {
	n := len(r.messages)
	if n == 0 || r.messages[n-1].Role != RoleTool {
		return false
	}
	if r.messages[n-1].hasEngineNote(topic) {
		return true
	}
	r.messages[n-1] = r.messages[n-1].WithEngineNote(topic, text)
	r.revision++
	return true
}

// InjectContext inserts a user message at the role-safe position: appended to the
// system prompt if the conversation ends in a tool result (a user message after a
// tool result breaks strict chat templates); appended at the end if it ends in an
// assistant message (the retry-exchange shape — the correction answers the superseded
// assistant message it follows, R1); otherwise inserted before the message that OPENS
// the current Exchange. With no opening message present it appends at the end.
//
// The insert anchors on lastExchangeOpening (exchange.go), NOT on the last user message:
// an Interjection is a user message committed INSIDE the running Exchange, and inserting
// above it would make this request-scoped injection the newest non-interjected user
// message — the derived opening — collapsing the Exchange every reaction reads down to
// the interjection alone. Anchoring on the opening puts the injection exactly where it has
// always gone (immediately before the human's ask, hence ahead of any interjection) and
// moves no boundary.
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
	idx := lastExchangeOpening(messageSlice(r.messages))
	if idx < 0 {
		r.messages = append(r.messages, msg)
		return
	}
	r.messages = insertMessage(r.messages, idx, msg)
	// Boundary maintenance (F2): committedLen tracks the same logical message across
	// request-scoped structural mutations. An insert BELOW the frozen boundary shifts every
	// committed message right by one, so the boundary advances to keep View() pinned to the
	// same committed history — without it an empty-superseded retry's correction lands below
	// the boundary and evicts the real user ask from the post-response scanners' View().
	if r.committedLen >= 0 && idx < r.committedLen {
		r.committedLen++
	}
}

// AppendSupersededAssistant appends a superseded assistant message (text + tool calls)
// to the end of the request — the loop's retry-exchange seam (engine seam, R1), NOT a
// reaction-mutation primitive: on a retry correction the loop appends the response
// it is retrying, then the correction via InjectContext, so the re-streamed request
// carries the exchange the sim's retry builders carried. The append is request-scoped
// — it is never committed to history. A wholly empty superseded response (empty text,
// no calls) appends nothing. calls is copied, so the caller's slice stays independent.
//
// The FIRST call freezes committedLen at the current length (item 10, sim parity): this
// superseded attempt, its correction, and every later accumulated retry are request-scoped
// and stay out of the post-response scanners' View(). It is frozen once — not advanced per
// retry — because the sim's detectors ran against the ORIGINAL committed request on every
// retry iteration, so the scanner view stays pinned to the pre-retry length throughout. The
// freeze precedes the empty-response short-circuit, so an empty superseded + a correction is
// bounded too.
func (r *Request) AppendSupersededAssistant(text string, calls []ToolCall) {
	if r.committedLen < 0 {
		r.committedLen = len(r.messages)
	}
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
//
// Rewriting the content invalidates any advice spans the message carried, so a ledger whose
// fence the new content no longer opens at its offset is dropped — the same guard
// Conversation.SetMessageContent carries, kept identical so the two edit seams cannot drift.
func (r *Request) SetMessageContent(index int, content string) {
	if index < 0 || index >= len(r.messages) {
		return
	}
	r.messages[index].Content = content
	r.messages[index].dropStaleAdvice()
	r.revision++
}

// SetTools replaces and reorders the tool menu (a shape-view reaction). The slice
// is copied so the caller cannot mutate the menu after the call.
func (r *Request) SetTools(tools []ToolDef) {
	r.tools = append([]ToolDef(nil), tools...)
	r.revision++
}

// SetExtra sets an unknown request field, allocating the carrier if needed (e.g. a
// pre-request reaction setting a provider-specific response_format).
func (r *Request) SetExtra(key string, v json.RawMessage) {
	if r.extras == nil {
		r.extras = make(map[string]json.RawMessage)
	}
	r.extras[key] = v
	r.revision++
}

// SetSampling merges sampling overrides field-wise: a non-nil field overwrites the
// current value, a nil field leaves it untouched — the contract SamplingParams states.
// The merge is what makes a partial set safe: the loop stamps the reply ceiling before
// any hook runs (ADR 0046, loop.go), and the summarizer sets both fields
// (agent/compact.go), so a hook setting only Temperature must not nil the cap. There is
// no clearing surface — a hook cannot reset a field back to nil.
func (r *Request) SetSampling(p SamplingParams) {
	if p.Temperature != nil {
		r.sampling.Temperature = p.Temperature
	}
	if p.MaxTokens != nil {
		r.sampling.MaxTokens = p.MaxTokens
	}
	r.revision++
}

// MergeSystem folds text into the system channel of msgs the one way llama.cpp chat templates
// reliably render — ONE merged system message: appended (after a blank line) to the first system
// message when there is one, else prepended as a new sole system message at position 0. It is
// pure: msgs is never written to, the result is a fresh slice, and only that slice's length tells a
// caller which branch ran. Both injection seams route through it — the Request's own mutators here
// (appendOrCreateSystem) and the wire projection's tool-block merge (internal/agent) — so the merge
// shape is spelled once.
func MergeSystem(msgs []Message, text string) []Message {
	if i := firstIndex(msgs, RoleSystem); i >= 0 {
		out := append([]Message(nil), msgs...)
		if out[i].Content == "" {
			out[i].Content = text
		} else {
			out[i].Content += "\n\n" + text
		}
		return out
	}
	return append([]Message{{Role: RoleSystem, Content: text}}, msgs...)
}

// appendOrCreateSystem appends text to the first system message, creating one at the
// front of the conversation if none exists (MergeSystem), and maintains the retry boundary
// when the second branch ran.
func (r *Request) appendOrCreateSystem(text string) {
	before := len(r.messages)
	r.messages = MergeSystem(r.messages, text)
	if len(r.messages) == before {
		return
	}
	// Boundary maintenance (F2): committedLen tracks the same logical message across
	// request-scoped structural mutations. A prepended system message shifts every committed
	// message right by one, so the boundary advances to keep View() pinned to the same
	// committed history — without it an empty-superseded retry that prepends its correction
	// (the ends-in-tool-result shape) shifts every index while the frozen boundary evicts the
	// newest tool result from the post-response scanners' View(). The append-to-existing-system
	// branch above changes no indices, so it needs no adjustment (its content-visibility
	// residual is the accepted F2 residual).
	if r.committedLen >= 0 {
		r.committedLen++
	}
}

// SamplingParams are the optional sampling overrides a pre-request hook may set; a
// nil field leaves the loop's value untouched.
type SamplingParams struct {
	Temperature *float64
	MaxTokens   *int
}

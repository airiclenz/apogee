package domain

import "encoding/json"

// ----------------------------------------------------------------------------
// Response — the post-response hook's working value
// ----------------------------------------------------------------------------

// Response is the model response a post-response reaction inspects and may intercept. The
// loop builds one with NewResponse from the parsed Upstream reply; reads go through
// the accessors, and an intercept is expressed by mutating in place.
type Response struct {
	text         string
	thinking     string
	toolCalls    []ToolCall
	finishReason FinishReason
	view         LoopView
	revision     int // bumped by each mutator — the acted-fire probe (R4), read via Revision

	thinkingModel  string            // the model the producing request named; "" when thinkingBlocks is empty
	thinkingBlocks []json.RawMessage // the reply's opaque reasoning blocks (SetThinkingBlocks)
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
// reactions validate tool calls against the menu; loop detection reads history.
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

// ThinkingBlocks reports the reply's opaque reasoning blocks — on the anthropic wire every signed
// `thinking` and `redacted_thinking` block, one JSON object each, verbatim and in reply order —
// together with the model the request that produced them named. Both are empty when the reply
// carried none (every openai-wire reply). Nothing above the provider reads into a block: a signed
// block goes back upstream byte-for-byte or the server refuses it, and Thinking stays the readable
// text. The slice is a copy; the blocks it holds are shared and must not be mutated.
func (r *Response) ThinkingBlocks() (model string, blocks []json.RawMessage) {
	return r.thinkingModel, append([]json.RawMessage(nil), r.thinkingBlocks...)
}

// SetThinkingBlocks attaches the reply's opaque reasoning blocks and the model the producing
// request named — the engine seam the loop sets as it assembles the Response, so the committed
// assistant message can carry them into history and the next request to that same model can
// replay them. Empty blocks clear both. It copies the slice and bumps Revision like every mutator.
func (r *Response) SetThinkingBlocks(model string, blocks []json.RawMessage) {
	if len(blocks) == 0 {
		r.thinkingModel, r.thinkingBlocks = "", nil
	} else {
		r.thinkingModel, r.thinkingBlocks = model, append([]json.RawMessage(nil), blocks...)
	}
	r.revision++
}

// SetText replaces the assistant text — the intercept path.
func (r *Response) SetText(s string) {
	r.text = s
	r.revision++
}

// SetToolCallArguments rewrites one tool call's arguments in place — a shape-work
// reaction writing back repaired/formatted content. An out-of-range
// index is a no-op.
func (r *Response) SetToolCallArguments(index int, args json.RawMessage) {
	if index < 0 || index >= len(r.toolCalls) {
		return
	}
	r.toolCalls[index].Arguments = args
	r.revision++
}

// AppendToolCall appends a synthesized tool call to the response and bumps the revision —
// the intercept seam a post-response reaction uses to add a delegation the model did not
// itself emit (a fan-out reaction synthesizing the first sub_agent call from a plan the
// model wrote out as text). The appended call is indistinguishable from a model-emitted one
// downstream: the loop reads it back through ToolCalls(), records it on the committed
// assistant message, and dispatches it through the full per-call Resolution — the ADR 0013
// recursion point for a sub_agent call. The caller owns the call's ID (the loop's
// synthesized-call style) and arguments; combined with a returned Outcome.Defer the appended
// call and the deferred correction both take effect (the dispatcher applies the in-place
// mutation, then routes the defer).
func (r *Response) AppendToolCall(call ToolCall) {
	r.toolCalls = append(r.toolCalls, call)
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

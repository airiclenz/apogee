package provider

import "encoding/json"

// Message is one role-tagged message in a provider request — the wire-shaped view of
// conversation state. It is deliberately decoupled from domain.Message so this package
// carries no dependency on the domain types' richer surface: the loop translates domain
// conversation state ↔ this wire shape at the seam (ADR 0010). ToolCalls is set only on
// an assistant message that invoked tools; ToolCallID only on a tool-result message,
// linking it back to the call it answers. The Client rewrites these onto the OpenAI wire
// schema (and degrades tool messages to user messages when the request offers no tools,
// matching the TS oracle).
type Message struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall // assistant-only: the tool calls the model emitted
	ToolCallID string     // tool-result-only: links a result to its originating call
}

// ToolCall is one tool invocation the model emitted, in the OpenAI "function" shape.
// The JSON tags serve both directions: marshalling assistant tool_calls onto the
// request and decoding tool_calls off a response (streamed or whole).
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // always "function" for this shape
	Function FunctionCall `json:"function"`
}

// FunctionCall is the name + raw-JSON arguments of a ToolCall. Arguments is the
// model-emitted argument string, kept verbatim (unparsed) so processing/ owns parsing.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolSpec is one tool offered to the model — the menu entry the Client renders into the
// request's "tools" array. Parameters is a JSON-Schema object kept opaque here.
type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// Sampling carries the optional generation knobs. A nil field is omitted from the
// request (the server's default applies), mirroring the TS oracle's `!== undefined`
// gating — pointers express "unset" distinctly from a meaningful zero.
type Sampling struct {
	Temperature   *float64
	TopP          *float64
	TopK          *int
	RepeatPenalty *float64
	MaxTokens     *int
}

// Request is the Upstream request the loop hands a Responder. The Client owns turning it
// into the OpenAI chat-completions JSON body; the loop never sees net/http. Stream
// selects the SSE path (Client.Stream) over the whole-response path (Client.Respond).
type Request struct {
	Model    string
	Messages []Message
	Tools    []ToolSpec
	Sampling Sampling
	Stream   bool

	// LogProbs asks the Upstream to report the candidate-token distribution alongside the
	// reply (OpenAI's `logprobs` + `top_logprobs`). The loop never sets it; `apogee probe
	// model` does, because the distribution is a far more stable identity signal than
	// generated text — ADR 0021 §6 prefers logprobs where the server exposes them. A server
	// that does not support the fields simply answers without them, so setting it is safe.
	LogProbs bool

	// ThinkingEffort asks the Upstream for a given amount of reasoning. It is a semantic seam
	// field — "this call wants this much chain-of-thought" — leaving the Client to decide how
	// that intent is expressed on a given server (ADR 0050). "" ⇒ nothing is added to the
	// request, so a caller that asks for nothing puts byte-identical bytes on the wire;
	// EffortOff ⇒ no chain-of-thought at all; a level ⇒ the template's effort dial. Callers
	// are the loop (from the Model profile, plus the session override) and the naming call
	// (internal/title), which asks for EffortOff because an eight-word title needs no
	// reasoning while a thinking model will otherwise spend its whole reply budget producing
	// one.
	ThinkingEffort Effort
}

// Effort is how hard a call asks the Upstream to think. It mirrors the domain vocabulary
// without importing it — the provider package stays domain-free, so the agent maps at the
// boundary the way toProviderSampling does. "" is not a fifth level: it is the ABSENCE of the
// setting, and absence emits nothing (ADR 0050).
type Effort string

const (
	// EffortOff asks for no chain-of-thought at all.
	EffortOff Effort = "off"
	// EffortLow is the shortest reasoning the template offers.
	EffortLow Effort = "low"
	// EffortMedium is the middle rung.
	EffortMedium Effort = "medium"
	// EffortHigh is the longest reasoning the template offers.
	EffortHigh Effort = "high"
)

// Usage is the token accounting an Upstream reply may carry (absent on servers that omit
// it — then it is the zero value).
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// RawResponse is the assembled non-streaming reply: the assistant text, an optional
// thinking channel (reasoning_content), any tool calls, the finish reason, and usage.
// "Raw" because tool-call arguments stay unparsed — processing/ parses them downstream.
type RawResponse struct {
	Content      string
	Thinking     string
	ToolCalls    []ToolCall
	FinishReason string
	Usage        Usage

	// TopCandidates are the candidate tokens the server reported for the FIRST generated
	// token position, most-likely first. It is non-nil only when the Request asked for
	// LogProbs *and* the server exposed them — an OpenAI-compatible server that ignores the
	// fields leaves it nil, which is a finding rather than a failure. It carries the tokens
	// the model *could* have emitted rather than the one that was drawn, so it survives the
	// sampling noise a response hash would mistake for a different model (ADR 0021 §6).
	TopCandidates []string
}

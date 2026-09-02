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

	// EffortDialect selects which wire shape expresses ThinkingEffort on this server. It is the
	// semantic per-server seam that keeps the intent above dialect-free: the loop says how hard
	// to think, this says which endpoint is listening, and the Client owns the mapping between
	// them (ADR 0060). The zero value (EffortDialectNone) is the historical
	// chat_template_kwargs mapping, so every existing caller's bytes are unchanged by the
	// field's arrival; it is ignored entirely when ThinkingEffort is "".
	EffortDialect EffortDialect
}

// Effort is how hard a call asks the Upstream to think. It mirrors the domain vocabulary
// without importing it — the provider package stays domain-free, so the agent maps at the
// boundary the way toProviderSampling does. The vocabulary is the seven-name union real servers
// report (off/low/medium/high plus minimal/xhigh/max), with "none" the OpenRouter spelling of
// the "off" rung; no one model offers all of them, and a level the bound model does not
// understand fails the turn rather than this type. "" is not a further level: it is the ABSENCE
// of the setting, and absence emits nothing (ADR 0050, amended by ADR 0060).
type Effort string

const (
	// EffortOff asks for no chain-of-thought at all.
	EffortOff Effort = "off"
	// EffortNone is the same rung as EffortOff under the spelling the OpenRouter dialect uses.
	EffortNone Effort = "none"
	// EffortMinimal is the barely-there rung the OpenAI-shaped servers report below "low".
	EffortMinimal Effort = "minimal"
	// EffortLow is the shortest reasoning the template offers.
	EffortLow Effort = "low"
	// EffortMedium is the middle rung.
	EffortMedium Effort = "medium"
	// EffortHigh is the longest reasoning the template offers.
	EffortHigh Effort = "high"
	// EffortXHigh is the rung above "high" on the templates that offer one.
	EffortXHigh Effort = "xhigh"
	// EffortMax is the topmost rung reported in the wild, above "xhigh" where both exist.
	EffortMax Effort = "max"
)

// EffortDialect names the wire shape a given server reads a thinking-effort intent in. It is a
// per-server fact — a property of the endpoint, not of the call — so it is set once per binding
// from what discovery saw (or from the server's `effort-dialect:` config key) and rides every
// request unchanged. Three dialects have been sighted, each with its own arm in the Client's
// mapping; growth is per-sighting, never a per-model-family table (ADR 0050, amended by
// ADR 0060). The zero value is EffortDialectNone, which reproduces today's wire exactly: a
// caller that names no dialect is served the llama.cpp kwargs mapping it was served before the
// field existed.
type EffortDialect string

const (
	// EffortDialectNone is the zero value: no dialect was named, so the request keeps the
	// historical chat_template_kwargs mapping.
	EffortDialectNone EffortDialect = ""
	// EffortDialectKwargs is llama.cpp's: the intent rides inside `chat_template_kwargs`, which
	// the server forwards into the chat template.
	EffortDialectKwargs EffortDialect = "kwargs"
	// EffortDialectReasoning is OpenRouter's: a top-level `reasoning` object carrying either an
	// effort level or an `enabled: false` switch.
	EffortDialectReasoning EffortDialect = "reasoning"
	// EffortDialectOpenAI is OpenAI's and Groq's: a top-level `reasoning_effort` string. Note the
	// spelling collision — this is a FIELD of the request body, whereas llama.cpp reads a
	// `reasoning_effort` ENTRY inside chat_template_kwargs; the two are not interchangeable.
	EffortDialectOpenAI EffortDialect = "openai"
	// EffortDialectOff is the absence of a dialect stated deliberately: this server takes no
	// effort key at all, in any shape, however loudly the caller asks. It is what a server entry's
	// `effort-dialect: off` forces (ADR 0060 decision 3) — the escape hatch for a server that
	// errors on a kwarg it does not know — and it is distinct from the zero EffortDialectNone,
	// which says only that nobody named a dialect and so keeps the historical kwargs mapping.
	EffortDialectOff EffortDialect = "off"
)

// Usage is the token accounting an Upstream reply may carry (absent on servers that omit
// it — then it is the zero value).
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	// CachedPromptTokens is how many of PromptTokens the server answered from its prefix cache
	// (OpenAI's `prompt_tokens_details.cached_tokens`). It is INFORMATIONAL only: a cached token
	// is still context the model reads, so it moves no budget — only the bill differs. 0 on every
	// server that omits the breakdown, which is most of them.
	CachedPromptTokens int
}

// RawResponse is the assembled non-streaming reply: the assistant text, an optional
// thinking channel (`reasoning_content`, or its `reasoning` alias), any tool calls, the
// finish reason, and usage.
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

// WireDirection tags which half of one round-trip a WireRecord holds.
type WireDirection string

const (
	// WireRequest tags the body the Client posted to the Upstream.
	WireRequest WireDirection = "request"
	// WireResponse tags what the Upstream answered with.
	WireResponse WireDirection = "response"
)

// WireRecord is one half of one Upstream round-trip, as bytes — the raw protocol an
// observer installed with WithWireObserver is handed. It exists so the traffic the Client
// already builds and parses can be *seen* (the Inspector) without the Client retaining or
// reformatting anything.
//
// What each direction holds, exactly:
//
//   - WireRequest — the marshalled request body exactly as posted, emitted once per
//     Respond/Stream call (not once per retry attempt: the retried bytes are identical).
//     Headers are never part of a record, so the Authorization bearer token cannot reach
//     an observer by construction.
//   - WireResponse — for a stream, the raw SSE "data:" payload lines as received,
//     newline-joined in arrival order (the terminal "[DONE]" included: this is the
//     protocol, not a summary of it), delivered once when the stream ends however it
//     ends. For a non-2xx reply, the error body after Client.sanitize has redacted the
//     API key and capped the length. The non-streaming success body is not recorded — it
//     is decoded straight off the connection and the loop streams.
//
// Payload is the Client's own buffer, but the Client neither mutates nor retains it after
// the observer returns, so an observer may keep the slice.
type WireRecord struct {
	Direction WireDirection
	Payload   []byte
}

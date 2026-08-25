package provider

import (
	"encoding/json"
	"fmt"
)

// This file holds the on-the-wire JSON structs — the literal OpenAI chat-completions
// request/response schema — kept separate from the loop-facing seam types in wire.go.
// buildBody maps a seam Request onto chatRequest; toRawResponse maps a decoded
// chatCompletionResponse back onto the seam RawResponse. Keeping the two layers apart is
// what lets the seam stay Go-idiomatic while the JSON stays exactly as the server expects.

// chatRequest is the request body. Sampling pointers and tools are omitted when unset so
// the server applies its own defaults; content/messages/stream are always present.
type chatRequest struct {
	Model         string         `json:"model,omitempty"`
	Messages      []chatMessage  `json:"messages"`
	Stream        bool           `json:"stream"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
	TopP          *float64       `json:"top_p,omitempty"`
	TopK          *int           `json:"top_k,omitempty"`
	RepeatPenalty *float64       `json:"repeat_penalty,omitempty"`
	MaxTokens     *int           `json:"max_tokens,omitempty"`
	Tools         []chatTool     `json:"tools,omitempty"`
	// LogProbs/TopLogProbs are pointers so they are OMITTED unless a caller asks for the
	// candidate distribution: an unasked-for `logprobs: false` on every request would change
	// the bytes every existing caller puts on the wire, and the byte-identical anchor holds
	// here too.
	LogProbs    *bool `json:"logprobs,omitempty"`
	TopLogProbs *int  `json:"top_logprobs,omitempty"`
	// ChatTemplateKwargs is passed through to the server's chat template (llama.cpp's
	// `chat_template_kwargs`) — today carrying the request's thinking effort, either switching a
	// Qwen-family template's thinking block off or setting its effort dial (ADR 0050). A nil map
	// is omitted, so the byte-identical anchor the logprobs pair holds to applies here too: a
	// caller that asks for nothing changes nothing on the wire.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
	// Reasoning is OpenRouter's thinking-effort shape: an object carrying either a level or an
	// `enabled: false` switch. ReasoningEffort is OpenAI's and Groq's: a TOP-LEVEL string, not to
	// be confused with the `reasoning_effort` entry the kwargs map above carries into a llama.cpp
	// chat template — same word, different place on the wire. Both are pointers so they are
	// omitted unless the bound server's dialect asks for them, holding the byte-identical anchor
	// the kwargs map and the logprobs pair hold to.
	Reasoning       *reasoningField `json:"reasoning,omitempty"`
	ReasoningEffort *string         `json:"reasoning_effort,omitempty"`
}

// reasoningField is the OpenRouter `reasoning` object. Effort names a level; Enabled is a
// pointer so `enabled: false` — the way that dialect switches reasoning off — survives
// marshalling instead of being omitted as a zero value.
type reasoningField struct {
	Effort  string `json:"effort,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

// chatMessage is one wire message. Content is a pointer so a tool-call-only assistant
// turn serialises content as JSON null (OpenAI's convention) rather than omitting it.
type chatMessage struct {
	Role       string     `json:"role"`
	Content    *string    `json:"content"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// streamOptions asks the server to include a final usage chunk on a streamed response.
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// chatTool is one tool offered to the model, in the OpenAI "function" envelope.
type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// chatCompletionResponse is the whole (non-streamed) reply. reasoning_content is the
// thinking channel some servers emit; usage is absent on servers that omit it, and logprobs
// is absent on every server that was not asked for it (or cannot supply it).
type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content          string     `json:"content"`
			ReasoningContent string     `json:"reasoning_content"`
			ToolCalls        []ToolCall `json:"tool_calls"`
		} `json:"message"`
		LogProbs     *logProbsJSON `json:"logprobs"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage *usageJSON `json:"usage"`
	// Error is the in-band failure member: present only when the server framed an upstream
	// failure as an HTTP 200 (see wireError). Respond checks it before mapping choices,
	// because a body carrying an error carries no usable choices either.
	Error *wireError `json:"error"`
}

// logProbsJSON is the subset of OpenAI's per-choice logprobs payload the probe reads: for
// each generated token position, the alternatives the model was choosing between. Only the
// token strings are kept — the probabilities themselves drift with temperature and server
// build, while the candidate SET is the stable shape of the distribution.
type logProbsJSON struct {
	Content []struct {
		Token       string `json:"token"`
		TopLogProbs []struct {
			Token string `json:"token"`
		} `json:"top_logprobs"`
	} `json:"content"`
}

type usageJSON struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// wireError is the in-band error member OpenAI-compatible aggregators deliver on an HTTP
// 200 — inside the JSON body on the non-streamed path, as an SSE data event on the
// streamed one. Ignoring it is what lets an upstream failure masquerade as a successful
// empty reply. Code is raw because servers send it either as a number (an HTTP status) or
// as a string slug; Metadata is raw because its shape is provider-specific (OpenRouter
// carries the originating provider's own error text in metadata.raw).
type wireError struct {
	Message string          `json:"message"`
	Code    json.RawMessage `json:"code"`
	// ErrorType is the aggregator's own class slug (OpenRouter sends
	// "provider_unavailable" when the upstream it fanned out to went away). It is read
	// because it is the only signal that a failure is transient when the code is a
	// non-numeric slug or absent altogether.
	ErrorType string          `json:"error_type"`
	Metadata  json.RawMessage `json:"metadata"`
}

// intCode returns the error code as an int, or 0 when the server sent a non-numeric one
// (e.g. "rate_limit_exceeded") or none at all. Classification branches on it only where a
// numeric HTTP status is meaningful; the message text carries the truth either way.
func (e wireError) intCode() int {
	var code int
	if err := json.Unmarshal(e.Code, &code); err != nil {
		return 0
	}
	return code
}

// render flattens the error member into one human-readable line: the message, plus the raw
// metadata when the server sent any. The metadata is kept verbatim rather than picked apart
// because its shape is provider-specific and it is often the only concrete detail in the
// reply (OpenRouter's metadata.raw holds the originating provider's own error text).
func (e wireError) render() string {
	if len(e.Metadata) == 0 || string(e.Metadata) == "null" {
		return e.Message
	}
	return fmt.Sprintf("%s (metadata: %s)", e.Message, e.Metadata)
}

// toRawResponse assembles the seam RawResponse from the first choice (the loop drives a
// single completion). A reply with no choices yields the zero RawResponse, not a panic.
func (r chatCompletionResponse) toRawResponse() RawResponse {
	var out RawResponse
	if len(r.Choices) > 0 {
		choice := r.Choices[0]
		out.Content = choice.Message.Content
		out.Thinking = choice.Message.ReasoningContent
		out.ToolCalls = choice.Message.ToolCalls
		out.FinishReason = choice.FinishReason
		out.TopCandidates = topCandidates(choice.LogProbs)
	}
	if r.Usage != nil {
		out.Usage = Usage(*r.Usage)
	}
	return out
}

// topCandidates extracts the candidate tokens for the FIRST generated token position — the
// one position every reply has, however short, and therefore the only one a probe can rely
// on. A server that reported logprobs without alternatives still yields the chosen token, so
// "the server exposes logprobs" and "the server exposes nothing" stay distinguishable. nil
// (not an empty slice) means the server exposed no distribution at all.
func topCandidates(lp *logProbsJSON) []string {
	if lp == nil || len(lp.Content) == 0 {
		return nil
	}
	first := lp.Content[0]
	if len(first.TopLogProbs) == 0 {
		if first.Token == "" {
			return nil
		}
		return []string{first.Token}
	}
	out := make([]string, 0, len(first.TopLogProbs))
	for _, c := range first.TopLogProbs {
		out = append(out, c.Token)
	}
	return out
}

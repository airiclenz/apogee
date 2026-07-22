package provider

import "encoding/json"

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

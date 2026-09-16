package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// This file is the Anthropic Messages codec — the wireCodec a server entry with
// `wire: anthropic` speaks (ADR 0078). It owns the request projection onto the Messages body
// (system fold, tool_result / tool_use blocks, output_config.effort), the whole-reply decode
// over anthropicResponse, and the in-band error body; the event-typed SSE parser is its other
// half, in wire_anthropic_stream.go. The JSON shapes live at the foot of this file; nothing
// outside the two branches on the Messages dialect.
//
// Thinking is never requested on this wire in v1: every request carries
// `thinking: {"type":"disabled"}` explicitly, because the current models run adaptive thinking
// when the key is omitted and their thinking blocks are signed and must be replayed verbatim —
// a carrier the seam does not have. The signed-thinking carrier is the follow-up bead
// apogee-4kl. `output_config.effort` is written independently of the thinking mode: it is the
// dial the wire's models read for how hard to work, and the ratified per-wire effort mapping.

const (
	// anthropicMessagesPath is the Messages endpoint every anthropic request is posted to.
	anthropicMessagesPath = "/v1/messages"
	// anthropicVersion is the API version header value the Messages API requires.
	anthropicVersion = "2023-06-01"
	// anthropicDefaultMaxTokens is the `max_tokens` written when the request pins none —
	// the field is mandatory on this wire, where chat-completions leaves it to the server.
	anthropicDefaultMaxTokens = 4096
	// anthropicThinkingDisabled is the one thinking mode this wire ever asks for.
	anthropicThinkingDisabled = "disabled"
)

// anthropicCodec speaks the Anthropic Messages protocol on behalf of one Client. The Messages
// path is fixed and the version header is a constant of the wire; the one thing it holds is
// that Client, for the codec-neutral service the stream parser needs mid-decode — the
// in-band error renderer, inBandErrorDelta (wire_anthropic_stream.go).
type anthropicCodec struct {
	client *Client
}

// path is the Messages endpoint; the chat path option is a chat-completions fact and is not read.
func (a *anthropicCodec) path() string { return anthropicMessagesPath }

// headers carries the API key as `x-api-key` and always the `anthropic-version` the Messages API
// requires — so a keyless request (a local proxy) still speaks a version. The JSON content type
// is the Client's, set on every wire.
func (a *anthropicCodec) headers(apiKey string) map[string]string {
	h := map[string]string{"anthropic-version": anthropicVersion}
	if apiKey != "" {
		h["x-api-key"] = apiKey
	}
	return h
}

// encode marshals the Messages body for req and reports whether it wrote an effort — the gate
// on thinkingEffortHint, as chatRequest.carriesEffort is on the openai wire.
func (a *anthropicCodec) encode(req Request) ([]byte, bool, error) {
	wire, err := a.buildBody(req)
	if err != nil {
		return nil, false, err
	}
	body, err := json.Marshal(wire)
	if err != nil {
		return nil, false, err
	}
	return body, wire.OutputConfig != nil, nil
}

// decodeWhole decodes one non-streamed reply. An error body — `{type:"error",error:{…}}`, which
// the API can also frame on an HTTP 200 behind an aggregator — comes back as the wireError with
// a zero RawResponse, never as an empty reply; a body that fails to decode returns the bare
// decode error for the Client to wrap.
func (a *anthropicCodec) decodeWhole(body io.Reader) (RawResponse, *wireError, error) {
	var decoded anthropicResponse
	if err := json.NewDecoder(body).Decode(&decoded); err != nil {
		return RawResponse{}, nil, err
	}
	if decoded.Error != nil {
		return RawResponse{}, decoded.Error.wireError(), nil
	}
	return decoded.toRawResponse(), nil, nil
}

// buildBody projects a Request onto the Messages body: every system message folds into the
// top-level `system` (blank-line joined, in order), the rest become content-block messages
// (see anthropicMessages), tools become `tools[]` with their schema under `input_schema`,
// `max_tokens` is always present (anthropicDefaultMaxTokens when the request pins none),
// the sampling knobs the wire knows are written only when set — there is no repeat penalty on
// this wire, so Sampling.RepeatPenalty is dropped — `thinking` is always disabled, and a named
// effort at or above low lands in `output_config.effort` (off/none/minimal ask for nothing
// and are omitted: the Messages API has no rung below low, and thinking is off regardless).
func (a *anthropicCodec) buildBody(req Request) (anthropicRequest, error) {
	body := anthropicRequest{
		Model:     req.Model,
		Stream:    req.Stream,
		MaxTokens: anthropicDefaultMaxTokens,
		Thinking:  anthropicThinking{Type: anthropicThinkingDisabled},
	}

	system, messages, err := anthropicMessages(req.Messages, len(req.Tools) > 0)
	if err != nil {
		return anthropicRequest{}, err
	}
	body.System = system
	body.Messages = messages

	s := req.Sampling
	if s.MaxTokens != nil {
		body.MaxTokens = *s.MaxTokens
	}
	body.Temperature = s.Temperature
	body.TopP = s.TopP
	body.TopK = s.TopK

	if effort, ok := anthropicEffort(req.ThinkingEffort); ok {
		body.OutputConfig = &anthropicOutputConfig{Effort: effort}
	}

	if len(req.Tools) > 0 {
		body.Tools = make([]anthropicTool, 0, len(req.Tools))
		for _, t := range req.Tools {
			body.Tools = append(body.Tools, anthropicTool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.Parameters,
			})
		}
	}
	return body, nil
}

// anthropicEffort maps a seam Effort onto the `output_config.effort` vocabulary: the five
// levels low..max pass through by name; off, none and minimal — rungs the Messages API does
// not have — and an absent or unrecognised effort write nothing.
func anthropicEffort(e Effort) (string, bool) {
	switch e {
	case EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return string(e), true
	default:
		return "", false
	}
}

// anthropicMessages renders the seam messages onto the wire: system messages are lifted out
// into the returned system text; a run of consecutive tool-result messages folds into ONE user
// message of tool_result blocks (the API wants every result of a parallel call in a single
// turn); an assistant message carries a text block for its content and one tool_use block per
// call. A message that ends up with no block at all — an assistant turn with neither text nor
// calls — is dropped: the API refuses an empty content array.
//
// Without tools the wire refuses tool_use and tool_result blocks outright (a 400 naming the
// missing `tools[]`), so a tool history sent with none — the compaction summariser does exactly
// that — is folded to prose: a tool result becomes plain user text and an assistant call is
// rendered as text (anthropicToolCallText), never as a block.
func anthropicMessages(msgs []Message, hasTools bool) (string, []anthropicMessage, error) {
	var systems []string
	out := make([]anthropicMessage, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "system":
			systems = append(systems, m.Content)
		case "tool":
			if !hasTools {
				out = appendMessage(out, "user", textBlocks(m.Content))
				continue
			}
			block := anthropicBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content}
			if n := len(out); n > 0 && out[n-1].isToolResults {
				out[n-1].Content = append(out[n-1].Content, block)
				continue
			}
			out = append(out, anthropicMessage{Role: "user", Content: []anthropicBlock{block}, isToolResults: true})
		case "assistant":
			blocks, err := assistantBlocks(m, hasTools)
			if err != nil {
				return "", nil, err
			}
			out = appendMessage(out, "assistant", blocks)
		default:
			out = appendMessage(out, "user", textBlocks(m.Content))
		}
	}
	return strings.Join(systems, "\n\n"), out, nil
}

// appendMessage appends one message of blocks to out, dropping a message that has none.
func appendMessage(out []anthropicMessage, role string, blocks []anthropicBlock) []anthropicMessage {
	if len(blocks) == 0 {
		return out
	}
	return append(out, anthropicMessage{Role: role, Content: blocks})
}

// textBlocks is one text block for content, or none for empty content.
func textBlocks(content string) []anthropicBlock {
	if content == "" {
		return nil
	}
	return []anthropicBlock{{Type: "text", Text: content}}
}

// assistantBlocks renders an assistant message: its text, then one tool_use block per call
// whose `input` is the call's argument string re-marshalled as an object — an argument string
// that is not valid JSON is an encode error naming the call, since the wire cannot carry it.
// Without tools the calls are appended to the text instead (see anthropicMessages).
func assistantBlocks(m Message, hasTools bool) ([]anthropicBlock, error) {
	if !hasTools {
		text := m.Content
		for _, tc := range m.ToolCalls {
			if text != "" {
				text += "\n"
			}
			text += anthropicToolCallText(tc)
		}
		return textBlocks(text), nil
	}

	blocks := textBlocks(m.Content)
	for _, tc := range m.ToolCalls {
		input, err := anthropicToolInput(tc)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, anthropicBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}
	return blocks, nil
}

// anthropicToolInput re-marshals a call's raw argument string as the object `input` carries:
// the wire wants a JSON object, and an empty argument string — what some models emit for a
// no-argument call — is the empty object.
func anthropicToolInput(tc ToolCall) (json.RawMessage, error) {
	args := strings.TrimSpace(tc.Function.Arguments)
	if args == "" {
		return json.RawMessage(`{}`), nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args), &object); err != nil {
		return nil, fmt.Errorf("apogee: tool call %s: arguments are not a JSON object: %w", tc.ID, err)
	}
	compact := &bytes.Buffer{}
	if err := json.Compact(compact, []byte(args)); err != nil {
		return nil, fmt.Errorf("apogee: tool call %s: arguments are not a JSON object: %w", tc.ID, err)
	}
	return json.RawMessage(compact.Bytes()), nil
}

// anthropicToolCallText is the prose form of a tool call for a request that offers no tools —
// the name applied to its raw arguments, the way the model would have written it.
func anthropicToolCallText(tc ToolCall) string {
	return fmt.Sprintf("%s(%s)", tc.Function.Name, tc.Function.Arguments)
}

// anthropicRequest is the Messages request body. Sampling pointers, tools, system and
// output_config are omitted when unset; model, messages, max_tokens, stream and thinking are
// always present.
type anthropicRequest struct {
	Model        string                 `json:"model,omitempty"`
	System       string                 `json:"system,omitempty"`
	Messages     []anthropicMessage     `json:"messages"`
	MaxTokens    int                    `json:"max_tokens"`
	Stream       bool                   `json:"stream"`
	Temperature  *float64               `json:"temperature,omitempty"`
	TopP         *float64               `json:"top_p,omitempty"`
	TopK         *int                   `json:"top_k,omitempty"`
	Tools        []anthropicTool        `json:"tools,omitempty"`
	Thinking     anthropicThinking      `json:"thinking"`
	OutputConfig *anthropicOutputConfig `json:"output_config,omitempty"`
}

// anthropicThinking is the `thinking` object — only ever {"type":"disabled"} from this codec.
type anthropicThinking struct {
	Type string `json:"type"`
}

// anthropicOutputConfig is the `output_config` object; effort is the one member written.
type anthropicOutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

// anthropicTool is one `tools[]` entry: the seam ToolSpec with its schema under input_schema.
type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// anthropicMessage is one wire message: a role and its content blocks. isToolResults marks a
// user message this codec built from tool results so the next result folds into it; it never
// reaches the wire.
type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`

	isToolResults bool
}

// anthropicBlock is one content block in either direction: text, tool_use (id/name/input),
// tool_result (tool_use_id/content/is_error) or thinking. The union is flat because every
// member is omitted when zero, so each block type serialises to exactly its own keys.
type anthropicBlock struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result — IsError is a pointer so it is absent, not false, when the seam carries
	// no outcome (it does not today).
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   *bool  `json:"is_error,omitempty"`
	// thinking
	Thinking string `json:"thinking,omitempty"`
}

// anthropicResponse is the whole-reply body, or the error body the API frames as
// `{"type":"error","error":{…}}` — the Error member decides which.
type anthropicResponse struct {
	Type       string           `json:"type"`
	Model      string           `json:"model"`
	Content    []anthropicBlock `json:"content"`
	StopReason string           `json:"stop_reason"`
	Usage      *anthropicUsage  `json:"usage"`
	Error      *anthropicError  `json:"error"`
}

// anthropicError is the `error` member: a class slug and a message.
type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// wireError maps the Messages error onto the seam's in-band error: the slug rides ErrorType,
// where classify reads a class when there is no numeric code (this wire sends none).
func (e anthropicError) wireError() *wireError {
	return &wireError{Message: e.Message, ErrorType: e.Type}
}

// anthropicUsage is the token accounting of a reply. cache_read_input_tokens are the prompt
// tokens served from the prefix cache and are NOT included in input_tokens on this wire.
type anthropicUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	CacheReadTokens int `json:"cache_read_input_tokens"`
}

// usage maps the wire accounting onto the seam Usage: the prompt is input plus cache reads
// (the model read both), the cache reads are reported on their own as well, and the total
// is the sum.
func (u anthropicUsage) usage() Usage {
	prompt := u.InputTokens + u.CacheReadTokens
	return Usage{
		PromptTokens:       prompt,
		CompletionTokens:   u.OutputTokens,
		TotalTokens:        prompt + u.OutputTokens,
		CachedPromptTokens: u.CacheReadTokens,
	}
}

// toRawResponse assembles the seam RawResponse: text blocks concatenate into Content, thinking
// blocks into Thinking, each tool_use block is one ToolCall with its input re-stringified as
// the argument string, and the stop reason is mapped onto the chat-completions vocabulary the
// loop reads (anthropicFinishReason).
func (r anthropicResponse) toRawResponse() RawResponse {
	out := RawResponse{Model: r.Model, FinishReason: anthropicFinishReason(r.StopReason)}
	for _, b := range r.Content {
		switch b.Type {
		case "text":
			out.Content += b.Text
		case "thinking":
			out.Thinking += b.Thinking
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:       b.ID,
				Type:     "function",
				Function: FunctionCall{Name: b.Name, Arguments: anthropicToolArguments(b.Input)},
			})
		}
	}
	if r.Usage != nil {
		out.Usage = r.Usage.usage()
	}
	return out
}

// anthropicToolArguments is the argument string of a tool_use block: its input object as
// compact JSON, or the empty object when the block carried none. The decoder already proved
// the input well-formed, so a compaction that still fails keeps the bytes as they came.
func anthropicToolArguments(input json.RawMessage) string {
	if len(input) == 0 {
		return "{}"
	}
	compact := &bytes.Buffer{}
	if err := json.Compact(compact, input); err != nil {
		return string(input)
	}
	return compact.String()
}

// anthropicFinishReason maps a Messages stop_reason onto the finish vocabulary the loop reads:
// end_turn and stop_sequence are "stop", max_tokens is "length", tool_use is "tool_calls", and
// anything else (refusal, pause_turn, a future value) passes through unchanged.
func anthropicFinishReason(stop string) string {
	switch stop {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return stop
	}
}

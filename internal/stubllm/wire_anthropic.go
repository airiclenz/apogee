package stubllm

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

// This file holds the literal Anthropic Messages JSON — the shapes POST /v1/messages reads and
// writes — kept apart from wire.go's chat-completions shapes for the same reason
// internal/provider keeps wire_anthropic.go apart from wirejson.go: each wire's schema is a
// contract with real servers and reads best as one uninterrupted list. The request side reduces
// to the same neutral [Request] the chat route logs, so a Script, a matcher and a capture are
// indifferent to which wire apogee spoke; the reply side renders one Turn as the event-typed
// SSE and the whole-message body the Messages API produces.

// messageID is the id every Messages reply carries; nothing reads it, as with completionID.
const messageID = "msg_stubllm"

// The Messages stream event types, as each payload's `type` names them.
const (
	eventMessageStart = "message_start"
	eventBlockStart   = "content_block_start"
	eventBlockDelta   = "content_block_delta"
	eventBlockStop    = "content_block_stop"
	eventMessageDelta = "message_delta"
	eventMessageStop  = "message_stop"
	eventError        = "error"
)

// The content block and fragment types the stub writes.
const (
	blockText       = "text"
	blockThinking   = "thinking"
	blockToolUse    = "tool_use"
	blockToolResult = "tool_result"
	deltaText       = "text_delta"
	deltaThinking   = "thinking_delta"
	deltaInputJSON  = "input_json_delta"
)

// anthropicRequest is the subset of the POST /v1/messages request the stub reads: enough to
// log what was asked, to match a Turn against it, and to choose the reply shape. The sampling
// pointers keep the absent-versus-zero distinction chatRequest keeps.
type anthropicRequest struct {
	Model        string                 `json:"model"`
	Stream       bool                   `json:"stream"`
	System       string                 `json:"system"`
	Messages     []anthropicMessage     `json:"messages"`
	Tools        []anthropicTool        `json:"tools"`
	MaxTokens    *int                   `json:"max_tokens"`
	Temperature  *float64               `json:"temperature"`
	OutputConfig *anthropicOutputConfig `json:"output_config"`
}

// anthropicOutputConfig is the `output_config` object; effort is the one member read.
type anthropicOutputConfig struct {
	Effort string `json:"effort"`
}

// anthropicTool is one offered tool; only the name is read, as with chatTool.
type anthropicTool struct {
	Name string `json:"name"`
}

// anthropicMessage is one wire message: a role and its content blocks.
type anthropicMessage struct {
	Role    string           `json:"role"`
	Content anthropicContent `json:"content"`
}

// anthropicContent is a message's content, which the Messages API accepts either as a plain
// string — one text block — or as an array of blocks. The provider client always sends blocks;
// the string form is accepted so a hand-built request in a test can stay short.
type anthropicContent []anthropicBlock

// UnmarshalJSON accepts both spellings of content.
func (c *anthropicContent) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		*c = anthropicContent{{Type: blockText, Text: &text}}
		return nil
	}
	var blocks []anthropicBlock
	if err := json.Unmarshal(data, &blocks); err != nil {
		return err
	}
	*c = blocks
	return nil
}

// anthropicBlock is one content block in either direction: text, thinking, tool_use
// (id/name/input) or tool_result (tool_use_id/content). The union is flat because every member
// is omitted when zero, so each block type serialises to exactly its own keys. Text and Thinking
// are pointers on purpose: a streamed block OPENS as `{"type":"text","text":""}` — the empty
// string present, the way the real API frames it — and the pointer is what lets the encoder
// write that empty member here while omitting it on every other block type.
type anthropicBlock struct {
	Type      string           `json:"type"`
	Text      *string          `json:"text,omitempty"`
	Thinking  *string          `json:"thinking,omitempty"`
	ID        string           `json:"id,omitempty"`
	Name      string           `json:"name,omitempty"`
	Input     json.RawMessage  `json:"input,omitempty"`
	ToolUseID string           `json:"tool_use_id,omitempty"`
	Content   anthropicContent `json:"content,omitempty"`
}

// text is the block's text, "" when it carries none.
func (b anthropicBlock) text() string {
	if b.Text == nil {
		return ""
	}
	return *b.Text
}

// logEntry reduces the request to the neutral log shape: the top-level system as a leading
// role-system Message (so a `when.system` match and a `from: system` capture read it exactly as
// they read a chat-completions system message), each user message's text blocks as one user
// Message and its tool_result blocks as role-tool Messages carrying the call id, and each
// assistant message's text and tool_use blocks as one assistant Message with ToolCalls. The
// output_config effort lands on Effort's fourth member. N, TurnIndex and At are take's to fill.
func (r anthropicRequest) logEntry() Request {
	return Request{
		Wire:     WireAnthropic,
		Model:    r.Model,
		Messages: r.messages(),
		Tools:    r.toolNames(),
		Stream:   r.Stream,
		Sampling: Sampling{MaxTokens: r.MaxTokens, Temperature: r.Temperature},
		Effort:   r.effort(),
	}
}

// messages reduces the wire messages to the log's shape (see logEntry).
func (r anthropicRequest) messages() []Message {
	out := make([]Message, 0, len(r.Messages)+1)
	if r.System != "" {
		out = append(out, Message{Role: "system", Content: r.System})
	}
	for _, m := range r.Messages {
		switch m.Role {
		case "assistant":
			out = append(out, assistantMessage(m.Content))
		default:
			out = append(out, userMessages(m.Role, m.Content)...)
		}
	}
	return out
}

// assistantMessage folds an assistant message's blocks: text blocks concatenate into Content,
// each tool_use block is one ToolCall whose Arguments are its input object as compact JSON —
// the string the chat wire carries, so a `tool_result` matcher finds the call by id either way.
func assistantMessage(blocks []anthropicBlock) Message {
	message := Message{Role: "assistant"}
	for _, b := range blocks {
		switch b.Type {
		case blockText:
			message.Content += b.text()
		case blockToolUse:
			message.ToolCalls = append(message.ToolCalls, ToolCall{
				ID:        b.ID,
				Name:      b.Name,
				Arguments: toolArguments(b.Input),
			})
		}
	}
	return message
}

// userMessages splits a user message into the log's shape: a run of text blocks is one Message
// of the wire role, and every tool_result block is a role-tool Message of its own, in block
// order — the two shapes a chat-completions request carries as separate messages.
func userMessages(role string, blocks []anthropicBlock) []Message {
	var out []Message
	var text strings.Builder
	flush := func() {
		if text.Len() == 0 {
			return
		}
		out = append(out, Message{Role: role, Content: text.String()})
		text.Reset()
	}
	for _, b := range blocks {
		switch b.Type {
		case blockText:
			text.WriteString(b.text())
		case blockToolResult:
			flush()
			out = append(out, Message{Role: "tool", ToolCallID: b.ToolUseID, Content: blocksText(b.Content)})
		}
	}
	flush()
	return out
}

// blocksText is the concatenated text of a tool_result's content blocks.
func blocksText(blocks []anthropicBlock) string {
	var text strings.Builder
	for _, b := range blocks {
		if b.Type == blockText {
			text.WriteString(b.text())
		}
	}
	return text.String()
}

// toolArguments is a tool_use input as the argument string the log carries: compact JSON, or
// the empty object when the block carried none. Input that will not compact keeps its bytes.
func toolArguments(input json.RawMessage) string {
	if len(input) == 0 {
		return defaultToolArguments
	}
	compact := &bytes.Buffer{}
	if err := json.Compact(compact, input); err != nil {
		return string(input)
	}
	return compact.String()
}

// effort reduces the request's effort dial to the log's shape.
func (r anthropicRequest) effort() Effort {
	if r.OutputConfig == nil {
		return Effort{}
	}
	return Effort{OutputEffort: r.OutputConfig.Effort}
}

// toolNames is the names of the tools the request offered, in wire order.
func (r anthropicRequest) toolNames() []string {
	if len(r.Tools) == 0 {
		return nil
	}
	out := make([]string, 0, len(r.Tools))
	for _, tool := range r.Tools {
		out = append(out, tool.Name)
	}
	return out
}

// anthropicEvent is one streamed payload. Every event type populates a different subset, and
// every member is omitted when unset so each event serialises to exactly the keys the real
// API writes for it.
type anthropicEvent struct {
	Type         string              `json:"type"`
	Index        *int                `json:"index,omitempty"`
	Message      *anthropicReply     `json:"message,omitempty"`
	ContentBlock *anthropicBlock     `json:"content_block,omitempty"`
	Delta        *anthropicDelta     `json:"delta,omitempty"`
	Usage        *anthropicUsage     `json:"usage,omitempty"`
	Error        *anthropicWireError `json:"error,omitempty"`
}

// anthropicDelta is a content_block_delta's fragment or a message_delta's closing metadata.
// StopSequence is raw so message_delta alone writes the `null` the real API writes there
// (jsonNull), and a block fragment carries no such key.
type anthropicDelta struct {
	Type         string          `json:"type,omitempty"`
	Text         string          `json:"text,omitempty"`
	PartialJSON  string          `json:"partial_json,omitempty"`
	Thinking     string          `json:"thinking,omitempty"`
	StopReason   string          `json:"stop_reason,omitempty"`
	StopSequence json.RawMessage `json:"stop_sequence,omitempty"`
}

// jsonNull is the literal null message_delta writes for stop_sequence.
var jsonNull = json.RawMessage("null")

// anthropicReply is the whole-message body, and the `message` member message_start carries.
// StopReason and StopSequence are pointers so message_start writes them as `null`, which is
// what the real API sends before the reply has ended.
type anthropicReply struct {
	ID           string           `json:"id"`
	Type         string           `json:"type"`
	Role         string           `json:"role"`
	Model        string           `json:"model"`
	Content      []anthropicBlock `json:"content"`
	StopReason   *string          `json:"stop_reason"`
	StopSequence *string          `json:"stop_sequence"`
	Usage        *anthropicUsage  `json:"usage,omitempty"`
}

// anthropicUsage is the accounting object. Pointers keep an event that reports only one side
// — message_start the input, message_delta the output — from writing zeros for the other.
type anthropicUsage struct {
	InputTokens     *int `json:"input_tokens,omitempty"`
	OutputTokens    *int `json:"output_tokens,omitempty"`
	CacheReadTokens *int `json:"cache_read_input_tokens,omitempty"`
}

// anthropicErrorBody is the error body: `{"type":"error","error":{…}}`, the whole reply on the
// non-streamed path and an `error` event's payload on the streamed one.
type anthropicErrorBody struct {
	Type  string             `json:"type"`
	Error anthropicWireError `json:"error"`
}

// anthropicWireError is the `error` member: a class slug and a message.
type anthropicWireError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// The Messages API's error-class slugs the stub renders a scripted `error` turn as.
const (
	slugInvalidRequest  = "invalid_request_error"
	slugAuthentication  = "authentication_error"
	slugPermission      = "permission_error"
	slugNotFound        = "not_found_error"
	slugRequestTooLarge = "request_too_large"
	slugRateLimit       = "rate_limit_error"
	slugAPI             = "api_error"
	slugOverloaded      = "overloaded_error"
)

// anthropicSlug is the class slug a scripted in-band error code renders as on the Messages wire,
// where an error carries no number and the slug is the whole verdict. The 4xx codes map to the
// classes the real API names for them; a 500 is the plain api_error; every other 5xx — the
// default 502 included — is the overloaded class, so an `error` turn a fixture scripts as
// retryable is retryable on this wire too (internal/provider's classify reads exactly that
// slug as transient).
func anthropicSlug(code int) string {
	switch code {
	case http.StatusBadRequest:
		return slugInvalidRequest
	case http.StatusUnauthorized:
		return slugAuthentication
	case http.StatusForbidden:
		return slugPermission
	case http.StatusNotFound:
		return slugNotFound
	case http.StatusRequestEntityTooLarge:
		return slugRequestTooLarge
	case http.StatusTooManyRequests:
		return slugRateLimit
	case http.StatusInternalServerError:
		return slugAPI
	}
	if code >= http.StatusInternalServerError {
		return slugOverloaded
	}
	return slugAPI
}

// anthropicWire is the error member this scripted error reaches the Messages wire as.
func (e InBandError) anthropicWire() anthropicWireError {
	return anthropicWireError{Type: anthropicSlug(e.code()), Message: e.Message}
}

// stopReason is the Messages stop_reason this Turn ends on: its finish_reason mapped onto the
// wire's vocabulary — stop is end_turn, tool_calls is tool_use, length is max_tokens — and any
// other scripted value passed through unchanged.
func (t Turn) stopReason() string {
	switch reason := t.finishReason(); reason {
	case "stop":
		return "end_turn"
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return reason
	}
}

// anthropicUsageOf renders the scripted accounting onto the Messages wire, where the cached
// share is NOT part of input_tokens: the input is the prompt minus the cached reads, and the
// cached reads ride cache_read_input_tokens on their own — so what the provider client adds
// back up is exactly the prompt the fixture scripted. Every member is written, zero included,
// the way the real API writes them on a whole reply; the streamed path splits it across
// message_start (inputSide) and message_delta (outputSide) as the real stream does.
func anthropicUsageOf(u Usage) *anthropicUsage {
	input, output, cached := u.Prompt-u.Cached, u.Completion, u.Cached
	return &anthropicUsage{InputTokens: &input, OutputTokens: &output, CacheReadTokens: &cached}
}

// inputSide is the accounting message_start carries: the input tokens and the cached reads.
func (u *anthropicUsage) inputSide() *anthropicUsage {
	return &anthropicUsage{InputTokens: u.InputTokens, CacheReadTokens: u.CacheReadTokens}
}

// outputSide is the accounting message_delta carries: the output tokens alone.
func (u *anthropicUsage) outputSide() *anthropicUsage {
	return &anthropicUsage{OutputTokens: u.OutputTokens}
}

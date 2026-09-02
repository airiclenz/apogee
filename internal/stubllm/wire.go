package stubllm

// This file holds the literal OpenAI chat-completions JSON — the shapes on the wire — kept
// apart from server.go's transport logic for the same reason internal/provider keeps
// wirejson.go apart from client.go: the schema is a contract with real servers and reads best
// as one uninterrupted list, while the code around it is about timing and framing.

// modelsReply is the GET /v1/models payload.
type modelsReply struct {
	Object string       `json:"object"`
	Data   []modelEntry `json:"data"`
}

type modelEntry struct {
	ID     string `json:"id"`
	Object string `json:"object"`
}

// chatRequest is the subset of the POST /v1/chat/completions request the stub reads: enough to
// log what was asked, to match a Turn against it, and to choose the reply shape.
type chatRequest struct {
	Model    string        `json:"model"`
	Stream   bool          `json:"stream"`
	Messages []chatMessage `json:"messages"`
	Tools    []chatTool    `json:"tools"`
}

// chatMessage is one message off a request. Content is a pointer because a tool-call-only
// assistant turn serialises it as JSON null, which is absence rather than an empty string.
type chatMessage struct {
	Role       string         `json:"role"`
	Content    *string        `json:"content"`
	ToolCallID string         `json:"tool_call_id"`
	ToolCalls  []wireToolCall `json:"tool_calls"`
}

// chatTool is one offered tool; only the name is read, because that is all a matcher or an
// assertion about "which tools were on the menu" needs.
type chatTool struct {
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

// messages reduces the request's wire messages to the log's shape.
func (r chatRequest) messages() []Message {
	out := make([]Message, 0, len(r.Messages))
	for _, m := range r.Messages {
		message := Message{Role: m.Role, ToolCallID: m.ToolCallID}
		if m.Content != nil {
			message.Content = *m.Content
		}
		for _, call := range m.ToolCalls {
			message.ToolCalls = append(message.ToolCalls, ToolCall{
				ID:        call.ID,
				Name:      call.Function.Name,
				Arguments: call.Function.Arguments,
			})
		}
		out = append(out, message)
	}
	return out
}

// toolNames is the names of the tools the request offered, in wire order.
func (r chatRequest) toolNames() []string {
	if len(r.Tools) == 0 {
		return nil
	}
	out := make([]string, 0, len(r.Tools))
	for _, tool := range r.Tools {
		out = append(out, tool.Function.Name)
	}
	return out
}

// sseEnvelope is one streamed data event. Choices is omitted on the terminal usage event,
// which is the shape servers send when stream_options.include_usage is on.
type sseEnvelope struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Model   string      `json:"model,omitempty"`
	Choices []sseChoice `json:"choices,omitempty"`
	Usage   *usageWire  `json:"usage,omitempty"`
}

type sseChoice struct {
	Index        int      `json:"index"`
	Delta        sseDelta `json:"delta"`
	FinishReason string   `json:"finish_reason,omitempty"`
}

// sseDelta is the incremental payload of one streamed choice. It carries BOTH wire spellings of
// the thinking channel, and the emitters fill exactly one of them (Turn.spellsBareReasoning), so
// a script that does not choose produces the bytes it always did.
type sseDelta struct {
	Content          string        `json:"content,omitempty"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
	Reasoning        string        `json:"reasoning,omitempty"`
	ToolCalls        []sseToolCall `json:"tool_calls,omitempty"`
}

// sseToolCall is one tool-call fragment: the first carries id, type and name, the rest carry
// only more argument text.
type sseToolCall struct {
	Index    int         `json:"index"`
	ID       string      `json:"id,omitempty"`
	Type     string      `json:"type,omitempty"`
	Function sseFunction `json:"function"`
}

type sseFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

// wholeReply is the non-streamed completion.
type wholeReply struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Model   string        `json:"model,omitempty"`
	Choices []wholeChoice `json:"choices"`
	Usage   *usageWire    `json:"usage,omitempty"`
}

type wholeChoice struct {
	Index        int          `json:"index"`
	Message      wholeMessage `json:"message"`
	FinishReason string       `json:"finish_reason"`
}

// wholeMessage is the assistant message of a non-streamed reply. Its two reasoning fields are
// the sseDelta's, for the same reason and under the same rule: exactly one is ever filled.
type wholeMessage struct {
	Role             string         `json:"role"`
	Content          string         `json:"content"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	Reasoning        string         `json:"reasoning,omitempty"`
	ToolCalls        []wireToolCall `json:"tool_calls,omitempty"`
}

// wireToolCall is a whole tool call, both on a request's assistant message and on a
// non-streamed reply.
type wireToolCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function sseFunction `json:"function"`
}

// usageWire is the accounting object. PromptTokensDetails is a pointer so it is omitted
// entirely unless the script asked for a cached share (see usageOf).
type usageWire struct {
	PromptTokens        int                  `json:"prompt_tokens"`
	CompletionTokens    int                  `json:"completion_tokens"`
	TotalTokens         int                  `json:"total_tokens"`
	PromptTokensDetails *promptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

type promptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

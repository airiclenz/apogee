package stubllm

// This file holds the literal OpenAI chat-completions JSON — the shapes on the wire — kept
// apart from server.go's transport logic for the same reason internal/provider keeps
// wirejson.go apart from client.go: the schema is a contract with real servers and reads best
// as one uninterrupted list, while the code around it is about timing and framing.

// modelsReply is the GET /v1/models payload in the OpenAI list shape.
type modelsReply struct {
	Object string       `json:"object"`
	Data   []modelEntry `json:"data"`
}

// modelEntry is one advertised model: the OpenAI members every server writes, plus the
// optional ones internal/provider reads — OpenRouter's `name`, `context_length` and
// `reasoning`, llama.cpp's `meta.n_ctx_train`. The optional members are omitted when unset so
// a Script that names only an id renders the minimal entry a plain OpenAI server sends, and
// `reasoning` is a pointer because its PRESENCE is the tell the client reads.
type modelEntry struct {
	ID            string         `json:"id"`
	Object        string         `json:"object"`
	Name          string         `json:"name,omitempty"`
	ContextLength int            `json:"context_length,omitempty"`
	Meta          *modelMeta     `json:"meta,omitempty"`
	Reasoning     *reasoningWire `json:"reasoning,omitempty"`
}

// modelMeta is llama.cpp's per-model `meta` object; the training window is the one member read.
type modelMeta struct {
	NCtxTrain int `json:"n_ctx_train"`
}

// reasoningWire is the per-model `reasoning` object, as OpenRouter writes it.
type reasoningWire struct {
	SupportedEfforts []string `json:"supported_efforts,omitempty"`
	DefaultEffort    string   `json:"default_effort,omitempty"`
	Mandatory        bool     `json:"mandatory,omitempty"`
}

// propsReply is the GET /props payload: the subset of llama.cpp's that internal/provider reads.
type propsReply struct {
	DefaultGenerationSettings struct {
		NCtx int `json:"n_ctx"`
	} `json:"default_generation_settings"`
	TotalSlots   int    `json:"total_slots"`
	ChatTemplate string `json:"chat_template"`
}

// openAIModels renders the advertised list in the OpenAI shape.
func openAIModels(models []DiscoveredModel) modelsReply {
	reply := modelsReply{Object: "list", Data: make([]modelEntry, 0, len(models))}
	for _, model := range models {
		entry := modelEntry{ID: model.ID, Object: "model", Name: model.Name, ContextLength: model.ContextLength}
		if model.NCtxTrain != 0 {
			entry.Meta = &modelMeta{NCtxTrain: model.NCtxTrain}
		}
		if model.Reasoning != nil {
			entry.Reasoning = &reasoningWire{
				SupportedEfforts: model.Reasoning.SupportedEfforts,
				DefaultEffort:    model.Reasoning.DefaultEffort,
				Mandatory:        model.Reasoning.Mandatory,
			}
		}
		reply.Data = append(reply.Data, entry)
	}
	return reply
}

// reply renders the scripted launch facts as the /props payload.
func (p Props) reply() propsReply {
	var reply propsReply
	reply.DefaultGenerationSettings.NCtx = p.NCtx
	reply.TotalSlots = p.TotalSlots
	reply.ChatTemplate = p.ChatTemplate
	return reply
}

// discovered reads a recorded OpenAI-shaped list back into the Script's form, which is how a
// real server's self-description reaches a fixture.
func (r modelsReply) discovered() []DiscoveredModel {
	models := make([]DiscoveredModel, 0, len(r.Data))
	for _, entry := range r.Data {
		model := DiscoveredModel{ID: entry.ID, Name: entry.Name, ContextLength: entry.ContextLength}
		if entry.Meta != nil {
			model.NCtxTrain = entry.Meta.NCtxTrain
		}
		if entry.Reasoning != nil {
			model.Reasoning = &ModelReasoning{
				SupportedEfforts: entry.Reasoning.SupportedEfforts,
				DefaultEffort:    entry.Reasoning.DefaultEffort,
				Mandatory:        entry.Reasoning.Mandatory,
			}
		}
		models = append(models, model)
	}
	return models
}

// props reads a recorded /props payload back into the Script's form.
func (r propsReply) props() *Props {
	return &Props{
		NCtx:         r.DefaultGenerationSettings.NCtx,
		TotalSlots:   r.TotalSlots,
		ChatTemplate: r.ChatTemplate,
	}
}

// chatRequest is the subset of the POST /v1/chat/completions request the stub reads: enough to
// log what was asked, to match a Turn against it, and to choose the reply shape. The sampling
// and thinking-effort keys are pointers and maps so an ABSENT key logs as nil rather than as a
// zero — a request that asked for nothing and one that asked for zero are different requests,
// and a test about "nothing reached the wire" needs to tell them apart.
type chatRequest struct {
	Model       string        `json:"model"`
	Stream      bool          `json:"stream"`
	Messages    []chatMessage `json:"messages"`
	Tools       []chatTool    `json:"tools"`
	MaxTokens   *int          `json:"max_tokens"`
	Temperature *float64      `json:"temperature"`
	// The three shapes a thinking-effort intent takes on the wire (internal/provider's
	// applyEffort): llama.cpp's chat-template kwargs, OpenRouter's `reasoning` object and the
	// top-level `reasoning_effort` string OpenAI and Groq read.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs"`
	Reasoning          map[string]any `json:"reasoning"`
	ReasoningEffort    *string        `json:"reasoning_effort"`
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

// sampling reduces the request's sampling keys to the log's shape.
func (r chatRequest) sampling() Sampling {
	return Sampling{MaxTokens: r.MaxTokens, Temperature: r.Temperature}
}

// effort reduces the request's thinking-effort keys to the log's shape: each recorded exactly
// as the body carried it, and nil where the body carried none.
func (r chatRequest) effort() Effort {
	out := Effort{ChatTemplateKwargs: r.ChatTemplateKwargs, Reasoning: r.Reasoning}
	if r.ReasoningEffort != nil {
		out.ReasoningEffort = *r.ReasoningEffort
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
// which is the shape servers send when stream_options.include_usage is on, and on the in-band
// error event an `error` turn ends with.
type sseEnvelope struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Model   string      `json:"model,omitempty"`
	Choices []sseChoice `json:"choices,omitempty"`
	Usage   *usageWire  `json:"usage,omitempty"`
	Error   *wireError  `json:"error,omitempty"`
}

// wireError is the in-band failure member an aggregator delivers on an HTTP 200 — as an SSE
// data event on the streamed path, inside the JSON body on the whole-reply one. Code is a
// number here because that is what a scripted `error` turn carries; the slug spellings some
// aggregators send instead are not scripted.
type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
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

// wholeReply is the non-streamed completion. Error is the in-band member an `error` turn
// adds; absent on every healthy reply.
type wholeReply struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Model   string        `json:"model,omitempty"`
	Choices []wholeChoice `json:"choices"`
	Usage   *usageWire    `json:"usage,omitempty"`
	Error   *wireError    `json:"error,omitempty"`
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

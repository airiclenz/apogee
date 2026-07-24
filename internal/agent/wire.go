package agent

import (
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/processing"
	"github.com/airiclenz/apogee/internal/provider"
)

// toProviderRequest drains the post-hook req onto the provider seam's wire shape — the
// translation boundary between the loop's domain state and the domain-free provider.Request
// (ADR 0010). It carries messages (with tool calls + tool-call IDs, load-bearing for a
// multi-Turn tool exchange), the tool menu, and the sampling a pre-request hook shaped; the
// provider wire has no carrier for SetExtra fields yet (response_format is a Phase-4 concern).
func (a *Agent) toProviderRequest(req *domain.Request) provider.Request {
	st := req.State()
	messages := make([]provider.Message, 0, len(st.Messages))
	for _, m := range st.Messages {
		messages = append(messages, provider.Message{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCalls:  toProviderToolCalls(m.ToolCalls),
			ToolCallID: m.ToolCallID,
		})
	}

	tools := toProviderTools(st.Tools)

	// A non-native tool-call format learns its tools from a text menu + emission instructions,
	// not the wire tools array (D2/D3/D4): render the block over THIS request's (mode-filtered)
	// menu, fold it into the wire system channel, and suppress the native array — sending both
	// would double-tell the model in two formats, and a template without tool support can error
	// on the array. The block is wire-only: it never enters domain history, the snapshot, or any
	// event. A native/zero profile renders "" (processing.InstructionsFor), so the request is
	// byte-identical — no injection, no suppression.
	if block := a.toolInstructions(st.Tools); block != "" {
		messages = injectSystemInstructions(messages, block)
		tools = nil
	}

	return provider.Request{
		Model:    st.Model,
		Messages: messages,
		Tools:    tools,
		Sampling: toProviderSampling(st.Sampling),
	}
}

// toolInstructions renders the non-native profile's wire-only tool menu + emission instructions
// for menu (this request's mode-filtered tool menu) — the emit-side mirror of the parse seam's
// ParserFor (processing.InstructionsFor). A native/zero profile or an empty menu renders "". The
// error path is unreachable at runtime: an unknown tool-call format fails construction via
// ParserFor before any request is built, so a defensively-caught error degrades to no injection
// (the request keeps the native array) rather than propagating up the no-error wire seam.
func (a *Agent) toolInstructions(menu []domain.ToolDef) string {
	block, err := processing.InstructionsFor(a.cfg.Profile, menu)
	if err != nil {
		return ""
	}
	return block
}

// injectSystemInstructions folds the rendered tool menu + format instructions into the wire
// request's system channel (D3): it appends block to the FIRST system message when the wire
// projection already carries one (an embedder can seed one via a hook), else prepends a new sole
// system message at position 0. One merged system message is the shape llama.cpp chat templates
// reliably render — the domain.Request.appendOrCreateSystem semantics applied at the wire seam.
// messages is freshly built by the caller, so the in-place edit is local to this request.
func injectSystemInstructions(messages []provider.Message, block string) []provider.Message {
	for i := range messages {
		if messages[i].Role != string(domain.RoleSystem) {
			continue
		}
		if messages[i].Content == "" {
			messages[i].Content = block
		} else {
			messages[i].Content += "\n\n" + block
		}
		return messages
	}
	sys := provider.Message{Role: string(domain.RoleSystem), Content: block}
	return append([]provider.Message{sys}, messages...)
}

// toProviderToolCalls maps domain tool calls onto the provider's "function" wire shape so
// an assistant message's tool calls survive the round-trip back to the Upstream (nil ⇒ nil).
func toProviderToolCalls(calls []domain.ToolCall) []provider.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]provider.ToolCall, 0, len(calls))
	for _, c := range calls {
		out = append(out, provider.ToolCall{
			ID:       c.ID,
			Type:     "function",
			Function: provider.FunctionCall{Name: c.Tool, Arguments: string(c.Arguments)},
		})
	}
	return out
}

// toProviderTools maps the domain tool menu onto provider tool specs (nil ⇒ nil).
func toProviderTools(defs []domain.ToolDef) []provider.ToolSpec {
	if len(defs) == 0 {
		return nil
	}
	specs := make([]provider.ToolSpec, 0, len(defs))
	for _, d := range defs {
		specs = append(specs, provider.ToolSpec{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.Schema,
		})
	}
	return specs
}

// toProviderSampling maps the two sampling knobs a hook may set onto the provider shape;
// the provider's other knobs (TopP/TopK/RepeatPenalty) stay unset (server default).
func toProviderSampling(p domain.SamplingParams) provider.Sampling {
	return provider.Sampling{Temperature: p.Temperature, MaxTokens: p.MaxTokens}
}

package reactions

import (
	"encoding/json"

	"github.com/airiclenz/apogee/internal/domain"
)

// ScheduleRef is the Schedule reference a Firing's payload carries, under the name this package's
// callers (Options.Schedule, the daemon and `/schedule` roots) have always used. The type itself is
// domain.ScheduleRef, because the payload it rides on is domain.SeamPayload — the ONE document
// every fired out-of-process Reaction reads, on either lane. This package fills that document for
// observe firings (match.go, runner.go) and owns only what the observe lane alone needs: the
// per-seam projections below, which cut a seam-closing notice's "value" out of the engine's live
// working value.
type ScheduleRef = domain.ScheduleRef

// requestValue projects the pre-request seam's working value: the outgoing request as the
// reactions left it, reduced to the two things a watcher reads — what the model is being sent
// and which tools it is being offered.
type requestValue struct {
	Messages []messageValue `json:"messages"`
	Tools    []string       `json:"tools,omitempty"`
}

// responseValue projects the post-response seam's working value: what the model answered, plus
// whether the loop still had the budget to re-stream the Turn.
type responseValue struct {
	Text      string          `json:"text,omitempty"`
	ToolCalls []toolCallValue `json:"tool_calls,omitempty"`
	Retryable bool            `json:"retryable"`
}

// toolResultValue projects the post-tool-result seam's working value: the originating call and
// the result as the reactions left it.
type toolResultValue struct {
	Call    toolCallValue `json:"call"`
	Content string        `json:"content,omitempty"`
	IsError bool          `json:"is_error"`
}

// conversationValue projects the history-rewrite seam's working value: the conversation after
// the rewrite.
type conversationValue struct {
	Messages []messageValue `json:"messages"`
}

// messageValue projects one conversation message — role, content and, on an assistant message,
// the calls it asked for. The Apogee-owned markers a Message also carries (Interjected, the
// preserved wire extras) are deliberately left out: they are engine bookkeeping, not something
// a user's script has any use for.
type messageValue struct {
	Role       string          `json:"role"`
	Content    string          `json:"content,omitempty"`
	ToolCalls  []toolCallValue `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

// toolCallValue projects one tool call. Arguments is the raw JSON object the model produced,
// copied rather than referenced, so the document survives the working value it came from.
type toolCallValue struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// projectSeamValue reduces a seam's working value to the document a fired reaction reads under
// "value". It is the ONE place the payload touches the engine's live state, and it copies
// everything it keeps: the reference a [domain.SeamClosedEvent] carries is valid only for the
// duration of Emit, and the loop resumes mutating the value the moment Emit returns.
//
// A value that is not the seam's own — a nil pointer, or a pair the engine could not have
// produced — projects to nothing, so a stream this package did not build costs a fired reaction
// an absent "value" rather than a panic on the engine's own goroutine.
func projectSeamValue(seam domain.Moment, value any) any {
	switch seam {
	case domain.MomentPreRequest:
		request, ok := value.(*domain.Request)
		if !ok || request == nil {
			return nil
		}
		state := request.State()
		return requestValue{Messages: projectMessages(state.Messages), Tools: projectToolNames(state.Tools)}
	case domain.MomentPostResponse:
		moment, ok := value.(domain.PostResponseMoment)
		if !ok || moment.Resp == nil {
			return nil
		}
		return responseValue{
			Text:      moment.Resp.Text(),
			ToolCalls: projectToolCalls(moment.Resp.ToolCalls()),
			Retryable: moment.Retryable,
		}
	case domain.MomentPreToolExec:
		edit, ok := value.(*domain.ToolCallEdit)
		if !ok || edit == nil {
			return nil
		}
		return toolCallValue{ID: edit.ID(), Name: edit.Tool(), Arguments: edit.Arguments()}
	case domain.MomentPostToolResult:
		moment, ok := value.(domain.ToolResultMoment)
		if !ok || moment.Edit == nil {
			return nil
		}
		return toolResultValue{
			Call:    projectToolCall(moment.Call),
			Content: moment.Edit.Content(),
			IsError: moment.Edit.IsError(),
		}
	case domain.MomentHistoryRewrite:
		conversation, ok := value.(*domain.Conversation)
		if !ok || conversation == nil {
			return nil
		}
		return conversationValue{Messages: projectMessages(conversation.Messages())}
	}
	return nil
}

// projectMessages projects a message list, preserving its order.
func projectMessages(messages []domain.Message) []messageValue {
	out := make([]messageValue, 0, len(messages))
	for _, m := range messages {
		out = append(out, messageValue{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCalls:  projectToolCalls(m.ToolCalls),
			ToolCallID: m.ToolCallID,
		})
	}
	return out
}

// projectToolCalls projects a tool-call list, preserving its order.
func projectToolCalls(calls []domain.ToolCall) []toolCallValue {
	if len(calls) == 0 {
		return nil
	}
	out := make([]toolCallValue, 0, len(calls))
	for _, call := range calls {
		out = append(out, projectToolCall(call))
	}
	return out
}

// projectToolCall projects one tool call, copying its argument bytes.
func projectToolCall(call domain.ToolCall) toolCallValue {
	return toolCallValue{
		ID:        call.ID,
		Name:      call.Tool,
		Arguments: append(json.RawMessage(nil), call.Arguments...),
	}
}

// projectToolNames reduces the tool menu to the names it offered — the whole schema would be
// several kilobytes of JSON on every single request, and the name is what a watcher branches on.
func projectToolNames(tools []domain.ToolDef) []string {
	if len(tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

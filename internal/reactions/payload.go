package reactions

import (
	"encoding/json"

	"github.com/airiclenz/apogee/internal/domain"
)

// ScheduleRef names the daemon or `/schedule` Schedule a Firing ran for. It is present only on a
// Firing's payload — a TUI session and a plain headless run belong to no Schedule — so a reaction
// can tell "the 6am docs sweep finished" from "the session I am sitting in finished".
type ScheduleRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Payload is the JSON document a fired reaction receives — on stdin for a command, as the POST body
// for a webhook. Its field names are a DOCUMENTED CONTRACT: a user's script reads them by name, so
// they are renamed only by a deliberate, documented break.
//
// The first block is present on every event and identifies the firing: which reaction fired, on what,
// when, and in which run. Depth and Turn are the emitting agent's, so a Hook fired by a sub-agent
// reports the child's nesting level rather than the top-level agent's, and CallID is that child's
// run identity — the id of the sub_agent call that spawned it, empty at Depth 0. Every field after
// that block is per-event and omitted when it does not apply, so a script can branch on "event"
// and read only what that event carries.
//
// The payload is NOT secret-scrubbed: it goes to the user's own command or URL, which is the same
// trust as the screen (ADR 0073 §6).
//
// The matcher fills the event-derived fields; the runner stamps the identity ones it alone knows —
// Reaction, Time, Workspace and Schedule — as it hands the payload to each subscribing reaction.
type Payload struct {
	// Event is the notice that fired, spelled exactly as the `events:` list spells it.
	Event Event `json:"event"`
	// Reaction is the name of the entry that fired, the `name:` from its config row.
	Reaction string `json:"reaction"`
	// Time is when the firing was matched, RFC 3339 with seconds resolution or finer.
	Time string `json:"time"`
	// Workspace is the absolute, symlink-resolved workspace the run is rooted in.
	Workspace string `json:"workspace"`
	// Depth is the emitting agent's sub-agent nesting level; 0 is the top-level agent.
	Depth int `json:"depth"`
	// Turn is the Turn index the event belongs to.
	Turn int `json:"turn"`
	// CallID is the run identity of the emitting agent — the id of the sub_agent call that
	// spawned it — and is empty at Depth 0.
	CallID string `json:"call_id,omitempty"`
	// Schedule names the Schedule this Firing ran for; absent outside a Firing.
	Schedule *ScheduleRef `json:"schedule,omitempty"`

	// Status is the Turn's StepStatus. turn-finished, exchange-finished.
	Status string `json:"status,omitempty"`
	// Faulted marks a Turn the loop ABANDONED rather than completed. turn-finished,
	// exchange-finished.
	Faulted bool `json:"faulted,omitempty"`
	// StepCapped marks an Exchange the delegate step cap ended rather than the model.
	// turn-finished, exchange-finished.
	StepCapped bool `json:"step_capped,omitempty"`

	// Tool is the tool that wrote the file, or the tool whose call is waiting on an Approval.
	// file-changed, approval-requested, approval-decided.
	Tool string `json:"tool,omitempty"`
	// Path is the absolute, symlink-resolved path the write landed on. file-changed.
	Path string `json:"path,omitempty"`

	// Reason is why the Approval was required, in the engine's own words. approval-requested,
	// approval-decided.
	Reason string `json:"reason,omitempty"`
	// Remedy is the optional one-line route out of the condition that forced the Approval.
	// approval-requested, approval-decided.
	Remedy string `json:"remedy,omitempty"`
	// SubAgentName is the display name of the child whose call is waiting, when it has one.
	// approval-requested, approval-decided.
	SubAgentName string `json:"sub_agent_name,omitempty"`
	// Scope is the optional statement of what the call reaches beyond what its arguments name.
	// approval-requested, approval-decided.
	Scope string `json:"scope,omitempty"`
	// Decision is the verdict the Approver returned, in the domain.ApprovalDecision spelling —
	// `allow`, `deny` or `allow-for-session`. approval-decided.
	Decision string `json:"decision,omitempty"`

	// Source is what faulted — a tool name, a Reaction id, or "loop". error.
	Source string `json:"source,omitempty"`
	// Error is the fault's message. error.
	Error string `json:"error,omitempty"`

	// Seam is the seam whose pass closed, in the Moment's own spelling — the notice is that
	// name plus `-finished`, so a script branching on "event" already knows it and one
	// branching on "seam" reads the in-loop point directly. The five seam-closing notices.
	Seam domain.Moment `json:"seam,omitempty"`
	// Reactions are the ids of the reactions booked as firings during the pass, in the order
	// they fired; absent when nothing acted, which is the ordinary pass. The five
	// seam-closing notices.
	Reactions []string `json:"reactions,omitempty"`
	// Value is the JSON projection of the seam's working value as the pass left it — the
	// request at pre-request, the response at post-response, the pending call at
	// pre-tool-exec, the call and its result at post-tool-result, the conversation at
	// history-rewrite. It is built while the engine's Emit is still running, because the
	// reference the event carries is valid only for that call (domain.SeamClosedEvent), and
	// it is a copy: nothing here points back into the loop's own state. The five
	// seam-closing notices.
	Value any `json:"value,omitempty"`
}

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

// Environment variable names a fired command finds the payload's headline facts under. They are a
// convenience for a one-line script that would otherwise pipe stdin through a JSON parser; the
// full document is always on stdin as well.
const (
	EnvEvent        = "APOGEE_REACTION_EVENT"
	EnvName         = "APOGEE_REACTION_NAME"
	EnvWorkspace    = "APOGEE_REACTION_WORKSPACE"
	EnvPath         = "APOGEE_REACTION_PATH"
	EnvScheduleID   = "APOGEE_REACTION_SCHEDULE_ID"
	EnvScheduleName = "APOGEE_REACTION_SCHEDULE_NAME"
)

// Env renders the payload's headline facts as `NAME=value` entries for a fired command's
// environment, in a fixed order. A fact the payload does not carry is OMITTED rather than set
// empty, so a script can test with `[ -n "$APOGEE_REACTION_PATH" ]` and a variable inherited from
// the user's own environment is not silently blanked by a reaction that has nothing to put there.
func (p Payload) Env() []string {
	env := make([]string, 0, 6)
	add := func(name, value string) {
		if value != "" {
			env = append(env, name+"="+value)
		}
	}
	add(EnvEvent, string(p.Event))
	add(EnvName, p.Reaction)
	add(EnvWorkspace, p.Workspace)
	add(EnvPath, p.Path)
	if p.Schedule != nil {
		add(EnvScheduleID, p.Schedule.ID)
		add(EnvScheduleName, p.Schedule.Name)
	}
	return env
}

package eventjson

import (
	"encoding/json"

	"github.com/airiclenz/apogee/internal/domain"
)

// The nineteen line kinds of ADR 0075 §4 — seventeen Event variants plus the two frames that
// bracket a run and are not Events. They are snake_case on purpose: a Hook event's kebab-case
// name for a neighbouring moment is a DIFFERENT moment, and the case difference is the signal.
const (
	kindToken             = "token"
	kindReasoning         = "reasoning"
	kindStreamReset       = "stream_reset"
	kindMessage           = "message"
	kindToolCall          = "tool_call"
	kindToolResult        = "tool_result"
	kindSubAgentPhase     = "sub_agent_phase"
	kindSubAgentNamed     = "sub_agent_named"
	kindChildInterjection = "child_interjection"
	kindApproval          = "approval"
	kindTurn              = "turn"
	kindMechanismFired    = "mechanism_fired"
	kindFloorGuard        = "floor_guard"
	kindError             = "error"
	kindPrune             = "prune"
	kindUsage             = "usage"
	kindAudit             = "audit"
	kindRunStarted        = "run_started"
	kindRunFinished       = "run_finished"
)

// Kinds returns every line kind the Event lines can carry, the two frames included, in the order
// ADR 0075 §4 lists them. It is the vocabulary itself rather than a derivation of it, so the
// manual's table of kinds is checkable against the code that writes them instead of drifting from
// it silently. The caller receives a fresh slice it may keep or sort.
func Kinds() []string {
	return []string{
		kindToken,
		kindReasoning,
		kindStreamReset,
		kindMessage,
		kindToolCall,
		kindToolResult,
		kindSubAgentPhase,
		kindSubAgentNamed,
		kindChildInterjection,
		kindApproval,
		kindTurn,
		kindMechanismFired,
		kindFloorGuard,
		kindError,
		kindPrune,
		kindUsage,
		kindAudit,
		kindRunStarted,
		kindRunFinished,
	}
}

// Encode maps one domain.Event to the three things a line is built from: its kind, the EventBase
// the envelope's turn/depth/call_id are stamped from, and the value that marshals to the line's
// `data` object.
//
// ok is false for exactly one variant — domain.WireEvent — and for a nil or unrecognised event.
// The Inspector's raw provider protocol is excluded by ADR 0075 decision 2: putting a wire format
// on a documented stdout contract would make it part of a public surface. A caller that sees false
// writes no line at all and, per the same decision, consumes no sequence number for it.
//
// base is read as ev.EventBase explicitly at every case, which matters for domain.AuditEvent
// alone: that variant declares a CallID of its own — the AUDITED call — which shadows the
// embedded one. The envelope must carry the SPAWNING delegation's id like every other line, so the
// embedded field is what travels here and the audited call rides `data.call_id`.
func Encode(ev domain.Event) (kind string, base domain.EventBase, data any, ok bool) {
	switch e := ev.(type) {
	case domain.TokenEvent:
		return kindToken, e.EventBase, tokenData{Text: e.Text}, true
	case domain.ReasoningEvent:
		return kindReasoning, e.EventBase, reasoningData{Text: e.Text}, true
	case domain.StreamResetEvent:
		return kindStreamReset, e.EventBase, streamResetData{}, true
	case domain.MessageEvent:
		return kindMessage, e.EventBase, messageData{Text: e.Text}, true
	case domain.ToolCallEvent:
		return kindToolCall, e.EventBase, toolCallData{
			Call:         toolCallOf(e.Call),
			ResolvedPath: e.ResolvedPath,
		}, true
	case domain.ToolResultEvent:
		return kindToolResult, e.EventBase, toolResultData{Result: toolResultOf(e.Result)}, true
	case domain.SubAgentPhaseEvent:
		return kindSubAgentPhase, e.EventBase, subAgentPhaseData{
			Phase:     string(e.Phase),
			Result:    toolResultOf(e.Result),
			Cancelled: e.Cancelled,
		}, true
	case domain.SubAgentNamedEvent:
		return kindSubAgentNamed, e.EventBase, subAgentNamedData{Name: e.Name}, true
	case domain.ChildInterjectionEvent:
		return kindChildInterjection, e.EventBase, childInterjectionData{
			Input:  userInputOf(e.Input),
			Landed: e.Landed,
		}, true
	case domain.ApprovalEvent:
		return kindApproval, e.EventBase, approvalData{
			Phase:    string(e.Phase),
			Request:  approvalRequestOf(e.Request),
			Decision: string(e.Decision),
		}, true
	case domain.TurnEvent:
		return kindTurn, e.EventBase, turnData{
			Status:     string(e.Status),
			Faulted:    e.Faulted,
			StepCapped: e.StepCapped,
		}, true
	case domain.MechanismFiredEvent:
		return kindMechanismFired, e.EventBase, mechanismFiredData{
			Mechanism: string(e.Mechanism),
			Hook:      string(e.Hook),
			Action:    e.Action,
		}, true
	case domain.FloorGuardEvent:
		return kindFloorGuard, e.EventBase, floorGuardData{
			Guard:  e.Guard,
			Action: e.Action,
			Detail: e.Detail,
		}, true
	case domain.ErrorEvent:
		return kindError, e.EventBase, errorData{Source: e.Source, Err: e.Err}, true
	case domain.PruneEvent:
		return kindPrune, e.EventBase, pruneData{Results: e.Results, Tokens: e.Tokens}, true
	case domain.UsageEvent:
		return kindUsage, e.EventBase, usageData{
			PromptTokens:                 e.PromptTokens,
			CompletionTokens:             e.CompletionTokens,
			TotalTokens:                  e.TotalTokens,
			CachedPromptTokens:           e.CachedPromptTokens,
			Model:                        e.Model,
			ContextWindow:                e.ContextWindow,
			CumulativePromptTokens:       e.CumulativePromptTokens,
			CumulativeCompletionTokens:   e.CumulativeCompletionTokens,
			CumulativeTotalTokens:        e.CumulativeTotalTokens,
			CumulativeCachedPromptTokens: e.CumulativeCachedPromptTokens,
			CumulativeCalls:              e.CumulativeCalls,
			Maintenance:                  e.Maintenance,
		}, true
	case domain.AuditEvent:
		return kindAudit, e.EventBase, auditData{
			Tool:     e.Tool,
			CallID:   e.CallID,
			Decision: e.Decision,
			Reason:   e.Reason,
			IsError:  e.IsError,
		}, true
	default:
		return "", domain.EventBase{}, nil, false
	}
}

// The per-variant `data` values. Every exported field carries an explicit snake_case tag and NONE
// carries omitempty: ADR 0075 decision 3 promises a consumer never has to test for a missing key,
// so a zero value is written as the zero and not dropped. The tags name the variant's own fields,
// so a member added to an Event variant is a member added here under the same name.

// tokenData is the token line: one streamed chunk of assistant text.
type tokenData struct {
	Text string `json:"text"`
}

// reasoningData is the reasoning line: one newly-revealed chunk of the model's reasoning channel.
type reasoningData struct {
	Text string `json:"text"`
}

// streamResetData is the stream_reset line. The variant carries nothing but its EventBase, so the
// object is empty — and it is still an object, because `data` is never null on an Event line.
type streamResetData struct{}

// messageData is the message line: a completed assistant message.
type messageData struct {
	Text string `json:"text"`
}

// toolCallData is the tool_call line: the requested call, and where its path argument really
// points when that differs from what the argument names.
type toolCallData struct {
	Call         toolCall `json:"call"`
	ResolvedPath string   `json:"resolved_path"`
}

// toolResultData is the tool_result line: one tool's outcome after execution.
type toolResultData struct {
	Result toolResult `json:"result"`
}

// subAgentPhaseData is the sub_agent_phase line: one delegation crossing a lifecycle boundary.
// Cancelled separates a finished phase that CLOSES A ROLLED-BACK bracket from one that reports a
// result, so a reader folds Result only when it is false (ADR 0075 decision 12).
type subAgentPhaseData struct {
	Phase     string     `json:"phase"`
	Result    toolResult `json:"result"`
	Cancelled bool       `json:"cancelled"`
}

// subAgentNamedData is the sub_agent_named line: the name the out-of-band namer gave a delegation
// the model left unnamed.
type subAgentNamedData struct {
	Name string `json:"name"`
}

// childInterjectionData is the child_interjection line: the fate of one message a human addressed
// to a running sub-agent.
type childInterjectionData struct {
	Input  userInput `json:"input"`
	Landed bool      `json:"landed"`
}

// approvalData is the approval line. Decision is meaningless on the requested phase and carries
// the verdict on the decided one, exactly as the variant does.
type approvalData struct {
	Phase    string          `json:"phase"`
	Request  approvalRequest `json:"request"`
	Decision string          `json:"decision"`
}

// turnData is the turn line: a Turn's quiescent boundary. Unlike the Hooks vocabulary's
// turn-finished this is emitted at EVERY depth.
type turnData struct {
	Status     string `json:"status"`
	Faulted    bool   `json:"faulted"`
	StepCapped bool   `json:"step_capped"`
}

// mechanismFiredData is the mechanism_fired line: a catalogued Mechanism acting at a hook point.
type mechanismFiredData struct {
	Mechanism string `json:"mechanism"`
	Hook      string `json:"hook"`
	Action    string `json:"action"`
}

// floorGuardData is the floor_guard line. Guard is the guard's CONFIG KEY, so a reader never has
// to map an internal name back to the switch that turns the behaviour off.
type floorGuardData struct {
	Guard  string `json:"guard"`
	Action string `json:"action"`
	Detail string `json:"detail"`
}

// errorData is the error line: a localised, recovered fault. The member is `err` because the tag
// is the variant's own field name folded to snake_case — the mechanical rule this whole mapping
// follows — and not the `error` a Hook event spells.
type errorData struct {
	Source string `json:"source"`
	Err    string `json:"err"`
}

// pruneData is the prune line: how many stale tool results the engine replaced and what it
// estimates that freed.
type pruneData struct {
	Results int `json:"results"`
	Tokens  int `json:"tokens"`
}

// usageData is the usage line: the token accounting an Upstream reply carried, the emitting
// agent's running totals, and the model and window that fill sits in.
type usageData struct {
	PromptTokens       int    `json:"prompt_tokens"`
	CompletionTokens   int    `json:"completion_tokens"`
	TotalTokens        int    `json:"total_tokens"`
	CachedPromptTokens int    `json:"cached_prompt_tokens"`
	Model              string `json:"model"`
	ContextWindow      int    `json:"context_window"`

	CumulativePromptTokens       int `json:"cumulative_prompt_tokens"`
	CumulativeCompletionTokens   int `json:"cumulative_completion_tokens"`
	CumulativeTotalTokens        int `json:"cumulative_total_tokens"`
	CumulativeCachedPromptTokens int `json:"cumulative_cached_prompt_tokens"`
	CumulativeCalls              int `json:"cumulative_calls"`

	Maintenance bool `json:"maintenance"`
}

// auditData is the audit line. CallID is the AUDITED call — the one this record is about — while
// the envelope's call_id is the spawning delegation, read off the embedded EventBase the variant's
// own field shadows.
type auditData struct {
	Tool     string `json:"tool"`
	CallID   string `json:"call_id"`
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	IsError  bool   `json:"is_error"`
}

// The nested mirrors. A domain struct that rides inside a variant gets its own tagged shape here
// rather than being marshalled directly, so the wire names are this package's decision and a
// domain field acquiring a json tag for some other reason can never move the contract.

// toolCall mirrors domain.ToolCall. Arguments is the model's raw argument JSON, embedded verbatim
// (ADR 0075 decision 11): what the model wrote is what a consumer reads.
type toolCall struct {
	ID        string          `json:"id"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

// toolResult mirrors domain.ToolResult. Summary is deliberately absent: it is a sealed interface
// whose implementations are view-facing, never persisted, and a JSON rendering of it would be a
// second contract this one does not want to own.
type toolResult struct {
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// approvalRequest mirrors domain.ApprovalRequest — every disclosure the gate carries, so a reader
// of the line sees exactly what an approval pane would.
type approvalRequest struct {
	Tool           string          `json:"tool"`
	Arguments      json.RawMessage `json:"arguments"`
	Reason         string          `json:"reason"`
	Remedy         string          `json:"remedy"`
	SubAgentTask   string          `json:"sub_agent_task"`
	SubAgentName   string          `json:"sub_agent_name"`
	CacheKey       string          `json:"cache_key"`
	MCPServerGrant bool            `json:"mcp_server_grant"`
	MCPServerAlias string          `json:"mcp_server_alias"`
	ResolvedPath   string          `json:"resolved_path"`
	Scope          string          `json:"scope"`
}

// userInput mirrors domain.UserInput — the message a human addressed to a running sub-agent.
type userInput struct {
	Text     string   `json:"text"`
	FileRefs []string `json:"file_refs"`
	SkillIDs []string `json:"skill_ids"`
}

// toolCallOf converts a domain.ToolCall to its wire mirror.
func toolCallOf(call domain.ToolCall) toolCall {
	return toolCall{ID: call.ID, Tool: call.Tool, Arguments: rawOrNull(call.Arguments)}
}

// toolResultOf converts a domain.ToolResult to its wire mirror, dropping Summary.
func toolResultOf(res domain.ToolResult) toolResult {
	return toolResult{CallID: res.CallID, Content: res.Content, IsError: res.IsError}
}

// approvalRequestOf converts a domain.ApprovalRequest to its wire mirror.
func approvalRequestOf(req domain.ApprovalRequest) approvalRequest {
	return approvalRequest{
		Tool:           req.Tool,
		Arguments:      rawOrNull(req.Arguments),
		Reason:         req.Reason,
		Remedy:         req.Remedy,
		SubAgentTask:   req.SubAgentTask,
		SubAgentName:   req.SubAgentName,
		CacheKey:       req.CacheKey,
		MCPServerGrant: req.MCPServerGrant,
		MCPServerAlias: req.MCPServerAlias,
		ResolvedPath:   req.ResolvedPath,
		Scope:          req.Scope,
	}
}

// userInputOf converts a domain.UserInput to its wire mirror.
func userInputOf(in domain.UserInput) userInput {
	return userInput{Text: in.Text, FileRefs: in.FileRefs, SkillIDs: in.SkillIDs}
}

// rawOrNull returns raw, or nil when it holds no bytes so the member encodes as null. It is not a
// cosmetic normalisation: encoding/json writes a nil json.RawMessage as `null`, but an EMPTY
// non-nil one contributes no bytes at all and would make the whole line unmarshalable.
func rawOrNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

package eventjson

import (
	"encoding/json"

	"github.com/airiclenz/apogee/internal/domain"
)

// The twenty-three line kinds of ADR 0075 §4 — twenty-one Event variants, workflow_phase (ADR 0087)
// and malformed_chunks the newest, plus the two frames that bracket a run and are not Events. They are snake_case on purpose: a notice Moment's kebab-case
// name for a neighbouring moment is a DIFFERENT moment, and the case difference is the signal.
// seam_closed is the one kind the Writer holds back unless asked for (Options.Seams): the mapping
// here is total, the gating is the Writer's.
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
	kindReactionFired     = "reaction_fired"
	kindError             = "error"
	kindPrune             = "prune"
	kindRefClipped        = "ref_clipped"
	kindUsage             = "usage"
	kindAudit             = "audit"
	kindSeamClosed        = "seam_closed"
	kindUpstreamAttempt   = "upstream_attempt"
	kindWorkflowPhase     = "workflow_phase"
	kindMalformedChunks   = "malformed_chunks"
	kindRunStarted        = "run_started"
	kindRunFinished       = "run_finished"
)

// Kinds returns every line kind the Event lines can carry, the two frames included, in the order
// ADR 0075 §4 lists them, a kind added since placed before the frames. It is the vocabulary itself rather than a derivation of it, so the
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
		kindReactionFired,
		kindError,
		kindPrune,
		kindRefClipped,
		kindUsage,
		kindAudit,
		kindSeamClosed,
		kindUpstreamAttempt,
		kindWorkflowPhase,
		kindMalformedChunks,
		kindRunStarted,
		kindRunFinished,
	}
}

// Encode maps one domain.Event to the three things a line is built from: its kind, the EventBase
// the envelope's turn/depth/call_id/run_id are stamped from, and the value that marshals to the
// line's `data` object.
//
// ok is false for the two SINK-ONLY variants — domain.WireEvent and domain.SubAgentGroupEvent —
// and for a nil or unrecognised event. The Inspector's raw provider protocol is excluded by ADR
// 0075 decision 2: putting a wire format on a documented stdout contract would make it part of a
// public surface. A sub-agent group's announced size is a drawing Driver's queued count (ADR 0025,
// amended 2026-10-05) and has no line kind: the stream's tool_call and sub_agent_phase lines
// already tell a consumer every member as it reaches it. A caller that sees false writes no line at
// all and, per the same decision, consumes no sequence number for it.
//
// ok is false as well for the two workflow phases that describe a Workflow's shape for a Driver
// that draws it — domain.WorkflowItemStarted and domain.WorkflowStageFinished — which the
// workflow_phase line's documented phase list does not carry (docs/manual/headless.md), so the
// NDJSON stream is the same whether the engine reports them or not.
//
// A domain.SeamClosedEvent maps to the seam_closed kind, but only PART of it: its Value is the
// seam's live working value, read-only and valid only for the duration of Emit, so no line carries
// it — a consumer reads which seam closed and which reactions fired, nothing more. Whether that
// line is written at all is the Writer's call (Options.Seams), not this mapping's.
//
// base is read as ev.EventBase explicitly at every case, which matters for domain.AuditEvent
// alone: that variant declares a CallID of its own — the AUDITED call — which shadows the
// embedded one. The envelope must carry the SPAWNING delegation's id like every other line, so the
// embedded field is what travels here and the audited call rides `data.call_id`.
//
// A usage line's `currency` is the one data member no Event carries: it is the run's configured
// label, which a Writer holds (Options.Currency, Writer.SetCurrency) and passes in through encode.
// Encode itself writes the label as "" — what an unpriced line carries anyway.
func Encode(ev domain.Event) (kind string, base domain.EventBase, data any, ok bool) {
	return encode(ev, "")
}

// encode is Encode with the run's currency label in hand, stated on a usage line only under
// FrameCurrency's rule.
func encode(ev domain.Event, currency string) (kind string, base domain.EventBase, data any, ok bool) {
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
			SpawnRunID:   e.SpawnRunID,
		}, true
	case domain.ToolResultEvent:
		return kindToolResult, e.EventBase, toolResultData{
			Result:      toolResultOf(e.Result),
			Tool:        e.Tool,
			WriteTarget: e.WriteTarget,
			SpawnRunID:  e.SpawnRunID,
		}, true
	case domain.SubAgentPhaseEvent:
		return kindSubAgentPhase, e.EventBase, subAgentPhaseData{
			Phase:  string(e.Phase),
			Result: toolResultOf(e.Result),
		}, true
	case domain.SubAgentNamedEvent:
		return kindSubAgentNamed, e.EventBase, subAgentNamedData{Name: e.Name}, true
	case domain.ChildInterjectionEvent:
		return kindChildInterjection, e.EventBase, childInterjectionData{
			Input:  userInputOf(e.Input),
			Landed: e.Landed,
			Reason: string(e.Reason),
		}, true
	case domain.ApprovalEvent:
		return kindApproval, e.EventBase, approvalData{
			Phase:    string(e.Phase),
			Request:  approvalRequestOf(e.Request),
			Decision: string(e.Decision),
			Rules:    allowRulesOf(e.Rules),
		}, true
	case domain.TurnEvent:
		return kindTurn, e.EventBase, turnData{
			Status:     string(e.Status),
			Faulted:    e.Faulted,
			StepCapped: e.StepCapped,
		}, true
	case domain.ReactionFiredEvent:
		return kindReactionFired, e.EventBase, reactionFiredData{
			Reaction: e.Reaction,
			Origin:   string(e.Origin),
			Moment:   string(e.Moment),
			Action:   e.Action,
			Detail:   e.Detail,
		}, true
	case domain.ErrorEvent:
		return kindError, e.EventBase, errorData{Source: e.Source, Err: e.Err}, true
	case domain.PruneEvent:
		return kindPrune, e.EventBase, pruneData{Results: e.Results, Tokens: e.Tokens}, true
	case domain.RefClippedEvent:
		return kindRefClipped, e.EventBase, refClippedData{Ref: e.Ref, Tokens: e.Tokens, Absolute: e.Absolute}, true
	case domain.UsageEvent:
		return kindUsage, e.EventBase, usageData{
			PromptTokens:                 e.PromptTokens,
			CompletionTokens:             e.CompletionTokens,
			TotalTokens:                  e.TotalTokens,
			CachedPromptTokens:           e.CachedPromptTokens,
			Model:                        e.Model,
			ServedModel:                  e.ServedModel,
			ContextWindow:                e.ContextWindow,
			CumulativePromptTokens:       e.Cumulative.PromptTokens,
			CumulativeCompletionTokens:   e.Cumulative.CompletionTokens,
			CumulativeTotalTokens:        e.Cumulative.TotalTokens,
			CumulativeCachedPromptTokens: e.Cumulative.CachedPromptTokens,
			CumulativeCalls:              e.Cumulative.Calls,
			Cost:                         CostAmount(e.CostMicros),
			Priced:                       e.Priced,
			CumulativeCost:               CostAmount(e.Cumulative.CostMicros),
			CumulativeUnpricedCalls:      e.Cumulative.UnpricedCalls,
			Currency:                     FrameCurrency(e.Cumulative, currency),
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
	case domain.SeamClosedEvent:
		return kindSeamClosed, e.EventBase, seamClosedData{
			Seam:  string(e.Seam.Closing()),
			Fired: firedOrEmpty(e.Fired),
		}, true
	case domain.UpstreamAttemptEvent:
		return kindUpstreamAttempt, e.EventBase, upstreamAttemptData{
			Server:       e.Server,
			Endpoint:     e.Endpoint,
			Model:        e.Model,
			RequestID:    e.RequestID,
			Index:        e.Index,
			TTFBMs:       e.TTFB.Milliseconds(),
			TTFTMs:       e.TTFT.Milliseconds(),
			LastMs:       e.Last.Milliseconds(),
			DurationMs:   e.Duration.Milliseconds(),
			OutputTokens: e.OutputTokens,
			Outcome:      e.Outcome,
		}, true
	case domain.WorkflowPhaseEvent:
		if !isLineWorkflowPhase(e.Phase) {
			return "", domain.EventBase{}, nil, false
		}
		return kindWorkflowPhase, e.EventBase, workflowPhaseData{
			Phase:    string(e.Phase),
			Workflow: e.Workflow,
			Name:     e.Name,
			Stage:    e.Stage,
			Item:     e.Item,
			Index:    e.Index,
			Resumed:  e.Resumed,
			Receipt:  workflowReceiptOf(e.Receipt),
			Detail:   e.Detail,
			Call:     e.Call,
		}, true
	case domain.MalformedChunksEvent:
		return kindMalformedChunks, e.EventBase, malformedChunksData{Count: e.Count}, true
	default:
		return "", domain.EventBase{}, nil, false
	}
}

// The per-variant `data` values. Every exported field carries an explicit snake_case tag and NONE
// carries omitempty: ADR 0075 decision 3 promises a consumer never has to test for a missing key,
// so a zero value is written as the zero and not dropped. The tags name the variant's own fields
// under the same names. A member added to an Event variant is added here only when the line's
// documented shape grows with it: domain.WorkflowPhaseEvent's ItemName, Stages, Items, Round,
// Rounds, Run and Attempt describe the Workflow's shape for a Driver that draws it, Resume is the
// resume command such a Driver shows its user, Tally is the end phase's item tally and Origin
// names a background sub_agent's run, read in process only; none is on the workflow_phase line
// (workflowPhaseData).

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
// points when that differs from what the argument names. SpawnRunID is the run id of the
// delegation a sub_agent call spawns — the run_id every line that delegation emits carries in its
// envelope — and "" for a call that spawns none. It joined the line additively (ADR 0075
// decision 10), so it sits last.
type toolCallData struct {
	Call         toolCall `json:"call"`
	ResolvedPath string   `json:"resolved_path"`
	SpawnRunID   string   `json:"spawn_run_id"`
}

// toolResultData is the tool_result line: one tool's outcome after execution, the tool it ran
// under, the path it wrote — "" when the call wrote none — and, for a delegation's result, the
// run id of the delegation it answers, the same spawn_run_id its tool_call line carried ("" for
// any other result). The three trailing members joined the line additively (ADR 0075 decision
// 10), so they sit after the result rather than before it.
type toolResultData struct {
	Result      toolResult `json:"result"`
	Tool        string     `json:"tool"`
	WriteTarget string     `json:"write_target"`
	SpawnRunID  string     `json:"spawn_run_id"`
}

// subAgentPhaseData is the sub_agent_phase line: one delegation crossing a lifecycle boundary.
// Cancelled has no source any more and is always false: a cancel settles every delegation with a
// result instead of rolling it back (ADR 0088, superseding ADR 0075 decision 12), so every finished
// phase reports one, and domain.SubAgentPhaseEvent no longer carries a flag for it. The key stays
// on the line because removing a member bumps the contract's `v` (ADR 0075); in a log written
// before that change, true marked a finished phase with no result.
type subAgentPhaseData struct {
	Phase     string     `json:"phase"`
	Result    toolResult `json:"result"`
	Cancelled bool       `json:"cancelled"`
}

// workflowPhaseData is the workflow_phase line: one Workflow crossing a lifecycle boundary. Stage,
// item, index, resumed and receipt are the zero values on the phases they do not describe, and the
// receipt's status is "" on every phase but item_finished. Call is on every phase: the call id the
// Workflow's item children carry as their envelope's call_id.
type workflowPhaseData struct {
	Phase    string          `json:"phase"`
	Workflow string          `json:"workflow"`
	Name     string          `json:"name"`
	Stage    string          `json:"stage"`
	Item     string          `json:"item"`
	Index    int             `json:"index"`
	Resumed  bool            `json:"resumed"`
	Receipt  workflowReceipt `json:"receipt"`
	Detail   string          `json:"detail"`
	Call     string          `json:"call"`
}

// isLineWorkflowPhase reports whether phase is one the workflow_phase line carries: every phase but
// the two a drawing Driver alone reads (Encode).
func isLineWorkflowPhase(phase domain.WorkflowPhase) bool {
	return phase != domain.WorkflowItemStarted && phase != domain.WorkflowStageFinished
}

// workflowReceipt is a workflow item's receipt on the line. Fields is an object on every line,
// empty when the receipt has none.
type workflowReceipt struct {
	Status  string            `json:"status"`
	Summary string            `json:"summary"`
	Fields  map[string]string `json:"fields"`
}

// workflowReceiptOf copies a receipt onto the line, a missing field map written as an empty object.
func workflowReceiptOf(receipt domain.WorkflowReceipt) workflowReceipt {
	fields := receipt.Fields
	if fields == nil {
		fields = map[string]string{}
	}
	return workflowReceipt{Status: receipt.Status, Summary: receipt.Summary, Fields: fields}
}

// subAgentNamedData is the sub_agent_named line: the name the out-of-band namer gave a delegation
// the model left unnamed.
type subAgentNamedData struct {
	Name string `json:"name"`
}

// childInterjectionData is the child_interjection line: the fate of one message a human addressed
// to a running sub-agent. Reason is why an undelivered message did not land and "" on a landed one;
// it is always present, like every other member, and its set is open (ADR 0075 §10).
type childInterjectionData struct {
	Input  userInput `json:"input"`
	Landed bool      `json:"landed"`
	Reason string    `json:"reason"`
}

// approvalData is the approval line. Decision is meaningless on the requested phase and carries
// the verdict on the decided one, exactly as the variant does. Rules lists the Allow rules that
// answered a gate in the Approver's place (decision `allowed-by-rule`), null on every other line.
type approvalData struct {
	Phase    string          `json:"phase"`
	Request  approvalRequest `json:"request"`
	Decision string          `json:"decision"`
	Rules    []allowRule     `json:"rules"`
}

// allowRule mirrors domain.AllowRule — the family a rule answers for, its text and its layer.
type allowRule struct {
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	Layer string `json:"layer"`
}

// allowRulesOf lists the rules on the line, nil for none — the member then encodes as null.
func allowRulesOf(rules []domain.AllowRule) []allowRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]allowRule, len(rules))
	for i, r := range rules {
		out[i] = allowRule{Kind: string(r.Kind), Text: r.Text, Layer: string(r.Layer)}
	}
	return out
}

// turnData is the turn line: a Turn's quiescent boundary. Unlike the Reaction vocabulary's
// turn-finished this is emitted at EVERY depth.
type turnData struct {
	Status     string `json:"status"`
	Faulted    bool   `json:"faulted"`
	StepCapped bool   `json:"step_capped"`
}

// reactionFiredData is the reaction_fired line: the ONE firing line of the Reaction core (ADR 0076
// D1). Reaction is the reaction's id — for an
// engine builtin, the same config key a user writes in config.yaml — so a reader never has to map
// an internal name back to the switch that turns the behaviour off.
type reactionFiredData struct {
	Reaction string `json:"reaction"`
	Origin   string `json:"origin"`
	Moment   string `json:"moment"`
	Action   string `json:"action"`
	Detail   string `json:"detail"`
}

// errorData is the error line: a localised, recovered fault. The member is `err` because the tag
// is the variant's own field name folded to snake_case — the mechanical rule this whole mapping
// follows — and not the `error` a notice Moment spells.
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

// refClippedData is the ref_clipped line: which attached reference entered the conversation
// clipped, the bound in tokens it was clipped to, and whether that bound was the absolute
// per-reference cap (true) or the reference's share of the History allocation (false).
type refClippedData struct {
	Ref      string `json:"ref"`
	Tokens   int    `json:"tokens"`
	Absolute bool   `json:"absolute"`
}

// malformedChunksData is the malformed_chunks line: how many undecodable stream chunks a reply
// that still ended successfully skipped — text or a tool call it carried may be missing.
type malformedChunksData struct {
	Count int `json:"count"`
}

// usageData is the usage line: the token accounting an Upstream reply carried, the emitting
// agent's running totals, and the model and window that fill sits in. served_model is the id the
// server put on the reply — what actually answered, beside the model that was asked for — and ""
// where the server named none; added within v 2, since a member a reader did not know is one it
// ignores.
//
// The priced Spend (money, ADR 0093) rides beside the tokens on the same additive terms: cost and
// priced are THIS call's amount and whether it was priced at all, cumulative_cost and
// cumulative_unpriced_calls the emitting agent's running sums, and currency the label every amount
// is in — the configured one only once a call behind the running sums was priced (FrameCurrency).
// Amounts are exact decimals (CostAmount), and an unpriced line writes the zero values rather than
// dropping the keys.
type usageData struct {
	PromptTokens       int    `json:"prompt_tokens"`
	CompletionTokens   int    `json:"completion_tokens"`
	TotalTokens        int    `json:"total_tokens"`
	CachedPromptTokens int    `json:"cached_prompt_tokens"`
	Model              string `json:"model"`
	ServedModel        string `json:"served_model"`
	ContextWindow      int    `json:"context_window"`

	CumulativePromptTokens       int `json:"cumulative_prompt_tokens"`
	CumulativeCompletionTokens   int `json:"cumulative_completion_tokens"`
	CumulativeTotalTokens        int `json:"cumulative_total_tokens"`
	CumulativeCachedPromptTokens int `json:"cumulative_cached_prompt_tokens"`
	CumulativeCalls              int `json:"cumulative_calls"`

	Cost                    json.Number `json:"cost"`
	Priced                  bool        `json:"priced"`
	CumulativeCost          json.Number `json:"cumulative_cost"`
	CumulativeUnpricedCalls int         `json:"cumulative_unpriced_calls"`
	Currency                string      `json:"currency"`

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

// seamClosedData is the seam_closed line: one seam finished passing. Seam is the seam's CLOSING
// notice name (`post-response-finished`, not `post-response`) so the line and the Reaction notice
// it stands beside spell the same fact the same way. Fired is never null: a pass in which nothing
// acted is a fact in its own right, and it reads as `[]`. The variant's Value is deliberately
// absent — see Encode.
type seamClosedData struct {
	Seam  string   `json:"seam"`
	Fired []string `json:"fired"`
}

// upstreamAttemptData is the upstream_attempt line: the measurement of one HTTP attempt a model
// call made against its server (ADR 0085). Every duration is whole milliseconds from the attempt's
// send — ttfb to the first body byte, ttft to the first model delta, last to the last one,
// duration to the attempt's end — and 0 where that point was never reached. endpoint is already
// redacted to scheme, host and path; output_tokens 0 means the server reported none; outcome is
// "ok", a fault class ("http_<code>", "overflow", "in_band", "transport", "idle", "stream_fault")
// or "cancelled".
type upstreamAttemptData struct {
	Server       string `json:"server"`
	Endpoint     string `json:"endpoint"`
	Model        string `json:"model"`
	RequestID    string `json:"request_id"`
	Index        int    `json:"index"`
	TTFBMs       int64  `json:"ttfb_ms"`
	TTFTMs       int64  `json:"ttft_ms"`
	LastMs       int64  `json:"last_ms"`
	DurationMs   int64  `json:"duration_ms"`
	OutputTokens int    `json:"output_tokens"`
	Outcome      string `json:"outcome"`
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

// userInput mirrors domain.UserInput — the message a human addressed to a running sub-agent. Its
// images are listed by name, media type and size only: an image's bytes never reach the stream.
type userInput struct {
	Text     string       `json:"text"`
	FileRefs []string     `json:"file_refs"`
	SkillIDs []string     `json:"skill_ids"`
	Images   []inputImage `json:"images"`
}

// inputImage mirrors one domain.Image of a userInput without its Data: Size is the byte count.
type inputImage struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Size      int    `json:"size"`
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
	return userInput{Text: in.Text, FileRefs: in.FileRefs, SkillIDs: in.SkillIDs, Images: inputImagesOf(in.Images)}
}

// inputImagesOf lists images by name, media type and size, nil for none — the member then encodes
// as null, as file_refs and skill_ids do.
func inputImagesOf(images []domain.Image) []inputImage {
	if len(images) == 0 {
		return nil
	}
	out := make([]inputImage, 0, len(images))
	for _, img := range images {
		out = append(out, inputImage{Name: img.Name, MediaType: img.MediaType, Size: len(img.Data)})
	}
	return out
}

// firedOrEmpty returns fired, or an empty non-nil slice when it holds nothing, so the member
// encodes as `[]` and never as null.
func firedOrEmpty(fired []string) []string {
	if fired == nil {
		return []string{}
	}
	return fired
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

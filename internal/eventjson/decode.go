package eventjson

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ErrUnsupportedVersion reports a line stamped with a `v` newer than this build reads: a removal,
// rename or changed meaning bumps `v` (ADR 0075 decision 10), so such a line cannot be trusted to
// mean what the members it shares with v:2 say. Decode wraps it; test with errors.Is.
var ErrUnsupportedVersion = errors.New("eventjson: line version is newer than this build reads")

// costFractionDigits is how many decimal places a `cost` number may carry: CostAmount cuts it from
// millionths of the currency unit, so six is the most an exact amount ever needs.
const costFractionDigits = 6

// Line is one decoded Event line: its envelope, and exactly one of Event, Started, Finished and
// Prompt — or none of them, for a kind this build does not know.
//
// A null envelope member decodes as its zero value: Session is "" before the run had an id, and
// Turn, Depth, CallID and RunID are zero on a frame and at Depth 0. Event carries the same four in
// its EventBase.
type Line struct {
	Kind    string
	V       int
	Seq     int
	Time    time.Time
	Session string
	Turn    int
	Depth   int
	CallID  string
	RunID   string

	Event    domain.Event
	Started  *RunStarted
	Finished *RunFinished
	Prompt   *Prompt
}

// Decode reads one Event line back into its envelope and its domain.Event, a frame for
// run_started and run_finished, or a Prompt for the journal's prompt line.
//
// It is lenient where ADR 0075 decision 10 makes a consumer lenient: an unknown kind returns the
// envelope with no payload and no error, and an unknown member is ignored. It errors on a line
// that is not a JSON object, names no kind, carries an unparsable time, carries a member of the
// wrong type, or is stamped with a `v` above lineVersion (ErrUnsupportedVersion). A line whose
// `data` is null — the writer's fallback for arguments json.Marshal refused — decodes to its
// Event with the envelope's identity and every other member zero.
//
// Encoding then decoding returns the original Event except where the line never carried the fact
// or carries it in a lossy form: SeamClosedEvent.Value, an image's bytes and a usage line's
// currency label are not on the line, and a sub_agent_phase line's constant `cancelled` has no
// field to land in; UpstreamAttemptEvent durations come back truncated to whole milliseconds; a
// nil Fired comes back as an empty slice and nil receipt Fields as an empty map, while empty Rules
// and Images come back nil; argument JSON comes back compacted and HTML-escaped, a null one as
// nil; MalformedArguments.Err comes back as an error carrying only the text, its sentinel chain
// not rebuilt; a Tool summary of an unknown kind comes back nil.
//
// Decoded text is not sanitized: a reader that paints it strips it at the view.
func Decode(line []byte) (Line, error) {
	env := envelope{Data: new(json.RawMessage)}
	if err := json.Unmarshal(line, &env); err != nil {
		return Line{}, fmt.Errorf("eventjson: decode line: %w", err)
	}
	if env.V > lineVersion {
		return Line{}, fmt.Errorf("%w: v %d, newest known %d", ErrUnsupportedVersion, env.V, lineVersion)
	}
	if env.Event == "" {
		return Line{}, errors.New("eventjson: decode line: no event kind")
	}
	stamp, err := time.Parse(time.RFC3339Nano, env.Time)
	if err != nil {
		return Line{}, fmt.Errorf("eventjson: decode %s line: time: %w", env.Event, err)
	}

	decoded := Line{
		Kind:    env.Event,
		V:       env.V,
		Seq:     env.Seq,
		Time:    stamp,
		Session: stringOrEmpty(env.Session),
		Turn:    intOrZero(env.Turn),
		Depth:   intOrZero(env.Depth),
		CallID:  stringOrEmpty(env.CallID),
		RunID:   stringOrEmpty(env.RunID),
	}
	if err := decoded.fill(rawBytesOf(env.Data)); err != nil {
		return Line{}, fmt.Errorf("eventjson: decode %s line: %w", env.Event, err)
	}
	return decoded, nil
}

// fill decodes the line's `data` into the member its kind names. payload is nil when `data` was
// null or absent, and every member is then left at its zero.
func (l *Line) fill(payload []byte) error {
	switch l.Kind {
	case kindRunStarted:
		l.Started = &RunStarted{}
		return unmarshalPayload(payload, l.Started)
	case kindRunFinished:
		l.Finished = &RunFinished{}
		return unmarshalPayload(payload, l.Finished)
	case kindPrompt:
		l.Prompt = &Prompt{}
		return unmarshalPayload(payload, l.Prompt)
	}

	decoder, known := eventDecoders[l.Kind]
	if !known {
		return nil
	}
	base := domain.EventBase{Turn: l.Turn, Depth: l.Depth, CallID: l.CallID, RunID: l.RunID}
	event, err := decoder(base, payload)
	if err != nil {
		return err
	}
	l.Event = event
	return nil
}

// eventDecoder rebuilds one kind's domain.Event from the envelope's identity and the line's `data`.
type eventDecoder func(base domain.EventBase, payload []byte) (domain.Event, error)

// decodeWith unmarshals `data` into the kind's own wire struct — the one encode marshals from — and
// hands it to build.
func decodeWith[T any](build func(domain.EventBase, T) (domain.Event, error)) eventDecoder {
	return func(base domain.EventBase, payload []byte) (domain.Event, error) {
		var data T
		if err := unmarshalPayload(payload, &data); err != nil {
			return nil, err
		}
		return build(base, data)
	}
}

// eventDecoders is encode's switch read backwards: one entry per Event kind.
var eventDecoders = map[string]eventDecoder{
	kindToken: decodeWith(func(base domain.EventBase, d tokenData) (domain.Event, error) {
		return domain.TokenEvent{EventBase: base, Text: d.Text}, nil
	}),
	kindReasoning: decodeWith(func(base domain.EventBase, d reasoningData) (domain.Event, error) {
		return domain.ReasoningEvent{EventBase: base, Text: d.Text}, nil
	}),
	kindStreamReset: decodeWith(func(base domain.EventBase, _ streamResetData) (domain.Event, error) {
		return domain.StreamResetEvent{EventBase: base}, nil
	}),
	kindMessage: decodeWith(func(base domain.EventBase, d messageData) (domain.Event, error) {
		return domain.MessageEvent{EventBase: base, Text: d.Text}, nil
	}),
	kindToolCall: decodeWith(func(base domain.EventBase, d toolCallData) (domain.Event, error) {
		return domain.ToolCallEvent{
			EventBase: base, Call: toolCallFrom(d.Call), ResolvedPath: d.ResolvedPath, SpawnRunID: d.SpawnRunID,
		}, nil
	}),
	kindToolResult:        decodeWith(toolResultEventFrom),
	kindSubAgentPhase:     decodeWith(subAgentPhaseEventFrom),
	kindSubAgentGroup:     decodeWith(subAgentGroupEventFrom),
	kindSubAgentNamed:     decodeWith(subAgentNamedEventFrom),
	kindChildInterjection: decodeWith(childInterjectionEventFrom),
	kindApproval:          decodeWith(approvalEventFrom),
	kindTurn: decodeWith(func(base domain.EventBase, d turnData) (domain.Event, error) {
		return domain.TurnEvent{
			EventBase: base, Status: domain.StepStatus(d.Status), Faulted: d.Faulted, StepCapped: d.StepCapped,
		}, nil
	}),
	kindReactionFired: decodeWith(func(base domain.EventBase, d reactionFiredData) (domain.Event, error) {
		return domain.ReactionFiredEvent{
			EventBase: base, Reaction: d.Reaction, Origin: domain.Origin(d.Origin),
			Moment: domain.Moment(d.Moment), Action: d.Action, Detail: d.Detail,
		}, nil
	}),
	kindError: decodeWith(func(base domain.EventBase, d errorData) (domain.Event, error) {
		return domain.ErrorEvent{EventBase: base, Source: d.Source, Err: d.Err}, nil
	}),
	kindPrune: decodeWith(func(base domain.EventBase, d pruneData) (domain.Event, error) {
		return domain.PruneEvent{EventBase: base, Results: d.Results, Tokens: d.Tokens}, nil
	}),
	kindRefClipped: decodeWith(func(base domain.EventBase, d refClippedData) (domain.Event, error) {
		return domain.RefClippedEvent{EventBase: base, Ref: d.Ref, Tokens: d.Tokens, Absolute: d.Absolute}, nil
	}),
	kindUsage: decodeWith(usageEventFrom),
	kindAudit: decodeWith(func(base domain.EventBase, d auditData) (domain.Event, error) {
		return domain.AuditEvent{
			EventBase: base, Tool: d.Tool, CallID: d.CallID, Decision: d.Decision, Reason: d.Reason,
			IsError: d.IsError,
		}, nil
	}),
	kindSeamClosed: decodeWith(func(base domain.EventBase, d seamClosedData) (domain.Event, error) {
		return domain.SeamClosedEvent{EventBase: base, Seam: seamClosedBy(d.Seam), Fired: d.Fired}, nil
	}),
	kindUpstreamAttempt: decodeWith(upstreamAttemptEventFrom),
	kindWorkflowPhase:   decodeWith(workflowPhaseEventFrom),
	kindMalformedChunks: decodeWith(func(base domain.EventBase, d malformedChunksData) (domain.Event, error) {
		return domain.MalformedChunksEvent{EventBase: base, Count: d.Count}, nil
	}),
}

// toolResultEventFrom rebuilds a domain.ToolResultEvent, its Tool summary included.
func toolResultEventFrom(base domain.EventBase, d toolResultData) (domain.Event, error) {
	result, err := toolResultFrom(d.Result)
	if err != nil {
		return nil, err
	}
	return domain.ToolResultEvent{
		EventBase: base, Result: result, Tool: d.Tool, WriteTarget: d.WriteTarget, SpawnRunID: d.SpawnRunID,
	}, nil
}

// subAgentPhaseEventFrom rebuilds a domain.SubAgentPhaseEvent. The line's `cancelled` is the
// constant false the variant no longer carries a field for, so it is read and dropped.
func subAgentPhaseEventFrom(base domain.EventBase, d subAgentPhaseData) (domain.Event, error) {
	result, err := toolResultFrom(d.Result)
	if err != nil {
		return nil, err
	}
	return domain.SubAgentPhaseEvent{
		EventBase: base, Phase: domain.SubAgentPhase(d.Phase), Result: result, StepCap: d.StepCap,
		CapRequested: d.CapRequested,
	}, nil
}

// subAgentGroupEventFrom rebuilds a domain.SubAgentGroupEvent.
func subAgentGroupEventFrom(base domain.EventBase, d subAgentGroupData) (domain.Event, error) {
	return domain.SubAgentGroupEvent{EventBase: base, Size: d.Size, Width: d.Width}, nil
}

// subAgentNamedEventFrom rebuilds a domain.SubAgentNamedEvent.
func subAgentNamedEventFrom(base domain.EventBase, d subAgentNamedData) (domain.Event, error) {
	return domain.SubAgentNamedEvent{EventBase: base, Name: d.Name}, nil
}

// childInterjectionEventFrom rebuilds a domain.ChildInterjectionEvent.
func childInterjectionEventFrom(base domain.EventBase, d childInterjectionData) (domain.Event, error) {
	return domain.ChildInterjectionEvent{
		EventBase: base, Input: userInputFrom(d.Input), Landed: d.Landed,
		Reason: domain.UndeliveredReason(d.Reason),
	}, nil
}

// approvalEventFrom rebuilds a domain.ApprovalEvent.
func approvalEventFrom(base domain.EventBase, d approvalData) (domain.Event, error) {
	return domain.ApprovalEvent{
		EventBase: base,
		Phase:     domain.ApprovalPhase(d.Phase),
		Request:   approvalRequestFrom(d.Request),
		Decision:  domain.ApprovalDecision(d.Decision),
		Rules:     allowRulesFrom(d.Rules),
	}, nil
}

// usageEventFrom rebuilds a domain.UsageEvent, reading both cost numbers back into millionths. The
// line's currency label belongs to the run, not the Event, and is dropped.
func usageEventFrom(base domain.EventBase, d usageData) (domain.Event, error) {
	cost, err := costMicrosOf(d.Cost)
	if err != nil {
		return nil, fmt.Errorf("cost: %w", err)
	}
	cumulativeCost, err := costMicrosOf(d.CumulativeCost)
	if err != nil {
		return nil, fmt.Errorf("cumulative_cost: %w", err)
	}
	return domain.UsageEvent{
		EventBase:          base,
		PromptTokens:       d.PromptTokens,
		CompletionTokens:   d.CompletionTokens,
		TotalTokens:        d.TotalTokens,
		CachedPromptTokens: d.CachedPromptTokens,
		Model:              d.Model,
		ContextWindow:      d.ContextWindow,
		Cumulative: domain.Usage{
			Calls:              d.CumulativeCalls,
			PromptTokens:       d.CumulativePromptTokens,
			CachedPromptTokens: d.CumulativeCachedPromptTokens,
			CompletionTokens:   d.CumulativeCompletionTokens,
			TotalTokens:        d.CumulativeTotalTokens,
			CostMicros:         cumulativeCost,
			PricedCalls:        d.CumulativePricedCalls,
			UnpricedCalls:      d.CumulativeUnpricedCalls,
		},
		CostMicros:   cost,
		Priced:       d.Priced,
		Maintenance:  d.Maintenance,
		DelegateFold: d.DelegateFold,
		ServedModel:  d.ServedModel,
	}, nil
}

// upstreamAttemptEventFrom rebuilds a domain.UpstreamAttemptEvent; each duration is the whole
// milliseconds the line carries.
func upstreamAttemptEventFrom(base domain.EventBase, d upstreamAttemptData) (domain.Event, error) {
	return domain.UpstreamAttemptEvent{
		EventBase:    base,
		Server:       d.Server,
		Endpoint:     d.Endpoint,
		Model:        d.Model,
		RequestID:    d.RequestID,
		Index:        d.Index,
		TTFB:         time.Duration(d.TTFBMs) * time.Millisecond,
		TTFT:         time.Duration(d.TTFTMs) * time.Millisecond,
		Last:         time.Duration(d.LastMs) * time.Millisecond,
		Duration:     time.Duration(d.DurationMs) * time.Millisecond,
		OutputTokens: d.OutputTokens,
		Outcome:      d.Outcome,
	}, nil
}

// workflowPhaseEventFrom rebuilds a domain.WorkflowPhaseEvent.
func workflowPhaseEventFrom(base domain.EventBase, d workflowPhaseData) (domain.Event, error) {
	return domain.WorkflowPhaseEvent{
		EventBase:  base,
		Phase:      domain.WorkflowPhase(d.Phase),
		Workflow:   d.Workflow,
		Name:       d.Name,
		Stage:      d.Stage,
		Item:       d.Item,
		ItemName:   d.ItemName,
		Index:      d.Index,
		Resumed:    d.Resumed,
		Receipt:    domain.WorkflowReceipt{Status: d.Receipt.Status, Summary: d.Receipt.Summary, Fields: d.Receipt.Fields},
		Detail:     d.Detail,
		Background: d.Background,
		Call:       d.Call,
		Origin:     d.Origin,
		Stages:     d.Stages,
		StageKinds: d.StageKinds,
		Items:      d.Items,
		Round:      d.Round,
		Rounds:     d.Rounds,
		Outcome:    domain.WorkflowStageOutcome(d.Outcome),
		Run:        d.Run,
		Attempt:    d.Attempt,
		Resume:     d.Resume,
		Tally:      workflowTallyFrom(d.Tally),
	}, nil
}

// workflowTallyFrom copies a line's tally back, nil for null.
func workflowTallyFrom(tally *workflowTally) *domain.WorkflowTally {
	if tally == nil {
		return nil
	}
	return &domain.WorkflowTally{
		OK: tally.OK, Partial: tally.Partial, Blocked: tally.Blocked, Unfinished: tally.Unfinished,
	}
}

// seamClosedBy returns the seam whose closing notice the line names, "" for a name no seam closes
// with — a seam added after this build reads as no seam rather than failing the line.
func seamClosedBy(closing string) domain.Moment {
	for _, seam := range domain.Seams() {
		if string(seam.Closing()) == closing {
			return seam
		}
	}
	return ""
}

// toolCallFrom rebuilds a domain.ToolCall from its wire mirror.
func toolCallFrom(call toolCall) domain.ToolCall {
	return domain.ToolCall{
		ID:        call.ID,
		Tool:      call.Tool,
		Arguments: argumentsFrom(call.Arguments),
		Malformed: malformedArgumentsFrom(call.Malformed),
	}
}

// malformedArgumentsFrom rebuilds a malformed-call marker, its error a plain one carrying the
// line's text — "" decodes as no error, as malformedArgumentsOf writes one.
func malformedArgumentsFrom(mirror *malformedArguments) *domain.MalformedArguments {
	if mirror == nil {
		return nil
	}
	marker := &domain.MalformedArguments{Raw: mirror.Raw}
	if mirror.Err != "" {
		marker.Err = errors.New(mirror.Err)
	}
	return marker
}

// argumentsFrom returns argument JSON as the line carried it, nil for a null member: rawOrNull
// wrote nil as null, and json.RawMessage decodes null as the four bytes rather than as nil.
func argumentsFrom(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}

// UnmarshalJSON decodes a tool result with its summary held as raw bytes: the summary's own `kind`
// names its shape, so toolSummaryFrom reads it after. A plain conversion type drops this method
// for the inner decode, and a non-nil *json.RawMessage in Summary is what encoding/json fills in
// place of building a map.
func (r *toolResult) UnmarshalJSON(data []byte) error {
	type plainToolResult toolResult
	plain := plainToolResult{Summary: new(json.RawMessage)}
	if err := json.Unmarshal(data, &plain); err != nil {
		return err
	}
	*r = toolResult(plain)
	return nil
}

// toolResultFrom rebuilds a domain.ToolResult, its Tool summary included.
func toolResultFrom(res toolResult) (domain.ToolResult, error) {
	summary, err := toolSummaryFrom(res.Summary)
	if err != nil {
		return domain.ToolResult{}, fmt.Errorf("summary: %w", err)
	}
	return domain.ToolResult{CallID: res.CallID, Content: res.Content, IsError: res.IsError, Summary: summary}, nil
}

// summaryDecoder rebuilds one Tool summary variant from its tagged mirror.
type summaryDecoder func(raw []byte) (domain.ToolSummary, error)

// summaryWith unmarshals a summary into its variant's mirror and hands it to build.
func summaryWith[T any](build func(T) domain.ToolSummary) summaryDecoder {
	return func(raw []byte) (domain.ToolSummary, error) {
		var mirror T
		if err := json.Unmarshal(raw, &mirror); err != nil {
			return nil, err
		}
		return build(mirror), nil
	}
}

// summaryDecoders is toolSummaryOf read backwards: one entry per `kind`.
var summaryDecoders = map[string]summaryDecoder{
	summaryReadSpan: summaryWith(func(s readSpanSummary) domain.ToolSummary {
		return domain.ReadSpan{Start: s.Start, End: s.End, Total: s.Total, Locate: s.Locate, LocatedOn: s.LocatedOn}
	}),
	summaryListedEntries: summaryWith(func(s listedEntriesSummary) domain.ToolSummary {
		return domain.ListedEntries{Total: s.Total, Skipped: s.Skipped}
	}),
	summaryMatchedLines: summaryWith(func(s matchedLinesSummary) domain.ToolSummary {
		return domain.MatchedLines{Total: s.Total}
	}),
	summaryDiffStat: summaryWith(func(s diffStatSummary) domain.ToolSummary {
		return domain.DiffStat{Added: s.Added, Removed: s.Removed}
	}),
	summaryChangedFiles: summaryWith(func(s changedFilesSummary) domain.ToolSummary {
		return domain.ChangedFiles{Staged: s.Staged, Unstaged: s.Unstaged, Untracked: s.Untracked}
	}),
	summaryEditRegions: summaryWith(func(s editRegionsSummary) domain.ToolSummary {
		return domain.EditRegions{Regions: editRegionsFrom(s.Regions)}
	}),
	summarySearchHits: summaryWith(func(s searchHitsSummary) domain.ToolSummary {
		return domain.SearchHits{Count: s.Count}
	}),
	summaryDelegationOutcome: summaryWith(func(s delegationOutcomeSummary) domain.ToolSummary {
		return domain.DelegationOutcome{
			Bound: delegationBoundNamed(s.Bound), IsStoppedByUser: s.StoppedByUser, HasNoReport: s.HasNoReport,
			SteeredMessages: s.SteeredMessages,
		}
	}),
}

// toolSummaryFrom rebuilds a Tool summary from the raw bytes toolResult.UnmarshalJSON kept. A null
// or absent summary, and one whose `kind` this build does not know, return nil: the summary is an
// optional view aid, and an unknown variant is skipped like an unknown kind (ADR 0075 decision 10).
func toolSummaryFrom(summary any) (domain.ToolSummary, error) {
	raw := rawBytesOf(summary)
	if raw == nil {
		return nil, nil
	}
	var tag struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &tag); err != nil {
		return nil, err
	}
	decoder, known := summaryDecoders[tag.Kind]
	if !known {
		return nil, nil
	}
	return decoder(raw)
}

// editRegionsFrom rebuilds edit regions, nil for null and empty for `[]`, as editRegionsOf wrote
// them.
func editRegionsFrom(regions []editRegion) []domain.EditRegion {
	if regions == nil {
		return nil
	}
	out := make([]domain.EditRegion, 0, len(regions))
	for _, region := range regions {
		out = append(out, domain.EditRegion{
			BeforeStart: region.BeforeStart,
			AfterStart:  region.AfterStart,
			Leading:     region.Leading,
			Removed:     region.Removed,
			Inserted:    region.Inserted,
			Trailing:    region.Trailing,
		})
	}
	return out
}

// delegationBoundNamed returns the bound a wire name spells, DelegationUnbounded for "" and for a
// name this build does not know.
func delegationBoundNamed(name string) domain.DelegationBound {
	for bound, spelled := range delegationBoundNames {
		if spelled == name {
			return bound
		}
	}
	return domain.DelegationUnbounded
}

// approvalRequestFrom rebuilds a domain.ApprovalRequest from its wire mirror.
func approvalRequestFrom(req approvalRequest) domain.ApprovalRequest {
	return domain.ApprovalRequest{
		Tool:           req.Tool,
		Arguments:      argumentsFrom(req.Arguments),
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

// allowRulesFrom rebuilds the rules that answered a gate, nil for none.
func allowRulesFrom(rules []allowRule) []domain.AllowRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]domain.AllowRule, len(rules))
	for i, r := range rules {
		out[i] = domain.AllowRule{Kind: domain.AllowRuleKind(r.Kind), Text: r.Text, Layer: domain.AllowRuleLayer(r.Layer)}
	}
	return out
}

// userInputFrom rebuilds a domain.UserInput. Its images come back by name and media type only:
// the line never carried their bytes.
func userInputFrom(in userInput) domain.UserInput {
	return domain.UserInput{
		Text:         in.Text,
		FileRefs:     in.FileRefs,
		Images:       inputImagesFrom(in.Images),
		SkillIDs:     in.SkillIDs,
		Recipe:       recipeLaunchFrom(in.Recipe),
		RecipeInputs: in.RecipeInputs,
	}
}

// inputImagesFrom lists the images by name and media type, nil for none.
func inputImagesFrom(images []inputImage) []domain.Image {
	if len(images) == 0 {
		return nil
	}
	out := make([]domain.Image, 0, len(images))
	for _, img := range images {
		out = append(out, domain.Image{Name: img.Name, MediaType: img.MediaType})
	}
	return out
}

// recipeLaunchFrom rebuilds a recipe launch, nil for null.
func recipeLaunchFrom(launch *recipeLaunch) *domain.RecipeLaunch {
	if launch == nil {
		return nil
	}
	return &domain.RecipeLaunch{SkillID: launch.SkillID, Text: launch.Text, Background: launch.Background}
}

// costMicrosOf reads a `cost` number back into millionths of the currency unit — CostAmount's
// inverse, from the decimal text itself so no float rounding enters. An empty number reads as 0.
func costMicrosOf(amount json.Number) (int64, error) {
	text := string(amount)
	if text == "" {
		return 0, nil
	}
	isNegative := strings.HasPrefix(text, "-")
	whole, fraction, _ := strings.Cut(strings.TrimPrefix(text, "-"), ".")
	if len(fraction) > costFractionDigits {
		return 0, fmt.Errorf("%q has more than %d decimal places", text, costFractionDigits)
	}
	fraction += strings.Repeat("0", costFractionDigits-len(fraction))

	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a decimal amount: %w", text, err)
	}
	millionths, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a decimal amount: %w", text, err)
	}
	const microsPerUnit = 1_000_000
	micros := units*microsPerUnit + millionths
	if isNegative {
		micros = -micros
	}
	return micros, nil
}

// unmarshalPayload decodes a line's `data` into target, leaving target untouched when payload is
// nil — the line's data was null or absent.
func unmarshalPayload(payload []byte, target any) error {
	if payload == nil {
		return nil
	}
	return json.Unmarshal(payload, target)
}

// rawBytesOf returns the bytes a *json.RawMessage placeholder caught, nil when the member was null
// (encoding/json then clears the placeholder) or absent (it then stays empty).
func rawBytesOf(member any) []byte {
	raw, isRaw := member.(*json.RawMessage)
	if !isRaw || raw == nil || len(*raw) == 0 {
		return nil
	}
	return *raw
}

// stringOrEmpty reads a nullable envelope string, "" for null.
func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// intOrZero reads a nullable envelope integer, 0 for null.
func intOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

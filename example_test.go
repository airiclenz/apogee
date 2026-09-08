package apogee_test

// Completeness guard (ADR 0010 / P1.0e). This external test file names the full
// public surface of package apogee. It compiles but never runs: if a re-export is
// dropped when the facade is regenerated — a missing type alias, const, sentinel, or
// forwarder — the build fails here rather than silently shrinking the public API.
//
// The completeness-guard declarations below are compile-time only by construction: every
// reference is a type declaration, a constant reference, or a function *value* — no method on
// a panic-stub working value (Request / Response / Conversation / ToolRegistry) is ever called.
//
// The file also carries a runnable godoc Example for the public Reaction arming surface (ADR
// 0076): Config.Reactions armed against the scripted upstream this package already drives, so
// what it shows is a real firing observed as a ReactionFiredEvent rather than a sketch.

import (
	"context"
	"fmt"

	"github.com/airiclenz/apogee"
)

// Type aliases — one zero-valued declaration per exported type. A dropped alias makes
// the type name undefined and fails compilation.
var (
	_ apogee.Agent
	_ apogee.Config
	_ apogee.RebindSpec
	_ apogee.UpstreamSpec
	_ apogee.DelegationTarget
	_ apogee.DelegationSeat
	_ apogee.ContextConfig
	_ apogee.DelegationConfig
	_ apogee.FloorConfig
	_ apogee.ContextFilesReport
	_ apogee.ContextFileNote
	_ apogee.ModelProfile
	_ apogee.ToolCallFormat
	_ apogee.ThinkingProfile
	_ apogee.ThinkingStyle
	_ apogee.ThinkingEffort
	_ apogee.Mode
	_ apogee.UserInput
	_ apogee.StepResult
	_ apogee.StepStatus
	_ apogee.EventSink
	_ apogee.Event
	_ apogee.TokenEvent
	_ apogee.ReasoningEvent
	_ apogee.StreamResetEvent
	_ apogee.MessageEvent
	_ apogee.ToolCallEvent
	_ apogee.ToolResultEvent
	_ apogee.ApprovalEvent
	_ apogee.TurnEvent
	_ apogee.SubAgentPhaseEvent
	_ apogee.SubAgentNamedEvent
	_ apogee.ChildInterjectionEvent
	_ apogee.SubAgentPhase
	_ apogee.ReactionFiredEvent
	_ apogee.ErrorEvent
	_ apogee.PruneEvent
	_ apogee.UsageEvent
	_ apogee.AuditEvent
	_ apogee.WireEvent
	_ apogee.Approver
	_ apogee.ApprovalRequest
	_ apogee.ApprovalDecision
	_ apogee.ApprovalPhase
	_ apogee.Asker
	_ apogee.AskRequest
	_ apogee.AskAnswer
	_ apogee.Presenter
	_ apogee.PresentRequest
	_ apogee.PresentOutcome
	_ apogee.PresentMethod
	_ apogee.SkillResolver
	_ apogee.ResolvedSkill
	_ apogee.Tool
	_ apogee.ExternalEffectTool
	_ apogee.ReadOnlyTool
	_ apogee.ExternalEffectKind
	_ apogee.ToolCall
	_ apogee.ToolResult
	_ apogee.ToolSummary
	_ apogee.ReadSpan
	_ apogee.ListedEntries
	_ apogee.MatchedLines
	_ apogee.DiffStat
	_ apogee.ChangedFiles
	_ apogee.EditRegion
	_ apogee.EditRegions
	_ apogee.SearchHits
	_ apogee.ToolRegistry
	_ apogee.ExternalEffects
	_ apogee.Role
	_ apogee.Message
	_ apogee.ToolDef
	_ apogee.Budget
	_ apogee.LoopView
	_ apogee.ConversationView
	_ apogee.Request
	_ apogee.SamplingParams
	_ apogee.Response
	_ apogee.FinishReason
	_ apogee.Conversation
	_ apogee.ToolCallEdit
	_ apogee.ToolResultEdit
	_ apogee.Confiner
	_ apogee.ConfinementCaps
	_ apogee.ConfinementBox
	_ apogee.Session
	_ apogee.Hook
	_ apogee.HookEvent
	_ apogee.HookPayload
	_ apogee.HookOptions
	_ apogee.HookRunner
	_ apogee.EventLines
	_ apogee.EventLinesOptions
	_ apogee.RunStarted
	_ apogee.RunFinished
)

// Forwarding constructors — referenced as values so the facade keeps delegating them.
var (
	_ = apogee.New
	_ = apogee.Resume
	_ = apogee.IsReadOnly
	_ = apogee.NewToolRegistry
	_ = apogee.DecodeSession
	_ = apogee.NewHookRunner
	_ = apogee.NewEventLines
)

// Re-exported consts and sentinel errors — one reference each.
var (
	_ = apogee.SeatFallbackNote
	_ = apogee.DelegateReportBlock
	_ = apogee.TaskListFence

	_ = apogee.FormatNative
	_ = apogee.FormatMarkdownFenced
	_ = apogee.FormatCustomRegex

	_ = apogee.ThinkingNone
	_ = apogee.ThinkingDelimited
	_ = apogee.ThinkingHarmony

	_ = apogee.EffortOff
	_ = apogee.EffortNone
	_ = apogee.EffortMinimal
	_ = apogee.EffortLow
	_ = apogee.EffortMedium
	_ = apogee.EffortHigh
	_ = apogee.EffortXHigh
	_ = apogee.EffortMax

	_ = apogee.ModePlan
	_ = apogee.ModeAskBefore
	_ = apogee.ModeAllowEdits
	_ = apogee.ModeAuto

	_ = apogee.StatusTurnComplete
	_ = apogee.StatusExchangeComplete
	_ = apogee.StatusCancelled

	_ = apogee.SubAgentStarted
	_ = apogee.SubAgentFinished

	_ = apogee.WireDirectionRequest
	_ = apogee.WireDirectionResponse

	_ = apogee.ApprovalAllow
	_ = apogee.ApprovalDeny
	_ = apogee.ApprovalAllowForSession
	_ = apogee.ApprovalRequested
	_ = apogee.ApprovalDecided

	_ = apogee.PresentOpened
	_ = apogee.PresentServed
	_ = apogee.PresentShown

	_ = apogee.EffectNetwork
	_ = apogee.EffectMCP

	_ = apogee.RoleSystem
	_ = apogee.RoleUser
	_ = apogee.RoleAssistant
	_ = apogee.RoleTool

	_ = apogee.FinishStop
	_ = apogee.FinishLength
	_ = apogee.FinishToolCalls

	_ = apogee.ErrAutoUnavailable
	_ = apogee.ErrConfinementUnavailable
	_ = apogee.ErrInvalidReaction
	_ = apogee.ErrSessionVersion
	_ = apogee.ErrInputPending
	_ = apogee.ErrNoOpenExchange
	_ = apogee.ErrNoSuchChild
	_ = apogee.ErrDuplicateTool
	_ = apogee.ErrInvalidTool

	// Version is the single-source-of-truth accessor for the embedded VERSION file.
	_ = apogee.Version
)

// reactionSink is the host's observer, narrowed to the firings of ONE Reaction and rendering
// each the way apogee's own debug view does: `reaction <id> @ <moment>: <action>`, with the
// optional detail in brackets. Every other Event is dropped, so the example's output is the
// armed Reaction's record and nothing else the loop happened to emit.
type reactionSink struct {
	watch string
	fired []string
}

func (s *reactionSink) Emit(e apogee.Event) {
	fired, ok := e.(apogee.ReactionFiredEvent)
	if !ok || fired.Reaction != s.watch {
		return
	}
	line := fmt.Sprintf("reaction %s @ %s: %s", fired.Reaction, fired.Moment, fired.Action)
	if fired.Detail != "" {
		line += " (" + fired.Detail + ")"
	}
	s.fired = append(s.fired, line)
}

// Example_armReaction arms one Reaction of the engine's own origin BESIDE the engine's builtins,
// through Config.Reactions: an advise Reaction at the post-tool-result Moment, which appends a
// note to every tool result before the model reads it. Arming is the whole surface — an id, the
// origin × class cell it occupies, the Moments it fires on, and one of the five sealed Go
// handlers — and a Reaction that ACTS books exactly one ReactionFiredEvent, which is what the
// host's EventSink observes here.
func Example_armReaction() {
	upstream := benchModel()
	defer upstream.Close()

	menu := apogee.NewToolRegistry()
	if err := menu.Register(stubTool{name: "list_dir"}); err != nil {
		fmt.Println("register:", err)
		return
	}

	const reactionID = "result-note"
	sink := &reactionSink{watch: reactionID}

	ag, err := apogee.New(apogee.Config{
		Endpoint: upstream.URL,
		Model:    benchModelName,
		Mode:     apogee.ModeAskBefore,
		Approver: allowAll{},
		Events:   sink,
		Tools:    menu,
		Reactions: []apogee.Reaction{{
			ID:     reactionID,
			Origin: apogee.OriginEngine,
			Class:  apogee.ClassAdvise,
			On:     []apogee.Moment{apogee.MomentPostToolResult},
			Handler: apogee.PostToolResultFunc(func(
				_ context.Context,
				_ apogee.LoopView,
				call apogee.ToolCall,
				result *apogee.ToolResultEdit,
			) (apogee.Outcome, error) {
				result.SetContent(result.Content() + "\n[note] " + call.Tool + " ran under review.")
				return apogee.Outcome{Detail: "noted " + call.Tool}, nil
			}),
		}},
	})
	if err != nil {
		fmt.Println("construct:", err)
		return
	}
	defer func() { _ = ag.Close() }()

	if err := ag.Submit(apogee.UserInput{Text: "list the workspace"}); err != nil {
		fmt.Println("submit:", err)
		return
	}
	for i := 0; i < 8; i++ {
		res, err := ag.Step(context.Background())
		if err != nil {
			fmt.Println("step:", err)
			return
		}
		if res.Status == apogee.StatusExchangeComplete {
			break
		}
	}

	for _, line := range sink.fired {
		fmt.Println(line)
	}
	// Output:
	// reaction result-note @ post-tool-result: fired (noted list_dir)
}

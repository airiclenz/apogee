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
// The file also carries runnable godoc Examples for the public Mechanism enable surface (ADR
// 0015). They are hermetic: construction builds and validates Mechanisms without dialing the
// Endpoint, and the catalogue query is a pure read.

import (
	"fmt"
	"slices"

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
	_ apogee.MechanismFiredEvent
	_ apogee.ErrorEvent
	_ apogee.UsageEvent
	_ apogee.AuditEvent
	_ apogee.WireEvent
	_ apogee.Approver
	_ apogee.ApprovalRequest
	_ apogee.ApprovalDecision
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
	_ apogee.ToolRegistry
	_ apogee.ExternalEffects
	_ apogee.HookPoint
	_ apogee.PreRequestHook
	_ apogee.PostResponseHook
	_ apogee.PreToolExecHook
	_ apogee.PostToolResultHook
	_ apogee.HistoryRewriter
	_ apogee.PostResponseDecision
	_ apogee.PostResponseAction
	_ apogee.RegisteredMechanism
	_ apogee.MechanismID
	_ apogee.MechanismDescriptor
	_ apogee.Capability
	_ apogee.SuppressionPolicy
	_ apogee.OrderingConstraints
	_ apogee.MechanismRegistry
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
	_ apogee.Confiner
	_ apogee.ConfinementCaps
	_ apogee.ConfinementBox
	_ apogee.Session
)

// Forwarding constructors — referenced as values so the facade keeps delegating them.
var (
	_ = apogee.New
	_ = apogee.Resume
	_ = apogee.IsReadOnly
	_ = apogee.NewToolRegistry
	_ = apogee.NewMechanismRegistry
	_ = apogee.BuildMechanisms
	_ = apogee.CataloguedMechanisms
	_ = apogee.DecodeSession
)

// Re-exported consts and sentinel errors — one reference each.
var (
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

	_ = apogee.ApprovalAllow
	_ = apogee.ApprovalDeny
	_ = apogee.ApprovalAllowForSession

	_ = apogee.PresentOpened
	_ = apogee.PresentServed
	_ = apogee.PresentShown

	_ = apogee.EffectNetwork
	_ = apogee.EffectMCP

	_ = apogee.HookPreRequest
	_ = apogee.HookPostResponse
	_ = apogee.HookPreToolExec
	_ = apogee.HookPostToolResult
	_ = apogee.HookHistoryRewrite

	_ = apogee.ActionRetry
	_ = apogee.ActionIntercept
	_ = apogee.ActionDefer

	_ = apogee.CapOffRamp
	_ = apogee.CapProactiveNudge
	_ = apogee.CapResponseRepair

	_ = apogee.SuppressStrikesThree
	_ = apogee.SuppressExempt

	_ = apogee.RoleSystem
	_ = apogee.RoleUser
	_ = apogee.RoleAssistant
	_ = apogee.RoleTool

	_ = apogee.FinishStop
	_ = apogee.FinishLength
	_ = apogee.FinishToolCalls

	_ = apogee.ErrAutoUnavailable
	_ = apogee.ErrConfinementUnavailable
	_ = apogee.ErrOrderingCycle
	_ = apogee.ErrIncompatibleMechanisms
	_ = apogee.ErrMissingRequirement
	_ = apogee.ErrUnknownMechanism
	_ = apogee.ErrSessionVersion
	_ = apogee.ErrInputPending
	_ = apogee.ErrNoOpenExchange
	_ = apogee.ErrNoSuchChild
	_ = apogee.ErrDuplicateTool
	_ = apogee.ErrInvalidTool

	// Version is the single-source-of-truth accessor for the embedded VERSION file.
	_ = apogee.Version
)

// discardSink is a no-op EventSink so the Examples construct an Agent hermetically — construction
// emits nothing and never dials the Endpoint.
type discardSink struct{}

func (discardSink) Emit(apogee.Event) {}

// Example_enableMechanismStack arms catalogued Mechanisms by ID through Config.EnableMechanisms.
// The arm must be internally compatible — a pair the catalogue declares IncompatibleWith fails New
// with ErrIncompatibleMechanisms — so it is planned from CataloguedMechanisms() itself, keeping each
// row only when it stacks with everything already chosen. Naming no ID keeps the example honest as
// the catalogue changes.
func Example_enableMechanismStack() {
	catalogue := apogee.CataloguedMechanisms()
	byID := make(map[apogee.MechanismID]apogee.MechanismDescriptor, len(catalogue))
	for _, d := range catalogue {
		byID[d.ID] = d
	}

	var arm []apogee.MechanismID
	for _, d := range catalogue {
		if slices.ContainsFunc(arm, func(sel apogee.MechanismID) bool {
			return slices.Contains(d.IncompatibleWith, sel) || slices.Contains(byID[sel].IncompatibleWith, d.ID)
		}) {
			continue // a row that refuses to stack with one already chosen
		}
		arm = append(arm, d.ID)
	}

	cfg := apogee.Config{
		Endpoint:         "http://localhost:11434",
		Model:            "local-model",
		Events:           discardSink{},
		EnableMechanisms: arm,
	}
	ag, err := apogee.New(cfg)
	if err != nil {
		fmt.Println("construct:", err)
		return
	}
	defer func() { _ = ag.Close() }()

	fmt.Println("construct: ok")
	// Output:
	// construct: ok
}

// Example_cataloguedMechanisms plans an arm AROUND one Mechanism the way the bench does: keep the
// Mechanism the arm is about, then drop every row declared incompatible with it, so no refused pair
// reaches New. The subject is taken from CataloguedMechanisms() rather than named, so the idiom —
// not any one row — is what the example shows.
func Example_cataloguedMechanisms() {
	catalogue := apogee.CataloguedMechanisms()
	if len(catalogue) == 0 {
		fmt.Println("no incompatible row survived the filter: true")
		return
	}
	armAbout := catalogue[0].ID

	var arm []apogee.MechanismID
	for _, d := range catalogue {
		if d.ID != armAbout && slices.Contains(d.IncompatibleWith, armAbout) {
			continue // a row that refuses to stack with the one this arm is about
		}
		arm = append(arm, d.ID)
	}

	clean := true
	for _, d := range catalogue {
		if d.ID != armAbout && slices.Contains(d.IncompatibleWith, armAbout) && slices.Contains(arm, d.ID) {
			clean = false
		}
	}
	fmt.Println("no incompatible row survived the filter:", clean)
	// Output:
	// no incompatible row survived the filter: true
}

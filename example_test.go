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
	_ apogee.ContextConfig
	_ apogee.ModelProfile
	_ apogee.ToolCallFormat
	_ apogee.ThinkingProfile
	_ apogee.ThinkingStyle
	_ apogee.Mode
	_ apogee.UserInput
	_ apogee.StepResult
	_ apogee.StepStatus
	_ apogee.EventSink
	_ apogee.Event
	_ apogee.TokenEvent
	_ apogee.StreamResetEvent
	_ apogee.MessageEvent
	_ apogee.ToolCallEvent
	_ apogee.ToolResultEvent
	_ apogee.ApprovalEvent
	_ apogee.MechanismFiredEvent
	_ apogee.ErrorEvent
	_ apogee.Approver
	_ apogee.ApprovalRequest
	_ apogee.ApprovalDecision
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
	_ apogee.Mechanism
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

	_ = apogee.ModePlan
	_ = apogee.ModeAskBefore
	_ = apogee.ModeAuto

	_ = apogee.StatusTurnComplete
	_ = apogee.StatusExchangeComplete
	_ = apogee.StatusCancelled

	_ = apogee.ApprovalAllow
	_ = apogee.ApprovalDeny
	_ = apogee.ApprovalAllowForSession

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
	_ = apogee.ErrOrderingCycle
	_ = apogee.ErrMissingRequirement
	_ = apogee.ErrUnknownMechanism
	_ = apogee.ErrSessionVersion
	_ = apogee.ErrInputPending
	_ = apogee.ErrDuplicateTool
	_ = apogee.ErrInvalidTool
)

// discardSink is a no-op EventSink so the Examples construct an Agent hermetically — construction
// emits nothing and never dials the Endpoint.
type discardSink struct{}

func (discardSink) Emit(apogee.Event) {}

// Example_enableMechanismStack arms the guided_decomposition + tool_result_cap stack by ID through
// Config.EnableMechanisms. guided_decomposition Requires tool_result_cap (ADR 0014), so both must be
// enabled together — enabling one alone fails New with ErrMissingRequirement.
func Example_enableMechanismStack() {
	cfg := apogee.Config{
		Endpoint:         "http://localhost:11434",
		Model:            "local-model",
		Events:           discardSink{},
		EnableMechanisms: []apogee.MechanismID{"guided_decomposition", "tool_result_cap"},
	}
	ag, err := apogee.New(cfg)
	if err != nil {
		fmt.Println("construct:", err)
		return
	}
	defer ag.Close()

	fmt.Println("armed:", cfg.EnableMechanisms)
	// Output:
	// armed: [guided_decomposition tool_result_cap]
}

// Example_cataloguedMechanisms plans a leave-one-out arm from CataloguedMechanisms() — the bench's
// idiom: to leave a Mechanism out, also drop every Mechanism that Requires it, so no half-armed stack
// reaches New (which would refuse with ErrMissingRequirement). Dropping tool_result_cap therefore also
// drops guided_decomposition, which Requires it.
func Example_cataloguedMechanisms() {
	const leaveOut = apogee.MechanismID("tool_result_cap")

	var arm []apogee.MechanismID
	for _, d := range apogee.CataloguedMechanisms() {
		if d.ID == leaveOut || slices.Contains(d.Requires, leaveOut) {
			continue // the left-out Mechanism, and any stack that Requires it
		}
		arm = append(arm, d.ID)
	}

	fmt.Println("left out:", leaveOut)
	fmt.Println("guided_decomposition still armed:", slices.Contains(arm, "guided_decomposition"))
	// Output:
	// left out: tool_result_cap
	// guided_decomposition still armed: false
}

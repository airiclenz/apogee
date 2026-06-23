package apogee_test

// Completeness guard (ADR 0010 / P1.0e). This external test file names the full
// public surface of package apogee. It compiles but never runs: if a re-export is
// dropped when the facade is regenerated — a missing type alias, const, sentinel, or
// forwarder — the build fails here rather than silently shrinking the public API.
//
// It is compile-time only by construction: every reference is a type declaration, a
// constant reference, or a function *value* — no method on a panic-stub working value
// (Request / Response / Conversation / ToolRegistry) is ever called.

import "github.com/airiclenz/apogee"

// Type aliases — one zero-valued declaration per exported type. A dropped alias makes
// the type name undefined and fails compilation.
var (
	_ apogee.Agent
	_ apogee.Config
	_ apogee.ContextConfig
	_ apogee.Mode
	_ apogee.UserInput
	_ apogee.StepResult
	_ apogee.StepStatus
	_ apogee.EventSink
	_ apogee.Event
	_ apogee.TokenEvent
	_ apogee.MessageEvent
	_ apogee.ToolCallEvent
	_ apogee.ToolResultEvent
	_ apogee.ApprovalEvent
	_ apogee.MechanismFiredEvent
	_ apogee.ErrorEvent
	_ apogee.Approver
	_ apogee.ApprovalRequest
	_ apogee.ApprovalDecision
	_ apogee.Tool
	_ apogee.ExternalEffectTool
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
	_ = apogee.NewToolRegistry
	_ = apogee.NewMechanismRegistry
	_ = apogee.DecodeSession
)

// Re-exported consts and sentinel errors — one reference each.
var (
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
	_ = apogee.ErrSessionVersion
	_ = apogee.ErrInputPending
)

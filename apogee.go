// Package apogee is the public, embeddable surface of the Apogee coding agent.
//
// Apogee is a terminal coding agent for small local LLMs that owns the full
// agentic loop — build request, call the Upstream, parse the response, dispatch
// tools, apply Mechanisms — and ships as both a product (the cmd/apogee TUI/CLI)
// and this reusable library. The TUI, the optional `apogee headless` CLI, and the
// external bench (apogee-sim) are all consumers of this one package over the same
// engine. Everything not in this package (and its sibling public subpackages) is
// internal and carries no stability promise.
//
// Layout (ADR 0010): this package is a THIN FACADE. The public types, interfaces,
// enums, and sentinel errors live in internal/domain (the ubiquitous language as
// Go); the engine lives in internal/agent; the provider seam lives in
// internal/provider. The root re-exports the public surface as type aliases,
// re-exported consts/errors, and forwarding constructors, and holds no engine logic.
// The invariant "internal/* never imports root" makes the dependency graph flow
// strictly downward toward internal/domain. example_test.go is a compile-time
// completeness guard: it names the full public surface so a forgotten alias fails the
// build.
//
// It is grounded in:
//
//	ADR 0001  embeddable, steppable, no ambient state; snapshot/resume + hygiene
//	          (forking is the bench's, composed from these primitives — not exposed)
//	ADR 0002  Tools are an open extension point; the Mechanism catalogue is curated
//	ADR 0003  Mechanisms are a constraint-declared registry → deterministic total order
//	ADR 0004  Auto mode requires Confinement, reported as a capability matrix
//	ADR 0005  sub-agent privileges are always ≤ the parent's
//	ADR 0006  Bypass mode — the honest "Mechanisms-off" floor
//	ADR 0007  Step / Turn / quiescent boundary; cancellation; recover-at-boundary
//	ADR 0008  Tools are stateless across Turns; external effects are non-forkable
//	ADR 0010  package layout: a domain core, an engine, and this thin root facade
//
// Stability: v0.x, no stability promise through Phase 3; v1.0.0 is cut at the end
// of Phase 3. Events and hook points are additively extensible — a new variant is a
// minor bump (so consumers must treat the Event set and enums as open).
package apogee

import (
	"github.com/airiclenz/apogee/internal/agent"
	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Engine handle (internal/agent) — re-exported as an alias; methods are the surface
// ----------------------------------------------------------------------------

// Agent is a single embeddable Apogee agent instance — the engine handle. Its
// methods (Submit / Step / Run / Mode / Snapshot / Close) are the public stepping
// surface; construct one with New or Resume. See internal/agent for the contract.
type Agent = agent.Agent

// New constructs an Agent from cfg, validating the Auto/Confinement gate (ADR 0004)
// and the Mechanism ordering graph (ADR 0003) before returning a ready-to-Step Agent.
func New(cfg Config) (*Agent, error) { return agent.New(cfg) }

// Resume reconstructs an Agent from a prior Session snapshot; cfg re-supplies the
// live delegates and state roots, snap supplies the serializable conversation state.
func Resume(cfg Config, snap Session) (*Agent, error) { return agent.Resume(cfg, snap) }

// ----------------------------------------------------------------------------
// Construction & autonomy (internal/domain)
// ----------------------------------------------------------------------------

// Config is the full construction surface (Upstream target, autonomy, delegates,
// registries, injected state roots). See domain.Config for the field contract.
type Config = domain.Config

// ContextConfig governs the structural context reducers (Budget, Compaction).
type ContextConfig = domain.ContextConfig

// Mode is the autonomy level governing whether tool calls need human approval.
type Mode = domain.Mode

const (
	ModePlan      = domain.ModePlan
	ModeAskBefore = domain.ModeAskBefore
	ModeAuto      = domain.ModeAuto
)

// ----------------------------------------------------------------------------
// Stepping & Turns (internal/domain)
// ----------------------------------------------------------------------------

// UserInput is one user message into an Exchange (text plus optional file refs).
type UserInput = domain.UserInput

// StepResult reports the outcome of one Step at the quiescent boundary.
type StepResult = domain.StepResult

// StepStatus is the disposition of a completed Step (open set).
type StepStatus = domain.StepStatus

const (
	StatusTurnComplete     = domain.StatusTurnComplete
	StatusExchangeComplete = domain.StatusExchangeComplete
	StatusCancelled        = domain.StatusCancelled
)

// ----------------------------------------------------------------------------
// Events (internal/domain)
// ----------------------------------------------------------------------------

// EventSink receives typed Events as the loop produces them, including inside a Step.
type EventSink = domain.EventSink

// Event is the sealed sum type of everything the loop reports. The seal (an
// unexported method) lives in internal/domain and is intentionally not re-exported,
// so external code switches on the variants but cannot add new ones.
type Event = domain.Event

// The Event variants. The set is additively extensible (a new variant is a minor bump).
type (
	TokenEvent          = domain.TokenEvent
	MessageEvent        = domain.MessageEvent
	ToolCallEvent       = domain.ToolCallEvent
	ToolResultEvent     = domain.ToolResultEvent
	ApprovalEvent       = domain.ApprovalEvent
	MechanismFiredEvent = domain.MechanismFiredEvent
	ErrorEvent          = domain.ErrorEvent
)

// ----------------------------------------------------------------------------
// Approval (internal/domain)
// ----------------------------------------------------------------------------

// Approver is the host-supplied human-in-the-loop gate on a single tool call.
type Approver = domain.Approver

// ApprovalRequest describes the pending tool call the human is asked to allow.
type ApprovalRequest = domain.ApprovalRequest

// ApprovalDecision is the Approver's verdict.
type ApprovalDecision = domain.ApprovalDecision

const (
	ApprovalAllow           = domain.ApprovalAllow
	ApprovalDeny            = domain.ApprovalDeny
	ApprovalAllowForSession = domain.ApprovalAllowForSession
)

// ----------------------------------------------------------------------------
// Tools (internal/domain)
// ----------------------------------------------------------------------------

// Tool is the public, open extension point: embedders may register their own.
type Tool = domain.Tool

// ExternalEffectTool is an optional interface a Tool implements when it reaches
// state Apogee does not own (network, MCP).
type ExternalEffectTool = domain.ExternalEffectTool

// ReadOnlyTool is an optional interface a Tool implements to declare it performs no
// writes — the signal Plan mode and Ask-Before Approval gate on.
type ReadOnlyTool = domain.ReadOnlyTool

// IsReadOnly reports whether a Tool has declared itself read-only; an undeclared tool
// is treated as write-capable.
func IsReadOnly(t Tool) bool { return domain.IsReadOnly(t) }

// ExternalEffectKind classifies a non-forkable external effect.
type ExternalEffectKind = domain.ExternalEffectKind

const (
	EffectNetwork = domain.EffectNetwork
	EffectMCP     = domain.EffectMCP
)

// ToolCall is a parsed request from the model to run a tool.
type ToolCall = domain.ToolCall

// ToolResult is what a tool returns to the loop (pre tool-result-capping).
type ToolResult = domain.ToolResult

// ToolRegistry is the injectable set of available tools.
type ToolRegistry = domain.ToolRegistry

// NewToolRegistry returns an empty registry.
func NewToolRegistry() *ToolRegistry { return domain.NewToolRegistry() }

// ExternalEffects is the single injectable boundary for non-forkable external effects.
type ExternalEffects = domain.ExternalEffects

// ----------------------------------------------------------------------------
// Mechanisms & hook points (internal/domain)
// ----------------------------------------------------------------------------

// HookPoint is where in the loop a Mechanism fires.
type HookPoint = domain.HookPoint

const (
	HookPreRequest     = domain.HookPreRequest
	HookPostResponse   = domain.HookPostResponse
	HookPreToolExec    = domain.HookPreToolExec
	HookPostToolResult = domain.HookPostToolResult
	HookHistoryRewrite = domain.HookHistoryRewrite
)

// The five hook interfaces a Mechanism (or bench experimental hook) may implement.
type (
	PreRequestHook     = domain.PreRequestHook
	PostResponseHook   = domain.PostResponseHook
	PreToolExecHook    = domain.PreToolExecHook
	PostToolResultHook = domain.PostToolResultHook
	HistoryRewriter    = domain.HistoryRewriter
)

// PostResponseDecision is the action a post-response Mechanism chooses.
type PostResponseDecision = domain.PostResponseDecision

// PostResponseAction enumerates the post-response decisions.
type PostResponseAction = domain.PostResponseAction

const (
	ActionRetry     = domain.ActionRetry
	ActionIntercept = domain.ActionIntercept
	ActionDefer     = domain.ActionDefer
)

// Mechanism is a catalogued unit of gated, self-regulating behaviour.
type Mechanism = domain.Mechanism

// MechanismID is the canonical, stable identifier of a Mechanism.
type MechanismID = domain.MechanismID

// MechanismDescriptor is per-Mechanism metadata orthogonal to its hook point.
type MechanismDescriptor = domain.MechanismDescriptor

// Capability is what a Mechanism does — and what Bypass switches on.
type Capability = domain.Capability

const (
	CapOffRamp        = domain.CapOffRamp
	CapProactiveNudge = domain.CapProactiveNudge
	CapResponseRepair = domain.CapResponseRepair
)

// SuppressionPolicy is how a Mechanism participates in self-regulation.
type SuppressionPolicy = domain.SuppressionPolicy

const (
	SuppressStrikesThree = domain.SuppressStrikesThree
	SuppressExempt       = domain.SuppressExempt
)

// OrderingConstraints declares a Mechanism's position relative to others.
type OrderingConstraints = domain.OrderingConstraints

// MechanismRegistry is the injectable catalogue plus the bench's experimental slots.
type MechanismRegistry = domain.MechanismRegistry

// NewMechanismRegistry returns a registry seeded with the built-in catalogue.
func NewMechanismRegistry() *MechanismRegistry { return domain.NewMechanismRegistry() }

// ----------------------------------------------------------------------------
// Hook working values (internal/domain)
// ----------------------------------------------------------------------------

// Role is a conversation message's role.
type Role = domain.Role

const (
	RoleSystem    = domain.RoleSystem
	RoleUser      = domain.RoleUser
	RoleAssistant = domain.RoleAssistant
	RoleTool      = domain.RoleTool
)

// Message is a read-only snapshot of one conversation message handed to hooks.
type Message = domain.Message

// ToolDef is one entry of the tool menu the model sees.
type ToolDef = domain.ToolDef

// Budget is the read-only context-budget view a hook reads.
type Budget = domain.Budget

// LoopView is the read-only window every hook has onto loop state.
type LoopView = domain.LoopView

// ConversationView is read-only history with tool-call/result pairing helpers.
type ConversationView = domain.ConversationView

// Request is the outgoing Upstream request a pre-request hook may shape.
type Request = domain.Request

// SamplingParams are the optional sampling overrides a pre-request hook may set.
type SamplingParams = domain.SamplingParams

// Response is the model response a post-response hook inspects and may intercept.
type Response = domain.Response

// FinishReason is the model's stop reason (open set).
type FinishReason = domain.FinishReason

const (
	FinishStop      = domain.FinishStop
	FinishLength    = domain.FinishLength
	FinishToolCalls = domain.FinishToolCalls
)

// Conversation is the serializable conversation state a history-rewrite hook edits.
type Conversation = domain.Conversation

// ----------------------------------------------------------------------------
// Confinement (internal/domain; backends in internal/platform)
// ----------------------------------------------------------------------------

// Confiner is the OS-level confinement facility required for Auto mode (ADR 0004).
// The interface is public (the host injects it via Config); the backends live in
// internal/platform.
type Confiner = domain.Confiner

// ConfinementCaps is the capability matrix a Confiner reports.
type ConfinementCaps = domain.ConfinementCaps

// ConfinementBox is the confinement policy for a run.
type ConfinementBox = domain.ConfinementBox

// ----------------------------------------------------------------------------
// Sessions (internal/domain)
// ----------------------------------------------------------------------------

// Session is the serializable, copyable conversation state (no live handles).
type Session = domain.Session

// DecodeSession deserializes a session, returning ErrSessionVersion if the schema
// version is newer than this build understands.
func DecodeSession(data []byte) (Session, error) { return domain.DecodeSession(data) }

// ----------------------------------------------------------------------------
// Sentinel errors (internal/domain)
// ----------------------------------------------------------------------------

var (
	// ErrAutoUnavailable is returned by New when Mode==Auto but the Confiner cannot
	// satisfy the Auto gate (missing or insufficient capabilities).
	ErrAutoUnavailable = domain.ErrAutoUnavailable

	// ErrOrderingCycle is returned by New / registry Add when Mechanism ordering
	// constraints form a cycle.
	ErrOrderingCycle = domain.ErrOrderingCycle

	// ErrSessionVersion is returned by Resume / DecodeSession for a snapshot whose
	// schema version this build does not understand.
	ErrSessionVersion = domain.ErrSessionVersion

	// ErrInputPending is returned by Submit when an Exchange is already in progress.
	ErrInputPending = domain.ErrInputPending

	// ErrDuplicateTool is returned by ToolRegistry.Register on a duplicate tool name.
	ErrDuplicateTool = domain.ErrDuplicateTool

	// ErrInvalidTool is returned by ToolRegistry.Register for an unaddressable tool
	// (currently an empty Name).
	ErrInvalidTool = domain.ErrInvalidTool
)

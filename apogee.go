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
// This file is a SIGNATURE SKETCH for Phase 0 review — the keystone seam the plan
// names "get right first". Bodies are stubs; the shipped facade is a thin wrapper
// over internal/ (mirroring apogee-sim's apogee.go). It is grounded in:
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
//
// Stability: v0.x, no stability promise through Phase 3; v1.0.0 is cut at the end
// of Phase 3. Events and hook points are additively extensible — a new variant is a
// minor bump (so consumers must treat the Event set and enums as open).
package apogee

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/airiclenz/apogee/internal/agent"
)

// ----------------------------------------------------------------------------
// Construction & lifecycle (ADR 0001)
// ----------------------------------------------------------------------------

// Agent is a single embeddable Apogee agent instance. It owns the loop,
// conversation state, tool dispatch, and Mechanism application. It holds no
// process-global state: every state root is injected through Config, so many
// Agents can run in one process against isolated directories (the property the
// bench relies on for isolation — ADR 0001).
//
// An Agent is not safe for concurrent use by multiple goroutines; drive one Agent
// from one goroutine (Step/Run), and observe it from another only via its EventSink.
type Agent struct {
	cfg      Config
	upstream agent.Responder    // provider seam (Decision C): fake in P0.6, real HTTP in Phase 1
	registry *MechanismRegistry // catalogued + experimental hooks driving the loop

	conv         conversation // serializable conversation state (ADR 0001)
	pendingInput *UserInput   // queued by Submit, consumed by the next Step
	inExchange   bool         // true between Submit and the Step that completes the Exchange
	turnIndex    int          // 0-based index of the next Turn
}

// New constructs an Agent from cfg. It validates the configuration — including the
// Auto-mode/Confinement gate (ADR 0004) and the Mechanism ordering graph (ADR 0003,
// a constraint cycle is a startup error) — and returns an error rather than
// silently degrading a misconfigured surface.
func New(cfg Config) (*Agent, error) { return newAgent(cfg, placeholderResponder{}) }

// Resume reconstructs an Agent from a prior Session snapshot. Config supplies the
// live delegates (Approver, Confiner, EventSink) and state roots again — only the
// serializable conversation state comes from snap. External connections (MCP,
// network) reconnect fresh; no server-side state is restored (ADR 0008).
func Resume(cfg Config, snap Session) (*Agent, error) {
	return resumeAgent(cfg, snap, placeholderResponder{})
}

// Close releases the Agent's resources. Because tools are stateless across Turns
// (ADR 0008), there is no live tool state to flush — Close tears down the provider
// client, any MCP connections, and the log sink. The Phase-0 slice holds no such live
// resources (the responder is in-process and hermetic), so Close is a no-op today; it
// exists now so embedders write the correct lifecycle before Phase 1 adds real teardown.
func (a *Agent) Close() error { return nil }

// Config is the full construction surface. It carries the Upstream target, the
// autonomy posture, the host-supplied delegates, the extension registries, and the
// injected state roots. A zero Config is not valid; Endpoint, Model, and Events are
// the minimum. A struct (not functional options) because every field is a
// deliberate, reviewable seam and ADR 0001 speaks of state "injected via Config".
type Config struct {
	// Upstream — the local OpenAI-compatible LLM server (CONTEXT: Upstream).
	Endpoint string
	Model    string

	// Autonomy.
	Mode   Mode // Plan / Ask-Before / Auto
	Bypass bool // ADR 0006: Mechanisms off, structure on (the hard-constraint floor)

	// Host-supplied delegates. The host (TUI / bench / embedder) owns these.
	Approver Approver  // the human-in-the-loop gate; required unless Mode==Plan
	Confiner Confiner  // nil ⇒ no confinement ⇒ Auto is refused (ADR 0004)
	Events   EventSink // where typed Events are pushed; required

	// Extension points. nil ⇒ the built-in defaults.
	Tools      *ToolRegistry      // open extension point (ADR 0002)
	Mechanisms *MechanismRegistry // curated catalogue + bench experimental hooks (ADR 0002/0003)

	// Injected state roots — no implicit ~/.apogee (ADR 0001). The bench points
	// these at ephemeral dirs so sim runs never touch the production Library.
	LibraryDir  string
	SessionsDir string
	ConfigDir   string

	// ExternalEffects is the single injectable boundary for non-forkable effects
	// (network, MCP). nil ⇒ live. The bench injects a deterministic stub for v1;
	// record/replay slots in behind the same interface later (ADR 0008).
	ExternalEffects ExternalEffects

	// Budget / Compaction knobs (context/) are structural and load-bearing — they
	// run even under Bypass. Defaults are sane; overrides are advanced.
	Context ContextConfig
}

// ContextConfig governs the structural context reducers — Budget and Compaction —
// which are NOT Mechanisms and stay on under Bypass (CONTEXT: Budget, Compaction).
type ContextConfig struct {
	MaxContextTokens  int // 0 ⇒ discover from the model
	ResponseReserve   int
	CompactionEnabled bool // generative summarisation; default true
}

// Mode is the autonomy level governing whether tool calls need human approval
// (CONTEXT: Agent mode). It is orthogonal to Config.Bypass.
type Mode string

const (
	// ModePlan is read-only: no writes, no command execution.
	ModePlan Mode = "plan"
	// ModeAskBefore requires an Approval for every tool call.
	ModeAskBefore Mode = "ask-before"
	// ModeAuto runs tool calls without per-call approval. Requires Confinement
	// (ADR 0004); a tool that cannot be confined still gates through Approval.
	ModeAuto Mode = "auto"
)

// ----------------------------------------------------------------------------
// Stepping & Turns (ADR 0007)
// ----------------------------------------------------------------------------

// Submit enqueues user input to begin (or continue) an Exchange. It does not run
// the loop; the next Step/Run consumes it. Submitting mid-Exchange is an error.
func (a *Agent) Submit(in UserInput) error {
	if a.pendingInput != nil || a.inExchange {
		return ErrInputPending
	}
	a.pendingInput = &in
	return nil
}

// Step advances the loop exactly one Turn and returns at a quiescent boundary — no
// in-flight stream, no in-flight tool call, conversation state fully serializable
// (ADR 0007). Streaming tokens and Approval prompts happen *inside* a Step (via the
// EventSink and Approver). Snapshot and Resume are valid only at the boundary Step
// returns at.
//
// Cancellation: cancelling ctx abandons the in-flight Upstream call or tool and
// returns at the next quiescent boundary with StepResult.Status == StatusCancelled
// and conversation state left serializable — never half-streamed (ADR 0007).
//
// Recovery: a panic in a tool or Mechanism is caught at that extension boundary,
// converted to an ErrorEvent, and the loop degrades to the quiescent boundary
// rather than unwinding into the host (ADR 0007 / ADR 0002). Step returns a non-nil
// error only for loop-level faults the Agent itself cannot localise.
func (a *Agent) Step(ctx context.Context) (StepResult, error) { return a.step(ctx) }

// Run steps the loop until the Exchange completes (a final no-tool response),
// cancellation, or a loop-level error — a convenience wrapper over Step for hosts
// that do not need Turn-level control. The bench drives Step directly.
func (a *Agent) Run(ctx context.Context) (StepResult, error) { panic("sketch: not implemented") }

// Mode reports the Agent's current Agent mode.
func (a *Agent) Mode() Mode { return a.cfg.Mode }

// UserInput is one user message into an Exchange: free text plus optional file
// references the context builder resolves. Stays a value (no live handles) so it
// snapshots cleanly.
type UserInput struct {
	Text     string
	FileRefs []string
}

// StepResult reports the outcome of one Step at the quiescent boundary.
type StepResult struct {
	Status    StepStatus
	TurnIndex int           // 0-based index of the Turn just completed
	Elapsed   time.Duration // wall time for this Turn
}

// StepStatus is the disposition of a completed Step. The set is open (additively
// extensible — treat unknown values defensively).
type StepStatus string

const (
	// StatusTurnComplete: the Turn finished and more Turns are pending (the model
	// requested tools; the loop will continue on the next Step).
	StatusTurnComplete StepStatus = "turn-complete"
	// StatusExchangeComplete: the model produced a final no-tool response; the
	// Agent now awaits the next Submit.
	StatusExchangeComplete StepStatus = "exchange-complete"
	// StatusCancelled: ctx was cancelled; state is serializable, resume is valid.
	StatusCancelled StepStatus = "cancelled"
)

// ----------------------------------------------------------------------------
// Events (ADR 0001 — consumed as Go values in-process)
// ----------------------------------------------------------------------------

// EventSink receives typed Events as the loop produces them, including *inside* a
// Step (streaming). The TUI adapts these to Bubble Tea messages; the bench consumes
// them as Go values. Emit must not block the loop for long — fan out if needed.
type EventSink interface {
	Emit(Event)
}

// Event is the sealed sum type of everything the loop reports. It is sealed (an
// unexported marker) so the variant set stays owned by Apogee and additively
// versioned; external code switches on the concrete types but cannot add variants.
type Event interface {
	eventDepth() int // sealing marker; also carries sub-agent nesting depth
}

// eventBase is embedded in every Event variant. Depth is the sub-agent nesting
// level (0 = top-level agent); a sub-agent's events nest into the parent's stream
// with Depth > 0 (ADR 0005). Turn is the Turn index the event belongs to.
type eventBase struct {
	Depth int
	Turn  int
}

func (b eventBase) eventDepth() int { return b.Depth }

// TokenEvent is one streamed chunk of assistant text.
type TokenEvent struct {
	eventBase
	Text string
}

// MessageEvent is a completed assistant message (the no-tool turn ends an Exchange).
type MessageEvent struct {
	eventBase
	Text string
}

// ToolCallEvent reports that the model requested a tool call (post-parse).
type ToolCallEvent struct {
	eventBase
	Call ToolCall
}

// ToolResultEvent reports a tool's result after execution (and after any
// post-tool-result Mechanisms have acted on it).
type ToolResultEvent struct {
	eventBase
	Result ToolResult
}

// ApprovalEvent reports that an Approval was requested/decided for a tool call.
// (The decision is obtained synchronously via the Approver; this event is for
// observers — TUI display, bench accounting.)
type ApprovalEvent struct {
	eventBase
	Request  ApprovalRequest
	Decision ApprovalDecision
}

// MechanismFiredEvent reports that a Mechanism (or experimental hook) fired at a
// hook point — the observability spine for self-regulation and bench attribution.
type MechanismFiredEvent struct {
	eventBase
	Mechanism MechanismID
	Hook      HookPoint
	Action    string // e.g. the PostResponseDecision taken, or "suppressed"
}

// ErrorEvent reports a localised, recovered fault — a tool or Mechanism panic
// caught at the extension boundary, or a tool execution error (ADR 0007). It does
// not imply the loop stopped.
type ErrorEvent struct {
	eventBase
	Source string // tool name / mechanism ID / "loop"
	Err    string
}

// ----------------------------------------------------------------------------
// Approval (CONTEXT: Approval; ADR 0004 — MCP gates through Approval even in Auto)
// ----------------------------------------------------------------------------

// Approver is the host-supplied human-in-the-loop gate on a single tool call. In
// Ask-Before mode it is consulted for every call; in Auto mode it is consulted only
// for tools that cannot be confined (e.g. MCP — ADR 0004). It is called
// synchronously inside a Step and may block on the human; cancelling ctx unblocks it.
type Approver interface {
	Approve(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error)
}

// ApprovalRequest describes the pending tool call the human is asked to allow.
type ApprovalRequest struct {
	Tool      string
	Arguments json.RawMessage
	Reason    string // why approval is required (e.g. "write", "unconfinable MCP tool")
}

// ApprovalDecision is the Approver's verdict.
type ApprovalDecision string

const (
	ApprovalAllow           ApprovalDecision = "allow"
	ApprovalDeny            ApprovalDecision = "deny"
	ApprovalAllowForSession ApprovalDecision = "allow-for-session"
)

// ----------------------------------------------------------------------------
// Tools (ADR 0002 open extension point; ADR 0008 stateless across Turns)
// ----------------------------------------------------------------------------

// Tool is the public, open extension point: embedders may register their own.
//
// Contract — stateless across Turns (ADR 0008): a tool's only durable side effect
// is filesystem writes; nothing live (process, REPL, socket, cursor) survives the
// quiescent boundary. terminal and python-exec are one-shot (fresh process per
// call). A tool needing persistence must serialize it into conversation state, not
// hold it live — this is what makes snapshot/resume and the bench's fork coherent.
type Tool interface {
	// Name is the stable identifier the model calls and the registry keys on.
	Name() string
	// Description and Schema are presented to the model (the JSON-schema of args).
	Description() string
	Schema() json.RawMessage
	// Execute runs the call. It must honour ctx cancellation (ADR 0007) and the
	// statelessness contract above. A panic here is caught at the loop's extension
	// boundary and surfaced as an ErrorEvent.
	Execute(ctx context.Context, call ToolCall) (ToolResult, error)
}

// ExternalEffectTool is an optional interface a Tool implements when it reaches
// state Apogee does not own (network, MCP). The loop routes these through
// Config.ExternalEffects so the bench can stub them deterministically (ADR 0008),
// and the Confiner/Approval gate treats them as unconfinable in Auto (ADR 0004).
type ExternalEffectTool interface {
	Tool
	ExternalEffect() ExternalEffectKind
}

// ExternalEffectKind classifies a non-forkable external effect.
type ExternalEffectKind string

const (
	EffectNetwork ExternalEffectKind = "network"
	EffectMCP     ExternalEffectKind = "mcp"
)

// ToolCall is a parsed request from the model to run a tool.
type ToolCall struct {
	ID        string
	Tool      string
	Arguments json.RawMessage
}

// ToolResult is what a tool returns to the loop (pre tool-result-capping).
type ToolResult struct {
	CallID  string
	Content string
	IsError bool
}

// ToolRegistry is the injectable set of available tools (ADR 0001 — injectable, no
// globals). A sub-agent receives a subset of the parent's registry, never a
// superset (ADR 0005).
type ToolRegistry struct {
	// unexported map[string]Tool
}

// NewToolRegistry returns an empty registry.
func NewToolRegistry() *ToolRegistry { panic("sketch: not implemented") }

// Register adds a tool, returning an error on a duplicate name.
func (r *ToolRegistry) Register(t Tool) error { panic("sketch: not implemented") }

// Subset returns a new registry containing only the named tools — the primitive a
// caller uses to narrow a sub-agent's tools (ADR 0005).
func (r *ToolRegistry) Subset(names ...string) *ToolRegistry { panic("sketch: not implemented") }

// ExternalEffects is the single injectable boundary for non-forkable external
// effects (ADR 0008). Production uses a live implementation; the bench injects a
// deterministic stub (network-unreachable / empty-MCP) without touching tool code.
type ExternalEffects interface {
	Do(ctx context.Context, call ToolCall) (ToolResult, error)
}

// ----------------------------------------------------------------------------
// Mechanisms & hook points (ADR 0003 registry; ADR 0002 curated)
// ----------------------------------------------------------------------------

// HookPoint is where in the loop a Mechanism fires — the primary classification
// (CONTEXT: Hook point). The set is fixed by the loop's structure.
type HookPoint string

const (
	HookPreRequest     HookPoint = "pre-request"      // shape the outgoing request
	HookPostResponse   HookPoint = "post-response"    // inspect response, choose an action
	HookPreToolExec    HookPoint = "pre-tool-exec"    // between decision-to-run and execution
	HookPostToolResult HookPoint = "post-tool-result" // act on a result before the model sees it
	HookHistoryRewrite HookPoint = "history-rewrite"  // edit conversation state (may attach widely)
)

// The five hook interfaces. A Mechanism (or a bench experimental hook) implements
// one or more of these in addition to — for a catalogued Mechanism — Mechanism.
// Hooks are public so the bench can register experimental hooks (ADR 0002); an
// embedder technically can too, but without a descriptor it does not join
// self-regulation and carries no stability promise.

// PreRequestHook shapes the outgoing request before it is sent. It reads the
// conversation, tool menu, and budget through req.View() and mutates the request in
// place (the request being built is the conversation as it will be sent).
type PreRequestHook interface {
	PreRequest(ctx context.Context, req *Request) error
}

// PostResponseHook inspects the model response (resp.View() for history and the tool
// menu) and chooses an action — mutating resp in place for ActionIntercept, or
// returning ActionRetry / ActionDefer.
type PostResponseHook interface {
	PostResponse(ctx context.Context, resp *Response) (PostResponseDecision, error)
}

// PreToolExecHook acts between the decision to run a tool and its execution. It
// receives the loop view because the decision is usually cross-Turn (e.g.
// short-circuiting a re-read of a file already read earlier needs the read history).
type PreToolExecHook interface {
	PreToolExec(ctx context.Context, call *ToolCall, view LoopView) error
}

// PostToolResultHook acts on a tool result before the model next sees it — the home
// of correct_tool_result, new to the loop (the proxy could not host it). It receives
// the originating call (the tool name and arguments live there, not on the result)
// and the loop view (error handling often counts prior failures across Turns).
type PostToolResultHook interface {
	PostToolResult(ctx context.Context, call ToolCall, result *ToolResult, view LoopView) error
}

// HistoryRewriter edits conversation state — the home of truncate_history. A
// capability that may attach at more than one point (CONTEXT: Hook point). The
// Conversation is itself the history, so this hook reads and mutates it directly.
type HistoryRewriter interface {
	RewriteHistory(ctx context.Context, conv *Conversation) error
}

// PostResponseDecision is the action a post-response Mechanism chooses (CONTEXT:
// Post-response decision). ActionIntercept is expressed by mutating the *Response in
// place (SetText / SetToolCallArguments) and carries no payload. ActionDefer carries
// the role-safe correction text to inject into the *next* request — the
// streaming-safe feed-forward path — held in conversation state as a Deferred
// Response Action so it survives a snapshot boundary. ActionRetry carries nothing.
type PostResponseDecision struct {
	Action PostResponseAction
	// Inject is the correction text injected into the next request when Action ==
	// ActionDefer (role-safe, like Request.InjectContext). Empty otherwise.
	Inject string
}

// PostResponseAction enumerates the post-response decisions.
type PostResponseAction string

const (
	ActionRetry     PostResponseAction = "retry"     // re-call the Upstream now
	ActionIntercept PostResponseAction = "intercept" // alter the response before the loop acts
	ActionDefer     PostResponseAction = "defer"     // schedule a correction into the next request
)

// Mechanism is a catalogued unit of gated, self-regulating behaviour (CONTEXT:
// Mechanism). It supplies a descriptor and ordering constraints, and implements at
// least one hook interface above; the registry type-asserts which. A hook without a
// descriptor is an experimental hook (no self-regulation — ADR 0002).
type Mechanism interface {
	Descriptor() MechanismDescriptor
	Ordering() OrderingConstraints
}

// MechanismID is the canonical, stable identifier of a Mechanism — also the stable
// tiebreak in the deterministic total order (ADR 0003).
type MechanismID string

// MechanismDescriptor is per-Mechanism metadata orthogonal to its hook point
// (CONTEXT: Mechanism descriptor). The single source of truth for what Bypass turns
// off (by Capability) and what may co-fire (IncompatibleWith).
type MechanismDescriptor struct {
	ID          MechanismID
	Capability  Capability
	Suppression SuppressionPolicy
	// IncompatibleWith constrains stacking — Mechanisms that must not co-fire.
	IncompatibleWith []MechanismID
}

// Capability is what a Mechanism does — and what Bypass switches on (ADR 0006:
// Bypass disables proactive-nudge + response-repair, keeps off-ramp).
type Capability string

const (
	CapOffRamp        Capability = "off-ramp"        // exempt recovery guarantee; survives Bypass
	CapProactiveNudge Capability = "proactive-nudge" // disabled under Bypass
	CapResponseRepair Capability = "response-repair" // disabled under Bypass
)

// SuppressionPolicy is how a Mechanism participates in self-regulation (CONTEXT:
// Adaptive Suppression, Off-ramp). Exempt off-ramps still earn their place by their
// own leave-one-out A/B (ADR 0006 / ADR 0009) — exempt-from-suppression is not
// exempt-from-validation.
type SuppressionPolicy string

const (
	SuppressStrikesThree SuppressionPolicy = "strikes-3" // suppressed after N non-helpful fires
	SuppressExempt       SuppressionPolicy = "exempt"    // never suppressed (off-ramps)
)

// OrderingConstraints declares a Mechanism's position relative to others at its hook
// point (ADR 0003 — seeded from apogee-sim's type, now owned here). The loop builds
// a deterministic total order by topological sort with a stable tiebreak by
// MechanismID; a constraint cycle is a startup error (ErrOrderingCycle).
type OrderingConstraints struct {
	Before []MechanismID
	After  []MechanismID
}

// MechanismRegistry is the injectable catalogue plus the bench's experimental-hook
// slots (ADR 0002/0003). The built-in catalogue is curated; Add is how internal
// Mechanisms join, AddExperimental is how the bench registers a candidate hook.
type MechanismRegistry struct {
	mechanisms   []Mechanism         // catalogued Mechanisms registered via Add
	experimental map[HookPoint][]any // bench experimental hooks registered via AddExperimental
}

// NewMechanismRegistry returns a registry seeded with the built-in catalogue. The
// Phase-0 catalogue is empty — the curated Mechanisms land with the catalogue→hook
// mapping session (Phase 4); P0.6 needs only the experimental-hook slots.
func NewMechanismRegistry() *MechanismRegistry {
	return &MechanismRegistry{experimental: make(map[HookPoint][]any)}
}

// Add registers a catalogued Mechanism. It returns an error if the Mechanism
// implements no hook interface. (The constraint-cycle check is performed by New over
// the whole graph — a startup gate, ADR 0003 — so a registry under construction can
// hold constraints that only close a cycle once every Mechanism is present.)
func (r *MechanismRegistry) Add(m Mechanism) error {
	if !implementsAnyHook(m) {
		return fmt.Errorf("apogee: mechanism %q: %w", m.Descriptor().ID, errNoHookInterface)
	}
	r.mechanisms = append(r.mechanisms, m)
	return nil
}

// AddExperimental registers a bench experimental hook at a hook point — a behaviour
// that is not (yet) a Mechanism (CONTEXT: Experimental hook). It runs but does not
// join self-regulation. hook must implement the interface for at.
func (r *MechanismRegistry) AddExperimental(at HookPoint, hook any) error {
	if !hookImplements(at, hook) {
		return fmt.Errorf("apogee: hook does not implement the interface for hook point %q", at)
	}
	if r.experimental == nil {
		r.experimental = make(map[HookPoint][]any)
	}
	r.experimental[at] = append(r.experimental[at], hook)
	return nil
}

// ----------------------------------------------------------------------------
// Hook working values & shared substrate (docs/design/hook-mutation-api.md)
// ----------------------------------------------------------------------------
//
// Request, Response, and Conversation are the loop's working values exposed to
// hooks. They stay opaque structs with method-only surfaces so the internal
// representation and the variant set remain Apogee-owned and additively versioned
// (ADR 0001): a hook reads Message snapshots and mutates by index against the owning
// container, never touching the backing storage. The operation set is scoped from
// apogee-sim's real Transform / Injector / Intervention footprint — not speculation
// (TDD §6.2).

// Role is a conversation message's role.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is a read-only snapshot of one conversation message handed to hooks. A hook
// reads Messages and mutates by index against the owning container (Request /
// Conversation); it never holds the loop's backing storage.
type Message struct {
	Role       Role
	Content    string
	ToolCalls  []ToolCall // RoleAssistant only
	ToolCallID string     // RoleTool only — links the result to its ToolCall.ID
}

// Extra reports a preserved unknown wire field on the message (reasoning_content,
// tool_choice, thinking, …). Round-trip preservation of these is load-bearing for
// snapshot/resume and the bench's fork, so they survive a history rewrite.
func (m Message) Extra(key string) (json.RawMessage, bool) { panic("sketch: not implemented") }

// ToolDef is one entry of the tool menu the model sees.
type ToolDef struct {
	Name        string
	Description string
	Schema      json.RawMessage // JSON-schema of arguments
}

// Budget is the read-only context-budget view a hook reads to gate token-sensitive
// behaviour (e.g. Library injection backs off as the window fills).
type Budget struct {
	ContextLimit  int
	Used          int     // estimated tokens used so far
	CharsPerToken float64 // model-specific estimate
}

// LoopView is the read-only window every hook has onto loop state beyond its own
// mutable value — the conversation so far, the tool menu, the budget, the Turn index,
// and a self-regulation query. It is the home of all cross-Turn reads: most
// Mechanisms decide by aggregating across Turns, so the primary mutable value (a
// *Response, *ToolCall, *ToolResult) is never sufficient alone. Request and Response
// expose it via their View method; the tool-stage hooks receive it as an argument.
type LoopView interface {
	Conversation() ConversationView
	Tools() []ToolDef
	Budget() Budget
	Turn() int
	// Fired reports how many times a Mechanism has fired this Session — the seam for
	// cross-Mechanism coupling (e.g. decompose muting itself once a read-loop
	// Mechanism has fired) without a shared mutable meta map.
	Fired(id MechanismID) int
}

// ConversationView is read-only history with the tool-call/result pairing helpers
// every history-inspecting Mechanism needs: the tool name and arguments live only on
// the originating ToolCall, never on the tool-result message, so resolving a result
// back to its call is mandatory for error-handling Mechanisms.
type ConversationView interface {
	Len() int
	At(i int) Message
	Range(fn func(i int, m Message) bool)
	// LastUser returns the most recent user message and its index.
	LastUser() (msg Message, index int, ok bool)
	// CallByID resolves a tool result to its originating call (for the name/args).
	CallByID(id string) (call ToolCall, index int, ok bool)
	// ResultFor resolves a tool call to its result message.
	ResultFor(callID string) (msg Message, index int, ok bool)
}

// Request is the outgoing Upstream request a pre-request hook may shape. Reads go
// through View; mutations are the characterised operation set from apogee-sim's
// pre-request Mechanisms.
type Request struct {
	// unexported: messages, tools menu, budgeted context, params, extras
}

// View exposes the read-only conversation/tools/budget window.
func (r *Request) View() LoopView { panic("sketch: not implemented") }

// Model is the target model id (the Library keys its lookup on this).
func (r *Request) Model() string { panic("sketch: not implemented") }

// Extra reports a preserved unknown request field (e.g. a grammar Mechanism checks
// for an existing response_format before setting one).
func (r *Request) Extra(key string) (json.RawMessage, bool) { panic("sketch: not implemented") }

// AppendToSystem appends text to the first system message (creating one if absent),
// but is a no-op if marker already occurs there — the idempotent inject the nudge
// Mechanisms (library, cot, decompose) share. Reports whether it injected.
func (r *Request) AppendToSystem(marker, text string) (injected bool) {
	panic("sketch: not implemented")
}

// InjectContext inserts a user message at the role-safe position: before the last
// user message, or appended to the system prompt if the conversation ends in a tool
// result (a user message after a tool result breaks strict chat templates).
func (r *Request) InjectContext(text string) { panic("sketch: not implemented") }

// SetMessageContent edits one message's content in place by index — tool-result
// capping and history-collapse of older messages. An out-of-range index is a no-op.
func (r *Request) SetMessageContent(index int, content string) { panic("sketch: not implemented") }

// SetTools replaces and reorders the tool menu (the tool-filter Mechanism).
func (r *Request) SetTools(tools []ToolDef) { panic("sketch: not implemented") }

// SetExtra sets an unknown request field, allocating the carrier if needed (e.g. a
// grammar constraint sets response_format).
func (r *Request) SetExtra(key string, v json.RawMessage) { panic("sketch: not implemented") }

// SetSampling overrides sampling parameters. Forward-looking — no current Mechanism
// mutates these; included so the surface need not change to add one.
func (r *Request) SetSampling(p SamplingParams) { panic("sketch: not implemented") }

// SamplingParams are the optional sampling overrides a pre-request hook may set; a
// nil field leaves the loop's value untouched.
type SamplingParams struct {
	Temperature *float64
	MaxTokens   *int
}

// Response is the model response a post-response hook inspects and may intercept.
type Response struct {
	// unexported: raw text, parsed tool calls, finish reason, thinking channel
}

// View exposes the read-only conversation/tools/budget window — response-repair
// Mechanisms validate tool calls against the menu; loop detection reads history.
func (r *Response) View() LoopView { panic("sketch: not implemented") }

// Text is the assistant's raw text content.
func (r *Response) Text() string { panic("sketch: not implemented") }

// ToolCalls are the parsed tool calls the model requested.
func (r *Response) ToolCalls() []ToolCall { panic("sketch: not implemented") }

// FinishReason is the model's stop reason.
func (r *Response) FinishReason() FinishReason { panic("sketch: not implemented") }

// Thinking is the harmony/thinking channel content when the model and parser expose
// it (ok == false when there is none).
func (r *Response) Thinking() (text string, ok bool) { panic("sketch: not implemented") }

// SetText replaces the assistant text — the intercept path (ActionIntercept).
func (r *Response) SetText(s string) { panic("sketch: not implemented") }

// SetToolCallArguments rewrites one tool call's arguments in place — the auto-fix
// Mechanism writing back repaired/formatted content (ActionIntercept).
func (r *Response) SetToolCallArguments(index int, args json.RawMessage) {
	panic("sketch: not implemented")
}

// FinishReason is the model's stop reason; the set is open (treat unknown values
// defensively).
type FinishReason string

const (
	FinishStop      FinishReason = "stop"
	FinishLength    FinishReason = "length"
	FinishToolCalls FinishReason = "tool_calls"
)

// Conversation is the serializable conversation state a history-rewrite hook edits.
// It is a cleanly copyable value with no live handles (ADR 0001) — what lets the
// bench fork by deep-copying it and the user resume from a snapshot. Summaries are
// not a separate structure: they are ordinary messages produced by generative
// Compaction (context/) and written back via Replace. A deferred Response Action
// (ActionDefer) is held here so it survives a snapshot/resume boundary.
type Conversation struct {
	// unexported: messages, deferred response actions
}

// Len reports the number of messages.
func (c *Conversation) Len() int { panic("sketch: not implemented") }

// At returns the message at index i.
func (c *Conversation) At(i int) Message { panic("sketch: not implemented") }

// Range iterates messages until fn returns false.
func (c *Conversation) Range(fn func(i int, m Message) bool) { panic("sketch: not implemented") }

// PrefixEnd is the index past the leading system messages and the first user message
// — the protected prefix a truncation must keep.
func (c *Conversation) PrefixEnd() int { panic("sketch: not implemented") }

// AssistantBoundaries are the indices of assistant messages — the only safe cut
// points, because a tool result must stay adjacent to the assistant call that
// produced it (strict chat templates).
func (c *Conversation) AssistantBoundaries() []int { panic("sketch: not implemented") }

// SetMessageContent edits one message's content in place by index.
func (c *Conversation) SetMessageContent(i int, content string) { panic("sketch: not implemented") }

// DropRange drops messages in [start, end) — history truncation drops the middle,
// keeping the prefix and a recent tail.
func (c *Conversation) DropRange(start, end int) { panic("sketch: not implemented") }

// Insert places a message at index i — e.g. a static gap note at a truncation cut.
func (c *Conversation) Insert(i int, m Message) { panic("sketch: not implemented") }

// Replace swaps the entire message list — generative Compaction writes its
// summarised history back through here.
func (c *Conversation) Replace(msgs []Message) { panic("sketch: not implemented") }

// ----------------------------------------------------------------------------
// Confinement (ADR 0004 — capability matrix; interface is public, backends internal)
// ----------------------------------------------------------------------------

// Confiner is the OS-level confinement facility required for Auto mode (ADR 0004).
// The interface is PUBLIC because the host injects it via Config; the backends
// (seatbelt / landlock / AppContainer) live in internal/platform. Capabilities
// reports the matrix the Auto gate reads — Auto requires both fs-write and
// network-egress confinement, evaluated per tool.
type Confiner interface {
	// Capabilities reports what this backend can actually enforce, here and now
	// (e.g. Linux landlock < ABI v4 reports NetworkEgress == false).
	Capabilities() ConfinementCaps
	// Confine runs fn under the confinement box. A tool the backend cannot confine
	// (ExternalEffect / MCP) must not be passed here in Auto — it gates through
	// Approval instead (ADR 0004).
	Confine(ctx context.Context, box ConfinementBox, fn func(context.Context) error) error
}

// ConfinementCaps is the capability matrix a Confiner reports (ADR 0004 — extensible
// beyond these two).
type ConfinementCaps struct {
	FSWrite       bool
	NetworkEgress bool
}

// AutoEligible reports whether these capabilities satisfy the Auto gate — both
// fs-write and network-egress confinement (ADR 0004). There is no escape hatch.
func (c ConfinementCaps) AutoEligible() bool { return c.FSWrite && c.NetworkEgress }

// ConfinementBox is the confinement policy for a run. Default = workspace-write-only
// + network default-deny + per-project allowlist (ADR 0004).
type ConfinementBox struct {
	WorkspaceRoot string
	WritablePaths []string
	NetworkAllow  []string // per-project allowlist; empty = default-deny
}

// ----------------------------------------------------------------------------
// Sessions (ADR 0001 — snapshot/resume is the user feature; the bench composes fork)
// ----------------------------------------------------------------------------

// Snapshot captures the Agent's conversation state at the current quiescent
// boundary as a copyable, serializable value (ADR 0001/0007). It is valid only at a
// boundary (between Steps). Apogee exposes snapshot/resume; it exposes no fork — the
// bench composes forking by deep-copying a Session and the sandbox directory.
func (a *Agent) Snapshot() (Session, error) {
	state, err := encodeConversation(a.conv)
	if err != nil {
		return Session{}, err
	}
	return Session{Version: sessionVersion, State: state}, nil
}

// Session is the serializable, copyable conversation state — no live handles, no
// process globals (ADR 0001). Deep-copying it yields an independent branch (the
// bench's fork primitive); Encode/Decode persist it (the user's resume feature).
type Session struct {
	Version int             // schema version; Resume rejects an unknown future version
	State   json.RawMessage // opaque serialized conversation state
}

// Encode serializes the session for storage.
func (s Session) Encode() ([]byte, error) { return json.Marshal(s) }

// DecodeSession deserializes a session, returning ErrSessionVersion if the schema
// version is newer than this build understands.
func DecodeSession(data []byte) (Session, error) {
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return Session{}, fmt.Errorf("apogee: decode session: %w", err)
	}
	if s.Version > sessionVersion {
		return Session{}, ErrSessionVersion
	}
	return s, nil
}

// ----------------------------------------------------------------------------
// Sentinel errors
// ----------------------------------------------------------------------------

var (
	// ErrAutoUnavailable is returned by New when Mode==Auto but the Confiner cannot
	// satisfy the Auto gate (missing, or insufficient capabilities — e.g. Linux
	// kernel < 6.7). Auto degrades to Ask-Before; it never runs unconfined (ADR 0004).
	ErrAutoUnavailable = errors.New("apogee: auto mode requires fs-write and network confinement")

	// ErrOrderingCycle is returned by New / registry Add when Mechanism ordering
	// constraints form a cycle — it must fail loudly at startup (ADR 0003).
	ErrOrderingCycle = errors.New("apogee: mechanism ordering constraints contain a cycle")

	// ErrSessionVersion is returned by Resume / DecodeSession for a snapshot whose
	// schema version this build does not understand.
	ErrSessionVersion = errors.New("apogee: unsupported session schema version")

	// ErrInputPending is returned by Submit when an Exchange is already in progress.
	ErrInputPending = errors.New("apogee: cannot submit input mid-exchange")
)

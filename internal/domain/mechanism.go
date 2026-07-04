package domain

import (
	"context"
	"errors"
	"fmt"
)

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
// place (SetText / SetToolCallArguments) and carries no payload. ActionRetry re-calls
// the Upstream in the same Turn; a non-empty Inject makes it a correction retry — the
// loop re-streams the request with the superseded assistant message and the correction
// appended, request-scoped, never committed to history (R1, amending catalogue C5).
// ActionDefer carries the correction into the *next* request — the feed-forward path —
// held in conversation state as a Deferred Response Action so it survives a snapshot
// boundary.
type PostResponseDecision struct {
	Action PostResponseAction
	// Inject is the correction text — injected into the retried request for
	// ActionRetry, or the next request for ActionDefer (role-safe, like
	// Request.InjectContext). Empty carries no correction (for ActionRetry, a bare
	// re-stream).
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

// errNoHookInterface is the wrapped cause Add returns when a Mechanism implements no
// hook interface. It is internal: the host sees it through the error chain, not by name.
var errNoHookInterface = errors.New("implements no hook interface")

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

// Experimental returns the experimental hooks registered at hook point at, in
// registration order. It is the read seam the engine drives the loop through without
// reaching into the registry's unexported storage (ADR 0010 — internal subsystems
// see domain through its methods, the same way the public surface does).
func (r *MechanismRegistry) Experimental(at HookPoint) []any { return r.experimental[at] }

// ValidateOrdering reports ErrOrderingCycle if the catalogued Mechanisms' Before/After
// constraints form a cycle (ADR 0003 — a constraint cycle is a startup error). New
// calls it once the whole graph is present.
func (r *MechanismRegistry) ValidateOrdering() error { return detectOrderingCycle(r.mechanisms) }

// ValidateIncompatibilities reports ErrIncompatibleMechanisms if two registered Mechanisms
// declare each other incompatible (MechanismDescriptor.IncompatibleWith). It is the second
// construction-time gate alongside ValidateOrdering — a loud startup failure (ADR 0003), so a
// config enabling two mutually-exclusive Mechanisms is refused rather than silently running
// both. New calls it once the whole graph is present.
func (r *MechanismRegistry) ValidateIncompatibilities() error {
	return detectIncompatibility(r.mechanisms)
}

// Ordered returns the catalogued Mechanisms that hook at at, in the deterministic total order
// the loop dispatches them (ADR 0003 / D4): a topological sort of their Before/After constraints
// with a stable tiebreak by canonical MechanismID, so the order is independent of registration
// order. Only Mechanisms implementing the interface for at are returned; a constraint naming a
// Mechanism absent from at is ignored (ordering is relative to the co-located Mechanisms). It is
// the read seam the engine dispatches catalogued Mechanisms through, the counterpart to
// Experimental for the descriptor-carrying catalogue.
func (r *MechanismRegistry) Ordered(at HookPoint) []Mechanism {
	present := make([]Mechanism, 0, len(r.mechanisms))
	for _, m := range r.mechanisms {
		if hookImplements(at, m) {
			present = append(present, m)
		}
	}
	return topoSort(present)
}

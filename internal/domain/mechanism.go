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

// The five hook interfaces. A catalogued Mechanism's Hook — and a bench experimental
// hook — implements one or more of these; a catalogued Mechanism's descriptor and
// ordering come from its catalogue row, not from the hook value itself (ADR 0003 as
// amended 2026-07-25). Hooks are public so the bench can register experimental hooks
// (ADR 0002); an embedder technically can too, but without a descriptor it does not
// join self-regulation and carries no stability promise.

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
// of error_enrichment (correct_tool_result is deferred, owner-ratified 2026-07-04: a
// bench-side experimental hook until a production trigger is found), new to the loop
// (the proxy could not host it). It receives the originating call (the tool name and
// arguments live there, not on the result) and the loop view (error handling often
// counts prior failures across Turns).
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

// RegisteredMechanism is a catalogued Mechanism as the registry holds it (CONTEXT:
// Mechanism — a catalogued unit of gated, self-regulating behaviour). Descriptor and
// Ordering are catalogue DATA supplied at registration (ADR 0003 as amended
// 2026-07-25), not something the value says about itself; Hook is the behaviour.
// Metadata and behaviour are joined once, where the catalogue row is built, so a
// Mechanism and the row describing it cannot disagree.
type RegisteredMechanism struct {
	// Descriptor is the Mechanism's static metadata (CONTEXT: Mechanism descriptor) —
	// the single source for what Bypass turns off and what may co-fire.
	Descriptor MechanismDescriptor
	// Ordering is the Mechanism's declared position relative to its peers at the same
	// hook point; the zero value declares no edge.
	Ordering OrderingConstraints
	// Hook is the behaviour: a value implementing at least one hook interface above;
	// the registry type-asserts which. A hook carrying NO descriptor is an experimental
	// hook instead — registered through AddExperimental, outside self-regulation (ADR 0002).
	Hook any
}

// SubAgentScoped is the optional seam a hook implements when a delegated sub-agent must NOT run
// this very instance. A hook carrying per-run mutable state implements it and returns a fresh
// instance for the child; a hook whose state is read-only after construction — every
// value-receiver Mechanism in the catalogue — implements nothing and is inherited verbatim,
// exactly as before this seam existed.
//
// It exists because a depth-0 fan-out runs SIBLING children AT ONCE (ADR 0039). Two siblings
// sharing one hook instance would touch its state from two goroutines — a data race — and would
// see each other's state besides, which is wrong even when it is race-free: a Mechanism's state
// is about ONE agent's run. MechanismRegistry.ForSubAgent applies this seam at every spawn, so
// no caller has to remember to.
//
// Returning the receiver is a legitimate answer — it says "sharing this instance across the
// delegation boundary is deliberate and safe", which is true of a hook whose only mutable state
// is a collaborator that guards itself. But it has to be SAID: a catalogue guard test refuses a
// pointer-shaped Mechanism (the shape per-instance state requires) that declares neither.
type SubAgentScoped interface {
	// ForSubAgent returns the hook instance a delegated sub-agent runs. It is called once per
	// spawn, on the spawning agent's goroutine, before the child exists.
	ForSubAgent() any
}

// MechanismID is the canonical, stable identifier of a Mechanism — also the stable
// tiebreak in the deterministic total order (ADR 0003).
type MechanismID string

// ExperimentalMechanismID is the synthetic MechanismID a descriptor-less experimental
// hook fires under (ADR 0002 — no descriptor, no self-regulation). It exists so
// MechanismFiredEvent.Mechanism is never empty for bench attribution, and it is
// RESERVED: Add refuses a catalogued Mechanism claiming it, so a real Mechanism can
// never masquerade as the bench's own instrument or inherit its always-booked fire
// accounting (R5, phase-4-review-fixes item 4).
const ExperimentalMechanismID MechanismID = "experimental"

// MechanismDescriptor is per-Mechanism metadata orthogonal to its hook point
// (CONTEXT: Mechanism descriptor). The single source of truth for what Bypass turns
// off (by Capability) and what may co-fire (IncompatibleWith).
type MechanismDescriptor struct {
	ID          MechanismID
	Capability  Capability
	Suppression SuppressionPolicy
	// IncompatibleWith constrains stacking — Mechanisms that must not co-fire.
	IncompatibleWith []MechanismID
	// Requires constrains stacking the other way — Mechanisms that must all be
	// registered for this one to be enabled (the dual of IncompatibleWith). It is the
	// enable-time declaration that this Mechanism is benched as a stack with its named
	// peers: enabling it without them is a startup error (ValidateRequirements, ADR 0014
	// §4). Enable-time only — live suppression of a required peer mid-Session is not
	// re-checked.
	Requires []MechanismID
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
//
// OWNERSHIP AND CONCURRENCY. A registry is MUTABLE while it is being built (Add /
// AddExperimental, both single-goroutine at construction) and READ-ONLY once the engine has it:
// the read seams (Ordered, Experimental) and the three validate gates only read, so any number
// of goroutines may drive one registry at once. Instance ownership is the separate question a
// concurrent depth-0 fan-out asks (ADR 0039), and ForSubAgent is its answer: an agent never
// hands a delegated child the registry it is itself running, so the two can never race through
// the container, and a hook that carries live state declares its own per-child instance
// (SubAgentScoped) rather than being silently shared.
type MechanismRegistry struct {
	mechanisms   []RegisteredMechanism // catalogued Mechanism rows registered via Add
	experimental map[HookPoint][]any   // bench experimental hooks registered via AddExperimental
}

// NewMechanismRegistry returns a registry seeded with the built-in catalogue. The
// Phase-0 catalogue is empty — the curated Mechanisms land with the catalogue→hook
// mapping session (Phase 4); P0.6 needs only the experimental-hook slots.
func NewMechanismRegistry() *MechanismRegistry {
	return &MechanismRegistry{experimental: make(map[HookPoint][]any)}
}

// Add registers a catalogued Mechanism row — its descriptor, its ordering constraints and
// its hook, joined by the catalogue that built it. It returns an error if the row carries
// no MechanismID at all (a blank canonical ID attributes its MechanismFiredEvents to
// nothing and sorts first in the stable tiebreak), claims the reserved experimental
// sentinel ID, re-uses an already-registered MechanismID (topoSort's byID map would
// otherwise silently drop one of the two — a loud failure instead, phase-4-review-fixes
// item 5), or carries a Hook implementing no hook interface.
// (The constraint-cycle check is performed by New over the whole graph — a startup gate,
// ADR 0003 — so a registry under construction can hold constraints that only close a
// cycle once every Mechanism is present.)
func (r *MechanismRegistry) Add(m RegisteredMechanism) error {
	id := m.Descriptor.ID
	if id == "" {
		return errors.New("apogee: mechanism ID is empty")
	}
	if id == ExperimentalMechanismID {
		return fmt.Errorf("apogee: mechanism ID %q is reserved for experimental hooks", id)
	}
	for _, registered := range r.mechanisms {
		if registered.Descriptor.ID == id {
			return fmt.Errorf("apogee: mechanism ID %q is already registered", id)
		}
	}
	if !implementsAnyHook(m.Hook) {
		return fmt.Errorf("apogee: mechanism %q: %w", id, errNoHookInterface)
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

// ForSubAgent returns the registry a delegated sub-agent runs: the same catalogue rows and the
// same experimental hooks, in a container of the CHILD's own, with every hook declaring itself
// SubAgentScoped replaced by the instance it hands that child. It is the Mechanism-side
// counterpart of security.Guards.ForSubAgent (ADR 0013 §3) and answers the same question the same
// way — isolate what is live, share what is read-only — one level up: the container is always
// fresh, so a parent and its children (and two siblings, which is the case that bites — ADR 0039)
// can never race through the registry itself, while a hook with nothing live to isolate is
// inherited by pointer exactly as it always was.
//
// A hook that declares nothing is therefore shared VERBATIM, which is what keeps the inheritance
// byte-identical for every Mechanism in today's catalogue: they are value hooks whose fields are
// read-only after construction, so a value receiver cannot mutate anything a sibling can observe.
// The rule a new Mechanism inherits: state that changes per run lives behind a pointer, and a
// pointer hook must say how it scopes to a child.
func (r *MechanismRegistry) ForSubAgent() *MechanismRegistry {
	sub := &MechanismRegistry{
		mechanisms:   make([]RegisteredMechanism, len(r.mechanisms)),
		experimental: make(map[HookPoint][]any, len(r.experimental)),
	}
	for i, m := range r.mechanisms {
		m.Hook = hookForSubAgent(m.Hook)
		sub.mechanisms[i] = m
	}
	for at, hooks := range r.experimental {
		scoped := make([]any, len(hooks))
		for i, hook := range hooks {
			scoped[i] = hookForSubAgent(hook)
		}
		sub.experimental[at] = scoped
	}
	return sub
}

// hookForSubAgent returns the instance of hook a delegated sub-agent runs: what the hook itself
// says (SubAgentScoped), or the hook unchanged when it says nothing.
func hookForSubAgent(hook any) any {
	if scoped, ok := hook.(SubAgentScoped); ok {
		return scoped.ForSubAgent()
	}
	return hook
}

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

// ValidateRequirements reports ErrMissingRequirement if any registered Mechanism declares a
// required peer (MechanismDescriptor.Requires) that is not itself registered. It is the third
// construction-time gate alongside ValidateOrdering and ValidateIncompatibilities — the dual of
// the incompatibility check: where incompatibility refuses two Mechanisms that must never
// co-fire, this refuses a Mechanism enabled without a peer it is benched as a stack with, a loud
// startup failure (ADR 0003 posture, ADR 0014 §4). Enable-time only: live suppression of a
// required peer mid-Session is accepted and not re-checked. New calls it once the whole graph is
// present.
func (r *MechanismRegistry) ValidateRequirements() error {
	return detectRequirements(r.mechanisms)
}

// Ordered returns the catalogued Mechanisms that hook at at, in the deterministic total order
// the loop dispatches them (ADR 0003 / D4): a topological sort of their Before/After constraints
// with a stable tiebreak by canonical MechanismID, so the order is independent of registration
// order. Only Mechanisms implementing the interface for at are returned; a constraint naming a
// Mechanism absent from at is ignored (ordering is relative to the co-located Mechanisms). It is
// the read seam the engine dispatches catalogued Mechanisms through, the counterpart to
// Experimental for the descriptor-carrying catalogue.
func (r *MechanismRegistry) Ordered(at HookPoint) []RegisteredMechanism {
	present := make([]RegisteredMechanism, 0, len(r.mechanisms))
	for _, m := range r.mechanisms {
		if hookImplements(at, m.Hook) {
			present = append(present, m)
		}
	}
	return topoSort(present)
}

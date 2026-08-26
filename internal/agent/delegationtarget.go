package agent

// The Delegation target latch (ADR 0045). A session may send every delegation to a SECOND
// server — the Sub-agent server, the one `servers:` entry flagged `sub-agents: true` — so a
// smart model orchestrates the session while a cheaper one, possibly on another box, does the
// delegated grunt work. What that server IS (endpoint, key, model, window, fan-out width, model
// profile) and what its delegations run WITH (Bypass, Mechanisms) is discovered by the host's
// second heartbeat monitor and handed to the engine here as one resolved value.
//
// The engine stays wire-silent (ADR 0031): it never learns that a `servers:` list exists, which
// entry carries the flag, or how a posture map spells itself. It is handed a resolved spec and
// builds a client from it exactly as SwitchUpstream does — the same posture Rebind takes for the
// per-model bindings and SwapTools for the tool set, which is also what keeps routing benchable
// all the way up: a bench Driver drives this setter directly, with no config file in sight.
//
// This file is the LATCH alone — the holder, its setter and its read seam. What a routed spawn
// then BUILDS from the target (upstream, window, profile, posture) lives with the spawn, in
// subagent.go.

import (
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
)

// DelegationTarget is the resolved Sub-agent server a delegation routes to: everything a spawn
// needs to build a child against another server, computed WHOLE by the composition root from the
// flagged entry's pins and its heartbeat's observations (ADR 0045 §3). A latched target means
// routing is ENGAGED; no target — nothing latched yet, the server down, no model bound there —
// means a spawn falls back to the parent's Upstream with the parent's posture, which is the
// behaviour every session had before this existed (ADR 0045 §4).
//
// It is treated as IMMUTABLE once latched: the host resolves a fresh value per beat and installs
// that, so a snapshot taken at a spawn stays valid for the life of the child even as later beats
// replace what the latch holds.
type DelegationTarget struct {
	// Endpoint is the OpenAI-compatible base URL a routed child dials — the flagged entry's
	// `endpoint:`. Required: a target without one cannot be dialed, so the host latches nil
	// instead and the spawn falls back.
	Endpoint string
	// APIKey is that endpoint's bearer token, empty for a server that needs none.
	APIKey string
	// Model is the model id a routed child sends on the wire — the entry's `model:` pin, else the
	// model its heartbeat observed bound there. Required for the same reason Endpoint is: a
	// delegation that cannot name a model is not a usable target, it is the fallback.
	Model string
	// ContextWindow is Model's bound context window in tokens — the entry's `context-window:` pin,
	// else the observed per-slot window. 0 means the target names NO window (neither pinned nor
	// observed), and it is the one field an unusable value does not make the whole target unusable:
	// the spawn keeps the PARENT's window instead of inheriting the zero, so a routed child is never
	// built windowless — with a dead Budget, no automatic Compaction and readings stamped 0 — over a
	// number nobody supplied (subagent.go).
	ContextWindow int
	// WorkingWindow is the room INSIDE ContextWindow a routed child actually works in, in tokens —
	// the flagged entry's `working-window:` pin. There is no observed half to fall back to: a server
	// reports no working room, it is an operator's bound on what a delegation there may spend. 0
	// means the target names none, and — unlike the window above — the zero IS carried to the child
	// as written, for MaxOutputTokens' reason: an absent bound is not a broken child, it is a child
	// working in the whole of the TARGET's window. Inheriting the parent's bound instead would fence
	// a run on this server by a number sized for another one's window.
	WorkingWindow int
	// MaxOutputTokens is the ceiling on ONE reply from this server, in tokens — the flagged entry's
	// `max-output-tokens:` pin (ADR 0046). 0 means the target pins NO cap, and — unlike the window
	// above — the zero is carried to the child as written rather than left to the parent's: a cap of
	// nothing is not a broken child, it is a child that derives its own cap from the window it runs
	// in, which by then is the TARGET's window. Inheriting the parent's pin instead would cap a
	// reply from this server at a number describing another one.
	MaxOutputTokens int
	// ResponseReserveFraction is the share of ContextWindow this server holds back for one reply —
	// the flagged entry's `response-reserve:` override, as written. 0 means the entry states NO
	// share, and — like the window above rather than the cap between them — the zero is NOT carried:
	// the child keeps the share the parent resolved, which is the run's own top-level
	// `response-reserve:` when nobody overrode it. That is the honest fall-through here, because a
	// share is a fraction of whatever window the child ends up in rather than a number describing one
	// server's slot, so the run-wide split is meaningful on the routed server too — where an absent
	// reply CEILING is not, being a token count off the retired server's window.
	ResponseReserveFraction float64
	// ParallelAgents is the RECEIVING server's fan-out width — the cap that bounds a routed
	// depth-0 fan-out and the guided-decomposition batch alike (ADR 0039's one width everywhere,
	// sourced from the server actually holding the slots). Anything < 2 means serial.
	ParallelAgents int
	// Profile is the model profile resolved for Model (ADR 0044): how the grunt model speaks the
	// wire, on the two axes of tool-call format and inline thinking channel. The zero value is
	// meaningful — native tool calls, no inline thinking — so a routed child on a model that
	// matches nothing parses exactly as an unprofiled session does.
	Profile domain.ModelProfile
	// Bypass is the flagged entry's `bypass:` posture (ADR 0045 §2). Non-nil REPLACES the
	// inherited value whole: delegations to this server run with this flag whatever the parent's
	// is. nil inherits the parent's LIVE flag at spawn, which is today's rule.
	Bypass *bool
	// Mechanisms builds the catalogue a routed child runs with, translated by the composition root
	// from the flagged entry's `mechanisms:` map. Non-nil REPLACES the inherited catalogue whole
	// (no per-ID merge — ADR 0045 §2), and it is a FACTORY rather than a registry because it is
	// called once per child: siblings in a fan-out run at once, so each needs a registry of its
	// own, the same live-state isolation MechanismRegistry.ForSubAgent already encodes. nil
	// inherits the parent's catalogue through ForSubAgent, as today.
	Mechanisms func() *domain.MechanismRegistry
}

// delegationLatch holds the current Delegation target behind an RWMutex. It is a POINTER on the
// Agent, and newChildAgent hands the child the parent's holder rather than a copy, so one latch
// serves a whole agent tree: a depth-2 spawn reads the same current target its grandparent's next
// spawn would, and the host has exactly one place to push to however deep the tree runs.
//
// The mutex is the whole reason it is a type of its own: the target is written by the host's
// heartbeat goroutine and read by every goroutine that spawns a child — including the workers of
// a depth-0 fan-out, which spawn AT ONCE (ADR 0039) — so the shared-mutable-state rule applies
// and the holder owns the lock for it.
type delegationLatch struct {
	mu     sync.RWMutex
	target *DelegationTarget
}

// set installs target as the current one, nil clearing it.
func (l *delegationLatch) set(target *DelegationTarget) {
	l.mu.Lock()
	l.target = target
	l.mu.Unlock()
}

// snapshot reports the currently latched target under the read lock, nil meaning none. The value
// behind the pointer is never mutated in place — the host installs a fresh one per beat — so the
// caller may read it long after the lock is dropped.
func (l *delegationLatch) snapshot() *DelegationTarget {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.target
}

// SetDelegationTarget latches the Sub-agent server every delegation in this agent tree routes to,
// or clears it with nil so delegations fall back to running on this session's own Upstream with
// this session's posture (ADR 0045 §4). The host resolves the spec whole — from the flagged
// entry's pins and its own heartbeat's observations — and re-states it on every beat; the engine
// applies what it is handed and reads no config of its own (ADR 0031).
//
// It is deliberately NOT idle-gated, and that is the contrast with Rebind, SwitchUpstream and
// SetProfile: those swap bindings the OPEN Exchange is mid-way through using, so they refuse
// mid-Exchange and wait for a quiescent boundary. This one changes nothing about the running
// session — it changes what the NEXT spawn builds — and beats land whenever the second monitor
// beats, which is squarely mid-Exchange for any delegation-heavy Turn. So it belongs to the
// anytime-goroutine-safe class alongside SetParallelAgents: it swaps one live field behind its own
// lock, and each spawn snapshots whatever is current. A child already running is never re-routed;
// a target that changes mid-fan-out reaches the spawns after it and leaves the ones before it
// alone.
//
// Calling it on a sub-agent is legal but re-routes the WHOLE tree, holder being shared: hosts push
// to the top-level Agent they constructed.
func (a *Agent) SetDelegationTarget(target *DelegationTarget) {
	a.delegation.set(target)
}

// delegationTarget snapshots the tree's current Delegation target for one spawn, nil meaning
// routing is not engaged and the caller takes the parent-inheriting path. It is the ONE read seam
// for the latch, so every consumer reads it race-free against the host's next beat.
func (a *Agent) delegationTarget() *DelegationTarget {
	return a.delegation.snapshot()
}

package main

// The Sub-agent server seam of the composition root (ADR 0045).
//
// One `servers:` entry may be flagged `sub-agents: true`, and every delegation this session makes
// then runs on THAT server — a smart model orchestrating while a cheaper one, possibly on another
// box, does the grunt work. What that server is serving, in which window and across how many slots,
// is not something a config file can know, so it is DISCOVERED: a second heartbeat Monitor observes
// the flagged entry on the session monitor's own cadence, and each beat is resolved into the one
// value the engine latches — the Delegation target.
//
// The split is ADR 0031's: the resolution happens HERE, at the layer that knows the file, the pins,
// the profile tiers and the Mechanism catalogue, and the engine is handed a finished spec it applies
// without reading a line of config. And it is ADR 0042's degrade: a server that cannot be reached,
// or that binds no model, resolves to nil — which the engine reads as "not routing", so delegations
// run on the session's own Upstream exactly as they did before any of this existed.

import (
	"context"
	"fmt"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/mechanisms"
	"github.com/airiclenz/apogee/internal/profiles"
)

// delegationSetter is the engine mutation a resolved target is pushed through, named as the ONE
// method it needs so the wiring below is exercisable without constructing an Agent — the
// upstreamSwitcher / parallelAgentsSetter posture. Satisfied by *lateEngine, and through it by
// *apogee.Agent.
type delegationSetter interface {
	SetDelegationTarget(*apogee.DelegationTarget)
}

// delegationWiring is this session's Sub-agent server: the flagged entry, the beat that observes it,
// and the two values the composition root resolves ONCE because nothing a beat reports can move them
// — the Mechanism catalogue its delegations run with, and where the model-profile match reads its
// user tier from.
//
// Its ZERO value is the ordinary session: no flag in the `servers:` list means no beat, no second
// Monitor, and nothing ever latched, which is behaviour identical to before routing existed. That is
// why it is held by value and why every method below opens by asking whether there is anything to
// observe — the wiring is always present, the server is not.
//
// It needs no lock of its own: every field is written once, at startup, before the renderer starts
// the cadence that reads them. What IS shared — the latch the resolved target is pushed into — owns
// its own mutex one layer down (agent.SetDelegationTarget).
type delegationWiring struct {
	// entry is the flagged `servers:` entry: the dial facts a target is built from, the pins that
	// outrank discovery, and the posture keys that ride the flag.
	entry config.ServerEntry
	// beat observes the Sub-agent server, and is nil when there is none. It is a func rather than the
	// *heartbeat.Monitor itself for the reason every seam this root hands across is: the resolution
	// is then exercisable against an observation a test writes down, with no server behind it.
	beat func(context.Context) heartbeat.Beat
	// catalogue builds the Mechanism registry ONE routed child runs with, and is nil when the entry
	// carries no `mechanisms:` map — which is the engine's signal to inherit the parent's catalogue
	// as it always has (ADR 0045 §2). It is a factory because siblings in a fan-out run at once and
	// each needs a registry of its own.
	catalogue func() *apogee.MechanismRegistry
	// userProfiles reads the `model-profiles:` user tier as it stands NOW, so a profile committed
	// mid-session reaches the next beat's resolution rather than the next launch.
	userProfiles func() []profiles.Entry
	// engine is where a resolved target is latched.
	engine delegationSetter
}

// newDelegationWiring finds the Sub-agent server in entries and builds everything a beat on it will
// need. With no entry flagged it answers the zero wiring — no monitor is constructed and no target
// is ever pushed, so a session whose config says nothing about delegation routing behaves exactly as
// it did before (ADR 0045 §4's floor).
//
// base is the session's own Config, and it is read for exactly what building the flagged entry's
// Mechanism catalogue needs: the state roots. The entry's own endpoint and `model:` pin replace the
// session's on the copy that build sees, so the identity a Library observation is filed under is the
// SUB-AGENT server's model rather than the orchestrator's.
//
// It fails the run rather than degrading when the flagged entry's `mechanisms:` map is defective —
// an unknown key, or a set the stacking gates refuse. That is the same posture the session's own
// block takes at this same boundary (mechanismIDs, wireSession): a typo in a file outlives the day
// it was written, and a posture that silently armed nothing would be invisible for months.
func newDelegationWiring(
	entries []config.ServerEntry,
	base apogee.Config,
	engine delegationSetter,
	userProfiles func() []profiles.Entry,
) (delegationWiring, error) {
	entry, ok := config.SubAgentServer(entries)
	if !ok {
		return delegationWiring{}, nil
	}
	catalogue, err := subAgentCatalogue(entry, base)
	if err != nil {
		return delegationWiring{}, err
	}
	return delegationWiring{
		entry: entry,
		// The discovery hint is the entry's own `model:` pin, empty when it pins none — the session
		// Monitor's contract verbatim (heartbeat.NewMonitor): while the server still serves the pinned
		// id, discovery resolves ITS window rather than the first advertised model's, and once the pin
		// vanishes the beat reports what is actually loaded.
		beat:         heartbeat.NewMonitor(entry.Endpoint, entry.Model, entry.APIKey).Beat,
		catalogue:    catalogue,
		userProfiles: userProfiles,
		engine:       engine,
	}, nil
}

// observe starts one beat on the Sub-agent server and hands back the join for it. With no Sub-agent
// server there is nothing to observe and the join is a no-op — no goroutine, no push, no latch.
//
// It is split into start-and-join because the session's beat runs in the same window: two five-second
// discoveries in SERIES would be exactly heartbeat.Interval, and that package's no-overlap property
// rests on a beat staying strictly shorter than the interval. Run side by side they cost the longer
// of the two, so the second server is observed on the same cadence without slowing the first.
//
// The push is unconditional — a resolved target on a usable beat, nil on an unusable one — because
// "the Sub-agent server is not answering" is exactly as much a fact about routing as "it is". The
// latch it lands in is anytime-safe (ADR 0045: deliberately never idle-gated), so a beat landing
// mid-Exchange re-points the delegations SPAWNED after it and leaves every running child alone.
func (d delegationWiring) observe(ctx context.Context) func() {
	if d.beat == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.engine.SetDelegationTarget(
			resolveDelegationTarget(d.entry, d.beat(ctx), d.userProfiles(), d.catalogue))
	}()
	return func() { <-done }
}

// resolveDelegationTarget turns one observation of the Sub-agent server into the spec a routed spawn
// is built from, and nil when that server cannot take a delegation right now.
//
// Every field is PIN-else-OBSERVE, which is the whole of ADR 0045 decision 4 and the same rank order
// the session's own bindings already follow: the file's pin is a statement about the server that
// discovery may not overrule, the beat answers whatever the file left open, and the last rank is the
// honest floor. So `model:` outranks the model the beat found bound, `context-window:` outranks the
// window /props reported, and `parallel-agents:` outranks the slot count — that last one through
// config.ResolveParallelAgents itself, so the Sub-agent server's width is resolved by the very
// function the session's own cap is resolved by (ADR 0039: one width everywhere).
//
// Two facts make a target UNUSABLE, and both mean the same thing to the caller: a beat that could not
// read the server at all, and a beat on a server with no model bound and no pin to name one. Neither
// is an error — an unreachable server is a finding (heartbeat.Beat) — so the answer is nil, the
// fallback, and the delegations run on the session's own Upstream with the session's posture.
//
// The POSTURE is copied across untranslated on purpose: `bypass:` is the entry's own pointer, whose
// nil-ness IS the inherit-versus-replace instruction, and the catalogue factory is whatever the
// entry's `mechanisms:` map built (nil when it has none). Neither is a per-beat resolution, because
// neither is something a server can be observed to have.
func resolveDelegationTarget(
	entry config.ServerEntry,
	observed heartbeat.Beat,
	userProfiles []profiles.Entry,
	catalogue func() *apogee.MechanismRegistry,
) *apogee.DelegationTarget {
	if !observed.Reachable {
		return nil
	}
	model := entry.Model
	if model == "" {
		model = observed.ActiveModel
	}
	if model == "" {
		return nil
	}
	window := entry.ContextWindow
	if window <= 0 {
		window = observed.ContextWindow
	}
	// The shape the grunt model speaks the wire in, matched on the model that just resolved (ADR
	// 0044). The notice half of the match is deliberately dropped: it explains a built-in match of
	// the model the HUMAN is looking at, and this resolution re-runs every ten seconds on a model
	// they are not — a beat is no place to repeat a sentence.
	profile, _ := resolveModelProfile(model, userProfiles)
	return &apogee.DelegationTarget{
		Endpoint:       entry.Endpoint,
		APIKey:         entry.APIKey,
		Model:          model,
		ContextWindow:  window,
		ParallelAgents: config.ResolveParallelAgents(entry.ParallelAgents, observed.TotalSlots),
		Profile:        profile,
		Bypass:         entry.Bypass,
		Mechanisms:     catalogue,
	}
}

// subAgentCatalogue builds the factory a routed child's Mechanism catalogue comes out of, and nil
// when the flagged entry carries no `mechanisms:` map at all — the absent key that leaves a child
// inheriting the parent's catalogue as it always has (ADR 0045 §2).
//
// A PRESENT map is the child's entire catalogue, so a map whose every key is false is not the same
// as no map: it builds an EMPTY registry, and a delegation to that server runs with no Mechanism at
// all. That is the replace-whole rule doing exactly what it says, and it is why the emptiness test
// below is on the map rather than on the ids it validates to.
//
// The build goes through the engine's own BuildMechanisms rather than assembling a registry here,
// because the Deps a catalogue row needs — the Library store, the identity ladder keyed on the
// model — are the engine's to derive (ADR 0015 §2). What this layer owns is the two things the
// engine cannot know: that the map's keys are catalogued at all (mechanismIDs, the same validation
// the session's own block gets, typo'd DISABLED keys included), and that the identity is the
// SUB-AGENT server's — hence the endpoint and model swapped onto the config copy the build reads.
//
// The result is built ONCE, at startup, and the factory hands each child a copy through ForSubAgent:
// the registry is a per-run instance surface, not a per-model resolution, so re-deriving it on every
// beat would buy nothing and would put a build error somewhere no one can see it.
func subAgentCatalogue(entry config.ServerEntry, base apogee.Config) (func() *apogee.MechanismRegistry, error) {
	if len(entry.Mechanisms) == 0 {
		return nil, nil
	}
	ids, err := mechanismIDs(entry.Mechanisms, mechanisms.KnownIDs())
	if err != nil {
		// mechanismIDs already carries the house "apogee: " prefix, so the entry that asked for it is
		// appended rather than prefixed (buildEnabledMechanisms' rule): the message would otherwise
		// print the prefix twice, and a user with two servers listed needs to know which one is meant.
		return nil, fmt.Errorf("%w — in the `sub-agents:` server %q", err, entry.Name)
	}
	cfg := base
	cfg.Endpoint = entry.Endpoint
	cfg.Model = entry.Model
	built, err := apogee.BuildMechanisms(cfg, ids)
	if err != nil {
		return nil, fmt.Errorf("%w — in the `sub-agents:` server %q", err, entry.Name)
	}
	return built.ForSubAgent, nil
}

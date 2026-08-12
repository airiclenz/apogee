package main

import (
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/profiles"
)

// delegationSpy records what the wiring pushed at the engine, in order. It needs no lock: observe
// pushes from its own goroutine and the join the caller runs establishes the happens-before edge, so
// a test reading these fields after the join reads what that goroutine wrote.
type delegationSpy struct {
	pushes []*apogee.DelegationTarget
}

func (s *delegationSpy) SetDelegationTarget(target *apogee.DelegationTarget) {
	s.pushes = append(s.pushes, target)
}

// beatSource is a Sub-agent server's observation written down: the wiring takes its beat as a func
// exactly so a resolution can be exercised against one of these rather than against a live server.
func beatSource(observed heartbeat.Beat) func(context.Context) heartbeat.Beat {
	return func(context.Context) heartbeat.Beat { return observed }
}

// A pin is a statement about the server that discovery may not overrule (ADR 0045 decision 4), and
// every field of a Delegation target follows that one rank order. The beat here disagrees with the
// entry on all three pinnable facts, so only the ranks can explain the result.
func TestResolveDelegationTargetPinsOutrankTheBeat(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{
		Name:           "grunt",
		Endpoint:       "http://127.0.0.1:2222",
		APIKey:         "grunt-key",
		SubAgents:      true,
		Model:          "pinned-model",
		ContextWindow:  32768,
		ParallelAgents: 3,
	}
	observed := heartbeat.Beat{
		Reachable:     true,
		ActiveModel:   "whatever-is-loaded",
		ContextWindow: 4096,
		TotalSlots:    8,
	}

	target := resolveDelegationTarget(entry, observed, nil, nil)
	if target == nil {
		t.Fatal("a reachable server resolved to no target; want the pinned one")
	}
	if target.Endpoint != entry.Endpoint || target.APIKey != entry.APIKey {
		t.Errorf("dial facts = %q/%q; want the entry's %q/%q",
			target.Endpoint, target.APIKey, entry.Endpoint, entry.APIKey)
	}
	if target.Model != "pinned-model" {
		t.Errorf("Model = %q; want the entry's pin to outrank the observed model", target.Model)
	}
	if target.ContextWindow != 32768 {
		t.Errorf("ContextWindow = %d; want the entry's 32768 pin to outrank the observed 4096", target.ContextWindow)
	}
	if target.ParallelAgents != 3 {
		t.Errorf("ParallelAgents = %d; want the entry's 3 pin to outrank the observed 8 slots", target.ParallelAgents)
	}
}

// With nothing pinned the beat answers every field it can, and the one it cannot — a server naming no
// slot count — falls to the serial floor rather than to "unbounded" (config.ResolveParallelAgents,
// the same ranks the session's own cap resolves through).
func TestResolveDelegationTargetObservesWhatIsNotPinned(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222", SubAgents: true}
	observed := heartbeat.Beat{Reachable: true, ActiveModel: "loaded-model", ContextWindow: 8192}

	target := resolveDelegationTarget(entry, observed, nil, nil)
	if target == nil {
		t.Fatal("a reachable server resolved to no target; want the observed one")
	}
	if target.Model != "loaded-model" || target.ContextWindow != 8192 {
		t.Errorf("model/window = %q/%d; want the beat's loaded-model/8192", target.Model, target.ContextWindow)
	}
	if target.ParallelAgents != 1 {
		t.Errorf("ParallelAgents = %d; want the serial floor when neither pin nor beat can say", target.ParallelAgents)
	}

	// And the slot count the same server reports a beat later widens it, with nothing else moving.
	observed.TotalSlots = 4
	if wider := resolveDelegationTarget(entry, observed, nil, nil); wider.ParallelAgents != 4 {
		t.Errorf("ParallelAgents with 4 observed slots = %d; want 4", wider.ParallelAgents)
	}
}

// The grunt model's dialect is resolved for the model that just bound on the SUB-AGENT server (ADR
// 0044 through ADR 0045): the session may be reading harmony channels while its delegations read
// `<think>` tags, so the match keys on the target's model and on the user tier this host holds.
func TestResolveDelegationTargetResolvesTheProfileForTheBoundModel(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222", SubAgents: true}
	observed := heartbeat.Beat{Reachable: true, ActiveModel: "cheap-thinker-7b"}
	user := []profiles.Entry{{
		Pattern: "cheap-thinker",
		Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingDelimited}},
	}}

	target := resolveDelegationTarget(entry, observed, user, nil)
	if target == nil {
		t.Fatal("a reachable server resolved to no target")
	}
	if got := target.Profile.Thinking.Style; got != domain.ThinkingDelimited {
		t.Errorf("Profile.Thinking.Style = %q; want the user entry matching the target's model", got)
	}
	// A model no tier knows keeps the zero profile — native tool calls, no inline thinking — which is
	// how an unprofiled delegation has always parsed.
	unmatched := resolveDelegationTarget(entry, heartbeat.Beat{Reachable: true, ActiveModel: "nobody-knows"}, user, nil)
	if unmatched.Profile != (domain.ModelProfile{}) {
		t.Errorf("Profile for an unmatched model = %+v; want the zero profile", unmatched.Profile)
	}
}

// The posture keys ride the routing untranslated: `bypass:` travels as the entry's own pointer,
// because its NIL-ness is the inherit-versus-replace instruction the engine reads (ADR 0045 §2), and
// the Mechanism catalogue travels as the factory the entry's map built.
func TestResolveDelegationTargetCarriesThePostureVerbatim(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222", SubAgents: true}
	observed := heartbeat.Beat{Reachable: true, ActiveModel: "loaded-model"}

	// Nothing on the entry: both postures are absent, and absent is what makes a child inherit.
	bare := resolveDelegationTarget(entry, observed, nil, nil)
	if bare.Bypass != nil {
		t.Errorf("Bypass with none on the entry = %v; want absent so the child inherits the parent's live flag", bare.Bypass)
	}
	if bare.Mechanisms != nil {
		t.Error("a catalogue was built for an entry with no `mechanisms:` map; want absent so the child inherits")
	}

	on := true
	entry.Bypass = &on
	catalogue := func() *apogee.MechanismRegistry { return apogee.NewMechanismRegistry() }
	dressed := resolveDelegationTarget(entry, observed, nil, catalogue)
	if dressed.Bypass == nil || !*dressed.Bypass {
		t.Errorf("Bypass = %v; want the entry's own true", dressed.Bypass)
	}
	if dressed.Mechanisms == nil {
		t.Fatal("Mechanisms = nil; want the catalogue factory the entry's map built")
	}
	if first, second := dressed.Mechanisms(), dressed.Mechanisms(); first == second {
		t.Error("the factory handed out the same registry twice; siblings in a fan-out need one each")
	}
}

// An unusable Sub-agent server is not an error, it is the FALLBACK: nil says "not routing", and the
// engine reads that as the parent's Upstream with the parent's posture — today's behaviour (ADR 0042).
func TestResolveDelegationTargetRefusesAnUnusableBeat(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222", SubAgents: true}

	unreachable := heartbeat.Beat{Failure: "connection refused"}
	if target := resolveDelegationTarget(entry, unreachable, nil, nil); target != nil {
		t.Errorf("an unreachable server resolved to %+v; want no target", target)
	}
	// Reachable but serving nothing this session can name, and no pin to name it with: a delegation
	// that cannot say which model it is talking to is not a usable target either.
	nameless := heartbeat.Beat{Reachable: true}
	if target := resolveDelegationTarget(entry, nameless, nil, nil); target != nil {
		t.Errorf("a server with no model bound resolved to %+v; want no target", target)
	}
	// The pin is what rescues that case: the file names the model the server will serve.
	entry.Model = "pinned-model"
	if target := resolveDelegationTarget(entry, nameless, nil, nil); target == nil || target.Model != "pinned-model" {
		t.Errorf("target with a pinned model on a model-less beat = %+v; want the pin", target)
	}
}

// No flag in the `servers:` list is the ordinary session, and it must stay bit-for-bit ordinary: no
// second Monitor is constructed, no beat runs, and the latch is never written — so a delegation goes
// exactly where it went before routing existed.
func TestNewDelegationWiringWithoutAFlagObservesNothing(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{
		{Name: "here", Endpoint: "http://127.0.0.1:1111"},
		{Name: "there", Endpoint: "http://127.0.0.1:2222"},
	}
	spy := &delegationSpy{}
	wiring, err := newDelegationWiring(entries, validCfg(t), spy, func() []profiles.Entry { return nil })
	if err != nil {
		t.Fatalf("newDelegationWiring with no flagged entry: %v", err)
	}
	if wiring.beat != nil {
		t.Error("a monitor was constructed for a list with no Sub-agent server")
	}
	wiring.observe(context.Background())()
	if len(spy.pushes) != 0 {
		t.Errorf("pushes = %d; want the latch never written without a Sub-agent server", len(spy.pushes))
	}
}

// The whole cycle at the seam the renderer's cadence drives: one beat on the Sub-agent server, one
// resolved target latched. A beat that finds the server gone latches nil in its place — the routing
// state changes on the same clock the server does, without waiting for a delegation to discover it.
func TestDelegationWiringObservePushesWhatTheBeatResolvedTo(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222", SubAgents: true, ParallelAgents: 2}
	spy := &delegationSpy{}
	wiring := delegationWiring{
		entry:        entry,
		beat:         beatSource(heartbeat.Beat{Reachable: true, ActiveModel: "loaded-model", ContextWindow: 8192}),
		userProfiles: func() []profiles.Entry { return nil },
		engine:       spy,
	}

	wiring.observe(context.Background())()
	if len(spy.pushes) != 1 || spy.pushes[0] == nil {
		t.Fatalf("pushes after a usable beat = %+v; want one resolved target", spy.pushes)
	}
	if got := spy.pushes[0]; got.Model != "loaded-model" || got.ContextWindow != 8192 || got.ParallelAgents != 2 {
		t.Errorf("latched target = %+v; want the beat's model/window and the entry's cap", got)
	}

	wiring.beat = beatSource(heartbeat.Beat{Failure: "connection refused"})
	wiring.observe(context.Background())()
	if len(spy.pushes) != 2 || spy.pushes[1] != nil {
		t.Fatalf("pushes after the server went away = %+v; want a second push clearing the latch", spy.pushes)
	}
}

// The flagged entry's `mechanisms:` map is the child's ENTIRE catalogue, built once at startup and
// handed to each child as a copy of its own. An absent map is the other instruction — inherit — and
// the two are told apart by the map, not by what it validates to.
func TestSubAgentCatalogueBuildsTheEntrysOwnMechanisms(t *testing.T) {
	t.Parallel()

	base := validCfg(t)
	base.LibraryDir = t.TempDir()

	absent, err := subAgentCatalogue(config.ServerEntry{Name: "grunt", SubAgents: true}, base)
	if err != nil {
		t.Fatalf("subAgentCatalogue with no map: %v", err)
	}
	if absent != nil {
		t.Error("an entry with no `mechanisms:` map built a catalogue; want nil so the child inherits")
	}

	// `library` is the one catalogued Mechanism that needs collaborators derived for it, so building
	// it here is what proves the build goes through the engine's own derivation rather than around it.
	entry := config.ServerEntry{
		Name:       "grunt",
		Endpoint:   "http://127.0.0.1:2222",
		SubAgents:  true,
		Mechanisms: map[string]bool{"library": true, "error_enrichment": false},
	}
	catalogue, err := subAgentCatalogue(entry, base)
	if err != nil {
		t.Fatalf("subAgentCatalogue with a library arm: %v", err)
	}
	if catalogue == nil {
		t.Fatal("a present map built no catalogue")
	}
	if first, second := catalogue(), catalogue(); first == nil || first == second {
		t.Error("the factory must hand each child its own registry")
	}

	// A map that enables nothing is still a map: replace-whole means the child runs with no Mechanism
	// at all, which is a catalogue rather than an inheritance.
	off, err := subAgentCatalogue(config.ServerEntry{Name: "grunt", Mechanisms: map[string]bool{"library": false}}, base)
	if err != nil {
		t.Fatalf("subAgentCatalogue with an all-false map: %v", err)
	}
	if off == nil {
		t.Error("an all-false map inherited the parent's catalogue; want an empty one of its own")
	}
}

// A typo in the flagged entry's posture is a defect in the file, and it fails the run at the startup
// boundary naming the entry — the same posture the session's own `mechanisms:` block gets, because a
// posture that silently armed nothing would be invisible for months.
func TestNewDelegationWiringRefusesADefectiveMechanismsMap(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{{
		Name:       "grunt",
		Endpoint:   "http://127.0.0.1:2222",
		SubAgents:  true,
		Mechanisms: map[string]bool{"libary": true},
	}}
	_, err := newDelegationWiring(entries, validCfg(t), &delegationSpy{}, func() []profiles.Entry { return nil })
	if err == nil {
		t.Fatal("a misspelled mechanism key was accepted; want the run refused")
	}
	if !strings.Contains(err.Error(), "libary") || !strings.Contains(err.Error(), "grunt") {
		t.Errorf("error = %q; want it to name both the key and the entry that asked for it", err)
	}
}

// The second monitor beats from the moment the renderer starts, which on a pre-bound session is
// before any Agent exists (ADR 0036 decision 3). The target is therefore REMEMBERED and latched at
// the bind — otherwise a Sub-agent server that was resolved and usable would sit unrouted until its
// next beat, for up to a whole interval after the human picked their server.
func TestLateEngineRemembersTheDelegationTargetUntilTheBind(t *testing.T) {
	t.Parallel()

	engine := newLateEngine(domain.ModeAskBefore, true)
	t.Cleanup(func() { _ = engine.Close() })

	target := &apogee.DelegationTarget{Endpoint: "http://127.0.0.1:2222", Model: "loaded-model", ParallelAgents: 2}
	engine.SetDelegationTarget(target)
	if engine.pendingDelegation != target {
		t.Fatalf("pendingDelegation = %+v; want the target held for the bind", engine.pendingDelegation)
	}

	if err := engine.Bind(func() (*apogee.Agent, error) { return apogee.New(validCfg(t)) }); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	// A cleared target is remembered as cleared: nothing usable was resolved, and a later bind must
	// not resurrect the server the last beat said was gone.
	engine.SetDelegationTarget(nil)
	if engine.pendingDelegation != nil {
		t.Errorf("pendingDelegation after a clear = %+v; want nil", engine.pendingDelegation)
	}
}

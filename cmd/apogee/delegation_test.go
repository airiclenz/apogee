package main

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/profiles"
	"github.com/airiclenz/apogee/internal/provider"
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
// The key the beat is handed is recorded, because "the beat authenticates with the key the entry's
// source resolved to" is itself a claim these tests make.
func beatSource(observed heartbeat.Beat) func(context.Context, string) heartbeat.Beat {
	return func(context.Context, string) heartbeat.Beat { return observed }
}

// keyedBeatSource is beatSource with the key written down: the pointer it fills is what a test reads
// to see which token the Sub-agent server was actually observed with.
func keyedBeatSource(observed heartbeat.Beat, seen *string) func(context.Context, string) heartbeat.Beat {
	return func(_ context.Context, apiKey string) heartbeat.Beat {
		*seen = apiKey
		return observed
	}
}

// noProfiles is a host whose `model-profiles:` map is empty — the tier read every resolution takes,
// written out once because most of these tests are about something else.
func noProfiles() []profiles.Entry { return nil }

// noticeSpy records what the human was told, in order. Like delegationSpy it needs no lock: a notice
// is emitted on the beat's own goroutine, and the join the caller runs is what makes it readable.
type noticeSpy struct {
	notes []string
}

func (s *noticeSpy) add(note string) { s.notes = append(s.notes, note) }

// staticServerList is the `servers:` reader the wiring takes, over a list that never moves — what a
// test that does its editing through relist rather than through the holder needs.
func staticServerList(entries []config.ServerEntry) func() []config.ServerEntry {
	return func() []config.ServerEntry { return entries }
}

// testDelegationWiring assembles a wiring already holding one Sub-agent server, with the entry's
// beat written down rather than dialled. notices may be nil — the Driver that shows nothing.
func testDelegationWiring(
	entry config.ServerEntry,
	observed heartbeat.Beat,
	engine delegationSetter,
	notices *noticeSpy,
) *delegationWiring {
	wiring := &delegationWiring{
		server:       &subAgentServer{entry: entry, beat: beatSource(observed)},
		userProfiles: noProfiles,
		keys:         config.NewKeyResolver(""),
		engine:       engine,
	}
	if notices != nil {
		wiring.notify = notices.add
	}
	return wiring
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

	// The key is the RESOLVED one the caller hands in — the entry names a source, and turning that
	// source into a token is the resolver's job one layer up (delegationWiring.observe).
	target := resolveDelegationTarget(entry, "grunt-key", observed, nil, nil)
	if target == nil {
		t.Fatal("a reachable server resolved to no target; want the pinned one")
	}
	if target.Endpoint != entry.Endpoint || target.APIKey != "grunt-key" {
		t.Errorf("dial facts = %q/%q; want the entry's %q and the resolved key",
			target.Endpoint, target.APIKey, entry.Endpoint)
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

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	observed := heartbeat.Beat{Reachable: true, ActiveModel: "loaded-model", ContextWindow: 8192}

	target := resolveDelegationTarget(entry, "", observed, nil, nil)
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
	if wider := resolveDelegationTarget(entry, "", observed, nil, nil); wider.ParallelAgents != 4 {
		t.Errorf("ParallelAgents with 4 observed slots = %d; want 4", wider.ParallelAgents)
	}
}

// The share a routed child divides ITS window by is the flagged entry's `response-reserve:`, carried
// as WRITTEN — and this is the one target field the rank order above does not reach. There is no
// observed half (a server reports no split) and, on purpose, no top-level key to fall back to here:
// an entry stating no share leaves the child on the share the PARENT resolved (internal/agent's
// routed spawn), which already IS the top-level key when nobody overrode it. Resolving against that
// key a second time would state the parent's own answer as the ENTRY's, and the spawn — which reads
// "the entry said nothing" as a 0 — could then no longer tell the two apart.
func TestResolveDelegationTargetCarriesTheEntrysResponseReserve(t *testing.T) {
	t.Parallel()

	observed := heartbeat.Beat{Reachable: true, ActiveModel: "loaded-model", ContextWindow: 8192}

	stated := config.ServerEntry{
		Name:            "grunt",
		Endpoint:        "http://127.0.0.1:2222",
		ResponseReserve: 0.35,
	}
	target := resolveDelegationTarget(stated, "", observed, nil, nil)
	if target == nil {
		t.Fatal("a reachable server resolved to no target; want the one stating a share")
	}
	if target.ResponseReserveFraction != 0.35 {
		t.Errorf("ResponseReserveFraction = %v; want the entry's 0.35 carried as written",
			target.ResponseReserveFraction)
	}

	// And the absent key stays absent: nothing here invents a share for an entry that states none,
	// because 0 is precisely how the spawn is told to leave the parent's standing.
	unstated := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	quiet := resolveDelegationTarget(unstated, "", observed, nil, nil)
	if quiet == nil {
		t.Fatal("a reachable server resolved to no target; want the one stating nothing")
	}
	if quiet.ResponseReserveFraction != 0 {
		t.Errorf("ResponseReserveFraction = %v for an entry stating no share; want 0 — the child keeps "+
			"the share its parent resolved", quiet.ResponseReserveFraction)
	}
}

// The grunt model's dialect is resolved for the model that just bound on the SUB-AGENT server (ADR
// 0044 through ADR 0045): the session may be reading harmony channels while its delegations read
// `<think>` tags, so the match keys on the target's model and on the user tier this host holds.
func TestResolveDelegationTargetResolvesTheProfileForTheBoundModel(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	observed := heartbeat.Beat{Reachable: true, ActiveModel: "cheap-thinker-7b"}
	user := []profiles.Entry{{
		Pattern: "cheap-thinker",
		Profile: domain.ModelProfile{Thinking: domain.ThinkingProfile{Style: domain.ThinkingDelimited}},
	}}

	target := resolveDelegationTarget(entry, "", observed, user, nil)
	if target == nil {
		t.Fatal("a reachable server resolved to no target")
	}
	if got := target.Profile.Thinking.Style; got != domain.ThinkingDelimited {
		t.Errorf("Profile.Thinking.Style = %q; want the user entry matching the target's model", got)
	}
	// A model no tier knows keeps the zero profile — native tool calls, no inline thinking — which is
	// how an unprofiled delegation has always parsed.
	unmatched := resolveDelegationTarget(entry, "", heartbeat.Beat{Reachable: true, ActiveModel: "nobody-knows"}, user, nil)
	if !reflect.DeepEqual(unmatched.Profile, domain.ModelProfile{}) {
		t.Errorf("Profile for an unmatched model = %+v; want the zero profile", unmatched.Profile)
	}
}

// The posture keys ride the routing untranslated: `bypass:` travels as the entry's own pointer,
// because its NIL-ness is the inherit-versus-replace instruction the engine reads (ADR 0045 §2), and
// the Mechanism catalogue travels as the factory the entry's map built.
func TestResolveDelegationTargetCarriesThePostureVerbatim(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	observed := heartbeat.Beat{Reachable: true, ActiveModel: "loaded-model"}

	// Nothing on the entry: both postures are absent, and absent is what makes a child inherit.
	bare := resolveDelegationTarget(entry, "", observed, nil, nil)
	if bare.Bypass != nil {
		t.Errorf("Bypass with none on the entry = %v; want absent so the child inherits the parent's live flag", bare.Bypass)
	}
	if bare.Mechanisms != nil {
		t.Error("a catalogue was built for an entry with no `mechanisms:` map; want absent so the child inherits")
	}

	on := true
	entry.Bypass = &on
	catalogue := func() *apogee.MechanismRegistry { return apogee.NewMechanismRegistry() }
	dressed := resolveDelegationTarget(entry, "", observed, nil, catalogue)
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

// The dialect follows the same rank order every other field does (ADR 0060 §3): the entry's forced
// `effort-dialect:` outranks the tell the beat saw, the beat answers what the file left open, and
// with neither the zero says this target names none — which leaves a routed child on the SESSION
// server's shape (internal/agent's routed spawn) and is what the advice below exists for.
func TestResolveDelegationTargetRanksTheEffortDialect(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	observed := heartbeat.Beat{
		Reachable:     true,
		ActiveModel:   "loaded-model",
		EffortSupport: provider.EffortSupport{Supported: true, Dialect: provider.EffortDialectReasoning},
	}

	forced := entry
	forced.EffortDialect = "kwargs"
	if got := resolveDelegationTarget(forced, "", observed, nil, nil).EffortDialect; got != provider.EffortDialectKwargs {
		t.Errorf("dialect with a pin = %q; want the entry's forced %q", got, provider.EffortDialectKwargs)
	}
	if got := resolveDelegationTarget(entry, "", observed, nil, nil).EffortDialect; got != provider.EffortDialectReasoning {
		t.Errorf("dialect with no pin = %q; want the beat's observed %q", got, provider.EffortDialectReasoning)
	}

	tellLess := observed
	tellLess.EffortSupport = provider.EffortSupport{}
	if got := resolveDelegationTarget(entry, "", tellLess, nil, nil).EffortDialect; got != provider.EffortDialectNone {
		t.Errorf("dialect with neither = %q; want the zero that names none", got)
	}
}

// An unusable Sub-agent server is not an error, it is the FALLBACK: nil says "not routing", and the
// engine reads that as the parent's Upstream with the parent's posture — today's behaviour (ADR 0042).
func TestResolveDelegationTargetRefusesAnUnusableBeat(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}

	unreachable := heartbeat.Beat{Failure: "connection refused"}
	if target := resolveDelegationTarget(entry, "", unreachable, nil, nil); target != nil {
		t.Errorf("an unreachable server resolved to %+v; want no target", target)
	}
	// Reachable but serving nothing this session can name, and no pin to name it with: a delegation
	// that cannot say which model it is talking to is not a usable target either.
	nameless := heartbeat.Beat{Reachable: true}
	if target := resolveDelegationTarget(entry, "", nameless, nil, nil); target != nil {
		t.Errorf("a server with no model bound resolved to %+v; want no target", target)
	}
	// The pin is what rescues that case: the file names the model the server will serve.
	entry.Model = "pinned-model"
	if target := resolveDelegationTarget(entry, "", nameless, nil, nil); target == nil || target.Model != "pinned-model" {
		t.Errorf("target with a pinned model on a model-less beat = %+v; want the pin", target)
	}
}

// An unset `sub-agents-server:` is the ordinary session — the DEFAULT since the key replaced the
// entry flag — and it must stay bit-for-bit ordinary: no second Monitor is constructed, no beat
// runs, no notice is said and the latch is never written, so a delegation goes exactly where it went
// before routing existed.
func TestNewDelegationWiringWithoutATargetObservesNothing(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{
		{Name: "here", Endpoint: "http://127.0.0.1:1111"},
		{Name: "there", Endpoint: "http://127.0.0.1:2222"},
	}
	spy := &delegationSpy{}
	notices := &noticeSpy{}
	wiring, err := newDelegationWiring(
		"", staticServerList(entries), validCfg(t), spy, noProfiles, notices.add, config.NewKeyResolver(""))
	if err != nil {
		t.Fatalf("newDelegationWiring with no target named: %v", err)
	}
	if wiring.server != nil {
		t.Error("a monitor was constructed for a config that named no Sub-agent server")
	}
	wiring.observe(context.Background())()
	if len(spy.pushes) != 0 {
		t.Errorf("pushes = %d; want the latch never written without a Sub-agent server", len(spy.pushes))
	}
	if len(notices.notes) != 0 {
		t.Errorf("notices = %q; want an unset key to say nothing at all", notices.notes)
	}
}

// The key names the entry, so the wiring builds against THAT entry wherever it sits in the list —
// and against no other, which is what a list carrying two plausible grunt boxes proves.
func TestNewDelegationWiringBuildsTheNamedEntry(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{
		{Name: "here", Endpoint: "http://127.0.0.1:1111"},
		{Name: "grunt", Endpoint: "http://127.0.0.1:2222", Model: "qwen3-4b"},
		{Name: "other-grunt", Endpoint: "http://127.0.0.1:3333"},
	}
	wiring, err := newDelegationWiring(
		"grunt", staticServerList(entries), validCfg(t), &delegationSpy{}, noProfiles, nil, config.NewKeyResolver(""))
	if err != nil {
		t.Fatalf("newDelegationWiring: %v", err)
	}
	if wiring.server == nil || wiring.server.entry.Name != "grunt" {
		t.Fatalf("wired server = %+v; want the entry the key names", wiring.server)
	}
	if wiring.missingNotice != "" {
		t.Errorf("missingNotice = %q; want nothing to be missing when the key names an entry", wiring.missingNotice)
	}
}

// A key naming an entry the list does not carry is not a refusal (SubAgentsServerTarget): nothing is
// observed, delegations fall back to the session's own server, and ONE sentence says which name went
// missing AND which names the file does carry — the whole defect being a spelling, the second half
// is what saves the human a trip into the file.
//
// It is said on the first OBSERVE rather than at construction, because the notify seam is the
// Bridge's and a send before Run has bound the program goes nowhere.
func TestNewDelegationWiringSaysWhichNameWentMissing(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{
		{Name: "here", Endpoint: "http://127.0.0.1:1111"},
		{Name: "there", Endpoint: "http://127.0.0.1:2222"},
	}
	spy := &delegationSpy{}
	notices := &noticeSpy{}
	wiring, err := newDelegationWiring(
		"grunt", staticServerList(entries), validCfg(t), spy, noProfiles, notices.add, config.NewKeyResolver(""))
	if err != nil {
		t.Fatalf("newDelegationWiring with a stale name: %v", err)
	}
	if wiring.server != nil {
		t.Error("a monitor was constructed for a name no entry carries")
	}
	if len(notices.notes) != 0 {
		t.Fatalf("notices at construction = %q; want the notice held until the first beat", notices.notes)
	}

	wiring.observe(context.Background())()
	want := `sub-agents: no servers entry named "grunt" — delegations run on the session server ` +
		`(configured: here, there)`
	if len(notices.notes) != 1 || notices.notes[0] != want {
		t.Fatalf("notices = %q; want exactly %q", notices.notes, want)
	}

	wiring.observe(context.Background())()
	if len(notices.notes) != 1 {
		t.Errorf("notices after a second beat = %q; want the missing name said once", notices.notes)
	}
}

// The whole cycle at the seam the renderer's cadence drives: one beat on the Sub-agent server, one
// resolved target latched. A beat that finds the server gone latches nil in its place — the routing
// state changes on the same clock the server does, without waiting for a delegation to discover it.
func TestDelegationWiringObservePushesWhatTheBeatResolvedTo(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222", ParallelAgents: 2}
	spy := &delegationSpy{}
	wiring := testDelegationWiring(entry,
		heartbeat.Beat{Reachable: true, ActiveModel: "loaded-model", ContextWindow: 8192}, spy, nil)

	wiring.observe(context.Background())()
	if len(spy.pushes) != 1 || spy.pushes[0] == nil {
		t.Fatalf("pushes after a usable beat = %+v; want one resolved target", spy.pushes)
	}
	if got := spy.pushes[0]; got.Model != "loaded-model" || got.ContextWindow != 8192 || got.ParallelAgents != 2 {
		t.Errorf("latched target = %+v; want the beat's model/window and the entry's cap", got)
	}

	wiring.server.beat = beatSource(heartbeat.Beat{Failure: "connection refused"})
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

	absent, err := subAgentCatalogue(config.ServerEntry{Name: "grunt"}, base)
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

// The posture keys are legal on EVERY entry now, which the flag era refused: the config loads, and
// the wiring builds the posture of the entry the key names — not of the one that merely carries a
// map. Which entry takes the delegations is the root key's answer, so the posture follows the target
// rather than a per-entry flag.
func TestNewDelegationWiringTakesThePostureOfTheNamedEntry(t *testing.T) {
	t.Parallel()

	base := validCfg(t)
	base.LibraryDir = t.TempDir()
	off, on := false, true
	entries := []config.ServerEntry{
		{
			Name: "here", Endpoint: "http://127.0.0.1:1111", Bypass: &off,
			Mechanisms: map[string]bool{"library": true},
		},
		{
			Name: "grunt", Endpoint: "http://127.0.0.1:2222", Bypass: &on,
			Mechanisms: map[string]bool{"error_enrichment": false},
		},
	}
	if err := config.ValidateServers(entries); err != nil {
		t.Fatalf("posture on an entry the key does not name: %v; want the list accepted", err)
	}

	wiring, err := newDelegationWiring(
		"grunt", staticServerList(entries), base, &delegationSpy{}, noProfiles, nil, config.NewKeyResolver(""))
	if err != nil {
		t.Fatalf("newDelegationWiring: %v", err)
	}
	if wiring.server == nil || wiring.server.entry.Bypass == nil || !*wiring.server.entry.Bypass {
		t.Fatalf("wired posture = %+v; want the named entry's own bypass", wiring.server)
	}
	if wiring.server.catalogue == nil {
		t.Error("the named entry's `mechanisms:` map built no catalogue")
	}
}

// A name that goes missing mid-session — the entry deleted while the key still points at it — ends
// routing exactly as clearing the key does, and says which name went missing on the next beat. A
// later edit ELSEWHERE re-renders the same sentence, which is not news and is not said twice.
func TestDelegationRelistSaysWhenTheNamedEntryLeavesTheList(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	spy := &delegationSpy{}
	notices := &noticeSpy{}
	wiring := testDelegationWiring(entry, heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, notices)
	wiring.observe(context.Background())()

	remaining := []config.ServerEntry{{Name: "here", Endpoint: "http://127.0.0.1:1111"}}
	if err := wiring.relist("grunt", remaining); err != nil {
		t.Fatalf("relist with the named entry deleted: %v", err)
	}
	if wiring.server != nil {
		t.Errorf("server after its entry was deleted = %+v; want none observed", wiring.server)
	}
	if len(spy.pushes) != 2 || spy.pushes[1] != nil {
		t.Fatalf("pushes = %+v; want the edit to clear the latch", spy.pushes)
	}

	said := len(notices.notes)
	wiring.observe(context.Background())()
	want := `sub-agents: no servers entry named "grunt" — delegations run on the session server ` +
		`(configured: here)`
	if len(notices.notes) != said+1 || notices.notes[said] != want {
		t.Fatalf("notices = %q; want %q said once the entry went missing", notices.notes, want)
	}

	if err := wiring.relist("grunt", remaining); err != nil {
		t.Fatalf("relist with the name still missing: %v", err)
	}
	wiring.observe(context.Background())()
	if len(notices.notes) != said+1 {
		t.Errorf("notices = %q; want the same missing name not said twice", notices.notes)
	}
}

// A typo in the named entry's posture is a defect in the file, and it fails the run at the startup
// boundary naming the entry — the same posture the session's own `mechanisms:` block gets, because a
// posture that silently armed nothing would be invisible for months.
func TestNewDelegationWiringRefusesADefectiveMechanismsMap(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{{
		Name:       "grunt",
		Endpoint:   "http://127.0.0.1:2222",
		Mechanisms: map[string]bool{"libary": true},
	}}
	_, err := newDelegationWiring(
		"grunt", staticServerList(entries), validCfg(t), &delegationSpy{}, noProfiles, nil, config.NewKeyResolver(""))
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

// ----------------------------------------------------------------------------
// The routing notices (ADR 0045 §4)
// ----------------------------------------------------------------------------

// One notice per routing STATE CHANGE and not one more: a server that keeps answering is not news on
// every beat, and a server that stays down is not news on every beat either. The states are the two
// the human cares about — delegations go to the flagged box, or they fall back to this session's own
// server — so the whole state machine is exercised by driving a server through both twice.
func TestDelegationNoticesOnlyOnARoutingStateChange(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	up := heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}
	down := heartbeat.Beat{Failure: "connection refused"}
	notices := &noticeSpy{}
	wiring := testDelegationWiring(entry, up, &delegationSpy{}, notices)

	beat := func(observed heartbeat.Beat) {
		t.Helper()
		wiring.server.beat = beatSource(observed)
		wiring.observe(context.Background())()
	}
	beat(up)   // engaged: said once
	beat(up)   // still engaged: nothing to say
	beat(down) // lost: said once
	beat(down) // still lost: nothing to say
	beat(up)   // recovered: said again

	// The dialect advice rides the FIRST engagement and is not repeated on the recovery: it
	// describes the entry the human wrote, and a server going down and coming back changed nothing
	// they could act on (dialectAdvice).
	want := []string{
		"sub-agents: routing to grunt (cheap-7b)",
		"sub-agents: grunt advertises no thinking-effort dialect — delegates there speak this session's; set effort-dialect: on its entry",
		"sub-agents: grunt unavailable — delegations run on the session server",
		"sub-agents: routing to grunt (cheap-7b)",
	}
	if len(notices.notes) != len(want) {
		t.Fatalf("notices = %q; want exactly one per transition plus the one-off dialect advice: %q",
			notices.notes, want)
	}
	for i, w := range want {
		if notices.notes[i] != w {
			t.Errorf("notice %d = %q; want %q", i, notices.notes[i], w)
		}
	}
}

// A flagged server that was never reachable degrades VISIBLY (ADR 0042): the first resolved state is
// worth a notice whichever way it goes, or a human who configured routing and got none would have
// nothing at all to read.
func TestDelegationNoticesTheFirstStateEvenWhenItIsUnusable(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	notices := &noticeSpy{}
	wiring := testDelegationWiring(entry, heartbeat.Beat{Failure: "connection refused"}, &delegationSpy{}, notices)

	wiring.observe(context.Background())()
	if len(notices.notes) != 1 ||
		notices.notes[0] != "sub-agents: grunt unavailable — delegations run on the session server" {
		t.Fatalf("notices = %q; want the unavailable line once", notices.notes)
	}
}

// A beat resolved against a server the file has since re-pointed is dropped whole — never latched,
// never narrated. Without that guard an observation in flight when the edit landed would restore the
// old server's target and hold it for a whole interval, which is the one thing a live edit must not
// leave behind.
func TestDelegationDropsALandingFromASupersededServer(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	spy := &delegationSpy{}
	notices := &noticeSpy{}
	wiring := testDelegationWiring(entry, heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, notices)

	wiring.land(wiring.generation-1, "grunt", &apogee.DelegationTarget{Model: "cheap-7b"}, nil)
	if len(spy.pushes) != 0 || len(notices.notes) != 0 {
		t.Errorf("a superseded landing pushed %+v and said %q; want neither", spy.pushes, notices.notes)
	}
}

// gatedDelegationSpy is a delegationSpy that can be stopped INSIDE a push, which is the only way a
// test can stand in the window an ordering is decided in: the FIRST push announces that it arrived
// and then waits to be released, so the test can run a config edit while a beat's landing is half
// done. Every later push — the edit's own — passes straight through, or the edit could never overtake
// the landing this is built to let it overtake.
type gatedDelegationSpy struct {
	mu      sync.Mutex
	pushes  []*apogee.DelegationTarget
	gated   bool
	arrived chan struct{} // closed by the first push, once it is in
	release chan struct{} // closed by the test, to let that push finish
}

func (s *gatedDelegationSpy) SetDelegationTarget(target *apogee.DelegationTarget) {
	s.mu.Lock()
	first := !s.gated
	s.gated = true
	s.mu.Unlock()
	if first {
		close(s.arrived)
		<-s.release
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pushes = append(s.pushes, target)
}

func (s *gatedDelegationSpy) snapshot() []*apogee.DelegationTarget {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*apogee.DelegationTarget(nil), s.pushes...)
}

// A beat that resolved a usable target and an edit that unflags the server can be in flight at the
// same instant, and only one order of the two is survivable. The edit must have the LAST word: let the
// superseded landing push after it and routing stays engaged against a server the file no longer
// flags — and with the flag gone there is no further beat to correct it, which is what makes this
// interleaving worse than the one the generation guard already drops.
//
// So the landing is held inside its own push and the edit is given that whole window to run in. It
// gets there only if the push happens outside the wiring's lock; under the lock it waits, lands after,
// and its nil is the last word either way.
func TestDelegationLandLosesToAnEditThatDropsTheServer(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	gate := &gatedDelegationSpy{arrived: make(chan struct{}), release: make(chan struct{})}
	wiring := testDelegationWiring(entry, heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, gate, nil)
	generation := wiring.generation

	landed := make(chan struct{})
	go func() {
		defer close(landed)
		wiring.land(generation, entry.Name, &apogee.DelegationTarget{Model: "cheap-7b"}, nil)
	}()
	<-gate.arrived // the beat is mid-push, which is the only place the edit can overtake it

	relisted := make(chan struct{})
	go func() {
		defer close(relisted)
		if err := wiring.relist("", []config.ServerEntry{{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}}); err != nil {
			t.Errorf("relist clearing the target: %v", err)
		}
	}()
	// The edit either finishes inside the window — the ordering under test — or is still held out of it
	// when the wait expires, and the assertion is the same for both: what the engine is left holding.
	select {
	case <-relisted:
	case <-time.After(50 * time.Millisecond):
	}
	close(gate.release)
	<-landed
	<-relisted

	pushes := gate.snapshot()
	if len(pushes) != 2 || pushes[len(pushes)-1] != nil {
		t.Fatalf("pushes = %+v; want the edit's nil to be the last word, so nothing stays routed", pushes)
	}
}

// ----------------------------------------------------------------------------
// The live `servers:` lifecycle (ADR 0037/0041 through ADR 0045)
// ----------------------------------------------------------------------------

// Naming a server mid-session starts observing it: nothing is latched by the edit itself — nothing
// has been observed yet — and the next beat resolves the target and says so.
func TestDelegationRelistNamesATarget(t *testing.T) {
	t.Parallel()

	spy := &delegationSpy{}
	notices := &noticeSpy{}
	wiring, err := newDelegationWiring("",
		staticServerList([]config.ServerEntry{{Name: "here", Endpoint: "http://127.0.0.1:1111"}}),
		validCfg(t), spy, noProfiles, notices.add, config.NewKeyResolver(""))
	if err != nil {
		t.Fatalf("newDelegationWiring: %v", err)
	}

	added := []config.ServerEntry{
		{Name: "here", Endpoint: "http://127.0.0.1:1111"},
		{Name: "grunt", Endpoint: "http://127.0.0.1:2222"},
	}
	if err := wiring.relist("grunt", added); err != nil {
		t.Fatalf("relist naming a target: %v", err)
	}
	if wiring.server == nil || wiring.server.entry.Name != "grunt" {
		t.Fatalf("server after the target was named = %+v; want the grunt entry observed", wiring.server)
	}
	if len(spy.pushes) != 0 || len(notices.notes) != 0 {
		t.Errorf("the edit itself pushed %+v and said %q; want both to wait for the first beat",
			spy.pushes, notices.notes)
	}

	wiring.server.beat = beatSource(heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"})
	wiring.observe(context.Background())()
	if len(spy.pushes) != 1 || spy.pushes[0] == nil || spy.pushes[0].Endpoint != "http://127.0.0.1:2222" {
		t.Fatalf("pushes after the first beat = %+v; want the new server's target", spy.pushes)
	}
	if len(notices.notes) != 2 || notices.notes[0] != "sub-agents: routing to grunt (cheap-7b)" {
		t.Errorf("notices = %q; want the routing line for the newly named server, then its dialect advice",
			notices.notes)
	}
}

// Clearing the key ends routing THERE AND THEN: the beat stops and the latch is cleared by the edit
// itself, because a target left standing for another interval would keep sending delegations to a
// server the file no longer names.
func TestDelegationRelistClearsTheTarget(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	spy := &delegationSpy{}
	wiring := testDelegationWiring(entry, heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, nil)
	wiring.observe(context.Background())()

	if err := wiring.relist("", []config.ServerEntry{{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}}); err != nil {
		t.Fatalf("relist clearing the target: %v", err)
	}
	if wiring.server != nil {
		t.Errorf("server after the key was cleared = %+v; want none observed", wiring.server)
	}
	if len(spy.pushes) != 2 || spy.pushes[1] != nil {
		t.Fatalf("pushes = %+v; want the edit to clear the latch", spy.pushes)
	}
	// And nothing beats any more: the cadence still calls observe, and it has nothing to observe.
	wiring.observe(context.Background())()
	if len(spy.pushes) != 2 {
		t.Errorf("pushes after a beat with no Sub-agent server = %+v; want none added", spy.pushes)
	}
}

// Moving the key to another entry is both of the above in one edit: what was latched dials a server
// the file no longer names, so it is cleared now, and the new server announces itself on its own
// first beat rather than inheriting the old one's state.
func TestDelegationRelistRePointsTheTarget(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	spy := &delegationSpy{}
	notices := &noticeSpy{}
	wiring := testDelegationWiring(entry, heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, notices)
	wiring.observe(context.Background())()

	moved := []config.ServerEntry{
		{Name: "grunt", Endpoint: "http://127.0.0.1:2222"},
		{Name: "cheaper", Endpoint: "http://127.0.0.1:3333"},
	}
	if err := wiring.relist("cheaper", moved); err != nil {
		t.Fatalf("relist re-pointing the target: %v", err)
	}
	if wiring.server == nil || wiring.server.entry.Name != "cheaper" {
		t.Fatalf("server after the re-point = %+v; want the cheaper entry", wiring.server)
	}
	if len(spy.pushes) != 2 || spy.pushes[1] != nil {
		t.Fatalf("pushes = %+v; want the old server's target cleared by the edit", spy.pushes)
	}

	wiring.server.beat = beatSource(heartbeat.Beat{Reachable: true, ActiveModel: "tiny-3b"})
	wiring.observe(context.Background())()
	// Four: each server announced itself and then advised about its own missing dialect — the
	// advice latch is forgotten with the rest of the routing state when the target moves.
	if len(notices.notes) != 4 || notices.notes[2] != "sub-agents: routing to cheaper (tiny-3b)" {
		t.Errorf("notices = %q; want the second server to announce itself", notices.notes)
	}
}

// An edit to the named entry ITSELF — a posture key, a pin — is not a change of server: routing
// carries on uninterrupted, nothing is said again, and the very next beat resolves against what the
// file now says. That last part is the freshness rule: no edit may leave a stale posture latched
// past one beat.
func TestDelegationRelistEditsTheNamedEntryInPlace(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	spy := &delegationSpy{}
	notices := &noticeSpy{}
	wiring := testDelegationWiring(entry, heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, notices)
	wiring.observe(context.Background())()
	if pushed := spy.pushes[0]; pushed.Bypass != nil || pushed.ContextWindow != 0 {
		t.Fatalf("the first target = %+v; want no posture and no window before the edit", pushed)
	}

	bypassed := entry
	on := true
	bypassed.Bypass, bypassed.ContextWindow = &on, 16384
	if err := wiring.relist("grunt", []config.ServerEntry{bypassed}); err != nil {
		t.Fatalf("relist editing the entry: %v", err)
	}
	if len(spy.pushes) != 1 {
		t.Fatalf("pushes = %+v; want routing left engaged by an edit to the same server", spy.pushes)
	}

	wiring.server.beat = beatSource(heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"})
	wiring.observe(context.Background())()
	next := spy.pushes[len(spy.pushes)-1]
	if next == nil || next.Bypass == nil || !*next.Bypass || next.ContextWindow != 16384 {
		t.Errorf("the next resolved target = %+v; want the edited posture and pin", next)
	}
	if len(notices.notes) != 2 {
		t.Errorf("notices = %q; want the same server not to re-announce itself or re-advise", notices.notes)
	}
}

// A `servers:` edit that leaves the Sub-agent server alone — the common case, since most of that
// list is about other servers — costs a comparison and nothing else: no new Monitor, no push, and
// no beat in flight invalidated.
func TestDelegationRelistIgnoresAnEditElsewhereInTheList(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	spy := &delegationSpy{}
	wiring := testDelegationWiring(entry, heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, nil)
	wiring.observe(context.Background())()
	server, generation := wiring.server, wiring.generation

	elsewhere := []config.ServerEntry{
		{Name: "here", Endpoint: "http://127.0.0.1:1111", ParallelAgents: 4},
		entry,
	}
	if err := wiring.relist("grunt", elsewhere); err != nil {
		t.Fatalf("relist with an untouched Sub-agent server: %v", err)
	}
	if wiring.server != server || wiring.generation != generation {
		t.Error("an edit elsewhere in the list replaced the Sub-agent server")
	}
	if len(spy.pushes) != 1 {
		t.Errorf("pushes = %+v; want the edit to touch nothing", spy.pushes)
	}
}

// A live edit is validate-then-commit, exactly as the startup build is loud: a named entry whose
// `mechanisms:` map this build does not know is refused with NOTHING installed, so the session keeps
// routing where it was routing while the human fixes the file.
func TestDelegationRelistRefusesADefectiveMechanismsMap(t *testing.T) {
	t.Parallel()

	base := validCfg(t)
	base.LibraryDir = t.TempDir()
	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	spy := &delegationSpy{}
	wiring := testDelegationWiring(entry, heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, nil)
	wiring.base = base
	wiring.observe(context.Background())()

	defective := entry
	defective.Mechanisms = map[string]bool{"libary": true}
	err := wiring.relist("grunt", []config.ServerEntry{defective})
	if err == nil {
		t.Fatal("a misspelled mechanism key was accepted; want the edit refused")
	}
	if !strings.Contains(err.Error(), "libary") || !strings.Contains(err.Error(), "grunt") {
		t.Errorf("error = %q; want it to name both the key and the entry that asked for it", err)
	}
	if wiring.server.entry.Mechanisms != nil || len(spy.pushes) != 1 {
		t.Errorf("server = %+v and pushes = %+v; want the refused edit to have installed nothing",
			wiring.server.entry, spy.pushes)
	}
}

// The target is named by a root key read out of the SAME file the `servers:` list lives in, so the
// door a live edit reaches routing through is that list's own apply (ADR 0037's dispatcher) — the
// same one an editor's exit and the watcher's report both end in, and it carries both halves at
// once. This is the wiring of that door: the file names a Sub-agent server, and the session starts
// observing it without a relaunch.
func TestApplySettingServersDrivesTheSubAgentServer(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	launchOpts := config.Options{Servers: []config.ServerEntry{{Name: "local", Endpoint: "http://127.0.0.1:1111"}}}
	live := newLiveSettings(launchOpts, nil)
	spy := &delegationSpy{}
	wiring := &delegationWiring{userProfiles: noProfiles, engine: spy}
	apply := applySettingFor(settingsApplier{live: live, configPath: path, delegation: wiring})

	writeSettingsFixture(t, path, "servers:\n"+
		"  - name: local\n    endpoint: http://127.0.0.1:1111\n"+
		"  - name: grunt\n    endpoint: http://127.0.0.1:2222\n"+
		"sub-agents-server: grunt\n")
	if _, err := apply("servers", "2 servers"); err != nil {
		t.Fatalf("apply servers naming a target: %v", err)
	}
	if wiring.server == nil || wiring.server.entry.Name != "grunt" {
		t.Fatalf("Sub-agent server after the edit = %+v; want the named grunt entry", wiring.server)
	}

	// A named entry this build refuses is refused BEFORE anything is installed, so the session
	// keeps both the list and the routing it was already running.
	writeSettingsFixture(t, path, "servers:\n"+
		"  - name: local\n    endpoint: http://127.0.0.1:1111\n"+
		"  - name: grunt\n    endpoint: http://127.0.0.1:2222\n"+
		"    mechanisms:\n      libary: true\n"+
		"sub-agents-server: grunt\n")
	if _, err := apply("servers", "2 servers"); err == nil {
		t.Fatal("apply of a defective `mechanisms:` map: want the refusal, got none")
	}
	if wiring.server.entry.Mechanisms != nil || len(live.serverList()) != 2 {
		t.Errorf("refused edit installed %+v and a %d-entry list; want neither touched",
			wiring.server.entry, len(live.serverList()))
	}

	writeSettingsFixture(t, path, "servers:\n  - name: local\n    endpoint: http://127.0.0.1:1111\n")
	if _, err := apply("servers", "1 server"); err != nil {
		t.Fatalf("apply servers clearing the target: %v", err)
	}
	if wiring.server != nil {
		t.Errorf("Sub-agent server after the key was cleared = %+v; want none", wiring.server)
	}
	if len(spy.pushes) != 1 || spy.pushes[0] != nil {
		t.Errorf("pushes = %+v; want exactly the edit's clearing push", spy.pushes)
	}
}

// ----------------------------------------------------------------------------
// The mid-session retarget (the `/sub-agents-server` seam)
// ----------------------------------------------------------------------------

// retargetableWiring is a wiring already routing to entry, with everything a RETARGET needs beside
// it: the live `servers:` list a name is resolved against and the session Config an entry's posture
// is built from. testDelegationWiring stops short of both, because every test before this one moved
// routing through the file rather than through the session.
func retargetableWiring(
	t *testing.T,
	entry config.ServerEntry,
	entries []config.ServerEntry,
	observed heartbeat.Beat,
	engine delegationSetter,
	notices *noticeSpy,
) *delegationWiring {
	t.Helper()
	wiring := testDelegationWiring(entry, observed, engine, notices)
	wiring.target, wiring.configured = entry.Name, entry.Name
	wiring.servers = staticServerList(entries)
	wiring.base = validCfg(t)
	return wiring
}

// The pick's whole act: the next spawn goes to the server the human just named. The latch is cleared
// on the spot rather than at the next beat — a delegation made in the seconds after the pick must not
// still reach the box they moved off — and the new server announces itself once, when it is first
// observed, through the same notice path every other routing change uses.
func TestDelegationRetargetMovesTheNextSpawnsServer(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{
		{Name: "grunt", Endpoint: "http://127.0.0.1:2222"},
		{Name: "cheaper", Endpoint: "http://127.0.0.1:3333"},
	}
	spy := &delegationSpy{}
	notices := &noticeSpy{}
	wiring := retargetableWiring(t, entries[0], entries,
		heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, notices)
	wiring.observe(context.Background())()

	if err := wiring.Retarget("cheaper"); err != nil {
		t.Fatalf("Retarget onto a configured entry: %v", err)
	}
	if wiring.server == nil || wiring.server.entry.Name != "cheaper" {
		t.Fatalf("server after the pick = %+v; want the cheaper entry", wiring.server)
	}
	if len(spy.pushes) != 2 || spy.pushes[1] != nil {
		t.Fatalf("pushes = %+v; want the old server unlatched by the pick itself", spy.pushes)
	}

	wiring.server.beat = beatSource(heartbeat.Beat{Reachable: true, ActiveModel: "tiny-3b"})
	wiring.observe(context.Background())()
	if len(spy.pushes) != 3 || spy.pushes[2] == nil || spy.pushes[2].Endpoint != "http://127.0.0.1:3333" {
		t.Fatalf("pushes = %+v; want the next beat latching the picked server", spy.pushes)
	}
	// Four: each server announced itself and then advised about its own missing dialect — the pick
	// forgets that latch with the rest of the routing state, exactly as a file edit does.
	if len(notices.notes) != 4 || notices.notes[2] != "sub-agents: routing to cheaper (tiny-3b)" {
		t.Fatalf("notices = %q; want the picked server to announce itself", notices.notes)
	}

	// Per state change, never per spawn (ADR 0045 §4): the server is the same one it was a beat ago,
	// so a second beat on it is not news.
	wiring.observe(context.Background())()
	if len(notices.notes) != 4 {
		t.Errorf("notices after a second beat = %q; want the routing change said once", notices.notes)
	}
}

// The pick is allowed while the agent RUNS, so a beat resolved against the old server can still be in
// flight when it lands. It is dropped whole — the generation the pick bumped is what drops it — or
// the old box would take the delegations for a whole interval after the human moved off it, with no
// further beat coming to correct it.
func TestDelegationRetargetMidRunDropsTheOldServersLanding(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{
		{Name: "grunt", Endpoint: "http://127.0.0.1:2222"},
		{Name: "cheaper", Endpoint: "http://127.0.0.1:3333"},
	}
	spy := &delegationSpy{}
	notices := &noticeSpy{}
	wiring := retargetableWiring(t, entries[0], entries,
		heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, notices)
	wiring.observe(context.Background())()
	inFlight := wiring.generation

	if err := wiring.Retarget("cheaper"); err != nil {
		t.Fatalf("Retarget mid-run: %v", err)
	}
	wiring.land(inFlight, "grunt", &apogee.DelegationTarget{Model: "cheap-7b"}, nil)

	if len(spy.pushes) != 2 || spy.pushes[1] != nil {
		t.Fatalf("pushes = %+v; want the old server's landing dropped, not re-latched", spy.pushes)
	}
	if len(notices.notes) != 2 {
		t.Errorf("notices = %q; want the dropped landing to narrate nothing", notices.notes)
	}
}

// racedClearAgainstALanding runs one clearing seam — a pick or a relist — against a beat landing on
// the generation that seam has just bumped, and answers what the engine was left holding. The
// clearing push is held INSIDE the gate, which is the only place the landing can overtake it, and the
// landing is then given that whole window to run in: it gets there only if the clear pushes outside
// the wiring's lock.
func racedClearAgainstALanding(
	t *testing.T,
	wiring *delegationWiring,
	gate *gatedDelegationSpy,
	clear func() error,
	landing string,
) []*apogee.DelegationTarget {
	t.Helper()

	cleared := make(chan struct{})
	go func() {
		defer close(cleared)
		if err := clear(); err != nil {
			t.Errorf("the clearing edit: %v", err)
		}
	}()
	<-gate.arrived // the clear is mid-push, which is the only window the landing can win

	landed := make(chan struct{})
	go func() {
		defer close(landed)
		// The generation the clear bumped, read after its push announced itself: a beat resolved
		// against the server the clear just installed lands on exactly this one.
		wiring.land(wiring.generation, landing, &apogee.DelegationTarget{Model: "tiny-3b"}, nil)
	}()
	// The landing either finishes inside the window — the ordering under test — or is still held out
	// of it when the wait expires, and the assertion is the same for both: what the engine is left
	// holding.
	select {
	case <-landed:
	case <-time.After(50 * time.Millisecond):
	}
	close(gate.release)
	<-cleared
	<-landed

	return gate.snapshot()
}

// The pick clears the latch and bumps the generation, so the very next beat resolved against the
// server it just installed lands on that new generation and is admitted. Push the nil after
// releasing the mutex and that landing can slip into the window: the target the picked server just
// resolved is clobbered by the pick's own nil, and the session stays unrouted until the next beat —
// the bug land's push is already written to avoid. So the pick pushes under the lock too, and the
// new server's target is the last word.
func TestDelegationRetargetPushesTheClearedTargetUnderTheLock(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{
		{Name: "grunt", Endpoint: "http://127.0.0.1:2222"},
		{Name: "cheaper", Endpoint: "http://127.0.0.1:3333"},
	}
	gate := &gatedDelegationSpy{arrived: make(chan struct{}), release: make(chan struct{})}
	wiring := retargetableWiring(t, entries[0], entries,
		heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, gate, nil)

	pushes := racedClearAgainstALanding(t, wiring, gate,
		func() error { return wiring.Retarget("cheaper") }, "cheaper")

	if len(pushes) != 2 || pushes[0] != nil || pushes[1] == nil {
		t.Fatalf("pushes = %+v; want the pick's nil first and the picked server's target last", pushes)
	}
}

// The same window, one seam over: a relist that re-points the name at another entry clears the stale
// latch and bumps the generation, and a beat on the newly installed server lands on that generation.
// Its target must be the last word, so the clearing push holds the mutex across it exactly as the
// pick's does.
func TestDelegationRelistPushesTheClearedTargetUnderTheLock(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}}
	moved := []config.ServerEntry{{Name: "grunt", Endpoint: "http://127.0.0.1:4444"}}
	gate := &gatedDelegationSpy{arrived: make(chan struct{}), release: make(chan struct{})}
	wiring := retargetableWiring(t, entries[0], entries,
		heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, gate, nil)

	pushes := racedClearAgainstALanding(t, wiring, gate,
		func() error { return wiring.relist("grunt", moved) }, "grunt")

	if len(pushes) != 2 || pushes[0] != nil || pushes[1] == nil {
		t.Fatalf("pushes = %+v; want the edit's nil first and the re-pointed server's target last", pushes)
	}
}

// A name the list does not carry is REFUSED here, which is where this seam parts company with the
// file's own key (missingNameNotice's degrade): the name came from a human picking in this session,
// so the honest answer is to say so and change nothing at all — routing stays exactly where it was.
func TestDelegationRetargetRefusesANameNoEntryCarries(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{
		{Name: "grunt", Endpoint: "http://127.0.0.1:2222"},
		{Name: "cheaper", Endpoint: "http://127.0.0.1:3333"},
	}
	spy := &delegationSpy{}
	notices := &noticeSpy{}
	wiring := retargetableWiring(t, entries[0], entries,
		heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, notices)
	wiring.observe(context.Background())()

	err := wiring.Retarget("chaper")
	if err == nil {
		t.Fatal("a name no entry carries was accepted; want the pick refused")
	}
	if !strings.Contains(err.Error(), "chaper") {
		t.Errorf("error = %q; want it to name what was asked for", err)
	}
	if wiring.server == nil || wiring.server.entry.Name != "grunt" {
		t.Fatalf("server after the refusal = %+v; want the old target untouched", wiring.server)
	}
	if len(spy.pushes) != 1 || len(notices.notes) != 2 {
		t.Errorf("a refused pick pushed %+v and said %q; want neither moved", spy.pushes, notices.notes)
	}
}

// The reload rule (relist): an entry whose `mechanisms:` map this build refuses is refused with
// NOTHING installed, and the message reaches the caller whole — it is the only place a human learns
// which key on which entry they mistyped.
func TestDelegationRetargetRefusesADefectiveMechanismsMap(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{
		{Name: "grunt", Endpoint: "http://127.0.0.1:2222"},
		{Name: "cheaper", Endpoint: "http://127.0.0.1:3333", Mechanisms: map[string]bool{"libary": true}},
	}
	spy := &delegationSpy{}
	wiring := retargetableWiring(t, entries[0], entries,
		heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, nil)
	wiring.observe(context.Background())()

	err := wiring.Retarget("cheaper")
	if err == nil {
		t.Fatal("a misspelled mechanism key was accepted; want the pick refused")
	}
	if !strings.Contains(err.Error(), "libary") || !strings.Contains(err.Error(), "cheaper") {
		t.Errorf("error = %q; want it to name both the key and the entry that asked for it", err)
	}
	if wiring.server == nil || wiring.server.entry.Name != "grunt" || len(spy.pushes) != 1 {
		t.Errorf("a refused pick installed %+v and pushed %+v; want the old target still live",
			wiring.server, spy.pushes)
	}
}

// An EMPTY name is the opt-out rather than a refusal: routing stops, the latch is cleared, and
// delegations run on this session's own upstream — the behaviour of a config that names no Sub-agent
// server at all.
func TestDelegationRetargetToNoServerFallsBackToTheSession(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}}
	spy := &delegationSpy{}
	wiring := retargetableWiring(t, entries[0], entries,
		heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, nil)
	wiring.observe(context.Background())()

	if err := wiring.Retarget(""); err != nil {
		t.Fatalf("Retarget onto no server: %v", err)
	}
	if wiring.server != nil {
		t.Fatalf("server after the opt-out = %+v; want none", wiring.server)
	}
	if len(spy.pushes) != 2 || spy.pushes[1] != nil {
		t.Fatalf("pushes = %+v; want the latch cleared by the opt-out", spy.pushes)
	}
	wiring.observe(context.Background())()
	if len(spy.pushes) != 2 {
		t.Errorf("pushes = %+v; want nothing observed once routing is off", spy.pushes)
	}
}

// The watcher fires a `servers:` re-read on ANY save (ADR 0041), and every such re-read carries the
// file's `sub-agents-server:` key — which a pick made in this session has deliberately outrun. So the
// key is compared against what the file said LAST rather than against the live target: a save about
// some other entry leaves the pick standing, and only a human editing the key itself moves routing
// back.
func TestDelegationRelistKeepsTheRetargetedServer(t *testing.T) {
	t.Parallel()

	entries := []config.ServerEntry{
		{Name: "grunt", Endpoint: "http://127.0.0.1:2222"},
		{Name: "cheaper", Endpoint: "http://127.0.0.1:3333"},
	}
	spy := &delegationSpy{}
	wiring := retargetableWiring(t, entries[0], entries,
		heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, nil)
	wiring.observe(context.Background())()
	if err := wiring.Retarget("cheaper"); err != nil {
		t.Fatalf("Retarget onto a configured entry: %v", err)
	}

	// A save somewhere else in the list: the file still names grunt, because nothing wrote the pick
	// down, and the pick must survive it.
	edited := []config.ServerEntry{
		{Name: "grunt", Endpoint: "http://127.0.0.1:2222", Model: "cheap-7b"},
		{Name: "cheaper", Endpoint: "http://127.0.0.1:3333"},
	}
	if err := wiring.relist("grunt", edited); err != nil {
		t.Fatalf("relist after a retarget: %v", err)
	}
	if wiring.server == nil || wiring.server.entry.Name != "cheaper" {
		t.Fatalf("server after an unrelated save = %+v; want the picked server kept", wiring.server)
	}

	// The key ITSELF moving is the other half: that is a human editing routing in the file, and the
	// file wins.
	if err := wiring.relist("grunt", entries); err != nil {
		t.Fatalf("relist re-pointing the key: %v", err)
	}
	if wiring.server == nil || wiring.server.entry.Name != "cheaper" {
		t.Fatalf("server after a save that did not move the key = %+v; want the pick kept", wiring.server)
	}
	if err := wiring.relist("grunt2", append(entries, config.ServerEntry{
		Name: "grunt2", Endpoint: "http://127.0.0.1:4444"})); err != nil {
		t.Fatalf("relist re-pointing the key: %v", err)
	}
	if wiring.server == nil || wiring.server.entry.Name != "grunt2" {
		t.Fatalf("server after the key was edited = %+v; want the file's own re-point to win", wiring.server)
	}
}

// ----------------------------------------------------------------------------
// The Sub-agent server's key source
// ----------------------------------------------------------------------------

// The named entry may carry a key SOURCE rather than a key, and the beat is where it is run: the
// Monitor that observes the grunt box authenticates with what the source answered, and the target
// the engine latches carries that same token, so the child talks to the server on the credential the
// beat proved works.
func TestDelegationBeatAndTargetCarryTheResolvedKey(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{
		Name: "grunt", Endpoint: "http://127.0.0.1:2222",
		APIKeyCmd: keyCommandFor(t, "sk-grunt-from-the-keychain"),
	}
	spy := &delegationSpy{}
	wiring := testDelegationWiring(entry, heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, nil)
	var beatenWith string
	wiring.server.beat = keyedBeatSource(heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, &beatenWith)

	wiring.observe(context.Background())()

	if beatenWith != "sk-grunt-from-the-keychain" {
		t.Errorf("the Sub-agent server was observed with key %q; want what the entry's command "+
			"printed — a keyed grunt box answers /v1/models with 401 like anything else", beatenWith)
	}
	if len(spy.pushes) != 1 || spy.pushes[0] == nil {
		t.Fatalf("pushes = %+v; want the one resolved target", spy.pushes)
	}
	if got := spy.pushes[0].APIKey; got != "sk-grunt-from-the-keychain" {
		t.Errorf("the latched target carries key %q; want the resolved one", got)
	}
}

// A key source that refuses takes the Sub-agent server out of routing for that beat — nothing is
// observed and no target is latched, so no delegation is sent to a server this session cannot
// authenticate against — and the human is told WHY in the resolver's own words, which name the entry
// and quote what the command said. The next beat asks again, because a locked keychain is fixable
// without editing a line of config.
func TestDelegationKeySourceFailureStopsRoutingAndSaysWhy(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{
		Name: "grunt", Endpoint: "http://127.0.0.1:2222",
		APIKeyCmd: missingKeyProgram,
	}
	spy := &delegationSpy{}
	notices := &noticeSpy{}
	wiring := testDelegationWiring(entry, heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, spy, notices)
	beaten := 0
	wiring.server.beat = func(context.Context, string) heartbeat.Beat {
		beaten++
		return heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}
	}

	wiring.observe(context.Background())()

	if beaten != 0 {
		t.Errorf("the server was beaten %d time(s) with no key in hand; the beat needs the key it "+
			"could not produce", beaten)
	}
	if len(spy.pushes) != 1 || spy.pushes[0] != nil {
		t.Fatalf("pushes = %+v; want exactly the one clearing push — delegations fall back to the "+
			"session's own server", spy.pushes)
	}
	if len(notices.notes) != 1 {
		t.Fatalf("notices = %q; want the one refusal", notices.notes)
	}
	if !strings.Contains(notices.notes[0], "grunt") || !strings.Contains(notices.notes[0], "api-key-cmd") {
		t.Errorf("notice = %q; want the resolver's own sentence, naming the entry and its source",
			notices.notes[0])
	}

	// A second beat is not a second sentence — the routing state has not moved — but it DOES ask the
	// source again: nothing about the failure was cached.
	wiring.observe(context.Background())()
	if len(notices.notes) != 1 {
		t.Errorf("notices after a second failing beat = %q; want no repeat of a state that never "+
			"changed", notices.notes)
	}
	if len(spy.pushes) != 2 || spy.pushes[1] != nil {
		t.Errorf("pushes = %+v; want the second beat's clearing push too", spy.pushes)
	}
}

// TestResolveDelegationTargetCarriesTheWorkingWindow pins the one bound with no observed half and
// no top-level rank: `working-window:` is a token count sized for THIS server's window, so the
// flagged entry's value travels as written and an entry that names none resolves to 0 — the child
// then works in the whole of the window resolved beside it, never in a room sized for the
// orchestrator's server.
func TestResolveDelegationTargetCarriesTheWorkingWindow(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{
		Name:          "grunt",
		Endpoint:      "http://127.0.0.1:2222",
		Model:         "pinned-model",
		ContextWindow: 131072,
		WorkingWindow: 32768,
	}
	observed := heartbeat.Beat{Reachable: true, ActiveModel: "pinned-model", ContextWindow: 131072}

	bounded := resolveDelegationTarget(entry, "", observed, nil, nil)
	if bounded == nil {
		t.Fatal("a reachable server resolved to no target; want the bounded one")
	}
	if bounded.WorkingWindow != 32768 {
		t.Errorf("WorkingWindow = %d; want the entry's 32768 carried as written", bounded.WorkingWindow)
	}

	entry.WorkingWindow = 0

	unbounded := resolveDelegationTarget(entry, "", observed, nil, nil)
	if unbounded == nil {
		t.Fatal("a reachable server resolved to no target; want the unbounded one")
	}
	if unbounded.WorkingWindow != 0 {
		t.Errorf("WorkingWindow = %d with no key on the entry; want the honest 0", unbounded.WorkingWindow)
	}
}

// The dialect advice is the operator half of the two-rung ladder: a flagged server that advertises
// no passive tell and pins no `effort-dialect:` leaves its delegates speaking the SESSION server's
// shape, and the human is told so in the words that name the remedy. The string is BINDING, so it
// is asserted verbatim off the emitting path rather than off a fixture.
func TestDelegationAdvisesWhenTheTargetNamesNoDialect(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{Name: "grunt", Endpoint: "http://127.0.0.1:2222"}
	notices := &noticeSpy{}
	wiring := testDelegationWiring(entry,
		heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, &delegationSpy{}, notices)

	wiring.observe(context.Background())()

	want := "sub-agents: grunt advertises no thinking-effort dialect — " +
		"delegates there speak this session's; set effort-dialect: on its entry"
	if len(notices.notes) != 2 || notices.notes[1] != want {
		t.Fatalf("notices = %q; want the routing line then %q", notices.notes, want)
	}

	// Once, not per beat: nothing about the entry changed, so there is nothing further to say.
	wiring.observe(context.Background())()
	if len(notices.notes) != 2 {
		t.Errorf("notices after a second beat = %q; want the advice said once", notices.notes)
	}
}

// And a server that DOES name one is advised about nothing: its delegates already speak its own
// shape, which is the whole point of the field.
func TestDelegationSaysNothingWhenTheTargetNamesADialect(t *testing.T) {
	t.Parallel()

	entry := config.ServerEntry{
		Name: "grunt", Endpoint: "http://127.0.0.1:2222", EffortDialect: "kwargs",
	}
	notices := &noticeSpy{}
	wiring := testDelegationWiring(entry,
		heartbeat.Beat{Reachable: true, ActiveModel: "cheap-7b"}, &delegationSpy{}, notices)

	wiring.observe(context.Background())()

	if len(notices.notes) != 1 || notices.notes[0] != "sub-agents: routing to grunt (cheap-7b)" {
		t.Errorf("notices = %q; want the routing line alone for a server that names its dialect", notices.notes)
	}
}

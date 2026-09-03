package agent

// Construction-path coverage for Config.EnableMechanisms (ADR 0015 §1–2, plan item 2): the engine
// builds each named catalogued Mechanism at New/Resume, merges it into Config.Mechanisms (a fresh
// registry when nil), and fails construction on an unknown ID, an unmet Requires stack, or a
// duplicate — all observed through the loop's own effects (MechanismFiredEvent, construction error),
// never the Agent's internals. The catalogued Mechanisms are built through the production catalogue,
// the same seam the config surface drives, so these prove the real build-and-merge path end to end.

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// brokenThenFixed scripts a write_file call whose Go payload is unbalanced — which the syntax
// Mechanism retries in place — followed by a recovered content reply, the shape that makes a
// catalogued Mechanism fire through the loop so a test can prove one was armed by
// Config.EnableMechanisms alone. The call itself is well formed (a known tool, valid arguments), so
// the tool-call repair Floor guard that runs ahead of every hook stands down and leaves the seam to
// the Mechanism.
func brokenThenFixed(recovered string) [][]provider.Delta {
	return [][]provider.Delta{
		toolCallScript("c1", "write_file", `{"path":"main.go","content":"package main\nfunc main() {"}`),
		contentScript(recovered),
	}
}

// TestEnableMechanisms_ArmsNamedMechanism: a valid ID list with a nil Config.Mechanisms builds the
// named catalogued Mechanism at construction and it fires through the real loop.
func TestEnableMechanisms_ArmsNamedMechanism(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "write_file", result: "ok"})
	cfg.EnableMechanisms = []domain.MechanismID{"syntax"}
	responder := &captureAllResponder{scripts: brokenThenFixed("recovered")}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "write the parser")

	if !hasFire(sink.events, "syntax", string(domain.ActionRetry)) {
		t.Error("syntax did not fire; Config.EnableMechanisms never armed it through construction")
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "recovered" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want %q (the Mechanism drove the retry)", me, ok, "recovered")
	}
}

// TestEnableMechanisms_UnknownIDFailsConstruction: a bogus ID fails New with a matchable
// ErrUnknownMechanism (item 1's sentinel, wrapped by mechanisms.Build).
func TestEnableMechanisms_UnknownIDFailsConstruction(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.EnableMechanisms = []domain.MechanismID{"not_a_real_mechanism"}

	_, err := newAgent(cfg, echoResponder{reply: "unreached"})
	if !errors.Is(err, domain.ErrUnknownMechanism) {
		t.Errorf("newAgent err = %v, want it to wrap domain.ErrUnknownMechanism", err)
	}
}

// TestEnableMechanisms_HalfStackFailsRequirement: a registered row whose Required peer is absent
// fails the requirements gate with ErrMissingRequirement (ADR 0014 §4 stacking, re-checked over the
// merged registry). The row is synthetic: no catalogued Mechanism declares Requires any more — the
// one that did named the tool-result cap, now a Floor guard — while the gate itself stays.
func TestEnableMechanisms_HalfStackFailsRequirement(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	reg := domain.NewMechanismRegistry()
	if err := reg.Add(requiresMech{id: "half_stack", requires: []domain.MechanismID{"absent_peer"}}.row()); err != nil {
		t.Fatalf("Add: %v", err)
	}
	cfg.Mechanisms = reg

	_, err := newAgent(cfg, echoResponder{reply: "unreached"})
	if !errors.Is(err, domain.ErrMissingRequirement) {
		t.Errorf("newAgent err = %v, want it to wrap domain.ErrMissingRequirement", err)
	}
}

// TestEnableMechanisms_DuplicateIDRejected: the same ID listed twice trips the registry's
// already-registered rejection at merge time (covering both a doubled list entry and an in-repo
// caller who pre-built the same Mechanism).
func TestEnableMechanisms_DuplicateIDRejected(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.EnableMechanisms = []domain.MechanismID{"syntax", "syntax"}

	_, err := newAgent(cfg, echoResponder{reply: "unreached"})
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Errorf("newAgent err = %v, want an already-registered rejection", err)
	}
}

// TestEnableMechanisms_MergeRejectionCarriesOnePrefix: the merge-time rejection is RETURNED to the
// host, and cmd/apogee/main.go prints a returned error verbatim — so it has to read as ONE
// "apogee: "-prefixed line. registry.Add's rejections already arrive prefixed (house
// convention for a returned error), so the enable-path context is appended rather than wrapping them
// in a second prefix ("apogee: enable mechanism "syntax": apogee: mechanism ID "syntax" is
// already registered").
func TestEnableMechanisms_MergeRejectionCarriesOnePrefix(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.EnableMechanisms = []domain.MechanismID{"syntax", "syntax"}

	_, err := newAgent(cfg, echoResponder{reply: "unreached"})
	if err == nil {
		t.Fatal("newAgent accepted a doubled EnableMechanisms entry; want the already-registered rejection")
	}

	msg := err.Error()
	if !strings.HasPrefix(msg, "apogee: ") {
		t.Errorf("newAgent err = %q; want it to start with %q", msg, "apogee: ")
	}
	if got := strings.Count(msg, "apogee: "); got != 1 {
		t.Errorf("newAgent err = %q; want exactly one %q prefix, got %d", msg, "apogee: ", got)
	}
	if !strings.Contains(msg, `"syntax"`) {
		t.Errorf("newAgent err = %q; want it to name the mechanism that failed", msg)
	}
}

// TestEnableMechanisms_MergesWithProvidedExperimentalHook: an EnableMechanisms list plus a
// Config.Mechanisms carrying an experimental hook leaves BOTH live — the catalogued Mechanism is
// merged INTO the provided registry, not replacing it (locked decision 2).
func TestEnableMechanisms_MergesWithProvidedExperimentalHook(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "write_file", result: "ok"})
	fired := false
	cfg.Mechanisms = domain.NewMechanismRegistry()
	if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, firingHook{fired: &fired}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}
	cfg.EnableMechanisms = []domain.MechanismID{"syntax"}
	responder := &captureAllResponder{scripts: brokenThenFixed("recovered")}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "write the parser")

	if !fired {
		t.Error("the pre-existing experimental hook did not fire; the merge replaced the provided registry")
	}
	if fireCountFor(sink.events, "syntax") == 0 {
		t.Error("the catalogued syntax Mechanism did not fire; EnableMechanisms was not merged in")
	}
}

// TestEnableMechanisms_NilAndEmptyBuildNothing: a nil and an empty list both arm NOTHING. Every
// Capability is default-off (D1) with no exception left — the two recoveries that used to be floored
// in are Floor guards now (ADR 0071) — so an embedder that hands New a Config with no
// `EnableMechanisms` gets an unarmed catalogue, and its recovery guarantees from Config.Floor.
//
// It is read off the registry rather than off fired events: what the list BUILDS is the claim, and a
// Mechanism that never triggers on a well-behaved reply would make an event-based assertion say
// nothing at all.
func TestEnableMechanisms_NilAndEmptyBuildNothing(t *testing.T) {
	cases := map[string][]domain.MechanismID{
		"nil":   nil,
		"empty": {},
	}
	for name, ids := range cases {
		t.Run(name, func(t *testing.T) {
			sink := &recordingSink{}
			cfg := baseConfig(sink)
			cfg.EnableMechanisms = ids

			a, err := newAgent(cfg, echoResponder{reply: "hi"})
			if err != nil {
				t.Fatalf("newAgent: %v", err)
			}

			if got := armedIDs(a, domain.HookPostResponse); len(got) != 0 {
				t.Errorf("armed post-response Mechanisms = %v, want nothing armed", got)
			}
		})
	}
}

// armedIDs is the canonical IDs a built Agent actually holds at one hook point, sorted — the direct
// read of what a Config's EnableMechanisms list constructed, independent of whether anything fired.
func armedIDs(a *Agent, at domain.HookPoint) []domain.MechanismID {
	var out []domain.MechanismID
	for _, m := range a.registry.Ordered(at) {
		out = append(out, m.Descriptor.ID)
	}
	slices.Sort(out)
	return out
}

// TestEnableMechanisms_ResumeArmsIdentically: Resume builds the same IDs the same way New does —
// mechanisms are Config, not session state — so a resumed Agent arms the named Mechanism afresh.
func TestEnableMechanisms_ResumeArmsIdentically(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "write_file", result: "ok"})
	cfg.EnableMechanisms = []domain.MechanismID{"syntax"}

	a, err := newAgent(cfg, &captureAllResponder{scripts: brokenThenFixed("recovered")})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "write the parser")
	if !hasFire(sink.events, "syntax", string(domain.ActionRetry)) {
		t.Fatal("syntax did not fire on the original Agent (test precondition)")
	}
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Resume into a fresh Agent with an equivalent Config (fresh sink + registry) and drive another
	// Mechanism-triggering Exchange: the resumed Agent must arm syntax identically.
	sink2 := &recordingSink{}
	cfg2 := configWithTools(sink2, fakeTool{name: "write_file", result: "ok"})
	cfg2.EnableMechanisms = []domain.MechanismID{"syntax"}
	b, err := resumeAgent(cfg2, snap, &captureAllResponder{scripts: brokenThenFixed("recovered again")})
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	runExchange(t, b, "keep going")

	if !hasFire(sink2.events, "syntax", string(domain.ActionRetry)) {
		t.Error("Resume did not arm syntax; mechanisms must be rebuilt from Config, not session state")
	}
}

// TestEnableMechanisms_LibraryBuildsFromLibraryDir: enabling `library` with a temp LibraryDir builds
// (the store is constructed and Loaded, no error), and enabling it with an EMPTY LibraryDir behaves
// exactly as cmd/apogee/wire.go's path does today — the store is always non-nil when `library` is
// enabled, so construction succeeds either way (parity, not new policy).
func TestEnableMechanisms_LibraryBuildsFromLibraryDir(t *testing.T) {
	t.Run("temp dir", func(t *testing.T) {
		cfg := baseConfig(&recordingSink{})
		cfg.LibraryDir = t.TempDir()
		cfg.EnableMechanisms = []domain.MechanismID{"library"}

		if _, err := newAgent(cfg, echoResponder{reply: "unused"}); err != nil {
			t.Errorf("newAgent with library + a temp LibraryDir: %v, want a clean build", err)
		}
	})

	t.Run("empty dir parity", func(t *testing.T) {
		cfg := baseConfig(&recordingSink{})
		cfg.LibraryDir = "" // wire.go builds a non-nil store even here, so construction still succeeds
		cfg.EnableMechanisms = []domain.MechanismID{"library"}

		if _, err := newAgent(cfg, echoResponder{reply: "unused"}); err != nil {
			t.Errorf("newAgent with library + an empty LibraryDir: %v, want wire.go parity (a clean build)", err)
		}
	})
}

// TestEnableMechanisms_NonLibraryArmIgnoresLibraryDir: a list with no `library` never wires a store,
// so LibraryDir is irrelevant — construction succeeds even when it points at a path that does not
// exist (nothing under it is ever read).
func TestEnableMechanisms_NonLibraryArmIgnoresLibraryDir(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.LibraryDir = "/no/such/dir/should/never/be/read"
	cfg.EnableMechanisms = []domain.MechanismID{"syntax"}

	if _, err := newAgent(cfg, echoResponder{reply: "unused"}); err != nil {
		t.Errorf("newAgent with a non-library arm: %v, want a clean build (LibraryDir untouched)", err)
	}
}

// TestBuildMechanisms_ArmsTheSameSetWithoutAnAgent: the host-facing half of the same build (ADR
// 0045). A Delegation target's Mechanisms posture is composed by the HOST, which needs the registry
// rather than an Agent, so BuildMechanisms hands one back off the very path New walks — the Deps
// derivation included, which is why `library` (the one row wanting collaborators) is what proves it.
// The registry comes back fresh and unowned; a child takes a copy through ForSubAgent.
func TestBuildMechanisms_ArmsTheSameSetWithoutAnAgent(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.LibraryDir = t.TempDir()

	registry, err := BuildMechanisms(cfg, []domain.MechanismID{"library"})
	if err != nil {
		t.Fatalf("BuildMechanisms with a library arm: %v, want the store derived for it", err)
	}
	if got := registry.Ordered(domain.HookPreRequest); len(got) != 1 || got[0].Descriptor.ID != "library" {
		t.Fatalf("armed pre-request rows = %+v, want the one library Mechanism", got)
	}
	if sub := registry.ForSubAgent(); sub == registry {
		t.Error("ForSubAgent handed back the same container; a child must never share the built one")
	}

	// Nothing named arms nothing — the empty registry a `mechanisms:` map with every key false
	// resolves to, which is a catalogue of its own rather than an inheritance.
	empty, err := BuildMechanisms(cfg, nil)
	if err != nil {
		t.Fatalf("BuildMechanisms with no ids: %v, want an empty registry", err)
	}
	if got := len(empty.Ordered(domain.HookPreRequest)); got != 0 {
		t.Errorf("armed rows with no ids = %d, want 0", got)
	}
}

// TestBuildMechanisms_RefusesWhatConstructionRefuses: the error is the construction error, raised
// where the host can still name the config that asked for it — an unknown ID wrapping
// ErrUnknownMechanism, exactly as New refuses the same list. BuildMechanisms builds into a FRESH
// registry and never reads cfg.Mechanisms, so the refusal it is checked on has to be one the
// shipped catalogue can still trip: the incompatibility gate no longer qualifies, its last two
// declarers having been promoted to Floor guards and retired outright in v0.20.0 (ADR 0071). The
// gate itself is pinned over synthetic rows in internal/domain.
func TestBuildMechanisms_RefusesWhatConstructionRefuses(t *testing.T) {
	cfg := baseConfig(&recordingSink{})

	_, err := BuildMechanisms(cfg, []domain.MechanismID{"no_such_mechanism"})
	if !errors.Is(err, domain.ErrUnknownMechanism) {
		t.Errorf("BuildMechanisms with an unknown ID = %v, want ErrUnknownMechanism", err)
	}
}

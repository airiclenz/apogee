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
)

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

// TestEnableMechanisms_MergeRejectionCarriesOnePrefix: a build-path rejection is RETURNED to the
// host, and cmd/apogee/main.go prints a returned error verbatim — so it has to read as ONE
// "apogee: "-prefixed line naming the ID that failed. The shipped catalogue is empty since v0.20.0
// (ADR 0071), so the reachable rejection is the unknown-ID one: mechanisms.Build's error already
// arrives prefixed (house convention for a returned error), and the enable path must not wrap it in
// a second prefix.
func TestEnableMechanisms_MergeRejectionCarriesOnePrefix(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.EnableMechanisms = []domain.MechanismID{"not_a_real_mechanism"}

	_, err := newAgent(cfg, echoResponder{reply: "unreached"})
	if err == nil {
		t.Fatal("newAgent accepted an uncatalogued EnableMechanisms entry; want a refusal")
	}

	msg := err.Error()
	if !strings.HasPrefix(msg, "apogee: ") {
		t.Errorf("newAgent err = %q; want it to start with %q", msg, "apogee: ")
	}
	if got := strings.Count(msg, "apogee: "); got != 1 {
		t.Errorf("newAgent err = %q; want exactly one %q prefix, got %d", msg, "apogee: ", got)
	}
	if !strings.Contains(msg, `"not_a_real_mechanism"`) {
		t.Errorf("newAgent err = %q; want it to name the mechanism that failed", msg)
	}
	// And the tail names the valid keys — "(none)" over the empty shipped catalogue rather than a
	// dangling "known: " that would read as a truncated message. That is the whole of what a host can
	// tell the user about which ids ARE arm-able in this build.
	if !strings.Contains(msg, "(none)") {
		t.Errorf("newAgent err = %q; want the known-ids tail to render %q for the empty catalogue", msg, "(none)")
	}
}

// TestEnableMechanisms_MergesWithProvidedExperimentalHook: a Config.Mechanisms carrying an
// experimental hook is the registry construction MERGES INTO, never one it replaces (locked
// decision 2). With the shipped catalogue empty since v0.20.0 (ADR 0071) there is no catalogued row
// left to merge alongside it, so what stands here is the half that still can be observed: the
// provided registry survives construction and its hook fires through the real loop.
func TestEnableMechanisms_MergesWithProvidedExperimentalHook(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "write_file", result: "ok"})
	fired := false
	cfg.Mechanisms = domain.NewMechanismRegistry()
	if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, firingHook{fired: &fired}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}

	a, err := newAgent(cfg, echoResponder{reply: "done"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "update the config file")

	if !fired {
		t.Error("the pre-existing experimental hook did not fire; construction replaced the provided registry")
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

// TestEnableMechanisms_ResumeArmsIdentically: Resume builds the ID list the same way New does —
// mechanisms are Config, not session state — so a resumed Agent walks the same build path and
// refuses the same list. With the shipped catalogue empty since v0.20.0 (ADR 0071) there is no row
// left to arm and observe firing, so the pin is the refusal: a Config whose EnableMechanisms names
// an uncatalogued ID fails resumeAgent exactly as it fails newAgent, which it could only do by
// rebuilding from Config rather than restoring from the snapshot.
func TestEnableMechanisms_ResumeArmsIdentically(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "write_file", result: "ok"})

	a, err := newAgent(cfg, echoResponder{reply: "done"})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "update the config file")
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	cfg2 := configWithTools(&recordingSink{}, fakeTool{name: "write_file", result: "ok"})
	cfg2.EnableMechanisms = []domain.MechanismID{"not_a_real_mechanism"}
	if _, err := resumeAgent(cfg2, snap, echoResponder{reply: "unreached"}); !errors.Is(err, domain.ErrUnknownMechanism) {
		t.Errorf("resumeAgent err = %v, want ErrUnknownMechanism; mechanisms must be rebuilt from Config, not session state", err)
	}
}

// TestBuildMechanisms_ArmsTheSameSetWithoutAnAgent: the host-facing half of the same build (ADR
// 0045). A Delegation target's Mechanisms posture is composed by the HOST, which needs the registry
// rather than an Agent, so BuildMechanisms hands one back off the very path New walks. The shipped
// catalogue is empty since v0.20.0 (ADR 0071), so every id list resolves to an empty registry — and
// the registry comes back fresh and unowned either way; a child takes a copy through ForSubAgent.
func TestBuildMechanisms_ArmsTheSameSetWithoutAnAgent(t *testing.T) {
	cfg := baseConfig(&recordingSink{})

	registry, err := BuildMechanisms(cfg, nil)
	if err != nil {
		t.Fatalf("BuildMechanisms with no ids: %v, want an empty registry", err)
	}
	if got := len(registry.Ordered(domain.HookPreRequest)); got != 0 {
		t.Errorf("armed pre-request rows = %d, want 0 — the shipped catalogue is empty", got)
	}
	if sub := registry.ForSubAgent(); sub == registry {
		t.Error("ForSubAgent handed back the same container; a child must never share the built one")
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

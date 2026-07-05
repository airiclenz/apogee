package agent

// Construction-path coverage for Config.EnableMechanisms (ADR 0015 §1–2, plan item 2): the engine
// builds each named catalogued Mechanism at New/Resume, merges it into Config.Mechanisms (a fresh
// registry when nil), and fails construction on an unknown ID, an unmet Requires stack, or a
// duplicate — all observed through the loop's own effects (MechanismFiredEvent, construction error),
// never the Agent's internals. The catalogued Mechanisms are built through the production catalogue,
// the same seam the config surface drives, so these prove the real build-and-merge path end to end.

import (
	"errors"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// emptyThenContent scripts an empty first reply — which the empty_response_recovery off-ramp retries
// in place — followed by a recovered content reply, the shape that makes the off-ramp fire through
// the loop so a test can prove a catalogued Mechanism was armed by Config.EnableMechanisms alone.
func emptyThenContent(recovered string) [][]provider.Delta {
	return [][]provider.Delta{emptyScript(), contentScript(recovered)}
}

// TestEnableMechanisms_ArmsNamedMechanism: a valid ID list with a nil Config.Mechanisms builds the
// named catalogued Mechanism at construction and it fires through the real loop.
func TestEnableMechanisms_ArmsNamedMechanism(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, result: "contents"})
	cfg.EnableMechanisms = []domain.MechanismID{"empty_response_recovery"}
	responder := &captureAllResponder{scripts: emptyThenContent("recovered")}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "please implement the parser")

	if !hasFire(sink.events, "empty_response_recovery", string(domain.ActionRetry)) {
		t.Error("empty_response_recovery did not fire; Config.EnableMechanisms never armed it through construction")
	}
	if me, ok := lastMessageEvent(sink.events); !ok || me.Text != "recovered" {
		t.Errorf("final MessageEvent = %+v (ok=%v), want %q (the off-ramp drove the retry)", me, ok, "recovered")
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

// TestEnableMechanisms_HalfStackFailsRequirement: enabling guided_decomposition without its Required
// peer tool_result_cap fails the requirements gate with ErrMissingRequirement (ADR 0014 §4 stacking,
// re-checked over the merged registry).
func TestEnableMechanisms_HalfStackFailsRequirement(t *testing.T) {
	cfg := baseConfig(&recordingSink{})
	cfg.EnableMechanisms = []domain.MechanismID{"guided_decomposition"}

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
	cfg.EnableMechanisms = []domain.MechanismID{"validate", "validate"}

	_, err := newAgent(cfg, echoResponder{reply: "unreached"})
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Errorf("newAgent err = %v, want an already-registered rejection", err)
	}
}

// TestEnableMechanisms_MergesWithProvidedExperimentalHook: an EnableMechanisms list plus a
// Config.Mechanisms carrying an experimental hook leaves BOTH live — the catalogued Mechanism is
// merged INTO the provided registry, not replacing it (locked decision 2).
func TestEnableMechanisms_MergesWithProvidedExperimentalHook(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, result: "contents"})
	fired := false
	cfg.Mechanisms = domain.NewMechanismRegistry()
	if err := cfg.Mechanisms.AddExperimental(domain.HookPreRequest, firingHook{fired: &fired}); err != nil {
		t.Fatalf("AddExperimental: %v", err)
	}
	cfg.EnableMechanisms = []domain.MechanismID{"empty_response_recovery"}
	responder := &captureAllResponder{scripts: emptyThenContent("recovered")}

	a, err := newAgent(cfg, responder)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "please implement the parser")

	if !fired {
		t.Error("the pre-existing experimental hook did not fire; the merge replaced the provided registry")
	}
	if fireCountFor(sink.events, "empty_response_recovery") == 0 {
		t.Error("the catalogued empty_response_recovery did not fire; EnableMechanisms was not merged in")
	}
}

// TestEnableMechanisms_NilAndEmptyBuildNothing: neither a nil nor an empty list arms anything — the
// default-off posture is untouched, so no MechanismFiredEvent is ever emitted.
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
			runExchange(t, a, "hello")

			if hasEvent[domain.MechanismFiredEvent](sink.events) {
				t.Error("a Mechanism fired though EnableMechanisms was nil/empty")
			}
		})
	}
}

// TestEnableMechanisms_ResumeArmsIdentically: Resume builds the same IDs the same way New does —
// mechanisms are Config, not session state — so a resumed Agent arms the named Mechanism afresh.
func TestEnableMechanisms_ResumeArmsIdentically(t *testing.T) {
	sink := &recordingSink{}
	cfg := configWithTools(sink, fakeTool{name: "read_file", readOnly: true, result: "contents"})
	cfg.EnableMechanisms = []domain.MechanismID{"empty_response_recovery"}

	a, err := newAgent(cfg, &captureAllResponder{scripts: emptyThenContent("recovered")})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	runExchange(t, a, "please implement the parser")
	if !hasFire(sink.events, "empty_response_recovery", string(domain.ActionRetry)) {
		t.Fatal("empty_response_recovery did not fire on the original Agent (test precondition)")
	}
	snap, err := a.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Resume into a fresh Agent with an equivalent Config (fresh sink + registry) and drive another
	// off-ramp-triggering Exchange: the resumed Agent must arm empty_response_recovery identically.
	sink2 := &recordingSink{}
	cfg2 := configWithTools(sink2, fakeTool{name: "read_file", readOnly: true, result: "contents"})
	cfg2.EnableMechanisms = []domain.MechanismID{"empty_response_recovery"}
	b, err := resumeAgent(cfg2, snap, &captureAllResponder{scripts: emptyThenContent("recovered again")})
	if err != nil {
		t.Fatalf("resumeAgent: %v", err)
	}
	runExchange(t, b, "keep going")

	if !hasFire(sink2.events, "empty_response_recovery", string(domain.ActionRetry)) {
		t.Error("Resume did not arm empty_response_recovery; mechanisms must be rebuilt from Config, not session state")
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
	cfg.EnableMechanisms = []domain.MechanismID{"validate"}

	if _, err := newAgent(cfg, echoResponder{reply: "unused"}); err != nil {
		t.Errorf("newAgent with a non-library arm: %v, want a clean build (LibraryDir untouched)", err)
	}
}

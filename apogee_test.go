package apogee_test

// Black-box public-API tests (P0.6e): the validation and session paths that the
// public surface exercises without a fake Responder. This package is external
// (apogee_test) precisely because the Auto-gate test injects platform.NewDenyConfiner,
// and internal/platform imports the root apogee package — an internal test package
// could not import it without an import cycle. The fake-Responder capstone lives in the
// white-box harness (harness_internal_test.go).

import (
	"context"
	"errors"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/platform"
)

type nopSink struct{}

func (nopSink) Emit(apogee.Event) {}

// orderingMech is a minimal Mechanism hook carrying the ID and ordering constraints its
// catalogue row is built from (mustAdd) — the trivial input the cycle gate needs. It
// implements PreRequestHook so MechanismRegistry.Add accepts it (a Mechanism must hook
// somewhere, ADR 0002).
type orderingMech struct {
	id     apogee.MechanismID
	before []apogee.MechanismID
	after  []apogee.MechanismID
}

func (orderingMech) PreRequest(context.Context, *apogee.Request) error { return nil }

func validConfig() apogee.Config {
	return apogee.Config{Endpoint: "http://localhost:0", Model: "test-model", Events: nopSink{}}
}

// ---------------------------------------------------------------------------

func TestNew_AutoModeGate(t *testing.T) {
	// Under ADR 0012 the Auto construction gate is CONDITIONAL: a NIL Confiner — no
	// confinement facility injected at all — is refused (ErrAutoUnavailable); a PRESENT
	// but incapable Confiner (deny-all: no fs-confinement on this host) is NOT refused —
	// Auto is entered and the subprocess surface gates through Approval ("confine if you
	// can, gate if you can't"). This reverses ADR 0004's refuse-deny-all behaviour.
	tests := []struct {
		name     string
		confiner apogee.Confiner
		wantErr  bool
	}{
		{name: "auto with no confiner is refused", confiner: nil, wantErr: true},
		{name: "auto with deny-all confiner enters Auto (subprocess gates)", confiner: platform.NewDenyConfiner(), wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			cfg.Mode = apogee.ModeAuto
			cfg.Confiner = tt.confiner

			_, err := apogee.New(cfg)

			if tt.wantErr {
				if !errors.Is(err, apogee.ErrAutoUnavailable) {
					t.Errorf("New err = %v, want ErrAutoUnavailable", err)
				}
				return
			}
			if err != nil {
				t.Errorf("New err = %v, want nil (deny-all confiner enters Auto, subprocess gates)", err)
			}
		})
	}
}

func TestNew_NonAutoModeNeedsNoConfiner(t *testing.T) {
	cfg := validConfig()
	cfg.Mode = apogee.ModeAskBefore

	if _, err := apogee.New(cfg); err != nil {
		t.Errorf("New(ask-before, no confiner) = %v, want nil", err)
	}
}

func TestNew_OrderingCycle(t *testing.T) {
	t.Run("cyclic registry is rejected", func(t *testing.T) {
		registry := apogee.NewMechanismRegistry()
		// a must come after b, and b must come after a → a 2-cycle.
		mustAdd(t, registry, orderingMech{id: "a", after: []apogee.MechanismID{"b"}})
		mustAdd(t, registry, orderingMech{id: "b", after: []apogee.MechanismID{"a"}})

		cfg := validConfig()
		cfg.Mechanisms = registry

		if _, err := apogee.New(cfg); !errors.Is(err, apogee.ErrOrderingCycle) {
			t.Errorf("New err = %v, want ErrOrderingCycle", err)
		}
	})

	t.Run("acyclic registry is accepted", func(t *testing.T) {
		registry := apogee.NewMechanismRegistry()
		mustAdd(t, registry, orderingMech{id: "a", before: []apogee.MechanismID{"b"}})
		mustAdd(t, registry, orderingMech{id: "b"})

		cfg := validConfig()
		cfg.Mechanisms = registry

		if _, err := apogee.New(cfg); err != nil {
			t.Errorf("New(acyclic) = %v, want nil", err)
		}
	})
}

func TestNew_RequiresMinimumConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*apogee.Config)
	}{
		{name: "missing Events", mutate: func(c *apogee.Config) { c.Events = nil }},
		{name: "missing Endpoint", mutate: func(c *apogee.Config) { c.Endpoint = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.mutate(&cfg)

			if _, err := apogee.New(cfg); err == nil {
				t.Error("New = nil error, want a validation error")
			}
		})
	}
}

// TestNew_ModelMayBeBoundLater pins the construction relaxation async startup needs (ADR 0024):
// Config.Model is no longer part of the minimum surface, so a host that starts before its
// Upstream answers constructs anyway and binds the observed model later through Rebind. The
// engine's own guard moved to Submit, which refuses while nothing is bound.
func TestNew_ModelMayBeBoundLater(t *testing.T) {
	cfg := validConfig()
	cfg.Model = ""

	agent, err := apogee.New(cfg)
	if err != nil {
		t.Fatalf("New with an empty Model = %v, want nil", err)
	}
	defer func() { _ = agent.Close() }()

	if err := agent.Submit(apogee.UserInput{Text: "too early"}); err == nil {
		t.Error("Submit with no model bound = nil error, want a refusal")
	}
	if err := agent.Rebind(apogee.RebindSpec{Model: "late-bound"}); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if err := agent.Submit(apogee.UserInput{Text: "now it flows"}); err != nil {
		t.Errorf("Submit after Rebind = %v, want nil", err)
	}
}

func TestAddExperimental_WrongInterface(t *testing.T) {
	registry := apogee.NewMechanismRegistry()
	// orderingMech implements PreRequestHook, not HistoryRewriter.
	if err := registry.AddExperimental(apogee.HookHistoryRewrite, orderingMech{id: "x"}); err == nil {
		t.Error("AddExperimental with mismatched hook point = nil error, want an error")
	}
}

func TestSession_RoundTrip(t *testing.T) {
	agent, err := apogee.New(validConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	snap, err := agent.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	encoded, err := snap.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := apogee.DecodeSession(encoded)
	if err != nil {
		t.Fatalf("DecodeSession: %v", err)
	}

	if decoded.Version != snap.Version {
		t.Errorf("round-trip Version = %d, want %d", decoded.Version, snap.Version)
	}
}

func TestDecodeSession_FutureVersion(t *testing.T) {
	// A version far beyond any near-term schema is from a newer build → rejected.
	future := []byte(`{"Version":999,"State":null}`)

	if _, err := apogee.DecodeSession(future); !errors.Is(err, apogee.ErrSessionVersion) {
		t.Errorf("DecodeSession err = %v, want ErrSessionVersion", err)
	}
}

func TestResume_FutureVersion(t *testing.T) {
	if _, err := apogee.Resume(validConfig(), apogee.Session{Version: 999}); !errors.Is(err, apogee.ErrSessionVersion) {
		t.Errorf("Resume err = %v, want ErrSessionVersion", err)
	}
}

func mustAdd(t *testing.T, registry *apogee.MechanismRegistry, m orderingMech) {
	t.Helper()
	registered := apogee.RegisteredMechanism{
		Descriptor: apogee.MechanismDescriptor{ID: m.id},
		Ordering:   apogee.OrderingConstraints{Before: m.before, After: m.after},
		Hook:       m,
	}
	if err := registry.Add(registered); err != nil {
		t.Fatalf("Add(%s): %v", m.id, err)
	}
}

// TestCataloguedMechanisms asserts the public catalogue query is non-empty, sorted by ID, and
// exposes the ADR 0014 guided_decomposition ↔ decompose IncompatibleWith relation — all through the
// public surface only (no internal import), so it also guards that the descriptor metadata is
// reachable without building any Mechanism (ADR 0015 §3). The Requires relation it used to read is
// gone from the catalogue: its peer, tool_result_cap, is the `tool-result-cap` Floor guard now.
func TestCataloguedMechanisms(t *testing.T) {
	got := apogee.CataloguedMechanisms()
	if len(got) == 0 {
		t.Fatal("CataloguedMechanisms() = empty, want the built-in catalogue")
	}

	for i := 1; i < len(got); i++ {
		if got[i-1].ID >= got[i].ID {
			t.Errorf("CataloguedMechanisms() not strictly sorted/duplicate-free at %d: %q then %q",
				i, got[i-1].ID, got[i].ID)
		}
	}

	var gd *apogee.MechanismDescriptor
	for i := range got {
		if got[i].ID == "guided_decomposition" {
			gd = &got[i]
			break
		}
	}
	if gd == nil {
		t.Fatal("CataloguedMechanisms() missing guided_decomposition")
	}
	if len(gd.IncompatibleWith) != 1 || gd.IncompatibleWith[0] != "truncate_history" {
		t.Errorf("guided_decomposition IncompatibleWith = %v, want [truncate_history]", gd.IncompatibleWith)
	}
}

// TestCataloguedMechanisms_ReturnsClonedDescriptors pins the documented clone contract (ADR 0015 §3):
// each query returns descriptors whose slice fields are independent of the static catalogue, so a
// caller may mutate a returned descriptor's IncompatibleWith freely (e.g. to compute a
// leave-one-out arm) without corrupting a later query. Mutating an element of the FIRST result's
// slices must leave a SECOND query pristine — reverting cloneDescriptor's slices.Clone would let the
// mutation reach back into the shared catalogue row and fail this test.
//
// It exercises IncompatibleWith alone: no catalogued row declares Requires any more, the one that did
// having named the tool-result cap now promoted to a Floor guard. Both fields are cloned by the same
// slices.Clone, so the contract stands on either.
func TestCataloguedMechanisms_ReturnsClonedDescriptors(t *testing.T) {
	first := apogee.CataloguedMechanisms()

	// guided_decomposition carries a non-empty IncompatibleWith ([decompose, truncate_history]).
	idx := -1
	for i := range first {
		if first[i].ID == "guided_decomposition" {
			idx = i
			break
		}
	}
	if idx == -1 {
		t.Fatal("CataloguedMechanisms() missing guided_decomposition")
	}
	if len(first[idx].IncompatibleWith) == 0 {
		t.Fatalf("guided_decomposition IncompatibleWith=%v; the clone test needs it non-empty",
			first[idx].IncompatibleWith)
	}

	wantIncompatible := first[idx].IncompatibleWith[0]

	// Mutate the returned slice element in place — reachable only if the caller owns the backing array.
	first[idx].IncompatibleWith[0] = "mutated_incompatible"

	second := apogee.CataloguedMechanisms()
	var gd *apogee.MechanismDescriptor
	for i := range second {
		if second[i].ID == "guided_decomposition" {
			gd = &second[i]
			break
		}
	}
	if gd == nil {
		t.Fatal("second CataloguedMechanisms() missing guided_decomposition")
	}
	if gd.IncompatibleWith[0] != wantIncompatible {
		t.Errorf("IncompatibleWith[0] = %q after mutating the first result; want the pristine %q — the returned slice aliases the static catalogue",
			gd.IncompatibleWith[0], wantIncompatible)
	}
}

// TestEnableErrors_MatchableThroughRoot proves the enable-time sentinels are matchable through the
// root re-exports: a half-armed Requires stack fails New with apogee.ErrMissingRequirement and a
// bogus ID fails with apogee.ErrUnknownMechanism (ADR 0015 §4, locked decision 5). The half stack is
// a registry a host injects rather than a catalogue pair: no catalogued row declares Requires any
// more, its one declarer's peer having become a Floor guard, while the gate and the sentinel stay.
func TestEnableErrors_MatchableThroughRoot(t *testing.T) {
	half := validConfig()
	registry := apogee.NewMechanismRegistry()
	if err := registry.Add(apogee.RegisteredMechanism{
		Descriptor: apogee.MechanismDescriptor{
			ID:       "half_stack",
			Requires: []apogee.MechanismID{"absent_peer"},
		},
		Hook: orderingMech{id: "half_stack"},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	half.Mechanisms = registry
	if _, err := apogee.New(half); !errors.Is(err, apogee.ErrMissingRequirement) {
		t.Errorf("New(half-stack) err = %v, want ErrMissingRequirement", err)
	}

	bogus := validConfig()
	bogus.EnableMechanisms = []apogee.MechanismID{"no_such_mechanism"}
	if _, err := apogee.New(bogus); !errors.Is(err, apogee.ErrUnknownMechanism) {
		t.Errorf("New(bogus-id) err = %v, want ErrUnknownMechanism", err)
	}
}

// TestInterjectChild_NoSuchChildMatchableThroughRoot proves the child-addressing refusal is
// matchable through the root re-export: an embedder outside the module cannot import
// internal/domain (ADR 0010), so apogee.ErrNoSuchChild must BE the sentinel InterjectChild
// returns. A spawn call-ID naming no running sub-agent is the refusal's own case.
func TestInterjectChild_NoSuchChildMatchableThroughRoot(t *testing.T) {
	a, err := apogee.New(validConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = a.InterjectChild("no-such-call-id", apogee.UserInput{Text: "hello"})
	if !errors.Is(err, apogee.ErrNoSuchChild) {
		t.Errorf("InterjectChild(unknown id) err = %v, want ErrNoSuchChild", err)
	}
}

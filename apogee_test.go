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

// orderingMech is a minimal catalogued Mechanism carrying only an ID and ordering
// constraints — the trivial input the cycle gate needs. It implements PreRequestHook
// so MechanismRegistry.Add accepts it (a Mechanism must hook somewhere, ADR 0002).
type orderingMech struct {
	id     apogee.MechanismID
	before []apogee.MechanismID
	after  []apogee.MechanismID
}

func (m orderingMech) Descriptor() apogee.MechanismDescriptor {
	return apogee.MechanismDescriptor{ID: m.id}
}

func (m orderingMech) Ordering() apogee.OrderingConstraints {
	return apogee.OrderingConstraints{Before: m.before, After: m.after}
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
		{name: "missing Model", mutate: func(c *apogee.Config) { c.Model = "" }},
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

func mustAdd(t *testing.T, registry *apogee.MechanismRegistry, m apogee.Mechanism) {
	t.Helper()
	if err := registry.Add(m); err != nil {
		t.Fatalf("Add(%s): %v", m.Descriptor().ID, err)
	}
}

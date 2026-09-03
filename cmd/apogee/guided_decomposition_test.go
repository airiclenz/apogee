package main

// Config-surface acceptance for guided_decomposition (ADR 0014, plan item 5): the
// `mechanisms:` block resolves to Config.EnableMechanisms (mechanisms.ResolveEnabled over the production
// catalogue) and construction (apogee.New) enforces the ADR 0014 §4 stacking gates — IncompatibleWith
// decompose — as a loud startup error, not silent misconfiguration.
//
// The Requires half of §4 no longer has a config-surface case: the peer it named, tool_result_cap, is
// the `tool-result-cap` Floor guard now (ADR 0071), so guided_decomposition declares no Requires edge
// and no catalogued row does. ErrMissingRequirement stays covered where the gate itself lives —
// internal/domain over synthetic rows, internal/agent over an injected registry.

import (
	"errors"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/mechanisms"
)

// guidedEnable resolves the enabled IDs the config surface would hand the engine (mechanisms.ResolveEnabled over
// the real catalogue) onto cfg.EnableMechanisms — the same seam runRoot drives. Building and the ADR
// 0014 §4 stacking gates fire at apogee.New, exactly as they do from a real `mechanisms:` block.
func guidedEnable(t *testing.T, cfg *apogee.Config, enabled map[string]bool) {
	t.Helper()
	ids, _, err := mechanisms.ResolveEnabled(enabled, mechanisms.KnownIDs())
	if err != nil {
		t.Fatalf("ResolveEnabled(%v): %v", enabled, err)
	}
	cfg.EnableMechanisms = ids
}

// Enabling guided_decomposition from the `mechanisms:` block boots cleanly — the capping peer it
// used to require is engine behaviour now, so the row arms on its own.
func TestGuidedDecomposition_BootsFromTheConfigSurface(t *testing.T) {
	t.Parallel()
	cfg := validCfg(t)
	guidedEnable(t, &cfg, map[string]bool{"guided_decomposition": true})

	agent, err := apogee.New(cfg)
	if err != nil {
		t.Fatalf("New with guided_decomposition: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
}

// A `mechanisms:` block still naming the retired tool_result_cap key boots exactly as before — the
// key is dropped with a notice, never refused (the roll's whole purpose).
func TestGuidedDecomposition_RetiredCapKeyStillBoots(t *testing.T) {
	t.Parallel()
	cfg := validCfg(t)
	guidedEnable(t, &cfg, map[string]bool{"guided_decomposition": true, "tool_result_cap": true})

	agent, err := apogee.New(cfg)
	if err != nil {
		t.Fatalf("New with a block still naming the retired tool_result_cap: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
}

// guided_decomposition and decompose steer the same "task too big" symptom by different means and are
// declared incompatible (locked decision 2): enabling both is refused at construction.
func TestGuidedDecomposition_IncompatibleWithDecompose(t *testing.T) {
	t.Parallel()
	cfg := validCfg(t)
	guidedEnable(t, &cfg, map[string]bool{
		"guided_decomposition": true,
		"decompose":            true,
	})

	if _, err := apogee.New(cfg); !errors.Is(err, apogee.ErrIncompatibleMechanisms) {
		t.Errorf("New with decompose also enabled: err = %v, want ErrIncompatibleMechanisms", err)
	}
}

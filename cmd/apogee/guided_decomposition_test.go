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

// A `mechanisms:` block still naming a retired key boots exactly as before — the key is dropped with
// a notice, never refused (the roll's whole purpose). Both of this row's own retired relations are
// covered: tool_result_cap, the peer it once Required, and decompose, the peer it once declared
// IncompatibleWith — a block naming either was valid at v0.20.0 and must still start.
func TestGuidedDecomposition_RetiredKeysStillBoot(t *testing.T) {
	t.Parallel()
	for _, retired := range []string{"tool_result_cap", "decompose"} {
		t.Run(retired, func(t *testing.T) {
			t.Parallel()
			cfg := validCfg(t)
			guidedEnable(t, &cfg, map[string]bool{"guided_decomposition": true, retired: true})

			agent, err := apogee.New(cfg)
			if err != nil {
				t.Fatalf("New with a block still naming the retired %s: %v", retired, err)
			}
			t.Cleanup(func() { _ = agent.Close() })
		})
	}
}

// guided_decomposition and truncate_history cannot stack — a mid-Exchange truncation can drop the
// enumeration message the fan-out cursor re-derives from (F7) — so enabling both is refused at
// construction. The decompose half of this gate (locked decision 2) went with that row's retirement.
func TestGuidedDecomposition_IncompatibleWithTruncateHistory(t *testing.T) {
	t.Parallel()
	cfg := validCfg(t)
	guidedEnable(t, &cfg, map[string]bool{
		"guided_decomposition": true,
		"truncate_history":     true,
	})

	if _, err := apogee.New(cfg); !errors.Is(err, apogee.ErrIncompatibleMechanisms) {
		t.Errorf("New with truncate_history also enabled: err = %v, want ErrIncompatibleMechanisms", err)
	}
}

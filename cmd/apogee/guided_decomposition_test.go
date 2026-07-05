package main

// Config-surface acceptance for the guided_decomposition stack (ADR 0014, plan item 5): the
// `mechanisms:` block resolves to Config.EnableMechanisms (mechanismIDs over the production
// catalogue) and construction (apogee.New) enforces the ADR 0014 §4 stacking gates — Requires
// tool_result_cap, IncompatibleWith decompose — as loud startup errors, not silent misconfiguration.

import (
	"errors"
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/mechanisms"
)

// guidedEnable resolves the enabled IDs the config surface would hand the engine (mechanismIDs over
// the real catalogue) onto cfg.EnableMechanisms — the same seam runRoot drives. Building and the ADR
// 0014 §4 stacking gates fire at apogee.New, exactly as they do from a real `mechanisms:` block.
func guidedEnable(t *testing.T, cfg *apogee.Config, enabled map[string]bool) {
	t.Helper()
	ids, err := mechanismIDs(enabled, mechanisms.KnownIDs())
	if err != nil {
		t.Fatalf("mechanismIDs(%v): %v", enabled, err)
	}
	cfg.EnableMechanisms = ids
}

// Enabling guided_decomposition without its Required peer tool_result_cap is a loud startup error
// with the ADR 0014 §4 wording (the item-1 ValidateRequirements gate, surfaced through New).
func TestGuidedDecomposition_RequiresToolResultCapToBoot(t *testing.T) {
	t.Parallel()
	cfg := validCfg(t)
	guidedEnable(t, &cfg, map[string]bool{"guided_decomposition": true})

	_, err := apogee.New(cfg)
	if err == nil {
		t.Fatal("New with guided_decomposition but no tool_result_cap: want a startup error, got nil")
	}
	const want = `mechanism "guided_decomposition" requires "tool_result_cap" — enable both or neither; they are benched as a stack`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to contain %q", err.Error(), want)
	}
}

// Enabling the full stack (guided_decomposition + tool_result_cap) boots cleanly.
func TestGuidedDecomposition_BootsWithToolResultCap(t *testing.T) {
	t.Parallel()
	cfg := validCfg(t)
	guidedEnable(t, &cfg, map[string]bool{"guided_decomposition": true, "tool_result_cap": true})

	agent, err := apogee.New(cfg)
	if err != nil {
		t.Fatalf("New with the guided_decomposition + tool_result_cap stack: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
}

// guided_decomposition and decompose steer the same "task too big" symptom by different means and are
// declared incompatible (locked decision 2): enabling both (with tool_result_cap present so only the
// incompatibility can surface) is refused at construction.
func TestGuidedDecomposition_IncompatibleWithDecompose(t *testing.T) {
	t.Parallel()
	cfg := validCfg(t)
	guidedEnable(t, &cfg, map[string]bool{
		"guided_decomposition": true,
		"tool_result_cap":      true, // satisfies Requires, so ONLY the decompose incompatibility surfaces
		"decompose":            true,
	})

	if _, err := apogee.New(cfg); !errors.Is(err, apogee.ErrIncompatibleMechanisms) {
		t.Errorf("New with decompose also enabled: err = %v, want ErrIncompatibleMechanisms", err)
	}
}

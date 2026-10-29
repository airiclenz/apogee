package agent

// ValidateSeed: the engine's whole seed check, exported for a Driver to run before it hands the
// lanes anywhere else. Its verdict is construction's own — a *SeedRefusedError carrying exactly
// the sentence New and Resume return for the same seed — and a valid seed passes.

import (
	"errors"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// assertSeedRefusedAsConstruction checks that ValidateSeed refuses the seed cfg carries as a
// *SeedRefusedError, and that both seedConstructors refuse it with that same sentence.
func assertSeedRefusedAsConstruction(t *testing.T, cfg domain.Config) {
	t.Helper()
	gen := domain.Generation{
		Bypass:            cfg.Bypass,
		Floor:             cfg.Floor,
		ContextFillNotice: cfg.ContextFillNotice,
		Observe:           cfg.Observe,
		Sync:              cfg.Sync,
	}
	err := ValidateSeed(gen, cfg.WorkspaceDir)
	var refused *SeedRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("ValidateSeed = %v, want a *SeedRefusedError", err)
	}
	for _, c := range seedConstructors(echoResponder(t, "reply")) {
		a, buildErr := c.build(t, cfg)
		if a != nil {
			_ = a.Close()
			t.Fatalf("%s built an Agent over a seed ValidateSeed refuses", c.name)
		}
		if buildErr == nil {
			t.Fatalf("%s accepted a seed ValidateSeed refuses", c.name)
		}
		if got, want := err.Error(), buildErr.Error(); got != want {
			t.Errorf("ValidateSeed = %q, want %s's %q", got, c.name, want)
		}
	}
}

// TestValidateSeed_RefusesAReservedSyncIDAsConstructionDoes — a sync entry taking a Floor guard's
// name, or the fill notice's, is refused with the sentence New and Resume return.
func TestValidateSeed_RefusesAReservedSyncIDAsConstructionDoes(t *testing.T) {
	t.Parallel()

	for _, id := range []string{FloorGuardKeys()[0], contextFillNoticeID} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			cfg := baseConfig(&recordingSink{})
			cfg.WorkspaceDir = t.TempDir()
			cfg.Sync = []domain.Reaction{goGate(id, domain.GateDecision{Verdict: domain.GateAllow})}

			assertSeedRefusedAsConstruction(t, cfg)
		})
	}
}

// TestValidateSeed_RefusesAnUnresolvableWorkspaceAsConstructionDoes — a sync entry whose
// `workspace:` cannot be resolved (a leading `~` with no home) is refused by the scope step, with
// the sentence New and Resume return. Not parallel: it clears HOME.
func TestValidateSeed_RefusesAnUnresolvableWorkspaceAsConstructionDoes(t *testing.T) {
	t.Setenv("HOME", "")
	cfg := baseConfig(&recordingSink{})
	cfg.WorkspaceDir = t.TempDir()
	entry := goGate("warden", domain.GateDecision{Verdict: domain.GateAllow})
	entry.Workspace = "~/x"
	cfg.Sync = []domain.Reaction{entry}

	assertSeedRefusedAsConstruction(t, cfg)
}

// TestValidateSeed_AcceptsAValidSeed — a well-formed sync lane passes with a nil error.
func TestValidateSeed_AcceptsAValidSeed(t *testing.T) {
	t.Parallel()

	gen := domain.Generation{
		Sync: []domain.Reaction{goGate("warden", domain.GateDecision{Verdict: domain.GateAllow})},
	}
	if err := ValidateSeed(gen, t.TempDir()); err != nil {
		t.Errorf("ValidateSeed = %v, want nil", err)
	}
}

package probe_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
	"github.com/airiclenz/apogee/internal/probe"
)

// The degradation notice fires in EXACTLY one cell of the {mode} × {FSWrite} × {confine}
// matrix: Auto, asking for confinement, on a backend that cannot fence the filesystem — the
// common case in containers, where landlock reports ENOSYS. Every other cell is silent: the
// three lower modes make no confinement promise, an already-unconfined Auto has its own louder
// warning, and a capable backend needs no explanation.
func TestDegradedNotice(t *testing.T) {
	t.Parallel()
	modes := []domain.Mode{domain.ModePlan, domain.ModeAskBefore, domain.ModeAllowEdits, domain.ModeAuto}
	fired := 0
	for _, mode := range modes {
		for _, fsWrite := range []bool{true, false} {
			for _, confine := range []bool{true, false} {
				caps := domain.ConfinementCaps{FSWrite: fsWrite}
				got := probe.DegradedNotice("landlock", caps, mode, confine)
				want := mode == domain.ModeAuto && confine && !fsWrite
				if (got != "") != want {
					t.Errorf("DegradedNotice(landlock, FSWrite=%v, %q, confine=%v) = %q; wantNotice = %v",
						fsWrite, mode, confine, got, want)
				}
				if got == "" {
					continue
				}
				fired++
				for _, want := range []string{"landlock", "approval", "/confine off", "/confine off --save"} {
					if !strings.Contains(got, want) {
						t.Errorf("notice %q does not mention %q", got, want)
					}
				}
			}
		}
	}
	if fired != 1 {
		t.Errorf("notice fired in %d cells of the matrix; want exactly 1 (auto + confine + no FSWrite)", fired)
	}
}

// The notice and the host report name the backend that answered, so the user can tell
// landlock-says-no from no-backend-at-all. domain.Confiner carries no name, so the label is
// derived from the concrete type — including for the host's real backend, whichever OS the
// tests run on. A nil backend is named rather than rendered as "<nil>".
func TestBackendName(t *testing.T) {
	t.Parallel()
	if got := probe.BackendName(platform.NewDenyConfiner()); got != "deny" {
		t.Errorf("BackendName(denyConfiner) = %q; want %q", got, "deny")
	}
	if got := probe.BackendName(stubConfiner{}); got != "stub" {
		t.Errorf("BackendName(stubConfiner) = %q; want %q", got, "stub")
	}
	if got := probe.BackendName(platform.NewConfiner()); got == "" {
		t.Error("BackendName(host backend) = \"\"; the report would name no backend at all")
	}
	if got := probe.BackendName(nil); got != "unknown backend" {
		t.Errorf("BackendName(nil) = %q; want %q", got, "unknown backend")
	}
}

// The capability matrix words BOTH bits, so a report never leaves the reader guessing which
// half of the matrix a backend answered for. It is the single rendering the TUI's /confine
// status also uses.
func TestCapabilityLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		caps domain.ConfinementCaps
		want string
	}{
		{"nothing enforced", domain.ConfinementCaps{}, "landlock (fs-write: unavailable · network: unavailable)"},
		{"fs only", domain.ConfinementCaps{FSWrite: true}, "landlock (fs-write: available · network: unavailable)"},
		{"both", domain.ConfinementCaps{FSWrite: true, NetworkEgress: true}, "landlock (fs-write: available · network: available)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := probe.CapabilityLine("landlock", tt.caps); got != tt.want {
				t.Errorf("CapabilityLine = %q; want %q", got, tt.want)
			}
		})
	}
}

// stubConfiner is a named backend that enforces nothing — it pins the label derivation against
// a type this test owns, independent of the host's real backend.
type stubConfiner struct{}

func (stubConfiner) Capabilities() domain.ConfinementCaps { return domain.ConfinementCaps{} }

func (stubConfiner) Confine(context.Context, domain.ConfinementBox, *exec.Cmd) error { return nil }

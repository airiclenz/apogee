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
		// The fence is real but incomplete (landlock ABI 1–2): the line names what it does not
		// cover, so /confine status, `apogee probe` and the startup line all carry it.
		{"fs with a residual", domain.ConfinementCaps{FSWrite: true, Residuals: []string{"truncate(2)"}},
			"landlock (fs-write: available · network: unavailable · unfenced: truncate(2))"},
		{"more than one residual", domain.ConfinementCaps{FSWrite: true, Residuals: []string{"truncate(2)", "refer(2)"}},
			"landlock (fs-write: available · network: unavailable · unfenced: truncate(2), refer(2))"},
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

// The residual notice fires in EXACTLY one cell of the {mode} × {FSWrite} × {confine} ×
// {residual} matrix: Auto, asking for confinement, on a backend that CAN fence but discloses a
// write-class access it cannot cover (landlock ABI 1–2 and truncate(2)). It is the sibling of the
// degradation notice, never its overlap — that one needs FSWrite false, this one needs it true —
// so the last assertion here is that no input makes both speak.
func TestResidualNotice(t *testing.T) {
	t.Parallel()
	modes := []domain.Mode{domain.ModePlan, domain.ModeAskBefore, domain.ModeAllowEdits, domain.ModeAuto}
	residualSets := [][]string{nil, {"truncate(2)"}}
	fired := 0
	for _, mode := range modes {
		for _, fsWrite := range []bool{true, false} {
			for _, confine := range []bool{true, false} {
				for _, residuals := range residualSets {
					caps := domain.ConfinementCaps{FSWrite: fsWrite, Residuals: residuals}
					got := probe.ResidualNotice("landlock", caps, mode, confine)
					want := mode == domain.ModeAuto && confine && fsWrite && len(residuals) > 0
					if (got != "") != want {
						t.Errorf("ResidualNotice(landlock, FSWrite=%v, residuals=%v, %q, confine=%v) = %q; wantNotice = %v",
							fsWrite, residuals, mode, confine, got, want)
					}
					// Mutually exclusive by construction: a residual is a disclosure, the
					// degradation notice is the unfenceable-host story, and a user must never
					// be handed both at once.
					if got != "" && probe.DegradedNotice("landlock", caps, mode, confine) != "" {
						t.Errorf("both notices fire for FSWrite=%v, residuals=%v, %q, confine=%v; they must be exclusive",
							fsWrite, residuals, mode, confine)
					}
					if got == "" {
						continue
					}
					fired++
					// It names the backend that answered and the access it leaves open, states
					// the consequence in the user's terms, and points at the kernel that closes
					// it — never at a remedy that loosens anything.
					for _, want := range []string{"landlock", "truncate(2)", "empty an existing file", "6.2"} {
						if !strings.Contains(got, want) {
							t.Errorf("residual notice %q does not mention %q", got, want)
						}
					}
					if strings.Contains(got, "/confine off") {
						t.Errorf("residual notice offers /confine off; a disclosure must not read as a remedy to loosen:\n%s", got)
					}
				}
			}
		}
	}
	if fired != 1 {
		t.Errorf("notice fired in %d cells of the matrix; want exactly 1 (auto + confine + FSWrite + a residual)", fired)
	}
}

// The auto ladder an UNATTENDED run is held to is the one a LAUNCH is held to (ADR 0033, decision
// 3) — never stricter, and never silently escalating: the verdict fires iff confinement was asked
// for AND the backend cannot fence, which is the mirror of caps.AutoEligible(). Both surfaces that
// offer Auto with nobody behind it — a Schedule's Firing and `apogee headless` — read the same
// sentence out of it, so it is asserted here for both nouns and for a named and an unnamed-fence
// backend: a user who meets this refusal at one surface must not meet a weaker story at the other.
func TestAutoUnattendedBlockedMirrorsTheAutoLadder(t *testing.T) {
	t.Parallel()

	fencing := domain.ConfinementCaps{FSWrite: true}
	var none domain.ConfinementCaps

	tests := []struct {
		name               string
		caps               domain.ConfinementCaps
		confineToWorkspace bool
		wantBlocked        bool
	}{
		{name: "a host that can fence offers auto", caps: fencing, confineToWorkspace: true},
		{
			name:               "a host that cannot fence blocks auto — an unattended run has no approval rung",
			caps:               none,
			confineToWorkspace: true,
			wantBlocked:        true,
		},
		{name: "the user's own unconfined opt-in offers auto anyway", caps: none},
		{name: "unconfined on a fencing host offers auto", caps: fencing},
	}
	for _, tt := range tests {
		for _, subject := range []string{"a firing", "a headless run"} {
			for _, backend := range []string{"deny", "landlock"} {
				t.Run(tt.name+" / "+subject+" on "+backend, func(t *testing.T) {
					t.Parallel()

					got := probe.AutoUnattendedBlocked(subject, backend, tt.caps, tt.confineToWorkspace)

					if blocked := got != ""; blocked != tt.wantBlocked {
						t.Fatalf("AutoUnattendedBlocked = %q (blocked=%v), want blocked=%v",
							got, blocked, tt.wantBlocked)
					}
					if tt.wantBlocked != (tt.confineToWorkspace && !tt.caps.AutoEligible()) {
						t.Fatalf("the case itself disagrees with caps.AutoEligible()=%v; the verdict is its mirror",
							tt.caps.AutoEligible())
					}
					if !tt.wantBlocked {
						return
					}
					want := "the " + backend + " backend on this host reports no filesystem confinement, " +
						"so auto falls back to approval — and " + subject + " has nobody to ask"
					if got != want {
						t.Errorf("the refusal reads\n  %q\nwant\n  %q", got, want)
					}
				})
			}
		}
	}
}

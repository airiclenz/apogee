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

// The notice carries the REASON the backend cannot fence, verbatim as the backend worded it, so a
// degraded session is diagnosable from the startup line alone — a user told only "the namespace
// backend reports no filesystem confinement" has to run /confine status to learn whether the probe
// timed out on a loaded box or bwrap was never installed. The wants below are the whole string the
// program prints, so the reason's placement is pinned and not merely its presence, and the third
// case is the cell three shipped backends land in with nothing to say: an empty sentence keeps the
// pre-existing notice exactly, never a dangling "why:".
func TestDegradedNoticeNamesTheReason(t *testing.T) {
	t.Parallel()
	const remedy = "  To run unconfined instead (safe ONLY on a disposable machine):\n" +
		"    /confine off          — this session\n" +
		"    /confine off --save   — and remember this host in ~/.apogee/config.yaml"
	tests := []struct {
		name    string
		backend string
		caps    domain.ConfinementCaps
		want    string
	}{
		// The probe started and ran out of budget: the host may well be capable, which is exactly
		// the story a bare "reports no filesystem confinement" loses.
		{"a probe that timed out", "namespace",
			domain.ConfinementCaps{Unavailable: "bwrap timed out", Cause: domain.CauseProbeTimedOut},
			"apogee: auto mode is gating terminal commands — the namespace backend on this host reports no\n" +
				"  filesystem confinement, so commands cannot be fenced and fall back to approval.\n" +
				"  why: bwrap timed out\n" + remedy},
		// The facility is not here at all — a different fix for the user, and so a different line.
		{"an absent backend", "namespace",
			domain.ConfinementCaps{Unavailable: "bwrap not on PATH", Cause: domain.CauseBackendAbsent},
			"apogee: auto mode is gating terminal commands — the namespace backend on this host reports no\n" +
				"  filesystem confinement, so commands cannot be fenced and fall back to approval.\n" +
				"  why: bwrap not on PATH\n" + remedy},
		// The no-backend stub, a macOS without sandbox-exec and a closed Windows token: nothing
		// to say, so nothing is said.
		{"a backend with nothing to say", "deny",
			domain.ConfinementCaps{Cause: domain.CauseBackendAbsent},
			"apogee: auto mode is gating terminal commands — the deny backend on this host reports no\n" +
				"  filesystem confinement, so commands cannot be fenced and fall back to approval.\n" + remedy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := probe.DegradedNotice(tt.backend, tt.caps, domain.ModeAuto, true)

			if got != tt.want {
				t.Errorf("DegradedNotice =\n%s\nwant\n%s", got, tt.want)
			}
			if tt.caps.Unavailable != "" && !strings.Contains(got, tt.caps.Unavailable) {
				t.Errorf("notice does not carry the backend's own reason %q:\n%s", tt.caps.Unavailable, got)
			}
		})
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
		name    string
		backend string
		caps    domain.ConfinementCaps
		want    string
	}{
		{"nothing enforced", "landlock", domain.ConfinementCaps{}, "landlock (fs-write: unavailable · network: unavailable)"},
		{"fs only", "landlock", domain.ConfinementCaps{FSWrite: true}, "landlock (fs-write: available · network: unavailable)"},
		{"both", "landlock", domain.ConfinementCaps{FSWrite: true, NetworkEgress: true}, "landlock (fs-write: available · network: available)"},
		// The fence is real but incomplete (landlock ABI 1–2): the line names what it does not
		// cover, so /confine status, `apogee probe` and the startup line all carry it.
		{"fs with a residual", "landlock", domain.ConfinementCaps{FSWrite: true, Residuals: []string{"truncate(2)"}},
			"landlock (fs-write: available · network: unavailable · unfenced: truncate(2))"},
		{"more than one residual", "landlock", domain.ConfinementCaps{FSWrite: true, Residuals: []string{"truncate(2)", "refer(2)"}},
			"landlock (fs-write: available · network: unavailable · unfenced: truncate(2), refer(2))"},
		// No fence at all: the line says WHY, after the network cell, so the three surfaces
		// tell the user which host fact stands between them and a confined Auto.
		{"unfenceable with a reason", "namespace", domain.ConfinementCaps{Unavailable: "bwrap not on PATH"},
			"namespace (fs-write: unavailable · network: unavailable · why: bwrap not on PATH)"},
		// A reason beside a working fence is stale by definition and is never rendered.
		{"fs with a stale reason", "landlock", domain.ConfinementCaps{FSWrite: true, Unavailable: "stale"},
			"landlock (fs-write: available · network: unavailable)"},
		// The network-egress residuals a deny box leaves open ride the SAME list: CapabilityLine is
		// the honesty surface for them, since the write-class startup banner filters them out.
		{"the network residuals", "landlock", domain.ConfinementCaps{FSWrite: true, NetworkEgress: true,
			Residuals: []string{domain.ResidualUDPEgress, domain.ResidualUnixEgress}},
			"landlock (fs-write: available · network: available · unfenced: connect(2) UDP, connect(2) AF_UNIX)"},
		// Both disclosures at once: the residual is named first, the reason last.
		{"residual and a reason", "landlock", domain.ConfinementCaps{Residuals: []string{"truncate(2)"}, Unavailable: "ENOSYS"},
			"landlock (fs-write: unavailable · network: unavailable · unfenced: truncate(2) · why: ENOSYS)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := probe.CapabilityLine(tt.backend, tt.caps); got != tt.want {
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

// The residual notice fires in EXACTLY the cells of the {mode} × {FSWrite} × {confine} ×
// {residual} matrix where Auto asks for confinement on a backend that CAN fence and discloses a
// WRITE-CLASS access it cannot cover (landlock ABI 1–2 and truncate(2)). A set holding only
// network-egress tokens is not one of them — those are CapabilityLine's to word — so the matrix
// carries a network-only set and a mixed set beside the two originals, and the mixed one fires the
// truncate story carrying no network token. It is the sibling of the degradation notice, never its
// overlap — that one needs FSWrite false, this one needs it true — so the last assertion here is
// that no input makes both speak.
func TestResidualNotice(t *testing.T) {
	t.Parallel()
	modes := []domain.Mode{domain.ModePlan, domain.ModeAskBefore, domain.ModeAllowEdits, domain.ModeAuto}
	residualSets := [][]string{
		nil,
		{"truncate(2)"},
		{domain.ResidualUDPEgress},
		{"truncate(2)", domain.ResidualUDPEgress},
	}
	fired := 0
	for _, mode := range modes {
		for _, fsWrite := range []bool{true, false} {
			for _, confine := range []bool{true, false} {
				for _, residuals := range residualSets {
					caps := domain.ConfinementCaps{FSWrite: fsWrite, Residuals: residuals}
					got := probe.ResidualNotice("landlock", caps, mode, confine)
					want := mode == domain.ModeAuto && confine && fsWrite && holdsWriteClassToken(residuals)
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
					if strings.Contains(got, domain.ResidualUDPEgress) {
						t.Errorf("residual notice words a network token the auto banner filters out:\n%s", got)
					}
					if strings.Contains(got, "/confine off") {
						t.Errorf("residual notice offers /confine off; a disclosure must not read as a remedy to loosen:\n%s", got)
					}
				}
			}
		}
	}
	if fired != 2 {
		t.Errorf("notice fired in %d cells of the matrix; want exactly 2 (auto + confine + FSWrite, for the two sets holding a write-class token)", fired)
	}
}

// holdsWriteClassToken is the matrix's own answer to "should this set say anything" — a set speaks
// iff it holds a token that is not one of the two network-egress ones. Spelled out here rather than
// reusing the production filter, so the test states the rule instead of quoting the code it checks.
func holdsWriteClassToken(residuals []string) bool {
	for _, residual := range residuals {
		if residual != domain.ResidualUDPEgress && residual != domain.ResidualUnixEgress {
			return true
		}
	}
	return false
}

// The network-egress residuals are disclosure for the capability line, never a startup banner: a
// landlock host at ABI 4+ discloses the UDP and pathname-AF_UNIX egress its deny box cannot fence,
// and that host must enter auto exactly as quietly as it did before it started saying so. Asserted
// in the ONE cell that would otherwise fire — auto + confine + FSWrite — so a filter that stopped
// filtering could not hide behind a mode or a flag. (TestResidualNotice's matrix owns the
// write-class story; this is its network counterpart, standalone by design.)
func TestResidualNoticeIsSilentForNetworkOnlyResiduals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		residuals []string
		wantSaid  bool
	}{
		{"udp_alone", []string{domain.ResidualUDPEgress}, false},
		{"unix_alone", []string{domain.ResidualUnixEgress}, false},
		{"both_network_classes", []string{domain.ResidualUDPEgress, domain.ResidualUnixEgress}, false},
		// The filter is a deny-list, not an allow-list: a write-class token riding alongside the
		// network ones still reaches the operator, and reaches it WITHOUT them in the sentence.
		{"a_write_class_token_alongside_them", []string{"truncate(2)", domain.ResidualUDPEgress, domain.ResidualUnixEgress}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			caps := domain.ConfinementCaps{FSWrite: true, NetworkEgress: true, Residuals: tt.residuals}

			got := probe.ResidualNotice("landlock", caps, domain.ModeAuto, true)

			if (got != "") != tt.wantSaid {
				t.Fatalf("ResidualNotice(residuals=%v) = %q; wantNotice = %v", tt.residuals, got, tt.wantSaid)
			}
			if got == "" {
				return
			}
			if !strings.Contains(got, "truncate(2)") {
				t.Errorf("notice drops the write-class token it exists to say:\n%s", got)
			}
			for _, unwanted := range []string{domain.ResidualUDPEgress, domain.ResidualUnixEgress} {
				if strings.Contains(got, unwanted) {
					t.Errorf("notice words the network residual %q; the auto banner is write-class only:\n%s", unwanted, got)
				}
			}
		})
	}
}

// Every token the notice names is worded on its own terms. truncate(2) is the only one this
// project has a consequence for; a write-class token it has never seen must still reach the
// operator, and must reach them with the NEUTRAL clause — what is true of any residual by
// definition — rather than borrowing truncate(2)'s "can still empty an existing file", which would
// state of the new access something that does not follow from it. The mixed row is the one that
// would have caught the old single-sentence shape: two tokens, two clauses, one consequence each.
func TestResidualNoticeWordsEachTokenOnItsOwnTerms(t *testing.T) {
	t.Parallel()

	const unknown = "refer(2)"

	t.Run("an unknown write-class token gets the neutral clause", func(t *testing.T) {
		t.Parallel()
		caps := domain.ConfinementCaps{FSWrite: true, Residuals: []string{unknown}}

		got := probe.ResidualNotice("landlock", caps, domain.ModeAuto, true)

		if got == "" {
			t.Fatalf("ResidualNotice(residuals=%v) said nothing; an unfenced write-class access must reach the operator", caps.Residuals)
		}
		if !strings.Contains(got, unknown) {
			t.Errorf("notice does not name the token it exists to disclose:\n%s", got)
		}
		if !strings.Contains(got, "can still perform it outside the workspace") {
			t.Errorf("notice does not word the token neutrally:\n%s", got)
		}
		for _, borrowed := range []string{"empty an existing file", "6.2", "create-and-write"} {
			if strings.Contains(got, borrowed) {
				t.Errorf("notice lends truncate(2)'s consequence %q to %s:\n%s", borrowed, unknown, got)
			}
		}
	})

	t.Run("two tokens get one clause each", func(t *testing.T) {
		t.Parallel()
		caps := domain.ConfinementCaps{FSWrite: true, Residuals: []string{"truncate(2)", unknown}}

		got := probe.ResidualNotice("landlock", caps, domain.ModeAuto, true)

		if !strings.Contains(got, "truncate(2) — ") || !strings.Contains(got, unknown+" — ") {
			t.Fatalf("notice does not word both tokens:\n%s", got)
		}
		// The specific consequence stays with the token it belongs to: it is said once, on
		// truncate(2)'s line, and the unknown token's line is a different sentence.
		if strings.Count(got, "empty an existing file") != 1 {
			t.Errorf("truncate(2)'s consequence is not said exactly once:\n%s", got)
		}
		truncateLine, unknownLine := "", ""
		for _, line := range strings.Split(got, "\n") {
			if strings.Contains(line, "truncate(2) — ") {
				truncateLine = line
			}
			if strings.Contains(line, unknown+" — ") {
				unknownLine = line
			}
		}
		if !strings.Contains(truncateLine, "empty an existing file") {
			t.Errorf("truncate(2)'s clause does not carry its own consequence:\n%s", got)
		}
		if strings.Contains(unknownLine, "empty an existing file") {
			t.Errorf("the unknown token's clause carries truncate(2)'s consequence:\n%s", got)
		}
	})
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

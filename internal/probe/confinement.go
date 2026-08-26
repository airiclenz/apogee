package probe

import (
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The shared confinement wording (ADR 0012's world, reported and never re-decided)
// ----------------------------------------------------------------------------
//
// These functions are the single source for how this project SAYS what a Confiner
// backend can enforce. The composition root prints the degradation notice at startup, the TUI
// renders /confine status from the same values, and the host report below states them
// off-session — three surfaces, one wording, because a user diagnosing a gating Auto must not
// be told two different stories depending on where they asked.

// BackendName renders the human label for a Confiner backend ("landlock", "seatbelt", "deny").
// domain.Confiner deliberately carries no name — it reports capabilities, not identity — so the
// label is derived from the concrete type ("*platform.landlockConfiner" → "landlock"). A shape
// it does not recognise degrades to the bare type name, which still tells the user which
// backend answered; a nil backend (a binary that wired none) is named as such rather than
// rendering "<nil>" in the middle of a sentence.
func BackendName(c domain.Confiner) string {
	if c == nil {
		return "unknown backend"
	}
	name := strings.TrimPrefix(fmt.Sprintf("%T", c), "*")
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	if trimmed := strings.TrimSuffix(name, "Confiner"); trimmed != "" {
		name = trimmed
	}
	return name
}

// CapabilityLine renders a backend and its capability matrix, e.g.
// "landlock (fs-write: available · network: unavailable)". fs-write is the load-bearing one (it
// is what Auto's subprocess disposition keys on — ADR 0012's FSWrite-only AutoEligible);
// network egress is reported beside it because a Confiner answers for both.
//
// A backend that fences writes but knowingly leaves a write-class access open discloses those
// in caps.Residuals (contract §5), and the line names them: "landlock (fs-write: available ·
// network: unavailable · unfenced: truncate(2))". Appending them HERE rather than at each
// surface is what makes /confine status, `apogee probe` and the startup line say it together —
// one function, three surfaces, as the rest of this file's wording already works.
func CapabilityLine(backend string, caps domain.ConfinementCaps) string {
	line := fmt.Sprintf("%s (fs-write: %s · network: %s",
		backend, availability(caps.FSWrite), availability(caps.NetworkEgress))
	if len(caps.Residuals) > 0 {
		line += " · unfenced: " + strings.Join(caps.Residuals, ", ")
	}
	return line + ")"
}

// availability words one capability bit for the matrix line.
func availability(ok bool) string {
	if ok {
		return "available"
	}
	return "unavailable"
}

// DegradedNotice returns the one-line-plus-remedy notice for Auto entered with confinement
// asked for (the default) on a host whose Confiner backend cannot fence the filesystem —
// caps.FSWrite == false. That is not a malfunction: the ladder is doing exactly what ADR 0012
// says ("confine if you can, gate if you can't"), so every terminal command takes the Approval
// path. Nothing said so, which is why Auto read as broken on containers where
// landlock_create_ruleset returns ENOSYS (ISSUES.md, 2026-07-21). The notice states the blast
// radius plainly and names the sanctioned route to the user's OWN decision — it never loosens
// anything by itself.
//
// It returns "" (no notice) in every other cell: the three lower modes make no confinement
// promise, an already-unconfined Auto has its own louder warning at the call site, and a
// backend that CAN fence needs no explanation. Pure so the wording is table-testable without
// capturing os.Stderr.
func DegradedNotice(backendName string, caps domain.ConfinementCaps, mode domain.Mode, confineToWorkspace bool) string {
	if mode != domain.ModeAuto || !confineToWorkspace || caps.FSWrite {
		return ""
	}
	return fmt.Sprintf(
		"apogee: auto mode is gating terminal commands — the %s backend on this host reports no\n"+
			"  filesystem confinement, so commands cannot be fenced and fall back to approval.\n"+
			"  To run unconfined instead (safe ONLY on a disposable machine):\n"+
			"    /confine off          — this session\n"+
			"    /confine off --save   — and remember this host in ~/.apogee/config.yaml",
		backendName)
}

// ResidualNotice returns the one-line-plus-consequence notice for Auto entered with confinement
// asked for, on a backend that CAN fence the filesystem but discloses a write-class access it
// cannot cover (caps.Residuals — landlock ABI 1–2, where truncate(2) has no bit; contract §5).
// The fence is doing its job and Auto is not degraded, so nothing is refused and nothing is
// loosened: the operator is simply told the one thing the fence does not stop, and which kernel
// closes it.
//
// It is the SIBLING of DegradedNotice, never its overlap: DegradedNotice speaks where FSWrite is
// false — the cell headless and the daemon refuse Auto on — and this one speaks only where
// FSWrite is true, so the two are mutually exclusive by construction. A residual is a disclosure,
// not a refusal, which is why no caller may treat it as a blocker. "" in every other cell, and
// pure so the wording is table-testable without capturing os.Stderr.
func ResidualNotice(backendName string, caps domain.ConfinementCaps, mode domain.Mode, confineToWorkspace bool) string {
	if mode != domain.ModeAuto || !confineToWorkspace || !caps.FSWrite || len(caps.Residuals) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"apogee: auto mode confines terminal commands, but the %s backend on this kernel cannot fence %s —\n"+
			"  a confined command can still empty an existing file outside the workspace (landlock ABI 1–2, kernel < 6.2).\n"+
			"  A kernel ≥ 6.2 closes it; until then treat auto's fence as create-and-write only.",
		backendName, strings.Join(caps.Residuals, ", "))
}

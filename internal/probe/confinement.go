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
// network: unavailable · unfenced: truncate(2))". A backend that cannot fence at all discloses
// the reason in caps.Unavailable, and the line says it last: "namespace (fs-write: unavailable ·
// network: unavailable · why: bwrap not on PATH)" — only while fs-write is unavailable, since a
// reason beside a working fence would be a stale story. Appending both HERE rather than at each
// surface is what makes /confine status, `apogee probe` and the startup line say it together —
// one function, three surfaces, as the rest of this file's wording already works.
func CapabilityLine(backend string, caps domain.ConfinementCaps) string {
	line := fmt.Sprintf("%s (fs-write: %s · network: %s",
		backend, availability(caps.FSWrite), availability(caps.NetworkEgress))
	if len(caps.Residuals) > 0 {
		line += " · unfenced: " + strings.Join(caps.Residuals, ", ")
	}
	if !caps.FSWrite && caps.Unavailable != "" {
		line += " · why: " + caps.Unavailable
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
// landlock_create_ruleset returns ENOSYS (the issue register, 2026-07-21). The notice states the
// blast radius plainly and names the sanctioned route to the user's OWN decision — it never
// loosens anything by itself.
//
// It names the backend AND why that backend cannot fence: caps.Unavailable — the same prose
// CapabilityLine renders as "why: …" for /confine status and `apogee probe` — goes on a line of
// its own beneath the fallback sentence, so a user whose session lost confinement reads the host
// fact off the startup notice instead of having to ask a second surface for it. The wording sits
// HERE, with this package's other two, for the reason the file header gives: three surfaces, one
// story. An empty sentence is not a missing reason (domain.ConfinementCaps.Unavailable) but it is
// nothing to print — the no-backend stub every OS without a real facility gets, a macOS without
// sandbox-exec, and a Windows token the session has closed all reach this cell with nothing to
// say — so an empty reason emits no line rather than a dangling "why:", leaving the notice
// exactly as it read before.
//
// It returns "" (no notice) in every other cell: the three lower modes make no confinement
// promise, an already-unconfined Auto has its own louder warning at the call site, and a
// backend that CAN fence needs no explanation. Pure so the wording is table-testable without
// capturing os.Stderr.
func DegradedNotice(backendName string, caps domain.ConfinementCaps, mode domain.Mode, confineToWorkspace bool) string {
	if mode != domain.ModeAuto || !confineToWorkspace || caps.FSWrite {
		return ""
	}
	why := ""
	if caps.Unavailable != "" {
		why = "  why: " + caps.Unavailable + "\n"
	}
	return fmt.Sprintf(
		"apogee: auto mode is gating terminal commands — the %s backend on this host reports no\n"+
			"  filesystem confinement, so commands cannot be fenced and fall back to approval.\n"+
			"%s"+
			"  To run unconfined instead (safe ONLY on a disposable machine):\n"+
			"    /confine off          — this session\n"+
			"    /confine off --save   — and remember this host in ~/.apogee/config.yaml",
		backendName, why)
}

// ResidualNotice returns the notice for Auto entered with confinement asked for, on a backend
// that CAN fence the filesystem but discloses a write-class access it cannot cover (caps.Residuals
// — landlock ABI 1–2, where truncate(2) has no bit; contract §5).
// It is write-class ONLY: the network-egress-class tokens are filtered out (writeClassResiduals),
// so a backend that discloses nothing but the egress a deny box leaves open says nothing here and
// no host gains a startup banner for a fence it never asked for. CapabilityLine still names them.
// The fence is doing its job and Auto is not degraded, so nothing is refused and nothing is
// loosened: the operator is simply told what the fence does not stop.
//
// Every token it names is WORDED — one clause each, from residualConsequence — rather than
// interpolated into a single sentence whose consequence belongs to truncate(2) alone. A set of two
// once read as "cannot fence truncate(2), refer(2) — a confined command can still empty an existing
// file", which states of refer(2) something that does not follow from it; a reader cannot tell a
// disclosure they must act on from one the sentence merely swept up.
//
// It is the SIBLING of DegradedNotice, never its overlap: DegradedNotice speaks where FSWrite is
// false — the cell headless and the daemon refuse Auto on — and this one speaks only where
// FSWrite is true, so the two are mutually exclusive by construction. A residual is a disclosure,
// not a refusal, which is why no caller may treat it as a blocker. "" in every other cell, and
// pure so the wording is table-testable without capturing os.Stderr.
func ResidualNotice(backendName string, caps domain.ConfinementCaps, mode domain.Mode, confineToWorkspace bool) string {
	if mode != domain.ModeAuto || !confineToWorkspace || !caps.FSWrite {
		return ""
	}
	writeClass := writeClassResiduals(caps.Residuals)
	if len(writeClass) == 0 {
		return ""
	}
	clauses := make([]string, 0, len(writeClass))
	for _, residual := range writeClass {
		clauses = append(clauses, "  "+residual+" — "+residualConsequence(residual))
	}
	return fmt.Sprintf(
		"apogee: auto mode confines terminal commands, but the %s backend on this kernel cannot fence:\n%s",
		backendName, strings.Join(clauses, "\n"))
}

// residualTruncate is the one residual token this package has consequence wording for. It is the
// spelling internal/platform's landlock backend emits; the two are not a shared constant because a
// drift here is HARMLESS by construction — an unrecognised token falls to the neutral clause below,
// which is true of every residual, where sharing a constant with the backend that discovers the gap
// would invert the dependency for no honesty gained.
const residualTruncate = "truncate(2)"

// residualConsequence words ONE residual token: what a confined command can still do with the
// access this backend leaves open, in the operator's terms. A token it knows gets the specific
// consequence and the kernel that closes it; a token it does not gets the NEUTRAL clause — the one
// thing true of every residual by definition (contract §5: an access the backend knowingly cannot
// fence) — never truncate(2)'s consequence borrowed for it. Silence is not an option either: a
// backend that grows a new unfenced write-class access must still reach the operator, so the
// default arm says what it can rather than dropping the token.
func residualConsequence(residual string) string {
	if residual == residualTruncate {
		return "a confined command can still empty an existing file outside the workspace\n" +
			"    (landlock ABI 1–2, kernel < 6.2). A kernel ≥ 6.2 closes it; until then treat auto's\n" +
			"    fence as create-and-write only."
	}
	return "the backend discloses this access rather than fencing it, so a\n" +
		"    confined command can still perform it outside the workspace."
}

// writeClassResiduals drops the network-egress-class tokens from a residual set, leaving the
// write-class ones ResidualNotice speaks for. It is a DENY-list on the two domain.Residual*
// network tokens rather than an allow-list of known write-class names: a backend that grows a
// NEW unfenced write-class access must still reach the notice, and silence is the one failure
// mode capability honesty cannot tolerate. CapabilityLine reads the unfiltered set — the whole
// disclosure belongs on the honesty surface; only the auto-mode banner is write-class.
func writeClassResiduals(residuals []string) []string {
	writeClass := make([]string, 0, len(residuals))
	for _, residual := range residuals {
		if residual == domain.ResidualUDPEgress || residual == domain.ResidualUnixEgress {
			continue
		}
		writeClass = append(writeClass, residual)
	}
	return writeClass
}

// AutoUnattendedBlocked returns the reason an UNATTENDED auto run may not start on this host, or ""
// when it may. Two surfaces offer Auto without a human behind it — a Schedule's Firing and `apogee
// headless` — and each refuses it itself ("the surface that offers Auto is the one that refuses it",
// ADR 0033, decision 3), so the verdict lives here once and differs only in the noun it calls the
// run (subject). A user who meets this refusal at one surface must not meet a weaker story at the
// other.
//
// It is the MIRROR of DegradedNotice's gate, with the mode fixed: an interactive Auto on a host
// that cannot fence keeps working because every terminal command falls back to the Approval path,
// and that is precisely the rung an unattended run does not have — its Approver denies rather than
// asks (ADR 0033, decision 2), so auto there would be a plan-shaped run that fails loudly at every
// terminal command.
// The unconfined case (`confine-to-workspace: false`) is NOT blocked: that is the user's own
// explicit "I am the sandbox", the same ladder that lets the session launch in Auto, and neither a
// schedule nor a headless run is held to a stricter bar than a launch (decision 3).
//
// Pure, so the wording is table-testable without a host.
func AutoUnattendedBlocked(subject, backend string, caps domain.ConfinementCaps, confineToWorkspace bool) string {
	if !confineToWorkspace || caps.AutoEligible() {
		return ""
	}
	return fmt.Sprintf(
		"the %s backend on this host reports no filesystem confinement, so auto falls back to "+
			"approval — and %s has nobody to ask", backend, subject)
}

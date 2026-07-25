package winlabel

import (
	"fmt"
	"strings"
)

// Remedy is ADR 0020 §2's manual undo — an explicit Medium label is behaviourally
// identical to no label at all. Every surface that reports outstanding labels quotes THIS
// string, so the off-session host report and the end-of-session teardown warning hand the user
// one remedy rather than two spellings of it.
const Remedy = "icacls <path> /setintegritylevel (OI)(CI)M /T /C"

// residueIndent aligns a continuation line under the host report's "labels:" field,
// which renders the notice verbatim (probe.Host.Report).
const residueIndent = "                 "

// residueNotice words the outstanding-journal findings, or "" when there are none. It
// is pure so the wording is table-testable on any host, and it names ADR 0020's manual
// remedy — an explicit Medium label is behaviourally identical to no label at all.
//
// The two findings are worded separately because their remedies genuinely differ: labels a
// readable journal describes are reverted by the next session automatically, whereas an
// UNREADABLE journal is a dead end for every automatic path — recovery skips what it cannot
// decode — so the manual undo is the only remedy there is.
func residueNotice(roots, unreadable []string) string {
	var lines []string
	if len(roots) > 0 {
		lines = append(lines,
			fmt.Sprintf("%d path(s) may still carry apogee's Low integrity label: %s", len(roots), strings.Join(roots, ", ")),
			"(a run was interrupted, or another apogee holds them now; a new session",
			fmt.Sprintf("reverts them automatically, or: %s)", Remedy))
	}
	if len(unreadable) > 0 {
		for _, path := range unreadable {
			lines = append(lines, fmt.Sprintf("journal present but unreadable: %s", path))
		}
		lines = append(lines,
			"(a crash or an edit left it undecodable, so no run can revert what it names;",
			fmt.Sprintf("delete it once the paths it covered are back to: %s)", Remedy))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n"+residueIndent)
}

// ProgressNotice words the "please wait" line the composition root prints on stderr
// before the first Low-labelling walk of root, so the one-time pass (ADR 0020 §2) stops being a
// silent hang on a workspace with a large .git or node_modules — the click-through-frustration
// trap the auto-confinement work was built to avoid.
//
// It takes NO object count on purpose: LabelTree streams via filepath.WalkDir, so a pre-count is
// a second full walk that doubles the ~1 ms/object cost, and an after-the-fact summary prints
// after the wait it was meant to explain. So the notice is indeterminate and upfront. It is
// worded as the fence doing its job, never as a malfunction, matching probe.DegradedNotice's
// tone, and it quotes Remedy verbatim where it names the manual undo, so the wait
// notice, the teardown warning and the host report all hand the user ONE spelling of the remedy
// rather than three. Pure, so the wording is table-testable on any host.
func ProgressNotice(root string) string {
	return fmt.Sprintf(
		"apogee: labelling the workspace %s Low so the confined child can write in it; "+
			"a large .git or node_modules may take several seconds (undo manually: %s)",
		root, Remedy)
}

// TeardownNotice words a confinement teardown that could not put the disk back, for
// the composition root to print on stderr at shutdown — the one moment the user can still act
// on it. It returns "" when err is nil, so the caller can state it unconditionally, exactly as
// it does with ConfinementResidue and the degradation notice.
//
// err is the backend's Close error; on Windows it already names the journal that SURVIVED the
// failed revert, which is what makes the labels recoverable. The remedy is residueNotice's
// verbatim, because this is the same situation seen from inside the session that caused it.
func TeardownNotice(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("apogee: WARNING — confinement teardown did not put every path back: %v; "+
		"a new session reverts them automatically, or: %s", err, Remedy)
}

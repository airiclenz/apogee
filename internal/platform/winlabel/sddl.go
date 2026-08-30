package winlabel

import "strings"

// The mandatory-label SDDL strings the backend writes (ADR 0020 §2).
//
//   - lowSDDL labels a path Low with NO_WRITE_UP and NO inheritance flags. It is the one label
//     the backend writes, on directories and files alike. The label pass reaches every existing
//     object by walking (LabelTree), and a Low child's own creations are labelled Low by the
//     kernel from its token, so inheritance buys nothing the walk does not already deliver —
//     and it cost something real: SetNamedSecurityInfo propagates an inheritable ACE to every
//     EXISTING descendant the moment the root is labelled, before the walk can decide anything,
//     which put apogee's Low label onto hard-linked files at every name they have, including
//     names outside the box (the pnpm-store leak). The accepted cost is that an object a
//     MEDIUM subject creates inside the box mid-run — apogee's own write tool, the user's
//     editor — stays implicitly Medium until the next label pass; the confined child reads it
//     but cannot edit it.
//   - clearSDDL is a NULL SACL: no mandatory label at all, which is the state
//     an unlabelled object is in (implicitly Medium with NO_WRITE_UP). It is what teardown
//     writes to a path that carried no label before the run. Clearing via a NULL SACL keeps
//     the restore inside LABEL_SECURITY_INFORMATION, which needs only WRITE_OWNER on the
//     object; asking additionally for UNPROTECTED_SACL_SECURITY_INFORMATION would drag in
//     the SACL privilege check (SeSecurityPrivilege) and fail for an ordinary user.
const (
	lowSDDL   = "S:(ML;;NW;;;LW)"
	clearSDDL = "S:"
)

// labelACEPrefix is the SDDL spelling of a mandatory-label ACE. A descriptor string
// containing it carries an explicit label that teardown must put back rather than clear.
const labelACEPrefix = "(ML;"

// foldPath case-folds a path for the journal's one-entry-per-path rule. Windows paths are
// case-insensitive, so C:\Work and c:\work name one location and must never become two journal
// entries — the same upper-casing windowsProtectedRoots dedupes its locations with, and the
// whole-path form of the component-wise fold hostRules.sameComponent applies.
func foldPath(p string) string { return strings.ToUpper(p) }

// lowLabelSIDs are the SDDL spellings of the Low integrity level — the ONE level this
// backend ever writes — in both the alias and the canonical form, so a descriptor is recognised
// whichever way the OS rendered it.
var lowLabelSIDs = map[string]bool{"LW": true, "S-1-16-4096": true}

// IsLowLabel reports whether sddl carries a mandatory-label ACE naming the Low integrity
// level. It is deliberately looser than comparing against lowSDDL verbatim: the same label
// read back from the OS carries descriptor flags (S:AI(…)), a label an earlier apogee wrote
// with the inheritable spelling and propagated reads back with the inherited ACE flag
// (OICIID), and the kernel's own Low label on an object the confined child created has yet
// another spelling — so a string equality test would recognise apogee's label only in the one
// form apogee happens to write it in.
func IsLowLabel(sddl string) bool {
	for rest := sddl; ; {
		start := strings.Index(rest, labelACEPrefix)
		if start < 0 {
			return false
		}
		rest = rest[start+len(labelACEPrefix):]
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return false
		}
		// What follows the consumed "(ML;" is flags;rights;object;inherit;sid — the integrity
		// level is the trailing SID field.
		if fields := strings.Split(rest[:end], ";"); len(fields) >= 5 &&
			lowLabelSIDs[strings.ToUpper(strings.TrimSpace(fields[4]))] {
			return true
		}
		rest = rest[end+1:]
	}
}

// descendantFacts are the two OS reads one descendant's label decision is made from, paired
// with the errors those reads may have failed with: its prior mandatory label (ReadSDDL) and
// the number of directory entries naming the same underlying file (hardLinkCount). Gathering
// them into a value is what keeps descendantDecision pure — the syscalls live in
// walk_windows.go, the policy does not — and lets a later fact join the decision without
// re-shaping its call.
type descendantFacts struct {
	prior    string
	priorErr error
	links    uint32
	linksErr error
}

// descriptorMayBeShared reports whether a descendant's security descriptor may also be the
// descriptor of a name OUTSIDE the box, from the hard-link count read for it (hardLinkCount)
// and the error that read may have failed with. On NTFS every hard link of a file names one
// MFT record and therefore one descriptor, so a write through the in-box name lands on the
// file wherever else it is linked — pnpm's node_modules is entirely hard links into a global
// store under %LOCALAPPDATA%, outside the box. A count that could not be READ answers true
// too: an unknown count cannot rule the shared-record case out, and neither walk may widen
// what it writes to on a guess.
//
// It is ONE rule because it must hold identically on both sides of the label lifecycle — the
// label walk skips such a path (descendantDecision) and teardown must skip the same one
// (clearDescendantDecision), or teardown writes over a descriptor the label walk deliberately
// refused to touch. Two copies of the rule could drift into exactly that gap.
func descriptorMayBeShared(links uint32, linksErr error) bool {
	return linksErr != nil || links > 1
}

// descendantDecision is the label walk's three-way decision for one descendant, from the
// outcome of reading its prior mandatory label and its hard-link count: shouldJournal reports
// whether the prior must be journalled before any label lands, shouldLabel whether the path
// may be labelled at all.
//
//   - A prior-read ERROR skips the path entirely — no journal entry, no label. Labelling
//     anyway would destroy a possibly-foreign label with no record of how to put it back,
//     which is the one thing ADR 0020 §2's journal-first invariant forbids; the cost is
//     LabelTree's tolerated-descendant one — that single path stays opaque to the confined
//     child and never gates the box.
//   - A file with MORE THAN ONE hard link takes that same tolerated rung, for the reason the
//     walk skips reparse points: on NTFS every hard link shares one MFT record and therefore
//     one security descriptor, so a Low label written through the in-box name lands on the
//     file wherever else it is linked — pnpm's node_modules is entirely hard links into a
//     global store under %LOCALAPPDATA%, outside the box. A path apogee cannot label without
//     mutating something outside the box is not labelled at all.
//   - A link count that could not be READ takes the rung too: an unknown count cannot rule
//     the shared-record case out, and the walk must not widen the fence on a guess. The cost
//     is the same one opaque path.
//   - A non-empty prior is journalled and then labelled; what the entry may SAY about the
//     prior remains recordEntry's decision.
//   - No prior (the overwhelmingly common case) is labelled with nothing to journal.
//
// It is pure so the decision is table-testable on any OS — the retire seam
// pattern.
func descendantDecision(f descendantFacts) (shouldJournal, shouldLabel bool) {
	if f.priorErr != nil || descriptorMayBeShared(f.links, f.linksErr) {
		return false, false
	}
	return f.prior != "", true
}

// clearDescendantDecision is the teardown walk's decision for one descendant, from its
// hard-link count alone: shouldClear reports whether ClearTree may write the NULL SACL over
// it. It is descendantDecision's mirror on the revert side, and it exists for the same
// reason — the clear is a WRITE to the very descriptor a hard link shares, so a path whose
// descriptor may reach outside the box must be left exactly as it is:
//
//   - A file with more than one hard link, or one whose count could not be read, is skipped
//     (descriptorMayBeShared). LabelTree refused that same path, so there is nothing of
//     apogee's on it to remove; clearing it anyway would erase whatever label the record
//     carries at its other names — the foreign label teardown exists to preserve, not destroy.
//   - Everything else is cleared, as before.
//
// The skip is a DECISION, not a failure: it is not counted into clearTreeOutcome, because a
// count that keeps the journal would strand it forever over a path apogee never labelled and
// have ConfinementResidue alarm about it every session.
//
// Its one residue is the path hard-linked AFTER the label pass reached it — labelled while it
// had a single name, skipped by the clear once it has two — which keeps apogee's Low label
// with nothing recorded. That is the same residue the walks' reparse-point skip already
// carries for a file replaced by a symlink mid-run, and the alternative is worse: writing over
// a record the label pass never wrote to, which is the case this decision exists for.
//
// It is pure so the decision is table-testable on any OS — the retire seam pattern.
func clearDescendantDecision(links uint32, linksErr error) (shouldClear bool) {
	return !descriptorMayBeShared(links, linksErr)
}

package winlabel

import "strings"

// The mandatory-label SDDL strings the backend writes (ADR 0020 §2).
//
//   - DirSDDL labels a DIRECTORY Low with NO_WRITE_UP, object- and
//     container-inheritable, so objects created inside the box during the run are Low too.
//   - FileSDDL labels an existing FILE Low; inheritance flags are meaningless
//     on a leaf, and the label pass must reach existing files because inheritance covers
//     newly created objects ONLY — a Low child editing a pre-existing source file would
//     otherwise be denied, which is the single most common thing an agent does.
//   - ClearSDDL is a NULL SACL: no mandatory label at all, which is the state
//     an unlabelled object is in (implicitly Medium with NO_WRITE_UP). It is what teardown
//     writes to a path that carried no label before the run. Clearing via a NULL SACL keeps
//     the restore inside LABEL_SECURITY_INFORMATION, which needs only WRITE_OWNER on the
//     object; asking additionally for UNPROTECTED_SACL_SECURITY_INFORMATION would drag in
//     the SACL privilege check (SeSecurityPrivilege) and fail for an ordinary user.
const (
	DirSDDL   = "S:(ML;OICI;NW;;;LW)"
	FileSDDL  = "S:(ML;;NW;;;LW)"
	ClearSDDL = "S:"
)

// LabelACEPrefix is the SDDL spelling of a mandatory-label ACE. A descriptor string
// containing it carries an explicit label that teardown must put back rather than clear.
const LabelACEPrefix = "(ML;"

// FoldPath case-folds a path for the journal's one-entry-per-path rule. Windows paths are
// case-insensitive, so C:\Work and c:\work name one location and must never become two journal
// entries — the same upper-casing windowsProtectedRoots dedupes its locations with, and the
// whole-path form of the component-wise fold hostRules.sameComponent applies.
func FoldPath(p string) string { return strings.ToUpper(p) }

// lowLabelSIDs are the SDDL spellings of the Low integrity level — the ONE level this
// backend ever writes — in both the alias and the canonical form, so a descriptor is recognised
// whichever way the OS rendered it.
var lowLabelSIDs = map[string]bool{"LW": true, "S-1-16-4096": true}

// IsLowLabel reports whether sddl carries a mandatory-label ACE naming the Low integrity
// level. It is deliberately looser than comparing against DirSDDL /
// FileSDDL verbatim: the same label read back from the OS carries descriptor flags
// (S:AI(…)) and, on a path that inherited it from a labelled root, the inherited ACE flag
// (OICIID), so a string equality test would recognise apogee's own label only in the one
// spelling apogee happens to write it in.
func IsLowLabel(sddl string) bool {
	for rest := sddl; ; {
		start := strings.Index(rest, LabelACEPrefix)
		if start < 0 {
			return false
		}
		rest = rest[start+len(LabelACEPrefix):]
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

// DescendantDecision is the label walk's three-way decision for one descendant, from
// the outcome of reading its prior mandatory label: shouldJournal reports whether the prior
// must be journalled before any label lands, shouldLabel whether the path may be labelled at
// all.
//
//   - A read ERROR skips the path entirely — no journal entry, no label. Labelling anyway
//     would destroy a possibly-foreign label with no record of how to put it back, which is
//     the one thing ADR 0020 §2's journal-first invariant forbids; the cost is labelTree's
//     tolerated-descendant one — that single path stays opaque to the confined child and
//     never gates the box.
//   - A non-empty prior is journalled and then labelled; what the entry may SAY about the
//     prior remains recordEntry's decision.
//   - No prior (the overwhelmingly common case) is labelled with nothing to journal.
//
// It is pure so the decision is table-testable on any OS — the Retire seam
// pattern.
func DescendantDecision(prior string, readErr error) (shouldJournal, shouldLabel bool) {
	if readErr != nil {
		return false, false
	}
	return prior != "", true
}

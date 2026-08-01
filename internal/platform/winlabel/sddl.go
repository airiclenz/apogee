package winlabel

import "strings"

// The mandatory-label SDDL strings the backend writes (ADR 0020 §2).
//
//   - dirSDDL labels a DIRECTORY Low with NO_WRITE_UP, object- and
//     container-inheritable, so objects created inside the box during the run are Low too.
//   - fileSDDL labels an existing FILE Low; inheritance flags are meaningless
//     on a leaf, and the label pass must reach existing files because inheritance covers
//     newly created objects ONLY — a Low child editing a pre-existing source file would
//     otherwise be denied, which is the single most common thing an agent does.
//   - clearSDDL is a NULL SACL: no mandatory label at all, which is the state
//     an unlabelled object is in (implicitly Medium with NO_WRITE_UP). It is what teardown
//     writes to a path that carried no label before the run. Clearing via a NULL SACL keeps
//     the restore inside LABEL_SECURITY_INFORMATION, which needs only WRITE_OWNER on the
//     object; asking additionally for UNPROTECTED_SACL_SECURITY_INFORMATION would drag in
//     the SACL privilege check (SeSecurityPrivilege) and fail for an ordinary user.
const (
	dirSDDL   = "S:(ML;OICI;NW;;;LW)"
	fileSDDL  = "S:(ML;;NW;;;LW)"
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
// level. It is deliberately looser than comparing against dirSDDL /
// fileSDDL verbatim: the same label read back from the OS carries descriptor flags
// (S:AI(…)) and, on a path that inherited it from a labelled root, the inherited ACE flag
// (OICIID), so a string equality test would recognise apogee's own label only in the one
// spelling apogee happens to write it in.
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
	if f.priorErr != nil || f.linksErr != nil || f.links > 1 {
		return false, false
	}
	return f.prior != "", true
}

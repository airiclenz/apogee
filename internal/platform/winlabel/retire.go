package winlabel

import (
	"fmt"
	"os"
	"strings"
)

// retire reverts one journal's disk mutation through revert and then decides the
// journal FILE's fate: it is removed only when the revert succeeded AND left nothing behind. A
// failed revert leaves the file exactly where it is, because the journal is the only record of
// the labels still sitting on the disk — deleting it would strand them permanently, whereas
// keeping it means the next NewConfiner retries the restore and, until one does,
// ConfinementResidue reports it (ADR 0020 §2).
//
// A revert may also succeed while HANDING OFF entries it deliberately did not act on — a
// foreign prior under a root a sibling journal still claims (restorablePriors). Those are not
// failures, but they are still undischarged instructions, so the journal is REWRITTEN to carry
// exactly them (under its original owner) rather than removed: the record of the foreign label
// survives sibling teardown ordering, and the first construction after the claiming journals
// are gone completes the restore. The remaining entries are returned so a session backend can
// keep its in-memory journal in step; nil means the journal is fully retired. On a revert
// error the return is nil and the file keeps everything it had.
//
// revert is injected — revertSparingLiveSiblings' closure over revertJournal in
// production, which is Windows-tagged — so the
// retention rule itself is table-testable on any OS, the same seam every other decision in this
// file is behind. path may be "" for a backend that keeps no journal file: there is then nothing
// to remove or rewrite and the revert outcome passes through unchanged.
func retire(path string, r Record, revert func(Record) ([]Entry, error)) ([]Entry, error) {
	remaining, err := revert(r)
	if err != nil {
		return nil, err
	}
	if path == "" {
		return remaining, nil
	}
	if len(remaining) > 0 {
		if err := WriteJournal(path, Record{PID: r.PID, Entries: remaining}); err != nil {
			return nil, err
		}
		return remaining, nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("apogee: confine: remove the label journal %q: %w", path, err)
	}
	return nil, nil
}

// clearTreeOutcome is ClearTree's below-root verdict: nil when failures is zero —
// every descendant is verifiably cleared or gone — else an error carrying the count and the
// first failure. Returning an error is what makes retire KEEP the journal, so
// labels the walk could not remove stay recorded, ConfinementResidue reports them meanwhile,
// and the next session or recovery retries them; a nil verdict over remaining failures would
// retire the journal above labels still on the disk (ADR 0020 §2's "verifiably reverted").
// It is pure so the accounting is table-testable on any OS — the retire seam
// pattern.
func clearTreeOutcome(root string, failures int, first error) error {
	if failures == 0 {
		return nil
	}
	return fmt.Errorf("apogee: confine: %d path(s) under %q could not be cleared of the mandatory label (first failure: %w)",
		failures, root, first)
}

// priorRestorable is the READ-side check on one journalled prior: from the path's current
// mandatory label and the error that read may have failed with, it reports whether the prior
// may be written back and whether the instruction to write it back must be dropped.
//
// Only the record side was ever vouched for — recordEntry refuses to journal a Low prior, so
// an honest journal never asks for apogee's own label to be put back. Nothing vouched for the
// read side: the revert applied every PriorSDDL a journal named, so a journal planted or
// corrupted under the apogee home made the next construction WRITE labels onto arbitrary paths
// (security finding F-08). The check is that apogee's own Low label must still be sitting on
// the path — that mark is what makes the path this run's, or a dead run's, to revert — and it
// is taken BEFORE anything is cleared, because the clear is what turns a path apogee labelled
// into an unlabelled one (judgePriors):
//
//   - A Low label, read cleanly, is restorable: apogee's mark is on the path, so the prior
//     recorded beneath it is apogee's to put back.
//   - Any other label, read cleanly, DROPS the instruction. Either the path was never
//     apogee's or someone has changed it since; nothing of apogee's is on it, so there is
//     nothing left to revert, and keeping the entry would re-attempt the write forever.
//   - A path that is GONE drops too — the verdict the restore loop already reaches for a
//     vanished path: an object that no longer exists carries no label to put back.
//   - Any OTHER read failure is neither. The verdict is unknown and both answers destroy
//     something: restoring writes a label onto a path that may not be apogee's, dropping
//     destroys the only record of a foreign one. The caller keeps the entry unjudged and
//     leaves it to a later run.
//
// It is pure so the decision is table-testable on any OS — the retire seam pattern — which
// matters most here: ReadSDDL errors off Windows, and this is the one decision a planted
// journal attacks.
func priorRestorable(current string, readErr error) (restore, drop bool) {
	if readErr != nil {
		return false, os.IsNotExist(readErr)
	}
	if IsLowLabel(current) {
		return true, false
	}
	return false, true
}

// rootClearable is the CLEAR-side check on one journalled root: from the root's path, its
// current mandatory label and the error that read may have failed with, it reports whether the
// revert may strip that tree.
//
// It is priorRestorable's mirror, and it closes the second prong of the same finding. The
// restore side was vouched for first — a prior is written back only onto a path apogee's own
// Low label still sits on — while the clear side obeyed the journal unconditionally:
// revertibleRoots handed every Root a journal named to ClearTree, whose write is a NULL SACL
// (clearSDDL), so a journal planted or corrupted under the apogee home made the next
// construction STRIP the mandatory label off an arbitrary tree (security finding F-08). The
// warrant is the one the restore side already takes:
//
//   - Apogee's own Low label, read cleanly, is what makes the tree apogee's to clear. Nothing
//     else marks it as this run's, or a dead run's, work.
//   - Any other label, or none at all, read cleanly, refuses the root: apogee never labelled
//     this tree, or someone has changed it since, so there is nothing of apogee's on it to
//     remove and the clear would only destroy someone else's label.
//   - A read that FAILED refuses too, a vanished path included. An unknown label vouches for
//     nothing, and unlike the restore side nothing is destroyed by declining — the journal is
//     kept whole and a later run judges the root again.
//   - A VOLUME root (C:\, \\server\share) is refused whatever label it carries, the same
//     refusal windowsLabelGuardrail makes on the way IN: nothing above a box may be labelled,
//     so nothing above a box may be cleared either, and a journal naming one describes a
//     mutation apogee would never have made.
//
// A refused root is SKIPPED, not an abort — exactly the disposition a root a live sibling
// still claims gets (revertibleRoots): the rest of the journal reverts as it would have.
//
// It is pure so the decision is table-testable on any OS — the retire seam pattern — which
// matters most here: ReadSDDL errors off Windows, and this is the one decision a planted
// journal attacks.
func rootClearable(root, current string, readErr error) bool {
	if readErr != nil {
		return false
	}
	if isVolumeRoot(root) {
		return false
	}
	return IsLowLabel(current)
}

// isVolumeRoot reports whether p names a volume root rather than a tree inside one: a drive
// root (C:\), a bare drive (C:, which names no location at all), a UNC share root
// (\\server\share) or a rooted path with no components below the anchor.
//
// The shape test is spelled HERE, over both separators, rather than through path/filepath: on
// Linux filepath.Split answers "C:\" as one file name in the current directory, so the refusal
// would hold only on Windows and the table case could never run — and package platform's
// hostRules.split, the guardrail this mirrors, is unimportable from this leaf (D2). Windows
// accepts / and \ interchangeably, so a journal may carry either spelling and both are folded
// to one here, the whole-path posture foldPath already takes.
func isVolumeRoot(p string) bool {
	q := strings.ReplaceAll(p, "/", `\`)
	if strings.HasPrefix(q, `\\`) {
		// UNC: \\server\share is the anchor itself, so two components or fewer name no tree.
		return len(pathComponents(q[2:])) <= 2
	}
	if len(q) >= 2 && q[1] == ':' && isDriveLetter(q[0]) {
		return len(pathComponents(q[2:])) == 0
	}
	return len(pathComponents(q)) == 0
}

// pathComponents splits a backslash-separated path into its non-empty components, so a
// trailing or doubled separator names no extra level.
func pathComponents(p string) []string {
	out := make([]string, 0, 4)
	for _, part := range strings.Split(p, `\`) {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// isDriveLetter reports whether b is a Windows drive letter, the first half of the C: anchor.
func isDriveLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// revertibleRoots returns the journalled roots a revert may clear: r's roots minus every
// root also named (Root == true) by a sibling journal whose owning process is still ALIVE,
// and minus every root apogee's own label no longer vouches for (rootClearable).
// Two sessions confining one workspace journal the same root, and the first to tear down
// must not strip the label out from under the survivor — its memoised label pass would never
// re-label, and every later confined write in that session would be denied.
//
// A spared root is NOT a failed revert and must not keep this journal: the live sibling's
// own journal names the root as a Root entry, so the clear obligation lives on in THAT
// journal — its teardown or, after a crash, recovery clears the root once no live session
// claims it — and this journal may still retire. A DEAD sibling spares nothing: its journal
// is an interrupted run whose roots recovery will clear anyway, and clearing them here first
// is the same idempotent operation.
//
// The clearability half is F-08's second prong. An entry already carrying the PERSISTED
// verdict (Entry.RootJudged) is taken as clearable without a fresh read, and that precedence
// is the point rather than an optimisation: a revert that cleared a root but failed a
// descendant keeps the journal (clearTreeOutcome), and a verdict re-taken on the retry would
// read the NULL SACL the clear itself wrote, refuse the root, and let the journal retire over
// descendants still labelled Low. Only an UNJUDGED root is read, which is the pre-clear pass's
// own case (judgePriors).
//
// Roots are compared case-folded (foldPath): C:\Work and c:\work name one location.
// alive is injected (ProcessAlive in production, which is Windows-tagged) and readLabel with
// it (ReadSDDL, likewise), so the decision is table-testable on any OS — the retire seam
// pattern.
func revertibleRoots(r Record, siblings []Record, alive func(int) bool, readLabel func(string) (string, error)) []string {
	claimed := make(map[string]bool)
	for _, sibling := range siblings {
		if !alive(sibling.PID) {
			continue
		}
		for _, root := range sibling.Roots() {
			claimed[foldPath(root)] = true
		}
	}
	out := make([]string, 0, len(r.Entries))
	for _, entry := range r.Entries {
		if !entry.Root || claimed[foldPath(entry.Path)] {
			continue
		}
		if !entry.RootJudged {
			current, readErr := readLabel(entry.Path)
			if !rootClearable(entry.Path, current, readErr) {
				continue
			}
		}
		out = append(out, entry.Path)
	}
	return out
}

// restorablePriors splits r's prior-label restores into what may be restored NOW and what
// must be HANDED OFF to a later run: a prior sitting at or under a root any sibling journal
// still names as a Root entry is deferred, everything else is restored by this revert.
//
// The deferral is what keeps a foreign prior on a shared root from being lost to sibling
// teardown ordering. Restoring it while the sibling journal's clear obligation is
// undischarged would first overwrite the Low label the sibling's session may still be fenced
// by, and then be wiped anyway by that sibling's ClearTree — at its own teardown, or at
// recovery once it is dead — with no record left anywhere, because the sibling saw only
// apogee's own Low label and journalled no prior. Liveness is deliberately NOT consulted
// here, unlike revertibleRoots: a sibling journal FILE is the undischarged claim whether its
// owner is alive (its Close will clear) or dead (recovery will), and either clear destroys a
// label restored now. The handed-off entries are returned VERBATIM — Root flag included, so
// the surviving journal still anchors the residue report and the eventual re-clear — and
// retire persists them as the journal's remains; the restore then happens at the
// first construction after the claiming journals are gone (ADR 0020 §2's "the journal
// survives until a real session's constructor finishes the restore").
//
// Containment is the case-folded whole-path prefix: a descendant's journalled path is the
// label walk's own spelling — the root plus its relative path — so the lexical test is exact
// here and nothing needs re-resolution.
func restorablePriors(r Record, siblings []Record) (restore map[string]string, handoff []Entry) {
	var claimed []string
	for _, sibling := range siblings {
		for _, root := range sibling.Roots() {
			claimed = append(claimed, foldPath(root))
		}
	}
	if len(claimed) == 0 {
		return r.PriorLabels(), nil
	}
	underClaim := func(path string) bool {
		folded := foldPath(path)
		for _, root := range claimed {
			if folded == root || strings.HasPrefix(folded, root+`\`) {
				return true
			}
		}
		return false
	}
	restore = make(map[string]string, len(r.Entries))
	for _, entry := range r.Entries {
		if entry.PriorSDDL == "" {
			continue
		}
		if underClaim(entry.Path) {
			handoff = append(handoff, entry)
			continue
		}
		restore[entry.Path] = entry.PriorSDDL
	}
	return restore, handoff
}

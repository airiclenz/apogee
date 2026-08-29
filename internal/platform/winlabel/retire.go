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

// revertibleRoots returns the journalled roots a revert may clear: r's roots minus every
// root also named (Root == true) by a sibling journal whose owning process is still ALIVE.
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
// Roots are compared case-folded (foldPath): C:\Work and c:\work name one location.
// alive is injected (ProcessAlive in production, which is Windows-tagged) so the decision is
// table-testable on any OS — the retire seam pattern.
func revertibleRoots(r Record, siblings []Record, alive func(int) bool) []string {
	claimed := make(map[string]bool)
	for _, sibling := range siblings {
		if !alive(sibling.PID) {
			continue
		}
		for _, root := range sibling.Roots() {
			claimed[foldPath(root)] = true
		}
	}
	roots := r.Roots()
	if len(claimed) == 0 {
		return roots
	}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if claimed[foldPath(root)] {
			continue
		}
		out = append(out, root)
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

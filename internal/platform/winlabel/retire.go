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
// foreign prior under a root a sibling journal still claims (restorablePriors), and the ROOT
// a LIVE sibling spared from the clear (revertibleRoots, handoffSparedRoots). Those are not
// failures, but they are still undischarged instructions, so the journal is REWRITTEN to carry
// exactly them (under its original owner) rather than removed: the record of the foreign
// label, and of the label still sitting on the spared tree, survives sibling teardown
// ordering, and the first construction after the claiming journals are gone completes the
// revert. A remaining ROOT entry keeps the file for exactly the reason a remaining prior does,
// and that is what closes the overlapping-close hole: two sessions confining one workspace
// each spared the shared root to the other and each then removed its own journal, leaving the
// Low label on the disk with nothing anywhere left to describe it — unrecoverably, since
// Recover and ResidueIn both skip a journal whose owner is alive (audit 2026-09-20).
//
// The remaining entries are returned so a session backend can keep its in-memory journal in
// step; nil means the journal is fully retired. On a revert error the return is nil and the
// file keeps everything it had.
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

// priorVerdict is priorRestorable's answer over one journalled prior. The zero value is the
// unknown read, the answer that acts on nothing.
type priorVerdict int

const (
	// priorUnknown is a read that failed for a reason other than "gone": the path's label is
	// not knowable, and both other answers destroy something, so the revert aborts and a
	// later run judges the entry again.
	priorUnknown priorVerdict = iota
	// priorRestore writes the journalled prior back: apogee's own Low label is still on the
	// path, so the prior recorded beneath it is apogee's to put back.
	priorRestore
	// priorDrop discards the instruction for good: the path is gone, so it carries no label
	// to put back and there is nothing left to revert.
	priorDrop
	// priorCarry neither writes nor discards: the path carries a label that is not apogee's,
	// so the prior travels on in the journal to a run that reads the path again — for at most
	// maxPriorCarries reverts (carryPrior).
	priorCarry
)

// String names the verdict, so a table failure reads as a decision rather than an integer.
func (v priorVerdict) String() string {
	switch v {
	case priorRestore:
		return "restore"
	case priorDrop:
		return "drop"
	case priorCarry:
		return "carry"
	default:
		return "unknown"
	}
}

// maxPriorCarries bounds the life of a carried prior: the number of reverts that may look at
// one and find no mark of apogee's on its path before the instruction is dropped. It exists
// because a carry keeps its journal on the disk — retire rewrites the file for whatever
// remains — so an unbounded one would leave a journal, and the residue notice over it, on the
// machine forever. Eight reverts is deliberately generous: the realistic case is a foreign
// label restored by hand or by a policy refresh within a session or two, and every revert of
// every session and recovery pass counts toward it.
const maxPriorCarries = 8

// carryPrior folds one carry verdict into an entry's carry count, returning the new count and
// whether the carry is exhausted — the point at which the prior is dropped instead.
//
// It is pure so the bound is table-testable on any OS — the retire seam pattern.
func carryPrior(carried int) (next int, exhausted bool) {
	next = carried + 1
	return next, next >= maxPriorCarries
}

// priorRestorable is the READ-side check on one journalled prior: from the path's current
// mandatory label and the error that read may have failed with, it reports what the revert may
// do with the instruction to write that prior back.
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
//   - Any OTHER label, read cleanly — a foreign one, or none at all — CARRIES the entry
//     rather than dropping it. Nothing of apogee's is on the path, so the prior must not be
//     written back on this run; but a drop destroys the only record of the label the path
//     carried before the run, and the state that produced this read is routinely temporary —
//     a policy refresh, another tool's label, a path this very revert is about to clear. So
//     the entry travels on and a later revert reads the path again, restoring the prior the
//     moment apogee's own mark is back on it. The carry is bounded (carryPrior): a journal
//     that carried forever would never retire.
//   - A path that is GONE drops — the verdict the restore loop already reaches for a
//     vanished path: an object that no longer exists carries no label to put back, and no
//     later run can bring it back either.
//   - Any OTHER read failure is unknown, and unlike the carry it cannot be deferred safely:
//     the caller cannot tell a transient denial from a path it must not touch, so the revert
//     ABORTS with the entry unjudged, nothing cleared, and the journal kept whole.
//
// It is pure so the decision is table-testable on any OS — the retire seam pattern — which
// matters most here: ReadSDDL errors off Windows, and this is the one decision a planted
// journal attacks.
func priorRestorable(current string, readErr error) priorVerdict {
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return priorDrop
		}
		return priorUnknown
	}
	if IsLowLabel(current) {
		return priorRestore
	}
	return priorCarry
}

// judgeEntries takes every pre-clear verdict one journal's entries need — the root's
// clearability (rootClearable) and the prior's disposition (priorRestorable) — off ONE label
// read per path, and records them on the entries IN PLACE. It reports whether anything moved
// (changed, so the journal is worth rewriting) and whether a PRIOR was settled destructively
// (settled: a restore vouched for, or an instruction discarded), which is what makes
// persisting the result a precondition rather than an optimisation.
//
// The two flags exist because the verdicts have different stakes. A root verdict lost to an
// unwritable apogee home costs nothing this run — the clear happens anyway, and a later run
// reads the root again — while a prior verdict is taken BEFORE the clear precisely because
// the clear destroys the evidence it rests on: a restorable prior re-judged after the clear
// would read an unlabelled path, and a discarded one re-judged would be discarded again over
// a path that has moved on. A plain carry sits with the root verdicts: nothing is written and
// nothing is thrown away, so a lost count merely lengthens the carry; an EXHAUSTED carry
// discards the prior and therefore settles it.
//
// A path whose entry needs neither verdict is never read, which is what keeps an already-judged
// root from re-reading the NULL SACL its own clear wrote (Entry.RootJudged) and an
// already-judged prior from re-reading a path the clear has unlabelled (Entry.Judged).
//
// An unknown read aborts, and aborts BEFORE the clear: the entry stays unjudged, retire keeps
// the journal whole, and the next run judges it against a path that still carries every label
// this one would have removed. Clearing first and retrying later would hand that retry an
// unlabelled path and the verdict "not apogee's". Verdicts already recorded on earlier entries
// stay recorded — they were taken off clean reads of their own paths.
//
// readLabel is injected (ReadSDDL in production, which is Windows-tagged) so every decision
// the pre-clear pass makes is table-testable on any OS — the retire seam pattern.
func judgeEntries(entries []Entry, readLabel func(string) (string, error)) (changed, settled bool, err error) {
	for i := range entries {
		entry := &entries[i]
		// A root that already carried a foreign label is ONE entry wearing both instructions
		// (LabelTree), and both verdicts are taken off the same read of the same path.
		judgeRoot := entry.Root && !entry.RootJudged
		judgePrior := entry.PriorSDDL != "" && !entry.Judged
		if !judgeRoot && !judgePrior {
			continue
		}
		current, readErr := readLabel(entry.Path)
		if judgeRoot && rootClearable(entry.Path, current, readErr) {
			entry.RootJudged = true
			changed = true
		}
		if !judgePrior {
			continue
		}
		switch priorRestorable(current, readErr) {
		case priorRestore:
			entry.Judged = true
			changed, settled = true, true
		case priorDrop:
			entry.PriorSDDL = ""
			changed, settled = true, true
		case priorCarry:
			next, exhausted := carryPrior(entry.Carried)
			entry.Carried = next
			if exhausted {
				entry.PriorSDDL = ""
				settled = true
			}
			changed = true
		default:
			return changed, settled, fmt.Errorf("apogee: confine: cannot read the mandatory label of %q to judge the prior the journal records for it: %v",
				entry.Path, readErr)
		}
	}
	return changed, settled, nil
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
// A refused root is SKIPPED, not an abort: the rest of the journal reverts as it would have.
// It is NOT handed back the way a root a live sibling still claims is (handoffSparedRoots).
// The two dispositions look alike and are not: the spared root demonstrably still carries
// apogee's label, so an obligation over it has to survive somewhere, while a refused root
// carries nothing of apogee's to remove and leaves this journal owing nothing over that tree.
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

// revertibleRoots splits r's journalled roots in two. clear is what this revert may strip:
// r's roots minus every root also named (Root == true) by a sibling journal whose owning
// process is still ALIVE, and minus every root apogee's own label no longer vouches for
// (rootClearable). spared is the roots the LIVE-sibling rule held back, which the caller hands
// off (handoffSparedRoots) so this journal survives carrying them.
// Two sessions confining one workspace journal the same root, and the first to tear down
// must not strip the label out from under the survivor — its memoised label pass would never
// re-label, and every later confined write in that session would be denied.
//
// A spared root is not a failed revert, but it DOES keep this journal, and that supersedes
// the rule this function used to state. The clear obligation does live on in the live
// sibling's own journal — its teardown or, after a crash, recovery clears the root once no
// live session claims it — but only for as long as that journal exists, and two sessions
// closing at once each spare the root to the other: both files would then be removed with the
// label still on the disk, with nothing left to describe it and no surface that would report
// it (Recover and ResidueIn both skip a journal whose owner is alive). Handing the root back
// instead makes the LAST journal to go the one that finds no live claim and clears the tree.
// A DEAD sibling spares nothing: its journal is an interrupted run whose roots recovery will
// clear anyway, and clearing them here first is the same idempotent operation.
//
// The clearability half hands nothing back. A root apogee's own label no longer vouches for
// carries nothing of apogee's to remove, so there is no obligation for the journal to keep —
// the opposite of the sparing case, where the label is demonstrably still on the tree.
//
// The clearability half is F-08's second prong. An entry already carrying the PERSISTED
// verdict (Entry.RootJudged) is taken as clearable without a fresh read, and that precedence
// is the point rather than an optimisation: a revert that cleared a root but failed a
// descendant keeps the journal (clearTreeOutcome), and a verdict re-taken on the retry would
// read the NULL SACL the clear itself wrote, refuse the root, and let the journal retire over
// descendants still labelled Low. Only an UNJUDGED root is read, which is the pre-clear pass's
// own case (judgeEntries).
//
// What the persisted verdict skips is the LABEL READ, never the GUARDRAIL. A volume root is
// refused here whatever the entry claims, because rootClearable's volume refusal is a
// statement about the SHAPE of the path — nothing above a box may be labelled, so nothing
// above a box may be cleared — and a journal that could switch it off by writing
// "root_judged": true into a planted entry would hand a stranger the whole of C:\ (F-08's
// clear prong, reopened through the flag that closed it). The flag vouches only that apogee
// once read apogee's own label on that path; it cannot vouch that the path is a tree.
//
// Roots are compared case-folded (foldPath): C:\Work and c:\work name one location.
// alive is injected (ProcessAlive in production, which is Windows-tagged) and readLabel with
// it (ReadSDDL, likewise), so the decision is table-testable on any OS — the retire seam
// pattern.
func revertibleRoots(r Record, siblings []Record, alive func(int) bool, readLabel func(string) (string, error)) (clear, spared []string) {
	claimed := make(map[string]bool)
	for _, sibling := range siblings {
		if !alive(sibling.PID) {
			continue
		}
		for _, root := range sibling.Roots() {
			claimed[foldPath(root)] = true
		}
	}
	clear = make([]string, 0, len(r.Entries))
	for _, entry := range r.Entries {
		if !entry.Root {
			continue
		}
		if claimed[foldPath(entry.Path)] {
			spared = append(spared, entry.Path)
			continue
		}
		// The guardrail is outside the persisted-verdict skip on purpose: the flag stands in
		// for a LABEL READ apogee itself took, and nothing else. A volume root is refused on
		// the shape of its path, which no read and no journal can change.
		if isVolumeRoot(entry.Path) {
			continue
		}
		if !entry.RootJudged {
			current, readErr := readLabel(entry.Path)
			if !rootClearable(entry.Path, current, readErr) {
				continue
			}
		}
		clear = append(clear, entry.Path)
	}
	return clear, spared
}

// handoffSparedRoots folds the roots a live sibling spared (revertibleRoots) into the hand-off
// restorablePriors built, producing the entries retire rewrites the journal to. A spared root
// that already appears there — one entry wearing both instructions, a root that also carried a
// foreign prior (LabelTree) — keeps its single entry and simply regains the Root flag rather
// than being listed twice, because a journal is one entry per path (recordEntry) and two would
// make the later revert walk the tree twice.
//
// The spared root is handed off UNJUDGED: RootJudged is cleared here, never carried over from
// the pre-clear pass (judgePriors). That verdict exists for one case only — a revert that
// cleared the root and then failed a descendant keeps the journal (clearTreeOutcome), and the
// retry must not re-read the NULL SACL its own clear wrote and refuse the root. NOTHING was
// cleared on a spared root, so there is no NULL SACL to beat, and a verdict carried forward
// would instead let a later Recover skip the read (revertibleRoots) and strip the label off
// whatever that tree carries by then — F-08's clear prong, reopened. The later pass reads the
// path again, which is exactly what should decide it.
//
// It is pure so the fold is table-testable on any OS — the retire seam pattern — and it is
// spelled here rather than in the Windows-tagged caller that joins the two sets for that
// reason.
func handoffSparedRoots(handoff []Entry, spared []string) []Entry {
	if len(spared) == 0 {
		return handoff
	}
	out := append([]Entry(nil), handoff...)
	for _, root := range spared {
		folded := foldPath(root)
		merged := false
		for i := range out {
			if foldPath(out[i].Path) != folded {
				continue
			}
			out[i].Root, out[i].RootJudged = true, false
			merged = true
			break
		}
		if !merged {
			out = append(out, Entry{Path: root, Root: true})
		}
	}
	return out
}

// restorablePriors splits r's prior-label restores into what may be restored NOW and what
// must be HANDED OFF to a later run: a prior an in-memory verdict has not vouched for
// (Entry.Judged), and a prior sitting at or under a root any sibling journal still names as a
// Root entry, are both deferred; everything else is restored by this revert.
//
// The Judged gate is the restore prong of F-08 held shut at the last seam. What this function
// hands to revertJournal is written to the disk with SetSDDL, so an entry that reaches the
// restore map is a label write onto the path it names — and the ONLY thing standing between a
// journal a stranger planted under the apogee home and that write is the pre-clear judgement
// (judgeEntries, priorRestorable), which sets Judged exactly when apogee's own Low label was
// found on the path. An unjudged prior is therefore never restorable here, whatever it looks
// like: it is either a carry waiting for a later read (priorCarry) or an instruction nothing
// has vouched for, and both belong in the hand-off, which writes nothing and merely keeps the
// record alive. Every prior a correct revert restores passes the pre-clear pass first, so the
// gate costs an honest journal nothing.
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
		if !entry.Judged || underClaim(entry.Path) {
			handoff = append(handoff, entry)
			continue
		}
		restore[entry.Path] = entry.PriorSDDL
	}
	return restore, handoff
}

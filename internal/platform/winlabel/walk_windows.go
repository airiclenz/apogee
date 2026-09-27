//go:build windows

package winlabel

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// The OS half of the label mechanism, and the only Windows-tagged file in this package: the
// two SACL primitives, the walks that apply and clear a mandatory label, the revert those
// walks compose, and the crash recovery that drives it over a previous run's journal. Every
// decision they consult — what an entry may say, which descendants are labelled, which
// journal files may be deleted — lives in the untagged files beside this one and is
// table-testable on any OS; what is here is the part that needs a real SACL.

// LabelTree labels root and everything beneath it Low, journalling each mutation into j
// BEFORE it is made. That ordering is ADR 0020 §2's one rule and the reason this walk and the
// journal are one package: a process killed mid-pass still leaves a complete-enough record to
// undo what it did, and the pairing is a property of this function rather than a convention
// its callers remember.
//
// It is idempotent per session — a root already walked returns immediately through j's memo,
// so the first confined command of a session pays the pass and the rest are free. The memo is
// keyed by the FOLDED path for the same reason the journal is: C:\Work and c:\work are one
// root, and labelling it twice would journal it twice.
//
// The root is read, recorded and labelled first, and what its entry may SAY about the prior
// is the journal's own decision — never apogee's own label as the state to restore. The
// order's one debris case is unwound here at its source: a root whose label write then fails
// has refused the box with NOTHING mutated, so its just-journalled entry describes a mutation
// that never happened. Left in place it is a phantom — every later Retire and Recover fails
// clearing a label that is not there (the same root is just as unwritable to the clear), the
// journal is never retired, and Residue alarms forever over a disk carrying no label. Only a
// JUST-ADDED entry qualifies: one that predates this attempt records an earlier pass whose
// root label may really be on the disk, and what the unwind may remove is likewise the
// journal's decision — an entry with a foreign prior is kept.
//
// The walk must recurse: the label carries no inheritance flags (lowSDDL), so nothing but the
// walk reaches an existing file, and a file that predates the labelling is implicitly Medium —
// a Low child editing an existing source file would be denied. Inheritance is left off on
// purpose: a SACL write propagates an inheritable ACE to every existing descendant the instant
// the root is labelled, which would put the label on the hard links below before the walk's
// skip could refuse them.
//
// Every label read and write, root and descendant alike, goes through ONE handle per object
// opened on the path itself and refused when it is a reparse point (labelRoot,
// labelDescendant, openLabelHandle), so the object journalled is the object labelled. That is
// what keeps a symlink or junction from being labelled through to a target outside the box —
// including a box ROOT swapped for a junction after resolveBoxRoot vetted it, which fails the
// pass (ADR 0020 §6) — and walked reparse points are skipped before any open. HARD links are
// skipped for the same reason by a different mechanism (hardLinkCount): they are not reparse
// points, but every name of a file shares one NTFS security descriptor, so labelling the
// in-box name labels the file at all its other names too. A failure on the ROOT fails the box
// (the fence would be a box the agent cannot write to at all); a failure on an individual
// descendant is tolerated, because a single locked or foreign-owned file makes that ONE path
// read-only to the confined child, exactly as if it were read-only on disk, and must not gate
// a whole session. A descendant whose PRIOR label cannot be read takes the same tolerated
// rung, but before anything is written: no journal entry can describe what the read did not
// deliver, so the path is left exactly as it is (descendantDecision) rather than labelled with
// no record of how to undo it.
//
// Errors come back PLAIN: package platform wraps domain.ErrConfinementUnavailable once at its
// own call site, which is what keeps this package a leaf.
//
// The lock is held for the whole walk — a multi-second critical section by design, exactly as
// the backend's own mutex was before this package existed — so every j. call below resolves
// to an UNEXPORTED body (Journal's lock discipline).
func LabelTree(root string, j *Journal) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.isLabelled(root) {
		return nil
	}
	if err := labelRoot(root, j); err != nil {
		return err
	}

	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == root {
				return fmt.Errorf("cannot walk %q: %v", root, walkErr)
			}
			return nil // an unreadable sub-tree stays unlabelled rather than failing the box
		}
		if path == root {
			return nil
		}
		if shouldSkip, walkResult := skipEntry(entry); shouldSkip {
			return walkResult
		}
		return labelDescendant(path, j)
	}); err != nil {
		return err
	}
	j.markLabelled(root)
	return nil
}

// labelRoot reads, journals and labels one box root through ONE reparse-checked handle
// (openLabelHandle), so the object whose prior is journalled is the object the label lands on:
// a root swapped for a junction after resolveBoxRoot vetted it is refused at the open, and one
// swapped while the handle is held is irrelevant, because the handle stays on the object it
// opened. Every failure fails the box. The identity the journal records still comes from
// Journal.stat, and must name the handle's own object when that read succeeds — a different
// object means the path was swapped between the two opens, and nothing is journalled or
// written.
//
// The caller holds j.mu (LabelTree).
func labelRoot(root string, j *Journal) error {
	handle, err := openLabelHandle(root, labelReadWriteAccess)
	if err != nil {
		return fmt.Errorf("cannot open %q to label it: %v", root, err)
	}
	defer handle.close()

	prior, err := handle.readSDDL()
	if err != nil {
		return fmt.Errorf("cannot read the mandatory label of %q: %v", root, err)
	}
	// The identity is read BEFORE the label goes on, so the journal names the object this
	// pass is about to mutate. A failed read journals no identity (withIdentity) and is not a
	// refusal: the root is labelled exactly as it was before the identity existed.
	rootStat, rootStatErr := j.stat(root)
	if rootStatErr == nil && !handle.names(rootStat) {
		return fmt.Errorf("cannot label %q: the path named a different object by the time its identity was read", root)
	}
	journalled, err := j.record(withIdentity(Entry{Path: root, Root: true, PriorSDDL: prior}, rootStat, rootStatErr))
	if err != nil {
		return err
	}
	if err := handle.setSDDL(lowSDDL); err != nil {
		if journalled {
			j.unwind(root)
		}
		return fmt.Errorf("cannot label %q Low: %v", root, err)
	}
	return nil
}

// labelDescendant reads, journals and labels one walked descendant through ONE reparse-checked
// handle, the same one-object discipline labelRoot keeps, and never fails the walk: every
// refusal is the tolerated-descendant rung, leaving that one path unlabelled and opaque to the
// confined child. A path the handle cannot open — a reparse point that appeared after the
// entry skip, a path gone since the walk listed it, an object this token may not write a label
// to — is left untouched with nothing journalled.
//
// The caller holds j.mu (LabelTree).
func labelDescendant(path string, j *Journal) error {
	handle, err := openLabelHandle(path, labelReadWriteAccess)
	if err != nil {
		return nil
	}
	defer handle.close()

	prior, priorErr := handle.readSDDL()
	// One open per path for the facts: the link count the decision needs and the identity the
	// journal records come from the same stat (Journal.stat), which must describe the handle's
	// own object — otherwise the count belongs to whatever the path named a moment later.
	st, statErr := j.stat(path)
	if statErr == nil && !handle.names(st) {
		return nil
	}
	shouldJournal, shouldLabel := descendantDecision(descendantFacts{
		prior: prior, priorErr: priorErr, links: st.links, linksErr: statErr,
	})
	if !shouldLabel {
		// Either the prior could not be read — so labelling would destroy a possibly-foreign
		// label with no journalled record to restore it from — or the path is (or may be) a
		// hard link, whose descriptor is shared with every other name of the same file,
		// including names outside the box. The path takes the tolerated-descendant rung
		// instead (descendantDecision): it stays unlabelled, and only that one path is opaque
		// to the confined child.
		return nil
	}
	if shouldJournal {
		// A Low prior here is apogee's own label — a tree being re-walked, or one a concurrent
		// session labelled — and the journal drops it rather than recording an instruction to
		// put it back.
		if _, err := j.record(withIdentity(Entry{Path: path, PriorSDDL: prior}, st, statErr)); err != nil {
			return err
		}
	}
	// A failed write is the tolerated rung too: a locked or foreign-owned file stays
	// read-only to the confined child, exactly as if it were read-only on disk.
	_ = handle.setSDDL(lowSDDL)
	return nil
}

// hardLinkCount returns how many directory entries name the same underlying file as path —
// one for an ordinary file, more when the file is hard-linked. LabelTree needs it because a
// hard link is NOT a reparse point and so survives the skip above, while sharing the very
// thing a label is written to: on NTFS all names of a file share one MFT record and therefore
// one security descriptor, so labelling the in-box name marks the file Low wherever else it is
// linked (descendantDecision).
//
// The count comes from a HANDLE (statHandle, which opens it through openHandle): nothing in
// fs.FileInfo carries it on Windows. FILE_READ_ATTRIBUTES is the whole access asked for — the
// least the query needs, and one an exclusively-locked file still grants — and the open
// answers for directories too (which NTFS never hard-links, so they report one). ClearTree
// consults it only when its write handle cannot be opened (clearDescendant); the label walk
// reads the same count through Journal.stat.
func hardLinkCount(path string) (uint32, error) {
	st, err := statHandle(path)
	return st.links, err
}

// statHandle is the one handle read behind hardLinkCount, returning everything that read
// reports: the link count and the object's identity — its volume serial number and its
// 64-bit NTFS file index, which together name one file on the machine however many names it
// has or gains. LabelTree reads both through this single open (Journal.stat, osStat), so
// journalling the identity costs no second CreateFile per descendant; the open semantics are
// openHandle's, reparse points included — the handle is on the path, never a target — but a
// reparse point is NOT refused here: an identity read answers for whatever object the path
// names.
func statHandle(path string) (fileStat, error) {
	handle, info, err := openHandle(path, windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		return fileStat{}, err
	}
	_ = windows.CloseHandle(handle)
	return statOf(info), nil
}

// openHandle is the one CreateFile behind every handle this file takes: access as asked,
// every share mode allowed so the open never disturbs another process,
// FILE_FLAG_BACKUP_SEMANTICS so directories open too, and FILE_FLAG_OPEN_REPARSE_POINT so the
// handle is on the path itself, never a link target. It returns the object's file information
// with the open handle, which the caller closes; a failure comes back as an *os.PathError, so
// os.IsNotExist and errors.Is(fs.ErrNotExist) both recognise a path that is gone.
func openHandle(path string, access uint32) (windows.Handle, windows.ByHandleFileInformation, error) {
	var info windows.ByHandleFileInformation
	pathW, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, info, &os.PathError{Op: "encode", Path: path, Err: err}
	}
	handle, err := windows.CreateFile(pathW, access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return 0, info, &os.PathError{Op: "open", Path: path, Err: err}
	}
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = windows.CloseHandle(handle)
		return 0, info, &os.PathError{Op: "read the file information of", Path: path, Err: err}
	}
	return handle, info, nil
}

// statOf folds one handle's file information into the facts the label walk reads.
func statOf(info windows.ByHandleFileInformation) fileStat {
	return fileStat{
		links:  info.NumberOfLinks,
		volume: info.VolumeSerialNumber,
		index:  uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
	}
}

// osStat supplies the Windows build's half of Journal's stat seam: the real handle read.
func osStat() statFunc { return statHandle }

// ClearTree removes the mandatory label from root and everything beneath it, returning it to
// the unlabelled (implicitly Medium) state it was in before the run. A path that has since
// been deleted is not an error — the tree is being restored, not reconstructed — and that is
// the ONLY failure the walk tolerates: every other descendant failure (a subtree the walk
// cannot enumerate, a label write the object's DACL denies) is counted and reported through
// clearTreeOutcome, because a nil return here is what retire takes as "verifiably reverted"
// before deleting the journal. Swallowing those failures would strand Low labels on the disk
// with no record and no residue report; returning them keeps the journal, so the next session
// or recovery retries.
//
// It skips exactly what LabelTree skips, and that pairing is the point: reparse points, whose
// target a label write would reach out of the box, and any entry whose Info() fails, since it
// may be one (both through entrySkipDecision, and again at the write's own reparse-checked
// handle — clearDescendant), and hard links, whose descriptor is
// the same NTFS record as every other name of the file (clearDescendantDecision). The clear is
// a WRITE — a NULL SACL — so clearing a path the label pass refused would destroy a label on a
// record apogee never wrote to, which is the mirror image of the harm the label-side skip
// exists to prevent. A skipped path is not a failure and is not counted. The ROOT is cleared
// through the same reparse-checked handle (SetSDDL), and a root that has become a reparse point
// since it was labelled is refused at that handle, never cleared through to its target. Like a
// root that has vanished, it is then a completed revert and returns nil (revertTargetGone):
// apogee never labelled a reparse point, so the directory the journal names is no longer at
// that path, there is nothing of apogee's there to clear, and an error would keep the journal
// forever over a write that can never succeed.
func ClearTree(root string) error {
	if err := SetSDDL(root, clearSDDL); err != nil {
		if revertTargetGone(err) {
			return nil
		}
		return fmt.Errorf("apogee: confine: clear the mandatory label of %q: %w", root, err)
	}
	failures := 0
	var first error
	fail := func(path string, err error) {
		failures++
		if first == nil {
			first = fmt.Errorf("%q: %v", path, err)
		}
	}
	// The callback only ever returns nil or SkipDir — failures go into the count — so the
	// walk itself cannot error.
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// An unenumerable point means any labels beneath it were not reached; a path
			// that has since vanished carries no label to clear.
			if !os.IsNotExist(walkErr) {
				fail(path, walkErr)
			}
			return nil
		}
		if path == root {
			return nil
		}
		if shouldSkip, walkResult := skipEntry(entry); shouldSkip {
			// The same skip LabelTree takes (entrySkipDecision), an unreadable Info() included:
			// a NULL SACL written through a link lands on its target outside the box.
			return walkResult
		}
		if err := clearDescendant(path); err != nil {
			fail(path, err)
		}
		return nil
	})
	return clearTreeOutcome(root, failures, first)
}

// clearDescendant writes the NULL SACL onto one walked descendant through ONE reparse-checked
// handle, so the hard-link count that clears the path and the object the write lands on are
// the same object. It returns only a failure ClearTree must count; everything the label pass
// skipped — a reparse point, a hard link — and a path gone since the walk listed it return nil.
//
// A handle that cannot be opened for the write falls back to the path's own link count before
// the failure is counted, because the open asks for WRITE_OWNER: a hard link the label pass
// skipped may well deny it, and counting it would keep the journal forever over a path apogee
// never labelled (clearDescendantDecision).
func clearDescendant(path string) error {
	handle, err := openLabelHandle(path, labelWriteAccess)
	if err != nil {
		if revertTargetGone(err) {
			return nil
		}
		links, linksErr := hardLinkCount(path)
		if !clearDescendantDecision(links, linksErr) {
			return nil
		}
		return err
	}
	defer handle.close()

	if !clearDescendantDecision(handle.info.NumberOfLinks, nil) {
		// The path is — or may be — one name of a file whose descriptor is shared with names
		// outside the box, so the NULL SACL below would land on all of them. LabelTree skipped
		// this same path for the same reason, so nothing of apogee's is on it to clear
		// (descendantDecision, clearDescendantDecision). The skip is not counted: a failure
		// here would keep the journal forever over a path apogee never labelled.
		return nil
	}
	return handle.setSDDL(clearSDDL)
}

// skipEntry reads one walked entry's Info() and hands the outcome to entrySkipDecision, the
// shared skip both LabelTree and ClearTree take before touching a descendant's descriptor.
func skipEntry(entry fs.DirEntry) (shouldSkip bool, walkResult error) {
	var mode fs.FileMode
	info, err := entry.Info()
	if err == nil {
		mode = info.Mode()
	}
	return entrySkipDecision(mode, err, entry.IsDir())
}

// osRevert supplies the Windows build's half of Journal's revert seam: the production revert
// IS the label walk above, and this is the one place it is handed to a journal (Open). The
// non-Windows build supplies nil from the same function, which is what lets Retire be declared
// ONCE, unguarded, beside the rest of the journal's lifecycle.
func osRevert() revertFunc {
	return func(home, own string) func(Record) ([]Entry, error) {
		return revertSparingLiveSiblings(home, own, ProcessAlive)
	}
}

// revertSparingLiveSiblings returns the production revert for the journal at own under home:
// revertJournal over the journal's roots MINUS every root a sibling journal with a live owning
// process still names and every root apogee's own label no longer vouches for
// (revertibleRoots, rootClearable), restoring only the priors no sibling journal still
// claims the tree of — the rest are handed back as the journal's remains, together with every
// root a live sibling spared (restorablePriors, handoffSparedRoots, retire). Teardown and
// recovery both revert through this closure, so neither ever clears a root out from under a
// concurrently running session, and neither restores a foreign prior a sibling's pending clear
// would destroy — the sibling read and both exclusions happen at revert time, when liveness
// and the claim set are current, not at construction.
//
// A spared root leaves this journal alive rather than discharged, which is what keeps two
// sessions closing at once from each sparing the shared root to the other and then deleting
// both records of it: the join of the two hand-off sets is the whole of what this revert did
// NOT do, and retire keeps the file for all of it.
//
// alive is the liveness the sibling exclusion reads: ProcessAlive at teardown, and at recovery
// the pass's own view (recoveryLiveness), under which a journal of this very process is an
// interrupted run rather than a live claim. The two must agree within one Recover pass, or a
// journal it recovers as dead also spares its roots as alive and the pair deadlock (Recover).
func revertSparingLiveSiblings(home, own string, alive func(pid int, started uint64) bool) func(Record) ([]Entry, error) {
	return func(r Record) ([]Entry, error) {
		if err := judgePriors(r, own); err != nil {
			return nil, err
		}
		siblings := siblingJournals(home, own)
		restore, handoff := restorablePriors(r, siblings)
		clear, spared := revertibleRoots(r, siblings, alive, readJudgedLabel, statHandle)
		if err := revertJournal(clear, restore); err != nil {
			return nil, err
		}
		return handoffSparedRoots(handoff, spared), nil
	}
}

// judgePriors settles which of one journal's instructions are still apogee's to carry out —
// which recorded priors may be put back, and which named ROOTS may be cleared — and settles it
// BEFORE the caller clears anything (F-08). A journal is an instruction to WRITE mandatory
// labels onto named paths and to STRIP them off named trees, and until these checks existed the
// revert obeyed it unconditionally — so a journal planted or corrupted under the apogee home
// relabelled arbitrary paths, and NULL-SACLed arbitrary trees, at the next construction. Both
// rules (priorRestorable, rootClearable) are the same one: apogee's own Low label must still be
// on the path, because nothing else marks it as this run's, or a dead run's, to revert. The
// clear side adds the guardrail the label pass makes on the way in — a volume root is refused
// whatever it carries — and it REFUSES rather than aborting: a root that fails the test is
// skipped by revertibleRoots and the rest of the journal reverts. Ahead of both rules, an entry
// that journalled the IDENTITY of the object it labelled is held to it (identityRefuses): the
// label is read off a path, and a stand-in created under that path since — labelled Low or not
// — is neither cleared nor restored onto, its prior carried instead (judgeEntries).
//
// Every decision it takes lives in judgeEntries, which reads through an injected seam and is
// therefore provable on any OS; what is Windows-tagged HERE is the real label read it is given
// and the journal rewrite that persists what it decided.
//
// The ORDER is the whole point. ClearTree is what turns a path apogee labelled into an
// unlabelled one, so a verdict taken after it cannot tell apogee's own work from a stranger's
// entry — every prior would read foreign and be dropped, and the foreign labels the journal
// exists to preserve would be lost. The judgement therefore runs here, above
// revertSparingLiveSiblings' clear, and covers the restore set and the HAND-OFF set alike:
// which of the two an entry lands in is decided after this, and a handed-off entry is exactly
// the one a later pass reads once some other session's clear has unlabelled its path.
//
// The verdict is recorded on the entry (Entry.Judged, Entry.RootJudged, Entry.Carried) and PERSISTED before
// the clear, because the paths that re-visit a path later — a hand-off waiting for a live
// sibling to retire, and a retry after a revert that failed (session.go's kept journal, ADR
// 0020 §2) — all read it after the clear, when the NULL SACL the clear itself wrote is all
// there is to read. A journal written by an older apogee carries no flags and is judged on its
// first pass, while it still reads Low. It is written back through own, this journal's own
// file: a write failure aborts the revert with NOTHING cleared, which is the same posture the
// label pass takes on the way in — no record on the disk, no mutation of the disk.
//
// That abort is a PRIOR's rule only. A root-only journal is the overwhelmingly common case
// (journal.go's Entries), and it wrote nothing here at all before the root verdict existed, so
// making the write a precondition of clearing would strand every label on an unwritable apogee
// home for runs that clear cleanly today. A root verdict is therefore taken in memory — the
// in-place r.Entries write below, which is the session's own slice — and a rewrite that fails
// with no prior judged falls through to the clear it has always performed; the verdict is
// simply not carried across processes on that run. A plain CARRY sits on that side of the
// line too (judgeEntries): it writes nothing and discards nothing, so a lost count only
// lengthens the carry, while the exhausted carry that finally discards a prior settles it and
// takes the abort with every other discard.
//
// A read failure other than "gone" aborts the revert too, and equally before the clear: the
// entry stays unjudged, the journal is kept whole by retire, and the next run judges it against
// a path that still carries every label this one would have removed. Clearing first and
// retrying later would hand that retry an unlabelled path and the verdict "not apogee's".
//
// The flags are written onto r.Entries IN PLACE, which is deliberate: Record.Entries is the
// slice the session's in-memory journal holds, so a verdict taken here survives a failed pass
// in memory as well as on disk and a repeated Close never re-judges a path the first one
// cleared. The persisted copy additionally drops the entries left describing no mutation at
// all — recordEntry's rule applied on the way out, so a dropped prior does not linger as an
// instruction to do nothing — while an entry that still names a ROOT keeps its clear
// obligation and only loses its prior.
func judgePriors(r Record, own string) error {
	judged, priorJudged, err := judgeEntries(r.Entries, readJudgedLabel, statHandle)
	if err != nil {
		return err
	}
	if !judged || own == "" {
		return nil
	}
	surviving := make([]Entry, 0, len(r.Entries))
	for _, entry := range r.Entries {
		if entry.Root || entry.PriorSDDL != "" {
			surviving = append(surviving, entry)
		}
	}
	if err := WriteJournal(own, Record{PID: r.PID, Started: r.Started, Entries: surviving}); err != nil && priorJudged {
		return err
	}
	return nil
}

// revertJournal undoes one journal's disk mutation: clear the label from every object under
// each of roots, then restore the prior descriptors. roots is the journal's root set minus
// what a live sibling session still claims and minus every root apogee's own Low label no
// longer vouches for (revertibleRoots, rootClearable) — a spared root is not a failure, but it
// is handed back rather than discharged, so this journal is rewritten to carry it and the last
// session to close is the one that clears the tree (handoffSparedRoots, retire).
// priors is likewise the journal's restorable subset
// (restorablePriors): a prior under a sibling-claimed root is handed off rather than restored
// here, because the sibling's pending clear would wipe it. Clearing first and restoring second
// is the order that matters — a prior label inside a root would otherwise be wiped by the walk
// that follows it.
//
// A prior is restored only onto a path that still carried the Low label this session — or a
// dead one — wrote, judged BEFORE the clear (judgePriors, priorRestorable): apogee's own mark
// is what makes the path apogee's to revert, and a journal a stranger planted names paths that
// never carried it. By the time this function runs, priors holds exactly the survivors of that
// judgement, so the loop below writes back only labels apogee is answerable for.
//
// A prior-labelled path that no longer exists is a completed revert, not a failure: the agent
// deleting or renaming a workspace file is routine activity, and an object that is gone
// carries no label to put back ("restored, not reconstructed" — the posture ClearTree's root
// already takes). Failing on it instead would wedge the lifecycle permanently: teardown would
// warn every session and recovery would retry and fail every startup, over a label that
// stopped existing. A prior-labelled path that is now a reparse point is the same case
// (revertTargetGone): the write refuses the link, writes nothing through it, and settles.
func revertJournal(roots []string, priors map[string]string) error {
	var firstErr error
	for _, root := range roots {
		if err := ClearTree(root); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for path, sddl := range priors {
		if err := SetSDDL(path, sddl); err != nil && !revertTargetGone(err) && firstErr == nil {
			firstErr = fmt.Errorf("apogee: confine: restore the prior label of %q: %w", path, err)
		}
	}
	return firstErr
}

// Recover finishes the restore for every journal under home whose owning process is gone —
// ADR 0020 §2's interrupted-cleanup remedy. A journal belonging to a LIVE process is left
// strictly alone: it belongs to a concurrently running apogee whose labels are still in use,
// and reverting them would un-fence its session. The same respect extends to root OVERLAP: a
// dead journal's root that a live journal also names stays labelled
// (revertSparingLiveSiblings), because the live session is using that very tree — its own
// journal keeps the clear obligation, and recovery gets the root once that session too is
// gone. The dead journal is not retired over that root either: the spared root is handed back
// and the file rewritten to carry it (handoffSparedRoots, retire), so the obligation survives
// in BOTH records and whichever outlives the other clears the tree.
//
// A journal whose revert fails survives this pass (retire): recovery is best-effort — there is
// no user to tell at construction time — but it must never destroy the record of labels it did
// not manage to remove, so a later run gets another attempt.
//
// A journal is not taken as an instruction to WRITE labels, either. Anything that can create a
// file under the apogee home could otherwise name arbitrary paths and have the next
// construction label them, so every prior a journal records is judged against the path's
// CURRENT label before anything is cleared, and one that does not still carry the Low label
// apogee wrote is dropped rather than applied (judgePriors, priorRestorable) — a prior is
// restored only onto a path that still carried apogee's own mark. The mark is read off a path,
// so an entry that journalled its object's identity must still name that very object as well
// (identityRefuses): a root or prior path whose object was replaced since is left alone,
// whatever label the replacement carries.
//
// A journal that cannot be DECODED is likewise left where it is: it names no roots to revert
// and no owner to check, so acting on it is impossible and deleting it would throw away the
// only trace of whatever it described. It is not silent, though — Residue reports it, which is
// the only way that state ever reaches a human.
//
// The pass repeats until no journal retires: a journal whose prior restore or whose own root
// was handed off because a sibling journal still claimed the root (restorablePriors,
// handoffSparedRoots) becomes completable the
// moment that sibling retires later in the same sweep, and which order the two are visited in
// is an accident of their PIDs' spellings — one more sweep finishes the restore now rather
// than deferring it to the next session. Each continuing sweep removes at least one file, so
// the loop is bounded by the journal count (recoverSweep).
//
// One liveness view serves the whole pass (recoveryLiveness): the journal this process owns is
// recovered as an interrupted run, so the sibling exclusion must not count it as a live claim
// either. Read two ways — dead when choosing what to recover, alive when sparing a dead
// sibling's root — the self-owned journal and a dead sibling sharing its root each spare the
// root to the other, neither retires, and the foreign prior on that root stays stripped until
// the process exits.
func Recover(home string) {
	alive := recoveryLiveness(os.Getpid(), ProcessAlive)
	recoverSweep(home, alive, func(own string) func(Record) ([]Entry, error) {
		return revertSparingLiveSiblings(home, own, alive)
	})
}

// ProcessAlive reports whether the journal owner named by pid and started is still running, so
// recovery never reverts the labels of a live apogee. The PID alone cannot say that: Windows
// recycles PIDs, and a stranger that inherits a dead owner's PID would otherwise read as that
// owner for as long as it runs — its journal never recovered, its roots spared from every
// sibling's clear. So a non-zero started (Record.Started, the owner's creation time) must also
// equal the running process's creation time; a process whose creation time differs, or cannot
// be read, is a different process and the owner reads as gone. started == 0 is a record that
// never journalled it (a journal written before Record.Started existed, or a flush whose read
// failed), which keeps the PID-only check. A PID that cannot be opened is treated as gone. It
// is exported for package platform's Windows-tagged lifecycle tests, which drive it against a
// real child process (D6).
func ProcessAlive(pid int, started uint64) bool {
	if pid <= 0 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	const stillActive = 259 // STILL_ACTIVE
	if code != stillActive {
		return false
	}
	if started == 0 {
		return true
	}
	running, ok := creationTime(handle)
	return ok && running == started
}

// processStarted returns the creation time of the process with this PID — the FILETIME
// GetProcessTimes reports, folded into one uint64 of 100-nanosecond intervals since 1601 — and
// whether it could be read. It is the value Record.Started journals beside the owning PID: a
// PID the OS has recycled names a different process, and that process's creation time is what
// gives it away. A PID that names no process, or one this token may not query, reads (0,
// false), and 0 is never a real creation time.
func processStarted(pid int) (uint64, bool) {
	if pid <= 0 {
		return 0, false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, false
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	return creationTime(handle)
}

// creationTime reads the creation time of the process behind handle, folded as processStarted
// describes, and whether it could be read. It is the one fold both the journal's writer
// (processStarted) and its liveness check (ProcessAlive) use, so the two cannot disagree on
// what a matching creation time looks like.
func creationTime(handle windows.Handle) (uint64, bool) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, false
	}
	started := uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)
	return started, started != 0
}

// ReadSDDL returns the object's mandatory-label descriptor in SDDL form, or "" when it carries
// no explicit label (the state teardown restores by clearing). Only a descriptor actually
// containing a label ACE is reported, so the journal never records "S:" noise as something to
// put back. The read goes through one reparse-checked handle (openLabelHandle): a path that is a
// reparse point is refused with errReparsePoint rather than read through to its target, and a
// path that does not exist reports an error os.IsNotExist recognises. It is exported for
// package platform's Windows-tagged lifecycle tests, which assert against real SACLs (D6).
func ReadSDDL(path string) (string, error) {
	handle, err := openLabelHandle(path, labelReadAccess)
	if err != nil {
		return "", err
	}
	defer handle.close()
	return handle.readSDDL()
}

// SetSDDL writes sddl's SACL as the object's mandatory label, through one reparse-checked
// handle (openLabelHandle): a path that is a reparse point is refused with errReparsePoint, so
// the write can never land on a link's target outside the box, however the path changed since
// anything above it was checked. Only LABEL_SECURITY_INFORMATION is written, which needs
// WRITE_OWNER on the object and no privilege — the caller owns its workspace, and asking for
// SACL_SECURITY_INFORMATION instead would demand SeSecurityPrivilege and fail for an ordinary
// user. It is exported alongside ReadSDDL for the same tests (D6).
func SetSDDL(path, sddl string) error {
	handle, err := openLabelHandle(path, labelWriteAccess)
	if err != nil {
		return err
	}
	defer handle.close()
	return handle.setSDDL(sddl)
}

// readJudgedLabel is the label read the pre-clear judgement takes (judgePriors,
// revertibleRoots): ReadSDDL, except that a path which is now a reparse point reads as carrying
// no label. apogee never labels a reparse point — the walks skip them and every label write
// refuses them — so nothing of apogee's is on one, and "no label" is the verdict that neither
// writes through the link nor wedges the journal: a prior carries (priorRestorable) and a root
// is spared (rootClearable), where the refusal itself would abort the whole revert.
func readJudgedLabel(path string) (string, error) {
	label, err := ReadSDDL(path)
	if errors.Is(err, errReparsePoint) {
		return "", nil
	}
	return label, err
}

// The access each kind of label handle asks for. Reading a mandatory label needs READ_CONTROL
// and writing one needs WRITE_OWNER (LABEL_SECURITY_INFORMATION); FILE_READ_ATTRIBUTES is what
// the reparse check reads the object's attributes with.
const (
	labelReadAccess      = windows.READ_CONTROL | windows.FILE_READ_ATTRIBUTES
	labelWriteAccess     = windows.WRITE_OWNER | windows.FILE_READ_ATTRIBUTES
	labelReadWriteAccess = labelReadAccess | labelWriteAccess
)

// labelHandle is one open of an object that its mandatory-label reads and writes go through,
// opened ON the path itself (FILE_FLAG_OPEN_REPARSE_POINT) and already vetted as no reparse
// point. Holding it pins the object: whatever the path is renamed or re-pointed to afterwards,
// every read and write through the handle reaches the object the check passed, which is what
// closes the window a path-based SetNamedSecurityInfo left between a check and its write.
type labelHandle struct {
	path   string
	handle windows.Handle
	info   windows.ByHandleFileInformation
}

// openLabelHandle opens path for the label access asked for and refuses it — handle closed,
// error wrapping errReparsePoint — when the object's own attributes carry
// FILE_ATTRIBUTE_REPARSE_POINT. The caller closes the handle it gets.
func openLabelHandle(path string, access uint32) (labelHandle, error) {
	handle, info, err := openHandle(path, access)
	if err != nil {
		return labelHandle{}, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(handle)
		return labelHandle{}, &os.PathError{Op: "open", Path: path, Err: errReparsePoint}
	}
	return labelHandle{path: path, handle: handle, info: info}, nil
}

// close releases the handle. Nothing is flushed through it, so a close failure has nothing to
// report.
func (h labelHandle) close() { _ = windows.CloseHandle(h.handle) }

// names reports whether st — a stat taken through another open of the same path — describes
// this handle's object, by the identity both reads report.
func (h labelHandle) names(st fileStat) bool {
	own := statOf(h.info)
	return st.volume == own.volume && st.index == own.index
}

// readSDDL reads the object's mandatory label through the handle, with ReadSDDL's contract.
func (h labelHandle) readSDDL() (string, error) {
	sd, err := windows.GetSecurityInfo(h.handle, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		return "", err
	}
	text := sd.String()
	if !strings.Contains(text, labelACEPrefix) {
		return "", nil
	}
	return text, nil
}

// setSDDL writes sddl's SACL as the object's mandatory label through the handle, with
// SetSDDL's contract.
func (h labelHandle) setSDDL(sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("parse label %q: %w", sddl, err)
	}
	sacl, _, err := sd.SACL()
	if err != nil {
		return fmt.Errorf("read label SACL from %q: %w", sddl, err)
	}
	return windows.SetSecurityInfo(h.handle, windows.SE_FILE_OBJECT,
		windows.LABEL_SECURITY_INFORMATION, nil, nil, nil, sacl)
}

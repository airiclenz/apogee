//go:build windows

package winlabel

import (
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
// purpose: SetNamedSecurityInfo propagates an inheritable ACE to every existing descendant the
// instant the root is labelled, which would put the label on the hard links below before the
// walk's skip could refuse them.
//
// Symlinks and other reparse points are skipped entirely — SetNamedSecurityInfo follows the
// link, so labelling one would silently mutate a target outside the box. HARD links are
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
	prior, err := ReadSDDL(root)
	if err != nil {
		return fmt.Errorf("cannot read the mandatory label of %q: %v", root, err)
	}
	journalled, err := j.record(Entry{Path: root, Root: true, PriorSDDL: prior})
	if err != nil {
		return err
	}
	if err := SetSDDL(root, lowSDDL); err != nil {
		if journalled {
			j.unwind(root)
		}
		return fmt.Errorf("cannot label %q Low: %v", root, err)
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
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		prior, priorErr := ReadSDDL(path)
		links, linksErr := hardLinkCount(path)
		shouldJournal, shouldLabel := descendantDecision(descendantFacts{
			prior: prior, priorErr: priorErr, links: links, linksErr: linksErr,
		})
		if !shouldLabel {
			// Either the prior could not be read — so labelling would destroy a
			// possibly-foreign label with no journalled record to restore it from — or the
			// path is (or may be) a hard link, whose descriptor is shared with every other
			// name of the same file, including names outside the box. The path takes the
			// tolerated-descendant rung instead (descendantDecision): it stays unlabelled,
			// and only that one path is opaque to the confined child.
			return nil
		}
		if shouldJournal {
			// A Low prior here is apogee's own label — a tree being re-walked, or one a
			// concurrent session labelled — and the journal drops it rather than recording
			// an instruction to put it back.
			if _, err := j.record(Entry{Path: path, PriorSDDL: prior}); err != nil {
				return err
			}
		}
		_ = SetSDDL(path, lowSDDL)
		return nil
	}); err != nil {
		return err
	}
	j.markLabelled(root)
	return nil
}

// hardLinkCount returns how many directory entries name the same underlying file as path —
// one for an ordinary file, more when the file is hard-linked. LabelTree needs it because a
// hard link is NOT a reparse point and so survives the skip above, while sharing the very
// thing a label is written to: on NTFS all names of a file share one MFT record and therefore
// one security descriptor, so labelling the in-box name marks the file Low wherever else it is
// linked (descendantDecision).
//
// The count comes from a HANDLE: nothing in fs.FileInfo carries it on Windows.
// FILE_READ_ATTRIBUTES is the whole access asked for — the least the query needs, and one an
// exclusively-locked file still grants — every share mode is allowed so opening never disturbs
// another process, FILE_FLAG_BACKUP_SEMANTICS lets the same call answer for directories (which
// NTFS never hard-links, so they report one), and FILE_FLAG_OPEN_REPARSE_POINT keeps the
// handle on the path itself rather than on a link target, the same posture the walk's own skip
// takes.
func hardLinkCount(path string) (uint32, error) {
	pathW, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("encode %q: %w", path, err)
	}
	handle, err := windows.CreateFile(pathW, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return 0, fmt.Errorf("open %q to count its links: %w", path, err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return 0, fmt.Errorf("read the file information of %q: %w", path, err)
	}
	return info.NumberOfLinks, nil
}

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
// target SetNamedSecurityInfo would follow out of the box, and hard links, whose descriptor is
// the same NTFS record as every other name of the file (clearDescendantDecision). The clear is
// a WRITE — a NULL SACL — so clearing a path the label pass refused would destroy a label on a
// record apogee never wrote to, which is the mirror image of the harm the label-side skip
// exists to prevent. A skipped path is not a failure and is not counted.
func ClearTree(root string) error {
	if err := SetSDDL(root, clearSDDL); err != nil {
		if os.IsNotExist(err) {
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
		if info, err := entry.Info(); err == nil && info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		links, linksErr := hardLinkCount(path)
		if !clearDescendantDecision(links, linksErr) {
			// The path is — or may be — one name of a file whose descriptor is shared with
			// names outside the box, so the NULL SACL below would land on all of them.
			// LabelTree skipped this same path for the same reason, so nothing of apogee's is
			// on it to clear (descendantDecision, clearDescendantDecision). The skip is not
			// counted: a failure here would keep the journal forever over a path apogee never
			// labelled.
			return nil
		}
		if err := SetSDDL(path, clearSDDL); err != nil && !os.IsNotExist(err) {
			fail(path, err)
		}
		return nil
	})
	return clearTreeOutcome(root, failures, first)
}

// osRevert supplies the Windows build's half of Journal's revert seam: the production revert
// IS the label walk above, and this is the one place it is handed to a journal (Open). The
// non-Windows build supplies nil from the same function, which is what lets Retire be declared
// ONCE, unguarded, beside the rest of the journal's lifecycle.
func osRevert() revertFunc { return revertSparingLiveSiblings }

// revertSparingLiveSiblings returns the production revert for the journal at own under home:
// revertJournal over the journal's roots MINUS every root a sibling journal with a live owning
// process still names and every root apogee's own label no longer vouches for
// (revertibleRoots, rootClearable), restoring only the priors no sibling journal still
// claims the tree of — the rest are handed back as the journal's remains (restorablePriors,
// retire). Teardown and recovery both revert through this closure, so neither ever clears a
// root out from under a concurrently running session, and neither restores a foreign prior a
// sibling's pending clear would destroy — the sibling read and both exclusions happen at
// revert time, when liveness and the claim set are current, not at construction.
func revertSparingLiveSiblings(home, own string) func(Record) ([]Entry, error) {
	return func(r Record) ([]Entry, error) {
		if err := judgePriors(r, own); err != nil {
			return nil, err
		}
		siblings := siblingJournals(home, own)
		restore, handoff := restorablePriors(r, siblings)
		if err := revertJournal(revertibleRoots(r, siblings, ProcessAlive, ReadSDDL), restore); err != nil {
			return nil, err
		}
		return handoff, nil
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
// skipped by revertibleRoots and the rest of the journal reverts.
//
// The ORDER is the whole point. ClearTree is what turns a path apogee labelled into an
// unlabelled one, so a verdict taken after it cannot tell apogee's own work from a stranger's
// entry — every prior would read foreign and be dropped, and the foreign labels the journal
// exists to preserve would be lost. The judgement therefore runs here, above
// revertSparingLiveSiblings' clear, and covers the restore set and the HAND-OFF set alike:
// which of the two an entry lands in is decided after this, and a handed-off entry is exactly
// the one a later pass reads once some other session's clear has unlabelled its path.
//
// The verdict is recorded on the entry (Entry.Judged, Entry.RootJudged) and PERSISTED before
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
// simply not carried across processes on that run.
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
	judged, priorJudged := false, false
	for i := range r.Entries {
		entry := &r.Entries[i]
		// A root that already carried a foreign label is ONE entry wearing both instructions
		// (LabelTree), and both verdicts are taken off the same read of the same path.
		judgeRoot := entry.Root && !entry.RootJudged
		judgePrior := entry.PriorSDDL != "" && !entry.Judged
		if !judgeRoot && !judgePrior {
			continue
		}
		current, readErr := ReadSDDL(entry.Path)
		if judgeRoot && rootClearable(entry.Path, current, readErr) {
			entry.RootJudged = true
			judged = true
		}
		if !judgePrior {
			continue
		}
		switch restore, drop := priorRestorable(current, readErr); {
		case restore:
			entry.Judged = true
		case drop:
			entry.PriorSDDL = ""
		default:
			return fmt.Errorf("apogee: confine: cannot read the mandatory label of %q to judge the prior the journal records for it: %v",
				entry.Path, readErr)
		}
		judged, priorJudged = true, true
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
	if err := WriteJournal(own, Record{PID: r.PID, Entries: surviving}); err != nil && priorJudged {
		return err
	}
	return nil
}

// revertJournal undoes one journal's disk mutation: clear the label from every object under
// each of roots, then restore the prior descriptors. roots is the journal's root set minus
// what a live sibling session still claims and minus every root apogee's own Low label no
// longer vouches for (revertibleRoots, rootClearable) — a spared root is not a failure,
// because the sibling's own journal carries the Root entry and with it the clear obligation,
// so this journal may still retire. priors is likewise the journal's restorable subset
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
// stopped existing.
func revertJournal(roots []string, priors map[string]string) error {
	var firstErr error
	for _, root := range roots {
		if err := ClearTree(root); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for path, sddl := range priors {
		if err := SetSDDL(path, sddl); err != nil && !os.IsNotExist(err) && firstErr == nil {
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
// gone.
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
// restored only onto a path that still carried apogee's own mark.
//
// A journal that cannot be DECODED is likewise left where it is: it names no roots to revert
// and no owner to check, so acting on it is impossible and deleting it would throw away the
// only trace of whatever it described. It is not silent, though — Residue reports it, which is
// the only way that state ever reaches a human.
//
// The pass repeats until no journal retires: a journal whose prior restore was handed off
// because a sibling journal still claimed the root (restorablePriors) becomes completable the
// moment that sibling retires later in the same sweep, and which order the two are visited in
// is an accident of their PIDs' spellings — one more sweep finishes the restore now rather
// than deferring it to the next session. Each continuing sweep removes at least one file, so
// the loop is bounded by the journal count.
func Recover(home string) {
	self := os.Getpid()
	for {
		retiredAny := false
		for _, path := range ListJournals(home) {
			r, err := ReadJournal(path)
			if err != nil {
				continue
			}
			if r.PID != self && ProcessAlive(r.PID) {
				continue
			}
			remaining, err := retire(path, r, revertSparingLiveSiblings(home, path))
			if err == nil && len(remaining) == 0 {
				retiredAny = true
			}
		}
		if !retiredAny {
			return
		}
	}
}

// ProcessAlive reports whether pid names a running process, so recovery never reverts the
// labels of a live apogee. A PID that cannot be opened is treated as gone. It is exported for
// package platform's Windows-tagged lifecycle tests, which drive it against a real child
// process (D6).
func ProcessAlive(pid int) bool {
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
	return code == stillActive
}

// ReadSDDL returns the object's mandatory-label descriptor in SDDL form, or "" when it carries
// no explicit label (the state teardown restores by clearing). Only a descriptor actually
// containing a label ACE is reported, so the journal never records "S:" noise as something to
// put back. It is exported for package platform's Windows-tagged lifecycle tests, which assert
// against real SACLs (D6).
func ReadSDDL(path string) (string, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.LABEL_SECURITY_INFORMATION)
	if err != nil {
		return "", err
	}
	text := sd.String()
	if !strings.Contains(text, labelACEPrefix) {
		return "", nil
	}
	return text, nil
}

// SetSDDL writes sddl's SACL as the object's mandatory label. Only
// LABEL_SECURITY_INFORMATION is requested, which needs WRITE_OWNER on the object and no
// privilege — the caller owns its workspace, and asking for SACL_SECURITY_INFORMATION
// instead would demand SeSecurityPrivilege and fail for an ordinary user. It is exported
// alongside ReadSDDL for the same tests (D6).
func SetSDDL(path, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("parse label %q: %w", sddl, err)
	}
	sacl, _, err := sd.SACL()
	if err != nil {
		return fmt.Errorf("read label SACL from %q: %w", sddl, err)
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.LABEL_SECURITY_INFORMATION, nil, nil, nil, sacl)
}

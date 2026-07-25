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
// The walk must recurse: inheritance applies to NEWLY CREATED objects only, so a file that
// predates the labelling is implicitly Medium and a Low child editing an existing source file
// would be denied.
//
// Symlinks and other reparse points are skipped entirely — SetNamedSecurityInfo follows the
// link, so labelling one would silently mutate a target outside the box. A failure on the
// ROOT fails the box (the fence would be a box the agent cannot write to at all); a failure
// on an individual descendant is tolerated, because a single locked or foreign-owned file
// makes that ONE path read-only to the confined child, exactly as if it were read-only on
// disk, and must not gate a whole session. A descendant whose PRIOR label cannot be read
// takes the same tolerated rung, but before anything is written: no journal entry can
// describe what the read did not deliver, so the path is left exactly as it is
// (descendantDecision) rather than labelled with no record of how to undo it.
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
	if err := SetSDDL(root, dirSDDL); err != nil {
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
		shouldJournal, shouldLabel := descendantDecision(prior, priorErr)
		if !shouldLabel {
			// The prior could not be read, so labelling would destroy a possibly-foreign
			// label with no journalled record to restore it from. The path takes the
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
		sddl := fileSDDL
		if entry.IsDir() {
			sddl = dirSDDL
		}
		_ = SetSDDL(path, sddl)
		return nil
	}); err != nil {
		return err
	}
	j.markLabelled(root)
	return nil
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
// process still names (revertibleRoots), restoring only the priors no sibling journal still
// claims the tree of — the rest are handed back as the journal's remains (restorablePriors,
// retire). Teardown and recovery both revert through this closure, so neither ever clears a
// root out from under a concurrently running session, and neither restores a foreign prior a
// sibling's pending clear would destroy — the sibling read and both exclusions happen at
// revert time, when liveness and the claim set are current, not at construction.
func revertSparingLiveSiblings(home, own string) func(Record) ([]Entry, error) {
	return func(r Record) ([]Entry, error) {
		siblings := siblingJournals(home, own)
		restore, handoff := restorablePriors(r, siblings)
		if err := revertJournal(revertibleRoots(r, siblings, ProcessAlive), restore); err != nil {
			return nil, err
		}
		return handoff, nil
	}
}

// revertJournal undoes one journal's disk mutation: clear the label from every object under
// each of roots, then restore the prior descriptors. roots is the journal's root set minus
// what a live sibling session still claims (revertibleRoots) — a spared root is not a failure,
// because the sibling's own journal carries the Root entry and with it the clear obligation,
// so this journal may still retire. priors is likewise the journal's restorable subset
// (restorablePriors): a prior under a sibling-claimed root is handed off rather than restored
// here, because the sibling's pending clear would wipe it. Clearing first and restoring second
// is the order that matters — a prior label inside a root would otherwise be wiped by the walk
// that follows it.
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

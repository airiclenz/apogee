package winlabel

import (
	"fmt"
	"os"
	"sync"
)

// Journal is one session's label journal: the on-disk Record, the file it lives in, and the
// roots this session has already labelled. It is what the Windows backend COMPOSES, instead of
// carrying the journal's state — the record, its home, its path and the once-per-root memo —
// as five fields and a mutex of its own.
//
// Safe for concurrent use. The label walk and Retire hold the lock for their whole duration,
// which is what serializes two goroutines confining at once, exactly as the backend's own
// mutex did before this type existed: a multi-second walk is one critical section here, as it
// was there.
//
// LOCK DISCIPLINE, written down because the one machine that can catch a slip is the one
// nothing here is developed on: an EXPORTED method takes j.mu; an unexported one assumes the
// caller already holds it and says so on its first doc line. No locked path may call an
// exported method — sync.Mutex is not reentrant, so a single slip is a self-deadlock that
// compiles clean, passes every gate on Linux and macOS, and hangs only on a real Windows run.
// Every exported method an internal caller also needs is therefore a two-line locking wrapper
// over the unexported body.
type Journal struct {
	mu sync.Mutex
	// home is the apogee home the label journals live under, "" when no user profile resolved.
	// It is kept beside path so teardown can read the SIBLING journals — the concurrently
	// running sessions whose live roots the revert must leave in place.
	home string
	// path is this process's journal file under home. "" means the session has nowhere to
	// write a journal, and therefore may label NOTHING at all (ADR 0020 §2).
	path string
	// rec is the in-memory twin of the file at path.
	rec Record
	// labelled records the folded box roots whose trees have already been labelled this
	// session, so the once-per-box cost is not paid once per command.
	labelled map[string]bool
	// revert is the OS seam Retire drives, fixed at Open from the build's osRevert. It is a
	// per-journal field rather than a package-level variable so a test can hand one journal a
	// stand-in without a global that two of them could race over or clobber for each other.
	// nil is the honest non-Windows answer — no facility labelled anything, so there is
	// nothing to put back — and Retire treats it as exactly that.
	revert revertFunc
}

// revertFunc is the shape of the revert Retire drives: given the journal's home and its own
// file, it returns the undo for one Record, handing back the entries it deliberately did not
// act on. The production implementation IS the Windows label walk (walk_windows.go), which is
// why the seam exists at all: it is what lets one Retire, declared once and unguarded, reach an
// OS-specific body without a build tag on the method itself.
type revertFunc func(home, own string) func(Record) ([]Entry, error)

// Open returns the label journal THIS process owns under home, touching the disk NOWHERE: it
// resolves the file's path without reading, writing or creating anything under it, so a report
// backend can construct one and still describe the journal directory rather than consume it.
//
// home may be "" — os.UserHomeDir failed, so there is no user profile to write a journal
// under, or a test deliberately withholds one. The journal is then not Writable, and the
// invariant that follows is ADR 0020 §2's: the one disk mutation apogee performs is only ever
// made against a record of how to undo it, so no journal means no label rather than an
// unrevertable one.
func Open(home string) *Journal {
	j := &Journal{labelled: make(map[string]bool), revert: osRevert()}
	if home == "" {
		return j
	}
	j.home = home
	j.path = JournalPath(home, os.Getpid())
	j.rec.PID = os.Getpid()
	return j
}

// Writable reports whether this journal has a file to write, which is what entitles its owner
// to label anything at all: no journal on the disk, no label on the disk (ADR 0020 §2). The
// backend asks BEFORE it resolves a box's roots, so a host with no user profile refuses the
// box outright rather than part-way through.
func (j *Journal) Writable() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.path != ""
}

// record folds one about-to-be-labelled path into the journal and persists it when the record
// changed. Assumes the caller holds j.mu.
//
// It reports whether the entry newly CHANGED the journal, which is what entitles the caller to
// unwind it should the label write that follows fail. Nothing is ever labelled without a
// journal on disk describing how to undo it, so a flush failure comes back as an error and the
// caller refuses the box; the error is plain — package platform wraps the confinement sentinel
// at its own call site.
func (j *Journal) record(entry Entry) (bool, error) {
	entries, changed := recordEntry(j.rec.Entries, entry, foldPath)
	j.rec.Entries = entries
	if !changed {
		return false, nil
	}
	if err := j.flush(); err != nil {
		return false, err
	}
	return true, nil
}

// unwind removes path's just-journalled entry after its label write failed — the phantom-entry
// undo, decided by unwindEntry (an entry recording a foreign prior stays). Assumes the caller
// holds j.mu.
//
// The re-flush is best-effort: the caller is already returning the label failure, which is the
// error that matters, and a stale on-disk entry costs at worst the pre-unwind behaviour —
// Retire rewrites or removes the whole file on success, and only a crash before that
// resurrects the phantom.
func (j *Journal) unwind(path string) { //nolint:unused // used by the windows build (walk_windows.go)
	entries, removed := unwindEntry(j.rec.Entries, path, foldPath)
	if !removed {
		return
	}
	j.rec.Entries = entries
	_ = j.flush()
}

// flush persists the in-memory record. Assumes the caller holds j.mu. A journal with no path
// writes nothing and reports no error: that is the belt on the backend's braces, which refuses
// a box before anything is journalled or labelled, so a flush with no file can no longer
// accompany a disk mutation.
func (j *Journal) flush() error {
	if j.path == "" {
		return nil
	}
	j.rec.PID = os.Getpid()
	return WriteJournal(j.path, j.rec)
}

// Entries returns a COPY of what the journal currently records. It exists for package
// platform's Windows-tagged lifecycle tests, which assert on what a label pass journalled
// while the backend is still open; the copy is what keeps such a reader from racing — or
// scribbling on — the pass that is still writing it.
func (j *Journal) Entries() []Entry {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]Entry(nil), j.rec.Entries...)
}

// PriorLabels returns the paths that carried an explicit label before the run, mapped to the
// descriptor teardown restores — a fresh map built under the lock, for the same readers and
// the same reason as Entries.
func (j *Journal) PriorLabels() map[string]string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.rec.PriorLabels()
}

// isLabelled reports whether root's tree has already been labelled this session. Assumes the
// caller holds j.mu — LabelTree asks at the top of its own critical section. The memo is keyed
// by the FOLDED path for the same reason the journal is: C:\Work and c:\work are one root, and
// labelling it twice would journal it twice.
func (j *Journal) isLabelled(root string) bool { return j.labelled[foldPath(root)] }

// markLabelled records that root's tree has been labelled, so the once-per-box cost is paid by
// the first confined command of a session and not by every one of them. Assumes the caller
// holds j.mu — LabelTree marks it at the end of the same critical section that walked it, so
// the memo can never claim a tree a concurrent walk has not finished.
func (j *Journal) markLabelled(root string) { j.labelled[foldPath(root)] = true }

// forgetLabelled drops the once-per-root memo, so a later pass walks those roots again.
// Assumes the caller holds j.mu. Retire calls it from inside its own critical section: the
// labels are off the disk, and a memo still claiming they are on it would leave the next
// Confine fenceless.
func (j *Journal) forgetLabelled() { j.labelled = make(map[string]bool) }

// ForgetLabelled is forgetLabelled with the lock taken. It exists for package platform's
// Windows-tagged test that re-walks an already-labelled tree (the self-perpetuating-residue
// scenario), which is the only caller that reopens the memo without a revert having emptied
// the disk first.
func (j *Journal) ForgetLabelled() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.forgetLabelled()
}

// Retire puts the disk back: every journalled root's tree is cleared of the mandatory label —
// minus any root a LIVE sibling session's journal still names (revertibleRoots), which stays
// fenced for that session — then the paths that carried an explicit label before the run get
// theirs back verbatim — minus any prior under a root a sibling journal still claims, which is
// handed off rather than restored into the sibling's live box and lost to its later clear
// (restorablePriors) — and the journal file is removed, but ONLY if nothing failed and nothing
// was handed off (the package-level retire). A failed revert keeps both the file and the
// in-memory record, so the labels it describes are still recoverable: the next NewConfiner
// retries them and Residue reports them meanwhile. A handoff is not a failure — the caller's
// Close returns nil — but the surviving entries stay in memory too, so a repeated Close
// converges instead of deleting the handoff record.
//
// The revert itself is reached through j.revert, the seam Open fixed from the build's
// osRevert: the production revert IS the label walk, which is Windows-tagged, and routing it
// through one seam is what lets this method be declared ONCE for every OS rather than as two
// build-tagged copies of one contract. It is handed this journal's home and its own file, so
// the sibling read and both exclusions happen at revert time, when liveness and the claim set
// are current, rather than at construction. A NIL seam is the non-Windows build's honest
// answer — no facility here labels anything, so there is nothing on the disk to put back — and
// it retires to a no-op rather than an error: the journal file, if a test made one, is left
// exactly as it is.
//
// The lock is held for the whole duration, and the memo is dropped through the UNEXPORTED
// forgetLabelled from inside that critical section — calling the exported wrapper here would
// deadlock on a mutex that is not reentrant, invisibly to every non-Windows gate.
func (j *Journal) Retire() error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.revert == nil {
		return nil
	}
	// The package-level retire is the retention rule over one journal FILE; this method is one
	// session's way in, and crash recovery drives the same rule over the files of runs that
	// never got here.
	remaining, err := retire(j.path, j.rec, j.revert(j.home, j.path))
	if err != nil {
		return fmt.Errorf("apogee: confine: could not revert every mandatory label; the journal %q is kept so the next run retries: %w",
			j.path, err)
	}
	j.rec = Record{Entries: remaining}
	j.forgetLabelled()
	return nil
}

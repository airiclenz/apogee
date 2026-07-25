package winlabel

import (
	"os"
	"testing"
)

// A journal with no home has no file, and every write through it is a no-op rather than an
// error: os.UserHomeDir failing is a routine host condition, and what must follow is the
// backend refusing to LABEL — not a crash, and above all not a label with no record of how to
// undo it (ADR 0020 §2).
func TestJournalWithoutAHomeIsNotWritableAndFlushesNothing(t *testing.T) {
	t.Parallel()

	j := Open("")
	if j.Writable() {
		t.Fatal("a journal opened with no home reports itself writable; the backend would then label a disk it cannot journal")
	}

	j.mu.Lock()
	flushErr := j.flush()
	changed, recordErr := j.record(Entry{Path: `C:\work`, Root: true})
	j.mu.Unlock()

	if flushErr != nil {
		t.Errorf("flush() = %v; a journal with nowhere to write reports no error — the refusal is the backend's, and it happens earlier", flushErr)
	}
	if recordErr != nil {
		t.Errorf("record() = %v; want no error, for the same reason", recordErr)
	}
	if !changed {
		t.Error("record() reported no change; the fold rules are unaffected by the missing file")
	}
}

// Entries hands out a COPY. The Windows lifecycle tests read it while a backend is still open,
// and a shared slice would let a reader scribble on the record teardown is about to revert.
func TestJournalEntriesReturnsACopy(t *testing.T) {
	t.Parallel()

	j := Open(t.TempDir())
	j.mu.Lock()
	if _, err := j.record(Entry{Path: `C:\work`, Root: true}); err != nil {
		j.mu.Unlock()
		t.Fatalf("record: %v", err)
	}
	j.mu.Unlock()

	got := j.Entries()
	if len(got) != 1 {
		t.Fatalf("Entries() = %+v, want the one journalled root", got)
	}
	got[0].Path = `C:\somewhere-else`
	got = append(got, Entry{Path: `D:\extra`, Root: true})

	after := j.Entries()
	if len(after) != 1 || after[0].Path != `C:\work` {
		t.Errorf("Entries() = %+v after a caller mutated its copy, want the journal untouched", after)
	}
}

// A record that changes nothing must not rewrite the file. The journal is flushed on every
// discovery during a walk of thousands of paths, and re-publishing an identical file per path
// would turn the once-per-box cost into a per-file one.
func TestJournalRecordThatChangesNothingDoesNotRewriteTheFile(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	j := Open(home)
	j.mu.Lock()
	if _, err := j.record(Entry{Path: `C:\work`, Root: true}); err != nil {
		j.mu.Unlock()
		t.Fatalf("record the root: %v", err)
	}
	j.mu.Unlock()

	path := JournalPath(home, os.Getpid())
	// A sentinel in place of the published journal: anything that rewrites the file replaces
	// it, so its survival is the proof — and it does not depend on a filesystem's mtime
	// resolution.
	const sentinel = "not the journal"
	if err := os.WriteFile(path, []byte(sentinel), 0o600); err != nil {
		t.Fatalf("plant the sentinel over %q: %v", path, err)
	}

	j.mu.Lock()
	changed, err := j.record(Entry{Path: `C:\WORK`, Root: true})
	j.mu.Unlock()
	if err != nil {
		t.Fatalf("re-record the same root: %v", err)
	}
	if changed {
		t.Error("re-recording the same root reported a change; one entry per path is the journal's rule")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q back: %v", path, err)
	}
	if string(raw) != sentinel {
		t.Errorf("the journal file was rewritten by a record that changed nothing: %s", raw)
	}
}

// ForgetLabelled reopens the once-per-root memo. Retire calls the unexported form after a
// successful revert — the labels are off the disk, so a memo still claiming they are on it
// would leave the next Confine fenceless.
func TestJournalForgetLabelledClearsTheMemo(t *testing.T) {
	t.Parallel()

	j := Open(t.TempDir())
	const root = `C:\work`
	j.mu.Lock()
	j.markLabelled(root)
	recorded, folded := j.isLabelled(root), j.isLabelled(`c:\WORK`)
	j.mu.Unlock()
	if !recorded {
		t.Fatal("the memo did not record a labelled root; the once-per-box pass would repeat")
	}
	// The memo is folded, like the journal: C:\Work and c:\work are one root.
	if !folded {
		t.Error("the memo missed a case-varied spelling of the same root; it would be labelled — and journalled — twice")
	}

	j.ForgetLabelled()
	j.mu.Lock()
	survived := j.isLabelled(root)
	j.mu.Unlock()
	if survived {
		t.Error("the memo survived ForgetLabelled; a reverted tree must be walked again")
	}
}

// Retire hands its remaining entries back and reopens the memo, and a failed revert surfaces
// as the wrapped keep-the-journal error. The revert itself is Windows-tagged, so it is planted
// on the journal's own seam here exactly as the retention rule's own tests inject it.
func TestJournalRetireKeepsTheHandoffAndReopensTheMemo(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	j := Open(home)
	j.mu.Lock()
	if _, err := j.record(Entry{Path: `C:\work`, Root: true}); err != nil {
		j.mu.Unlock()
		t.Fatalf("record the root: %v", err)
	}
	j.markLabelled(`C:\work`)
	j.mu.Unlock()

	handoff := []Entry{{Path: `C:\work\vendor`, PriorSDDL: "S:AI(ML;;NW;;;ME)"}}
	var gotHome, gotOwn string
	j.revert = func(home, own string) func(Record) ([]Entry, error) {
		gotHome, gotOwn = home, own
		return func(Record) ([]Entry, error) { return handoff, nil }
	}
	if err := j.Retire(); err != nil {
		t.Fatalf("Retire: %v", err)
	}

	if gotHome != home || gotOwn != JournalPath(home, os.Getpid()) {
		t.Errorf("the revert was built from home %q / own %q, want this journal's own home and file", gotHome, gotOwn)
	}
	if got := j.Entries(); len(got) != 1 || got[0].Path != handoff[0].Path {
		t.Errorf("entries after Retire = %+v, want the handed-off entry, so a repeated Close converges", got)
	}
	j.mu.Lock()
	memoSurvived := j.isLabelled(`C:\work`)
	j.mu.Unlock()
	if memoSurvived {
		t.Error("the memo survived Retire; the labels are off the disk and the next Confine must re-apply them")
	}
}

// A journal with NO revert seam retires to a no-op. That is the non-Windows build's state —
// osRevert supplies nil there, because no facility on such a host labels anything — and it must
// stay a no-op rather than an error or, far worse, a journal file deleted as though a revert
// had really happened. The seam is cleared explicitly rather than inferred from the build, so
// the case is covered on Windows too, where Open fills it in.
func TestJournalRetireWithoutARevertSeamDoesNothing(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	j := Open(home)
	j.mu.Lock()
	if _, err := j.record(Entry{Path: `C:\work`, Root: true}); err != nil {
		j.mu.Unlock()
		t.Fatalf("record the root: %v", err)
	}
	j.mu.Unlock()

	j.revert = nil
	if err := j.Retire(); err != nil {
		t.Fatalf("Retire with no revert seam = %v, want nil — there is no disk mutation to undo", err)
	}
	if got := j.Entries(); len(got) != 1 {
		t.Errorf("entries after Retire = %+v, want the record untouched: nothing was reverted", got)
	}
	if _, err := os.Stat(JournalPath(home, os.Getpid())); err != nil {
		t.Errorf("stat the journal file after Retire: %v; a revert that did not happen must not remove it", err)
	}
}

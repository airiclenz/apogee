//go:build windows

package winlabel

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// The Windows-tagged half of F-08's clear-side remediation. The rule itself (rootClearable)
// and the filter that applies it (revertibleRoots) are pure and tabled in retire_test.go, so
// they run on every OS; what needs a real SACL — and therefore this file — is the pre-clear
// pass that TAKES the root verdict: that it is taken once and skipped ever after, and that a
// root-only journal it cannot rewrite still clears the tree it always cleared.

// lowLabelledDir returns a temporary directory carrying apogee's own Low label — the state a
// root is in when a revert runs over its own journal. A host that will not let the test write
// a mandatory label to its own temp directory can prove nothing here, so the test skips rather
// than failing over a machine policy.
func lowLabelledDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if err := SetSDDL(dir, lowSDDL); err != nil {
		t.Skipf("cannot write a mandatory label to %q on this host: %v", dir, err)
	}
	return dir
}

func TestJudgePriorsSkipsAnAlreadyJudgedRoot(t *testing.T) {
	// A root verdict is taken ONCE, before the first clear, and every later revert reads the
	// persisted answer instead of the disk (Entry.RootJudged) — because after the clear the
	// disk says only what ClearTree wrote. The skip is observable through the journal file: a
	// pass that judged anything rewrites it, so an untouched file proves nothing was judged.
	root := lowLabelledDir(t)
	own := filepath.Join(t.TempDir(), "labels-4242.json")
	r := Record{PID: 4242, Entries: []Entry{{Path: root, Root: true, RootJudged: true}}}

	if err := judgePriors(r, own); err != nil {
		t.Fatalf("judgePriors over an already-judged root: %v", err)
	}
	if !r.Entries[0].RootJudged {
		t.Error("the persisted root verdict was dropped by a pass that should not have re-judged it")
	}
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Errorf("the journal at %q was rewritten; an already-judged root must be skipped, not re-judged", own)
	}
}

func TestJudgePriorsClearsARootOnlyJournalItCannotRewrite(t *testing.T) {
	// Persisting the root verdict must never become a PRECONDITION of clearing. A root-only
	// journal is the overwhelmingly common case and wrote nothing here before the verdict
	// existed, so an unwritable apogee home would otherwise strand every label on runs that
	// clear cleanly today. The write failure is swallowed, the verdict stays in memory, and the
	// revert goes on; only a PRIOR's verdict keeps the abort.
	root := lowLabelledDir(t)
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("plant the unwritable journal home: %v", err)
	}
	own := filepath.Join(blocker, "labels-4242.json")
	r := Record{PID: 4242, Entries: []Entry{{Path: root, Root: true}}}

	if err := judgePriors(r, own); err != nil {
		t.Fatalf("judgePriors aborted a root-only journal it could not rewrite: %v", err)
	}
	if !r.Entries[0].RootJudged {
		t.Error("the root verdict was not taken in memory, so the retry after a failed descendant would re-judge a cleared root")
	}
}

func TestJudgePriorsRewriteKeepsTheOwnersCreationTime(t *testing.T) {
	// The pre-clear rewrite persists a verdict about the SAME owner's journal, so it keeps the
	// owner's creation time along with its PID: dropping it would turn every judged journal
	// into a legacy "not recorded" owner, judged by a PID the OS may since have recycled.
	root := lowLabelledDir(t)
	own := filepath.Join(t.TempDir(), "labels-4242.json")
	r := Record{PID: 4242, Started: 133_000_000_000_000_002, Entries: []Entry{{Path: root, Root: true}}}

	if err := judgePriors(r, own); err != nil {
		t.Fatalf("judgePriors over an unjudged root: %v", err)
	}
	rewritten, err := ReadJournal(own)
	if err != nil {
		t.Fatalf("read the journal the root verdict rewrote: %v", err)
	}
	if rewritten.PID != r.PID || rewritten.Started != r.Started {
		t.Errorf("rewritten journal owner = PID %d Started %d, want PID %d Started %d",
			rewritten.PID, rewritten.Started, r.PID, r.Started)
	}
}

func TestJournalFlushStampsThisProcesssCreationTime(t *testing.T) {
	t.Parallel()

	// Every journal this process writes names its owner by PID AND creation time, so a later
	// reader can tell this process from a stranger that inherits its PID once it is gone.
	want, ok := processStarted(os.Getpid())
	if !ok || want == 0 {
		t.Fatalf("processStarted(self) = %d, %v; this process's own creation time must be readable", want, ok)
	}

	home := t.TempDir()
	j := Open(home)
	j.mu.Lock()
	_, err := j.record(Entry{Path: `C:\work`, Root: true})
	j.mu.Unlock()
	if err != nil {
		t.Fatalf("record the root: %v", err)
	}

	rec, err := ReadJournal(JournalPath(home, os.Getpid()))
	if err != nil {
		t.Fatalf("read the flushed journal: %v", err)
	}
	if rec.Started != want {
		t.Errorf("flushed journal Started = %d, want this process's creation time %d", rec.Started, want)
	}
}

// freshFileIdentity reads path's volume serial and file index through a handle of the test's
// own, independent of statHandle, so the comparison below checks the walk against the OS
// rather than against itself.
func freshFileIdentity(t *testing.T, path string) (uint32, uint64) {
	t.Helper()

	pathW, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("encode %q: %v", path, err)
	}
	handle, err := windows.CreateFile(pathW, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatalf("open %q: %v", path, err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		t.Fatalf("read the file information of %q: %v", path, err)
	}
	return info.VolumeSerialNumber, uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
}

// labelForTest runs LabelTree over root with j and clears the tree afterwards. A host that will
// not let the test write a mandatory label to its own temp directory can prove nothing here, so
// that is probed FIRST and skips; once the host can label, a LabelTree error is a failure —
// never a skip, which would hide exactly the refusal these tests exist to rule out.
func labelForTest(t *testing.T, root string, j *Journal) {
	t.Helper()

	lowLabelledDir(t) // skips when the host cannot label; the probe dir is not root
	t.Cleanup(func() { _ = ClearTree(root) })
	if err := LabelTree(root, j); err != nil {
		t.Fatalf("LabelTree(%q): %v", root, err)
	}
}

// rootEntry returns the journal's entry for root, failing the test when there is none.
func rootEntry(t *testing.T, j *Journal, root string) Entry {
	t.Helper()

	for _, entry := range j.Entries() {
		if entry.Root && strings.EqualFold(entry.Path, root) {
			return entry
		}
	}
	t.Fatalf("journal entries %+v name no root %q", j.Entries(), root)
	return Entry{}
}

func TestLabelTreeJournalsTheRootsFileIdentity(t *testing.T) {
	// The journal names the OBJECT the pass labelled, not only its path: the identity recorded
	// for the root is the one a fresh handle on that directory reports, and it reaches the
	// file on the disk as well as the in-memory record.
	root := t.TempDir()
	j := Open(t.TempDir())
	labelForTest(t, root, j)

	wantVolume, wantIndex := freshFileIdentity(t, root)
	if wantIndex == 0 {
		t.Skipf("the volume holding %q reports no file index; nothing to compare", root)
	}
	got := rootEntry(t, j, root)
	if got.Volume != wantVolume || got.FileIndex != wantIndex {
		t.Errorf("journalled identity = (vol %#x, fid %#x), want (vol %#x, fid %#x) from a fresh handle",
			got.Volume, got.FileIndex, wantVolume, wantIndex)
	}

	onDisk, err := ReadJournal(j.path)
	if err != nil {
		t.Fatalf("read the journal file: %v", err)
	}
	for _, entry := range onDisk.Entries {
		if entry.Root && strings.EqualFold(entry.Path, root) && (entry.Volume != wantVolume || entry.FileIndex != wantIndex) {
			t.Errorf("on-disk entry %+v does not carry the identity (vol %#x, fid %#x)", entry, wantVolume, wantIndex)
		}
	}
}

func TestLabelTreeLabelsARootWhoseIdentityCannotBeRead(t *testing.T) {
	// The identity is best-effort: a root whose handle read fails is journalled with NO
	// identity — judged later by the label-read rules, like an older journal's entry — and is
	// still labelled. A failed read never refuses the box.
	root := t.TempDir()
	child := filepath.Join(root, "child.txt")
	if err := os.WriteFile(child, []byte("x"), 0o600); err != nil {
		t.Fatalf("plant a descendant: %v", err)
	}
	j := Open(t.TempDir())
	j.stat = func(path string) (fileStat, error) {
		if strings.EqualFold(path, root) {
			return fileStat{links: 1, volume: 0xBAD, index: 0xBAD}, errors.New("planted identity read failure")
		}
		return statHandle(path)
	}
	labelForTest(t, root, j)

	got := rootEntry(t, j, root)
	if got.Volume != 0 || got.FileIndex != 0 {
		t.Errorf("root entry %+v carries an identity from a read that failed; want zero", got)
	}
	for _, path := range []string{root, child} {
		label, err := ReadSDDL(path)
		if err != nil {
			t.Fatalf("read the label of %q: %v", path, err)
		}
		if !IsLowLabel(label) {
			t.Errorf("label of %q = %q, want Low: a failed identity read must not stop the labelling", path, label)
		}
	}
}

// The identity half of the journal's trust (audit 2026-09-20, "a forgeable confinement
// journal"): Recover acts only on the object a journal entry labelled. These drive the real
// label APIs end to end — LabelTree journals, the object is deleted and recreated under the same
// name, and Recover runs over the journal this process wrote, which it recovers as an
// interrupted run (recoveryLiveness).

// foreignSDDL is a label apogee never writes: an explicit Medium label, the stand-in for another
// tool's work. foreignPriorSDDL is a second, distinct Medium label, so a restore of the one
// cannot be mistaken for the other still sitting on the path.
const (
	foreignSDDL      = "S:(ML;;NW;;;ME)"
	foreignPriorSDDL = "S:(ML;;NWNR;;;ME)"
)

// labelOrSkip writes sddl to path, skipping the test when the host will not let it: a machine
// policy is not a defect of the revert under test.
func labelOrSkip(t *testing.T, path, sddl string) {
	t.Helper()

	if err := SetSDDL(path, sddl); err != nil {
		t.Skipf("cannot write %q to %q on this host: %v", sddl, path, err)
	}
}

// mustReadLabel reads path's mandatory label, failing the test when it cannot.
func mustReadLabel(t *testing.T, path string) string {
	t.Helper()

	label, err := ReadSDDL(path)
	if err != nil {
		t.Fatalf("read the label of %q: %v", path, err)
	}
	return label
}

// ownJournal is the journal file this process writes under home (Open).
func ownJournal(home string) string { return JournalPath(home, os.Getpid()) }

// rewriteOwnJournal applies edit to every entry of this process's journal under home and writes
// it back — the state an older apogee, or a pass that persisted a verdict, leaves on the disk.
func rewriteOwnJournal(t *testing.T, home string, edit func(*Entry)) {
	t.Helper()

	r, err := ReadJournal(ownJournal(home))
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	for i := range r.Entries {
		edit(&r.Entries[i])
	}
	if err := WriteJournal(ownJournal(home), r); err != nil {
		t.Fatalf("rewrite the journal: %v", err)
	}
}

// requireIdentity skips when the entry journalled no identity — a volume that reports no file
// index — since there is then nothing for the revert to hold the object to.
func requireIdentity(t *testing.T, entry Entry) {
	t.Helper()

	if entry.Volume == 0 && entry.FileIndex == 0 {
		t.Skipf("the volume holding %q reports no file identity; nothing to hold the revert to", entry.Path)
	}
}

// recreate deletes path and creates a fresh object of the same kind under the same name,
// skipping when the volume hands the new object the old identity (it never should: NTFS bumps
// the sequence number of a reused record).
func recreate(t *testing.T, path string, dir bool, journalled Entry) {
	t.Helper()

	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("delete %q: %v", path, err)
	}
	var err error
	if dir {
		err = os.Mkdir(path, 0o700)
	} else {
		err = os.WriteFile(path, []byte("planted"), 0o600)
	}
	if err != nil {
		t.Fatalf("recreate %q: %v", path, err)
	}
	if vol, idx := freshFileIdentity(t, path); vol == journalled.Volume && idx == journalled.FileIndex {
		t.Skipf("the volume reissued the journalled identity to the new %q", path)
	}
}

func TestRecoverClearsOnlyTheRootObjectItJournalled(t *testing.T) {
	// A journalled root is cleared only while the object behind its path is the one the label
	// pass wrote to. A stand-in created under the same name is left exactly as it is — whether
	// it reads Low (so the label read vouches for it) or the journal carries a persisted verdict
	// that skips the read. The identity-less entry of an older journal has only the label read
	// to go on and still clears, as it always has.
	tests := []struct {
		name          string
		replace       bool
		standIn       string
		rootJudged    bool
		stripIdentity bool
		wantCleared   bool
	}{
		{name: "the_intact_root_is_cleared", wantCleared: true},
		{name: "a_stand_in_labelled_low_is_left_alone", replace: true, standIn: lowSDDL},
		{name: "a_persisted_verdict_does_not_clear_a_stand_in", replace: true, standIn: foreignSDDL, rootJudged: true},
		{name: "a_legacy_entry_is_judged_by_its_label_alone", replace: true, standIn: lowSDDL, stripIdentity: true, wantCleared: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "box")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatalf("make the box root: %v", err)
			}
			home := t.TempDir()
			j := Open(home)
			labelForTest(t, root, j)
			journalled := rootEntry(t, j, root)
			requireIdentity(t, journalled)

			rewriteOwnJournal(t, home, func(entry *Entry) {
				entry.RootJudged = entry.RootJudged || tt.rootJudged
				if tt.stripIdentity {
					entry.Volume, entry.FileIndex = 0, 0
				}
			})
			if tt.replace {
				recreate(t, root, true, journalled)
				labelOrSkip(t, root, tt.standIn)
			}
			before := mustReadLabel(t, root)

			Recover(home)

			after := mustReadLabel(t, root)
			if tt.wantCleared {
				if IsLowLabel(after) {
					t.Errorf("label of %q = %q after Recover, want it cleared", root, after)
				}
				return
			}
			if after != before {
				t.Errorf("label of %q = %q after Recover, want the stand-in's %q untouched", root, after, before)
			}
		})
	}
}

func TestRecoverCarriesAPriorWhoseObjectWasReplaced(t *testing.T) {
	// A journalled PRIOR is written back only onto the object the label pass took it from. A
	// file recreated under the same name — whether it now reads Low, as the revert's own label
	// would, or carries some other foreign label — gets nothing restored onto it, and the prior
	// is CARRIED: the journal survives holding it, unjudged, one carry spent. A prior whose path
	// has simply gone still drops, and the journal retires.
	tests := []struct {
		name      string
		standIn   string
		deleteOff bool
	}{
		{name: "a_stand_in_labelled_low_carries_the_prior", standIn: lowSDDL},
		{name: "a_stand_in_with_a_foreign_label_carries_the_prior", standIn: foreignSDDL},
		{name: "a_vanished_path_drops_the_prior", deleteOff: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "box")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatalf("make the box root: %v", err)
			}
			file := filepath.Join(root, "vendor.dll")
			if err := os.WriteFile(file, []byte("original"), 0o600); err != nil {
				t.Fatalf("plant the labelled file: %v", err)
			}
			labelOrSkip(t, file, foreignPriorSDDL)
			home := t.TempDir()
			j := Open(home)
			labelForTest(t, root, j)

			var journalled Entry
			for _, entry := range j.Entries() {
				if strings.EqualFold(entry.Path, file) && entry.PriorSDDL != "" {
					journalled = entry
				}
			}
			if journalled.Path == "" {
				t.Fatalf("journal entries %+v record no prior for %q", j.Entries(), file)
			}
			requireIdentity(t, journalled)

			if tt.deleteOff {
				if err := os.Remove(file); err != nil {
					t.Fatalf("delete %q: %v", file, err)
				}
			} else {
				recreate(t, file, false, journalled)
				labelOrSkip(t, file, tt.standIn)
			}

			Recover(home)

			if tt.deleteOff {
				if survivors := ListJournals(home); len(survivors) != 0 {
					t.Errorf("journals %v survived; a prior whose path is gone drops and the journal retires", survivors)
				}
				return
			}
			if label := mustReadLabel(t, file); label == journalled.PriorSDDL {
				t.Errorf("label of the stand-in %q = %q: the journalled prior was restored onto an object the label pass never touched", file, label)
			}
			kept, err := ReadJournal(ownJournal(home))
			if err != nil {
				t.Fatalf("the journal did not survive the carry: %v", err)
			}
			var carried *Entry
			for i := range kept.Entries {
				if strings.EqualFold(kept.Entries[i].Path, file) {
					carried = &kept.Entries[i]
				}
			}
			if carried == nil {
				t.Fatalf("kept journal %+v no longer carries the prior of %q", kept.Entries, file)
			}
			if carried.PriorSDDL != journalled.PriorSDDL || carried.Judged || carried.Carried != 1 {
				t.Errorf("carried entry = %+v; want prior %q, unjudged, carried once", *carried, journalled.PriorSDDL)
			}
			if carried.Volume != journalled.Volume || carried.FileIndex != journalled.FileIndex {
				t.Errorf("carried entry = %+v lost the identity of %+v", *carried, journalled)
			}
		})
	}
}

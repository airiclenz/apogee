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

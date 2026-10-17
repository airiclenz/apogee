//go:build !windows

package session

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// shareTestID is the session id every share-group test writes under.
const shareTestID = "20260601T120000Z-5ba7e000"

// The share-group modes (WithShareGroup) on a POSIX host: what a shared store writes, what it
// re-modes, and what it leaves alone. Unix-only because the group a file carries is read off
// syscall.Stat_t, which Windows has no counterpart of — and a share group is refused there anyway.

// shareTestGID answers a supplementary group of this process other than its effective gid, so a
// chown to it is observable; the test is skipped when the process has none.
func shareTestGID(t *testing.T) int {
	t.Helper()
	groups, err := os.Getgroups()
	if err != nil {
		t.Skipf("read this process's groups: %v", err)
	}
	for _, gid := range groups {
		if gid != os.Getegid() {
			return gid
		}
	}
	t.Skip("this process has no supplementary group to share with")
	return 0
}

// sharedStoreLayout is a store rooted at <home>/sessions, as every caller roots one, so the home a
// shared store re-modes is a directory the test owns.
func sharedStoreLayout(t *testing.T) (home, dir string) {
	t.Helper()
	home = t.TempDir()
	return home, filepath.Join(home, "sessions")
}

// shareTestRecord is the record every share-group test writes.
func shareTestRecord() Record {
	return sampleRecord(shareTestID, time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
}

// assertAccess fails unless path has mode perm and is owned by group gid.
func assertAccess(t *testing.T, path string, perm os.FileMode, gid int) {
	t.Helper()
	assertMode(t, path, perm)
	if got := fileGID(t, path); got != gid {
		t.Errorf("%s gid = %d, want %d", path, got, gid)
	}
}

// assertMode fails unless path has mode perm. The owner-private cases assert the mode alone: the
// group a fresh file is given is the platform's choice (the effective gid on Linux, the
// directory's on the BSDs), and with no group bit set it grants nothing.
func assertMode(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != perm {
		t.Errorf("%s mode = %v, want %v", path, got, perm)
	}
}

// fileGID answers the group that owns path.
func fileGID(t *testing.T, path string) int {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return int(info.Sys().(*syscall.Stat_t).Gid)
}

// writeAll saves rec, holds it and opens its journal — every kind of file the store writes —
// releasing the hold and closing the journal before it returns.
func writeAll(t *testing.T, st *Store, rec Record) {
	t.Helper()
	if err := st.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}
	release, err := st.Hold(rec.Meta.ID)
	if err != nil {
		t.Fatalf("Hold: %v", err)
	}
	defer func() { _ = release() }()
	journal, err := st.OpenJournal(rec.Meta.ID)
	if err != nil {
		t.Fatalf("OpenJournal: %v", err)
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("close journal: %v", err)
	}
}

func TestStoreShareGroup_Writes_AreGroupReadableAndGroupOwned(t *testing.T) {
	gid := shareTestGID(t)
	home, dir := sharedStoreLayout(t)
	st := NewStore(dir, WithShareGroup(gid))
	rec := shareTestRecord()

	writeAll(t, st, rec)

	id := rec.Meta.ID
	assertAccess(t, home, sharedHomePerm, gid)
	assertAccess(t, dir, sharedDirPerm, gid)
	assertAccess(t, filepath.Join(dir, id+".json"), sharedFilePerm, gid)
	assertAccess(t, st.JournalPath(id), sharedFilePerm, gid)
	assertAccess(t, st.lockPath(id), sharedFilePerm, gid)
}

func TestStoreShareGroup_TempFile_IsSharedBeforeItIsWritten(t *testing.T) {
	gid := shareTestGID(t)
	_, dir := sharedStoreLayout(t)
	st := NewStore(dir, WithShareGroup(gid))
	if err := st.ensureDir(); err != nil {
		t.Fatalf("ensureDir: %v", err)
	}
	var tempPath string
	var tempInfo os.FileInfo
	inspect := func(file *os.File) error {
		if err := st.setFileAccess(file); err != nil {
			return err
		}
		tempPath = file.Name()
		info, err := file.Stat()
		tempInfo = info
		return err
	}

	err := atomicWrite(filepath.Join(dir, "x.json"), []byte("{}"), inspect)

	if err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	if filepath.Dir(tempPath) != dir {
		t.Fatalf("temp file %q not in %q", tempPath, dir)
	}
	if got := tempInfo.Mode().Perm(); got != sharedFilePerm {
		t.Errorf("temp file mode = %v, want %v", got, sharedFilePerm)
	}
	if got := int(tempInfo.Sys().(*syscall.Stat_t).Gid); got != gid {
		t.Errorf("temp file gid = %d, want %d", got, gid)
	}
}

func TestStoreShareGroup_OwnerPrivateFiles_AreRemodedOnTheirNextWrite(t *testing.T) {
	gid := shareTestGID(t)
	home, dir := sharedStoreLayout(t)
	rec := shareTestRecord()
	id := rec.Meta.ID
	private := NewStore(dir)
	writeAll(t, private, rec)
	assertMode(t, private.lockPath(id), filePerm)
	assertMode(t, filepath.Join(dir, id+".json"), filePerm)

	shared := NewStore(dir, WithShareGroup(gid))
	writeAll(t, shared, rec)

	assertAccess(t, home, sharedHomePerm, gid)
	assertAccess(t, dir, sharedDirPerm, gid)
	assertAccess(t, filepath.Join(dir, id+".json"), sharedFilePerm, gid)
	assertAccess(t, shared.JournalPath(id), sharedFilePerm, gid)
	assertAccess(t, shared.lockPath(id), sharedFilePerm, gid)
}

func TestStoreShareGroup_Unset_KeepsEveryFileOwnerPrivate(t *testing.T) {
	_, dir := sharedStoreLayout(t)
	st := NewStore(dir)
	rec := shareTestRecord()

	writeAll(t, st, rec)

	id := rec.Meta.ID
	assertMode(t, dir, dirPerm)
	assertMode(t, filepath.Join(dir, id+".json"), filePerm)
	assertMode(t, st.JournalPath(id), filePerm)
	assertMode(t, st.lockPath(id), filePerm)
}

func TestStoreShareGroup_ScratchBesideTheStore_IsUntouched(t *testing.T) {
	gid := shareTestGID(t)
	home, dir := sharedStoreLayout(t)
	scratch := filepath.Join(home, "scratch")
	if err := os.Mkdir(scratch, dirPerm); err != nil {
		t.Fatalf("mkdir scratch: %v", err)
	}
	scratchGID := fileGID(t, scratch)

	writeAll(t, NewStore(dir, WithShareGroup(gid)), shareTestRecord())

	assertAccess(t, scratch, dirPerm, scratchGID)
}

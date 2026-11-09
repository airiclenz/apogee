//go:build windows

package session

import (
	"errors"
	"os"
	"slices"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// An owner releasing a session while a watcher follows its journal cannot remove the file — the
// watcher's os.Open, as followedJournal (internal/tui/watch.go) opens it, shares read and write
// but not delete — and RemoveJournal counts that sharing violation as success rather than wait on
// a watcher that is itself waiting for the hold to read free. The journal it leaves has a free
// lock, so Live does not list it, and the startup sweep removes it once the watcher has let go.
// The raw os.Remove error is logged before it is asserted, so the CI log names the errno this
// case really gives on Windows whether the run is green or red.
func TestWindowsRemoveJournalUnderAWatcher(t *testing.T) {
	t.Parallel()
	st, id := holdStore(t)
	release := liveJournal(t, st, id, "line\n")
	watcher, err := os.Open(st.JournalPath(id))
	if err != nil {
		t.Fatalf("open the journal as a watcher: %v", err)
	}
	// Registered after holdStore's t.TempDir, so it runs first: the temp dir goes only once the
	// watcher's handle is closed.
	t.Cleanup(func() { _ = watcher.Close() })

	removeErr := os.Remove(st.JournalPath(id))

	// errno stays 0 when the error carries no Errno; the assertion below then fails on its own.
	var errno syscall.Errno
	_ = errors.As(removeErr, &errno)
	t.Logf("RemoveJournal watcher case: os.Remove = %v (errno %d)", removeErr, uint(errno))
	if !errors.Is(removeErr, windows.ERROR_SHARING_VIOLATION) {
		t.Fatalf("os.Remove of a journal a watcher holds open = %v, want ERROR_SHARING_VIOLATION", removeErr)
	}

	if err := st.RemoveJournal(id); err != nil {
		t.Fatalf("RemoveJournal under a watcher = %v, want nil (the sweep collects the leftover)", err)
	}
	if !journalExists(t, st, id) {
		t.Fatal("the journal is gone, want it left behind while the watcher holds it open")
	}

	if err := release(); err != nil {
		t.Fatalf("release the hold: %v", err)
	}
	live, err := st.Live()
	if err != nil {
		t.Fatalf("Live: %v", err)
	}
	if slices.ContainsFunc(live, func(entry LiveEntry) bool { return entry.ID == id }) {
		t.Errorf("Live lists %s after its owner released it, want the leftover journal skipped", id)
	}

	removed, err := st.SweepJournals()
	if err == nil || removed != 0 {
		t.Errorf("SweepJournals while the watcher is open = (%d, %v), want (0, a removal error)", removed, err)
	}
	if !journalExists(t, st, id) {
		t.Fatal("the sweep removed a journal the watcher still holds open")
	}

	if err := watcher.Close(); err != nil {
		t.Fatalf("close the watcher: %v", err)
	}
	removed, err = st.SweepJournals()
	if err != nil || removed != 1 {
		t.Fatalf("SweepJournals once the watcher closed = (%d, %v), want (1, nil)", removed, err)
	}
	if journalExists(t, st, id) {
		t.Error("the sweep kept the leftover journal once the watcher closed")
	}
}

// A journal with the read-only attribute set is still removed: os.Remove clears the attribute and
// retries itself, so isRemoveSharingError accepting ERROR_SHARING_VIOLATION alone leaves no
// read-only journal behind.
func TestWindowsRemoveJournalOfAReadOnlyJournal(t *testing.T) {
	t.Parallel()
	st, id := holdStore(t)
	liveJournal(t, st, id, "line\n")
	const readOnlyPerm os.FileMode = 0o444
	if err := os.Chmod(st.JournalPath(id), readOnlyPerm); err != nil {
		t.Fatalf("set the journal read-only: %v", err)
	}

	err := st.RemoveJournal(id)

	if err != nil {
		t.Fatalf("RemoveJournal of a read-only journal = %v, want nil", err)
	}
	if journalExists(t, st, id) {
		t.Error("RemoveJournal left the read-only journal behind")
	}
}

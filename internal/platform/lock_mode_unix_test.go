//go:build !windows

package platform

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// AcquireLockWaitMode's mode and group on a POSIX host. Unix-only because the group is read off
// syscall.Stat_t, and Windows files carry no POSIX group to set.

// modeTestPerm is the mode the tests ask AcquireLockWaitMode for: group-readable, unlike the
// lockFilePerm every other acquire leaves.
const modeTestPerm os.FileMode = 0o640

func TestAcquireLockWaitMode_ExistingOwnerOnlyLock_IsRemodedAndHandedToTheGroup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.lock")
	if err := os.WriteFile(path, nil, lockFilePerm); err != nil {
		t.Fatalf("seed lock file: %v", err)
	}
	gid := os.Getegid()

	release, err := AcquireLockWaitMode(path, 0, modeTestPerm, gid)

	if err != nil {
		t.Fatalf("AcquireLockWaitMode: %v", err)
	}
	defer release()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != modeTestPerm {
		t.Errorf("mode = %v, want %v", got, modeTestPerm)
	}
	if got := int(info.Sys().(*syscall.Stat_t).Gid); got != gid {
		t.Errorf("gid = %d, want %d", got, gid)
	}
}

func TestAcquireLockWaitMode_HeldLock_IsRefusedAndLeftAsItIs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.lock")
	holder, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	defer holder()

	release, err := AcquireLockWaitMode(path, 0, modeTestPerm, -1)

	if err == nil {
		release()
		t.Fatal("AcquireLockWaitMode took a held lock")
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("stat: %v", statErr)
	}
	if got := info.Mode().Perm(); got != lockFilePerm {
		t.Errorf("a refused acquire changed the holder's mode to %v, want %v", got, lockFilePerm)
	}
}

func TestAcquireLock_ExistingLock_KeepsItsMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.lock")
	if err := os.WriteFile(path, nil, modeTestPerm); err != nil {
		t.Fatalf("seed lock file: %v", err)
	}
	if err := os.Chmod(path, modeTestPerm); err != nil {
		t.Fatalf("chmod seed: %v", err)
	}

	release, err := AcquireLock(path)

	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	defer release()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != modeTestPerm {
		t.Errorf("mode = %v, want the file's own %v", got, modeTestPerm)
	}
}

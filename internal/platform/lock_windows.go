//go:build windows

package platform

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// The Windows half of the advisory single-instance lock (lock.go): LockFileEx over a one-byte
// region far past anything the file will ever hold.
//
// Windows byte-range locks are MANDATORY, not advisory: a range another handle holds
// exclusively cannot even be READ. Locking the file's contents would therefore hide the PID
// inside it from the one process that wants it — the loser of the race, which is what prints
// "already running (pid N)" — and from a human running `type schedules.lock`. So the lock sits
// at lockRegionOffset, a region beyond end-of-file (locking past EOF is legal and does not grow
// the file), and byte 0 onwards stays plainly readable.
//
// Like flock on POSIX, the lock is keyed to the HANDLE, not the process: a second handle on the
// same path conflicts even inside one process, which is what makes lock_test.go's in-process
// conflict a real one here too.

const (
	// lockRegionOffset is where the locked byte sits: 1 TiB in, unreachably far past the few
	// bytes of PID this file ever holds, so the lock and the diagnostic never overlap.
	lockRegionOffset = 1 << 40
	// lockRegionLength is one byte — the region only has to exist for two handles to contend
	// over it.
	lockRegionLength = 1
)

// lockRegion is the OVERLAPPED naming the locked byte. Both LockFileEx and UnlockFileEx must be
// handed the same offset, so it is built in one place rather than spelled twice.
func lockRegion() windows.Overlapped {
	return windows.Overlapped{
		Offset:     uint32(lockRegionOffset & 0xFFFFFFFF),
		OffsetHigh: uint32(lockRegionOffset >> 32),
	}
}

// lockFile takes the exclusive byte-range lock on file's handle without blocking, reporting
// errLockHeld when the lock is already somebody else's. LOCKFILE_FAIL_IMMEDIATELY is what turns
// the wait into ERROR_LOCK_VIOLATION, which is Windows' "somebody has it".
func lockFile(file *os.File) error {
	overlapped := lockRegion()
	err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		lockRegionLength,
		0,
		&overlapped,
	)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return errLockHeld
	}
	return err
}

// unlockFile drops the byte-range lock. Windows releases a closed handle's locks eventually
// rather than promptly, so the explicit call is what makes release deterministic — a restart
// right after a clean shutdown must never lose the race to its own predecessor.
func unlockFile(file *os.File) error {
	overlapped := lockRegion()
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, lockRegionLength, 0, &overlapped)
}

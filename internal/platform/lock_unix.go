//go:build !windows

package platform

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// The POSIX half of the advisory single-instance lock (lock.go): flock(2).
//
// flock is the right one of the two POSIX file locks here. Its lock belongs to the OPEN FILE
// DESCRIPTION, so it is exactly as long-lived as the descriptor AcquireLock keeps, and closing
// some OTHER descriptor on the same path cannot touch it. fcntl(2) record locks are owned by
// the PROCESS and are dropped the moment ANY descriptor on the file is closed — any library
// that happened to open and close the lock path would silently disarm the daemon's guard.
//
// Owning the description rather than the process is also what makes the in-process conflict in
// lock_test.go a real one: a second open() of the same path is a second description, and flock
// refuses it exactly as it refuses another process.
//
// The lock is purely advisory on POSIX — it constrains nothing but other flock callers — which
// is all this needs and is why the PID inside the file stays readable to the refused caller.

// lockFile takes the exclusive flock on file's open file description without blocking,
// reporting errLockHeld when the lock is already somebody else's.
func lockFile(file *os.File) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) {
		return errLockHeld
	}
	return err
}

// unlockFile drops the flock. Closing the descriptor would drop it too; doing it explicitly
// keeps the release path identical on both platforms, where Windows genuinely needs the
// explicit call.
func unlockFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

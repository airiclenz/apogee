package platform

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
)

// The advisory single-instance lock (ADR 0034 decision 7): the mechanism by which a second
// `apogee daemon` refuses to start while a first one is running — two daemons watching one
// schedules file would double-fire every schedule.
//
// The refusal is an OS lock held on an open descriptor for the holder's whole lifetime, NOT a
// PID file the newcomer reads and judges. That is the whole design: a lock the kernel owns is
// dropped BY the kernel when the holder dies — clean exit, crash, SIGKILL, power loss alike —
// so there is no stale state to detect, no liveness probe to get wrong, and no window in which
// a dead daemon's leftovers keep the live one out. Nothing here ever asks whether a PID is
// alive, because the lock already answered.
//
// The PID written into the file is therefore diagnostics only — the number the loser prints
// and a human `cat`s — and is never consulted as a fact. It can be absent, stale or garbage
// without changing a single decision this file makes.
//
// The per-OS halves are lock_unix.go (flock) and lock_windows.go (LockFileEx). They share this
// file's shape: a non-blocking exclusive lock on the descriptor, [errLockHeld] when the lock is
// somebody else's, and the raw error when the call failed for any other reason.

const (
	// lockFilePerm keeps the lock file owner-only: it is machine-local coordination state in
	// the user's ~/.apogee, and nothing but the owning user has business holding it.
	lockFilePerm = 0o600

	// pidReadLimit caps what the PID read looks at. A PID is a handful of digits; anything
	// longer is not one, and reading past that would only give a corrupt file a way to make a
	// diagnostic expensive.
	pidReadLimit = 32
)

// errLockHeld is the platform-neutral signal each per-OS lockFile returns when the OS says the
// lock is already somebody else's, rather than that the call failed. It is unexported because
// it never leaves this file: [AcquireLock] turns it into the [LockHeldError] a caller reads.
var errLockHeld = errors.New("apogee: lock: held by another process")

// LockHeldError is [AcquireLock]'s refusal when another live process already holds the lock.
//
// It is a type rather than a sentinel because the line the caller prints needs the two facts
// inside it — which file is held, and who the holder said it was — so `apogee daemon` can say
// "apogee daemon is already running (pid N)" by reading fields instead of parsing a message.
type LockHeldError struct {
	// Path is the lock file another process holds.
	Path string
	// PID is the process id the holder recorded in that file, or 0 when it carried no readable
	// one. Diagnostics only: it is never probed for liveness and never acted on — the lock, not
	// the number, is what says the holder is alive.
	PID int
}

// Error renders the refusal as one line naming the path, and the holder's PID when the file
// carried a readable one.
func (e *LockHeldError) Error() string {
	if e.PID <= 0 {
		return fmt.Sprintf("apogee: lock %s is held by another process", e.Path)
	}
	return fmt.Sprintf("apogee: lock %s is held by another process (pid %d)", e.Path, e.PID)
}

// AcquireLock takes the exclusive advisory OS lock on path and holds it until the returned
// release is called. It never blocks: a lock held elsewhere is refused immediately.
//
// The file is created when absent and is NEVER removed, not even on release — unlinking it
// would let the next caller lock a deleted inode while another process still held the live one,
// which is precisely the double-fire this lock exists to prevent. On success the caller's PID
// is written into it, replacing whatever a previous holder left, so `cat` answers "who has it".
//
// Errors: a *[LockHeldError] when another process holds the lock; a wrapped error naming the
// path when the file could not be opened or the lock call failed for any other reason. release
// is nil on every error. On success it is safe to call more than once — the second and later
// calls do nothing — so a caller can defer it and still release early.
func AcquireLock(path string) (release func(), err error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, lockFilePerm)
	if err != nil {
		return nil, fmt.Errorf("apogee: lock %s: open: %w", path, err)
	}

	if err := lockFile(file); err != nil {
		// Read the holder's PID while the descriptor is still open: it is the only diagnostic
		// the refusal can carry, and failing to read it is not a failure to refuse.
		held := errors.Is(err, errLockHeld)
		pid := 0
		if held {
			pid = readLockPID(file)
		}
		file.Close()
		if held {
			return nil, &LockHeldError{Path: path, PID: pid}
		}
		return nil, fmt.Errorf("apogee: lock %s: %w", path, err)
	}

	if err := writeLockPID(file); err != nil {
		releaseLock(file)
		return nil, fmt.Errorf("apogee: lock %s: record the pid: %w", path, err)
	}

	var once sync.Once
	return func() { once.Do(func() { releaseLock(file) }) }, nil
}

// releaseLock drops the lock and closes the descriptor. Both failures are unreported by
// design: the signature the caller holds is a bare func(), release runs on the way out of a
// process that is finishing anyway, and neither failure leaves anything for a caller to
// repair — process exit drops the lock regardless.
func releaseLock(file *os.File) {
	_ = unlockFile(file)
	_ = file.Close()
}

// writeLockPID records the current process's PID in the held lock file, truncating first so a
// previous holder's longer number cannot leave trailing digits behind. Diagnostics only (see
// the file comment); it is written after the lock is taken, never before, so a refused caller
// can never overwrite the real holder's number.
func writeLockPID(file *os.File) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		return err
	}
	return nil
}

// readLockPID reports the PID recorded in the lock file, or 0 when it holds no readable one.
// Every failure — a short read, an empty file, junk where digits belong — collapses to 0: the
// refusal it decorates stands either way, so there is nothing here worth failing over.
func readLockPID(file *os.File) int {
	buf := make([]byte, pidReadLimit)
	n, err := file.ReadAt(buf, 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

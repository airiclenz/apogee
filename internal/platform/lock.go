package platform

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
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
// somebody else's, and the raw error when the call failed for any other reason — and the same
// shape for [ProbeLock]'s non-blocking SHARED lock on a read-only descriptor.

const (
	// lockFilePerm keeps the lock file owner-only: it is machine-local coordination state in
	// the user's ~/.apogee, and nothing but the owning user has business holding it. The one
	// widening is the caller's own, explicit one ([AcquireLockWaitMode]).
	lockFilePerm = 0o600

	// pidReadLimit caps what the PID read looks at. A PID is a handful of digits; anything
	// longer is not one, and reading past that would only give a corrupt file a way to make a
	// diagnostic expensive.
	pidReadLimit = 32

	// lockRetryInterval is how long [AcquireLockWait] sleeps between two non-blocking attempts.
	// A holder here keeps the lock for the few milliseconds of a file rewrite, so a short poll
	// hands the lock over promptly without spinning a core while it waits.
	lockRetryInterval = 10 * time.Millisecond
)

// errLockHeld is the platform-neutral signal each per-OS lockFile returns when the OS says the
// lock is already somebody else's, rather than that the call failed. It is unexported because
// it never leaves this file: [AcquireLock] turns it into the [LockHeldError] a caller reads.
var errLockHeld = errors.New("apogee: lock: held by another process")

// LockHeldError is [AcquireLock]'s refusal when another live process already holds the lock,
// and [AcquireLockWait]'s when it still holds it once the wait has run out.
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
// which is precisely the double-fire this lock exists to prevent. The one stated exception is a
// session record's lock (internal/session, Store.Delete): it follows its record, unlinked only
// after the record itself is gone and the hold released, and the race is benign there because
// whoever locks the unlinked inode is holding a session that no longer exists. On success the
// caller's PID is written into it, replacing whatever a previous holder left, so `cat` answers
// "who has it".
//
// Errors: a *[LockHeldError] when another process holds the lock; a wrapped error naming the
// path when the file could not be opened or the lock call failed for any other reason. release
// is nil on every error. On success it is safe to call more than once — the second and later
// calls do nothing — so a caller can defer it and still release early.
func AcquireLock(path string) (release func(), err error) {
	return acquireLock(path, 0, nil)
}

// AcquireLockWait is [AcquireLock] for a caller that would rather queue than be refused: when
// another holder has the lock it retries every [lockRetryInterval] until the lock is free or
// timeout has elapsed. It is for short, bounded critical sections — a file rewrite two
// processes must not interleave — never for a lifetime lock like the daemon's, whose second
// instance must be told at once that it is not wanted.
//
// A timeout of zero or less makes exactly one attempt, like [AcquireLock]. The file, the PID it
// records and the release it returns behave exactly as [AcquireLock] documents.
//
// Errors: a *[LockHeldError] when the lock is still held elsewhere once timeout has elapsed; a
// wrapped error naming the path, returned at once without waiting, when the file could not be
// opened or the lock call failed for any reason other than contention. release is nil on every
// error.
func AcquireLockWait(path string, timeout time.Duration) (release func(), err error) {
	return acquireLock(path, timeout, nil)
}

// AcquireLockWaitMode is [AcquireLockWait] for a lock file whose mode and group the caller owns
// rather than this file: once the lock is taken, the file is set to perm and — when gid is 0 or
// more — handed to group gid, explicitly on the held descriptor (fchmod, fchown), never by the
// create mode alone. The create mode is filtered by the umask and applies only to a file the
// open creates, so a lock file an earlier run left owner-private is re-moded here on its next
// acquire. A gid below zero leaves the file's group as it is.
//
// It is for a lock other users may need to open read-only — a session's hold, which a watcher in
// the share group probes with [ProbeLock]. The price of that read access is stated, not hidden:
// anyone who can open the file can take the SHARED lock a probe takes and keep it, so a group
// member can hold off an [AcquireLockWaitMode] for as long as it likes, and is refused only once
// the timeout runs out.
//
// The mode and group are applied after the lock is taken, never before, for the reason the PID
// is written then: a refused caller must not touch the real holder's file. Windows files carry
// no POSIX group, so a gid of 0 or more fails the acquire there (os.File.Chown).
//
// Errors: [AcquireLockWait]'s, plus a wrapped error naming the path when the mode or the group
// could not be set — the lock is released again then, and release is nil.
func AcquireLockWaitMode(path string, timeout time.Duration, perm os.FileMode, gid int) (release func(), err error) {
	return acquireLock(path, timeout, &lockAccess{perm: perm, gid: gid})
}

// lockAccess is the mode and group [AcquireLockWaitMode] sets on a lock file it holds; a nil one,
// [AcquireLock]'s and [AcquireLockWait]'s, leaves the file as the open made it.
type lockAccess struct {
	perm os.FileMode
	gid  int
}

// apply sets the mode and, for a gid of 0 or more, the group on the held lock file. The group goes
// first: a chown by a non-root caller may clear mode bits, so the mode is the last word.
func (a *lockAccess) apply(file *os.File) error {
	if a.gid >= 0 {
		if err := file.Chown(-1, a.gid); err != nil {
			return fmt.Errorf("set the group: %w", err)
		}
	}
	if err := file.Chmod(a.perm); err != nil {
		return fmt.Errorf("set the mode: %w", err)
	}
	return nil
}

// acquireLock is the one body behind [AcquireLock], [AcquireLockWait] and [AcquireLockWaitMode]:
// open the file, take the lock (retrying through contention until timeout has elapsed), set the
// mode and group access asks for (nil: none), record the PID. The descriptor stays open across the
// retries, so each attempt contends with the holder exactly as the first one did.
func acquireLock(path string, timeout time.Duration, access *lockAccess) (release func(), err error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, lockFilePerm)
	if err != nil {
		return nil, fmt.Errorf("apogee: lock %s: open: %w", path, err)
	}

	if err := lockFileWithin(file, timeout); err != nil {
		// Read the holder's PID while the descriptor is still open: it is the only diagnostic
		// the refusal can carry, and failing to read it is not a failure to refuse.
		held := errors.Is(err, errLockHeld)
		pid := 0
		if held {
			pid = readLockPID(file)
		}
		_ = file.Close()
		if held {
			return nil, &LockHeldError{Path: path, PID: pid}
		}
		return nil, fmt.Errorf("apogee: lock %s: %w", path, err)
	}

	if access != nil {
		if err := access.apply(file); err != nil {
			releaseLock(file)
			return nil, fmt.Errorf("apogee: lock %s: %w", path, err)
		}
	}

	if err := writeLockPID(file); err != nil {
		releaseLock(file)
		return nil, fmt.Errorf("apogee: lock %s: record the pid: %w", path, err)
	}

	var once sync.Once
	return func() { once.Do(func() { releaseLock(file) }) }, nil
}

// ProbeLock reports whether another descriptor currently holds the lock on path, without ever
// taking it for longer than the probe itself and without touching the file: it opens path
// READ-ONLY — never creating, writing or truncating it, so a lock file's bytes and mtime survive
// any number of probes, and a file the prober may only read is probeable — takes a SHARED,
// non-blocking lock, and drops it again at once. Shared, because the probe states its intent:
// it conflicts with an exclusive holder, which is the only thing it asks about, and two probes
// never refuse each other.
//
// It is for a reader that must never become a holder — a watcher asking whether a session's
// owner is still running. held is true when an [AcquireLock] holder has the lock, and pid is then
// the number that holder recorded (0 when unreadable; diagnostics only, exactly as
// [LockHeldError.PID]). A lock file that does not exist is held by no one: (false, 0, nil).
//
// The probe is a real, if momentary, lock: an [AcquireLock] that lands inside it is refused as if
// held. A caller that takes holds where probes may run absorbs that with [AcquireLockWait] and a
// timeout of a few milliseconds.
//
// Errors: a wrapped error naming the path when the file exists but could not be opened, or the
// lock call failed for any reason other than contention.
func ProbeLock(path string) (held bool, pid int, err error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, 0, nil
		}
		return false, 0, fmt.Errorf("apogee: probe lock %s: open: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	switch err := probeLockFile(file); {
	case errors.Is(err, errLockHeld):
		return true, readLockPID(file), nil
	case err != nil:
		return false, 0, fmt.Errorf("apogee: probe lock %s: %w", path, err)
	}
	if err := unlockFile(file); err != nil {
		return false, 0, fmt.Errorf("apogee: probe lock %s: unlock: %w", path, err)
	}
	return false, 0, nil
}

// lockFileWithin takes the lock on file, retrying every [lockRetryInterval] while it is held
// elsewhere and timeout has not yet elapsed. It returns [errLockHeld] once the time is up, and
// any other lockFile failure at once — only contention is worth waiting out.
func lockFileWithin(file *os.File, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := lockFile(file)
		if !errors.Is(err, errLockHeld) || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(min(lockRetryInterval, time.Until(deadline)))
	}
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

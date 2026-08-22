package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The lock tests run entirely in this process, on both supported platforms, because the
// conflict a second AcquireLock creates here is the real one: flock keys on the open file
// description and LockFileEx keys on the handle, so a second open() of the same path contends
// with the first exactly as another process would (lock_unix.go, lock_windows.go). That is why
// there is no helper-subprocess scaffolding below — it would test the same kernel behaviour
// with more moving parts.

// lockPath returns a lock file path inside a temp directory of the test's own, so every test
// contends only with itself and the set can run in parallel.
func lockPath(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "daemon.lock")
}

// acquire takes the lock, failing the test when it is refused, and registers release so a
// failing assertion cannot leave the lock held for the rest of the run.
func acquire(t *testing.T, path string) func() {
	t.Helper()

	release, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock(%s) = %v, want the lock", path, err)
	}
	t.Cleanup(release)
	return release
}

func TestAcquireLockRefusesASecondHolder(t *testing.T) {
	t.Parallel()

	path := lockPath(t)
	acquire(t, path)

	release, err := AcquireLock(path)

	if release != nil {
		t.Errorf("AcquireLock returned a release for a refused lock, want nil")
	}
	var held *LockHeldError
	if !errors.As(err, &held) {
		t.Fatalf("AcquireLock on a held lock = %v, want a *LockHeldError", err)
	}
	if held.Path != path {
		t.Errorf("LockHeldError.Path = %q, want %q", held.Path, path)
	}
	if held.PID != os.Getpid() {
		t.Errorf("LockHeldError.PID = %d, want the holder's pid %d", held.PID, os.Getpid())
	}
	message := held.Error()
	if !strings.Contains(message, path) || !strings.Contains(message, strconv.Itoa(os.Getpid())) {
		t.Errorf("LockHeldError.Error() = %q, want it to name the path and the holder's pid", message)
	}
}

func TestAcquireLockReleaseThenAcquireAgain(t *testing.T) {
	t.Parallel()

	path := lockPath(t)
	release := acquire(t, path)

	release()

	second, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock after release = %v, want the lock", err)
	}
	second()
}

func TestAcquireLockReleaseIsIdempotent(t *testing.T) {
	t.Parallel()

	path := lockPath(t)
	release := acquire(t, path)

	release()
	release()
	release()

	// The repeated releases must have dropped the lock exactly once and left nothing broken
	// behind: the only observable proof is that the lock is takeable again.
	second, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock after three releases = %v, want the lock", err)
	}
	second()
}

func TestAcquireLockRecordsThePIDAndKeepsTheFile(t *testing.T) {
	t.Parallel()

	path := lockPath(t)
	release := acquire(t, path)

	// The PID is readable WHILE the lock is held — that is the whole point of writing it, and
	// on Windows it is what the lock region's placement past EOF buys.
	recorded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the held lock file: %v", err)
	}
	if got := strings.TrimSpace(string(recorded)); got != strconv.Itoa(os.Getpid()) {
		t.Errorf("lock file holds %q, want the holder's pid %d", got, os.Getpid())
	}

	release()

	// Release drops the lock and keeps the file: unlinking it would let the next caller lock a
	// deleted inode while another process still held the live one.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("stat the lock file after release = %v, want it to still exist", err)
	}
}

func TestAcquireLockReportsAnUnusablePath(t *testing.T) {
	t.Parallel()

	// A lock path whose parent directory does not exist cannot be opened, and that is a plain
	// failure — NOT the "somebody else has it" refusal a caller turns into "already running".
	path := filepath.Join(t.TempDir(), "absent", "daemon.lock")

	release, err := AcquireLock(path)

	if release != nil {
		t.Errorf("AcquireLock returned a release for an unusable path, want nil")
	}
	if err == nil {
		t.Fatal("AcquireLock on an unusable path = nil, want an error")
	}
	var held *LockHeldError
	if errors.As(err, &held) {
		t.Errorf("AcquireLock on an unusable path = %v, want a plain error, not a *LockHeldError", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("AcquireLock error = %q, want it to name the path", err)
	}
}

func TestAcquireLockErrorRendersAnUnknownPID(t *testing.T) {
	t.Parallel()

	// The PID is best-effort diagnostics: a lock file that carries no readable one still yields
	// a refusal, just without the parenthetical.
	tests := []struct {
		name string
		pid  int
		want string
	}{
		{name: "known", pid: 4321, want: "apogee: lock /tmp/daemon.lock is held by another process (pid 4321)"},
		{name: "unreadable", pid: 0, want: "apogee: lock /tmp/daemon.lock is held by another process"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := &LockHeldError{Path: "/tmp/daemon.lock", PID: tt.pid}

			if got := err.Error(); got != tt.want {
				t.Errorf("LockHeldError.Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

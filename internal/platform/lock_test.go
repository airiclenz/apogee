package platform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/testtime"
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

func TestAcquireLockWaitTakesTheLockOnceTheHolderReleases(t *testing.T) {
	t.Parallel()

	path := lockPath(t)
	release := acquire(t, path)
	go func() {
		time.Sleep(testtime.Fixed(50 * time.Millisecond))
		release()
	}()

	second, err := AcquireLockWait(path, testtime.Budget(10*time.Second))

	if err != nil {
		t.Fatalf("AcquireLockWait while the holder releases = %v, want the lock", err)
	}
	second()
}

func TestAcquireLockWaitRefusesAHolderThatOutlastsTheTimeout(t *testing.T) {
	t.Parallel()

	// Zero is the documented single attempt; the positive timeout must actually be waited out
	// before the refusal, not cut short at the first contended attempt.
	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{name: "single attempt", timeout: 0},
		{name: "waited out", timeout: 60 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := lockPath(t)
			acquire(t, path)
			started := time.Now()

			release, err := AcquireLockWait(path, tt.timeout)

			elapsed := time.Since(started)
			if release != nil {
				t.Errorf("AcquireLockWait returned a release for a refused lock, want nil")
			}
			var held *LockHeldError
			if !errors.As(err, &held) {
				t.Fatalf("AcquireLockWait on a held lock = %v, want a *LockHeldError", err)
			}
			if held.Path != path || held.PID != os.Getpid() {
				t.Errorf("LockHeldError = %+v, want path %q and the holder's pid %d", held, path, os.Getpid())
			}
			if elapsed < tt.timeout {
				t.Errorf("AcquireLockWait refused after %v, want it to wait the full %v", elapsed, tt.timeout)
			}
		})
	}
}

func TestAcquireLockWaitReportsAnUnusablePathWithoutWaiting(t *testing.T) {
	t.Parallel()

	// Only contention is worth waiting out: a path that cannot be opened fails at once, as a
	// plain error, however long the caller was prepared to wait.
	const patience = time.Minute
	path := filepath.Join(t.TempDir(), "absent", "config.yaml.lock")
	started := time.Now()

	release, err := AcquireLockWait(path, patience)

	if release != nil {
		t.Errorf("AcquireLockWait returned a release for an unusable path, want nil")
	}
	var held *LockHeldError
	if err == nil || errors.As(err, &held) {
		t.Fatalf("AcquireLockWait on an unusable path = %v, want a plain error", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("AcquireLockWait error = %q, want it to name the path", err)
	}
	if elapsed := time.Since(started); elapsed >= patience {
		t.Errorf("AcquireLockWait took %v on an unusable path, want an immediate failure", elapsed)
	}
}

// The probe sees an AcquireLock holder and its recorded pid, and leaves the lock file exactly as
// it found it — bytes and mtime alike — since a watcher must never be a writer.
func TestProbeLockSeesAHolderAndLeavesTheFileUntouched(t *testing.T) {
	t.Parallel()

	path := lockPath(t)
	acquire(t, path)
	// Back-date the mtime so a write the probe made would show as a fresh stamp rather than one
	// lost inside the filesystem's timestamp granularity.
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatalf("back-date the lock file: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the held lock file: %v", err)
	}

	held, pid, err := ProbeLock(path)

	if err != nil || !held || pid != os.Getpid() {
		t.Fatalf("ProbeLock on a held lock = (%v, %d, %v), want (true, %d, nil)", held, pid, err, os.Getpid())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read the lock file: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("lock file after the probe = %q, want it unchanged from %q", after, before)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the lock file: %v", err)
	}
	if !info.ModTime().Equal(past) {
		t.Errorf("lock file mtime after the probe = %v, want it unchanged at %v", info.ModTime(), past)
	}
}

// A free lock probes free, and the probe drops its own momentary lock: the next AcquireLock
// takes it at once.
func TestProbeLockOnAFreeLockLeavesItFree(t *testing.T) {
	t.Parallel()

	path := lockPath(t)
	release := acquire(t, path)
	release()

	held, pid, err := ProbeLock(path)

	if err != nil || held || pid != 0 {
		t.Fatalf("ProbeLock on a free lock = (%v, %d, %v), want (false, 0, nil)", held, pid, err)
	}
	acquire(t, path)
}

// The probe opens the file read-only, so a lock file the prober may only read is probeable, held
// or free.
func TestProbeLockOnAReadOnlyFile(t *testing.T) {
	t.Parallel()

	t.Run("held", func(t *testing.T) {
		t.Parallel()
		path := lockPath(t)
		acquire(t, path)
		if err := os.Chmod(path, 0o400); err != nil {
			t.Fatalf("make the lock file read-only: %v", err)
		}
		held, pid, err := ProbeLock(path)
		if err != nil || !held || pid != os.Getpid() {
			t.Errorf("ProbeLock on a held read-only file = (%v, %d, %v), want (true, %d, nil)", held, pid, err, os.Getpid())
		}
	})

	t.Run("free", func(t *testing.T) {
		t.Parallel()
		path := lockPath(t)
		if err := os.WriteFile(path, []byte("4242\n"), 0o400); err != nil {
			t.Fatalf("write a read-only lock file: %v", err)
		}
		held, pid, err := ProbeLock(path)
		if err != nil || held || pid != 0 {
			t.Errorf("ProbeLock on a free read-only file = (%v, %d, %v), want (false, 0, nil)", held, pid, err)
		}
	})
}

// A lock file that does not exist is held by no one, and the probe never creates it.
func TestProbeLockOnAMissingFileCreatesNothing(t *testing.T) {
	t.Parallel()

	path := lockPath(t)

	held, pid, err := ProbeLock(path)

	if err != nil || held || pid != 0 {
		t.Fatalf("ProbeLock on a missing file = (%v, %d, %v), want (false, 0, nil)", held, pid, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stat after probing a missing lock = %v, want it still absent", err)
	}
}

// A path that exists but cannot be opened is a plain failure naming the path, not "not held".
func TestProbeLockReportsAnUnusablePath(t *testing.T) {
	t.Parallel()

	// A regular file standing where a directory should be: open fails with ENOTDIR, which is
	// neither "missing" nor "held". Windows answers the same open with ERROR_PATH_NOT_FOUND, which
	// is os.ErrNotExist there — so it reads as "missing", correctly, and has no case to pin.
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a path under a regular file as missing, not unusable")
	}
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatalf("write the blocking file: %v", err)
	}
	path := filepath.Join(parent, "daemon.lock")

	held, _, err := ProbeLock(path)

	if err == nil || held {
		t.Fatalf("ProbeLock on an unusable path = (%v, %v), want (false, an error)", held, err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("ProbeLock error = %q, want it to name the path", err)
	}
}

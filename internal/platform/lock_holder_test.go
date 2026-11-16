package platform

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/testtime"
)

// The one lock test that needs a second process. Every other lock test runs in this process
// (lock_test.go says why that is enough for contention), but the claim the lock's whole design
// rests on — the kernel drops it when its holder dies, so a dead daemon never keeps the next one
// out — can only be shown by a holder that actually dies. The holder is this test binary re-run
// with TestLockHolderHelper selected alone, so the test needs no host tool and runs on every OS.

const (
	// lockHolderEnv names the lock path the re-run binary holds. Set, it makes
	// TestLockHolderHelper the holder; unset, as in every ordinary run, that test does nothing.
	lockHolderEnv = "APOGEE_LOCKTEST_HOLDER"

	// lockHolderReady is the one line the holder prints once it has the lock.
	lockHolderReady = "held"
)

// lockHolderStart is how long a re-run test binary may take to start and take the lock — a
// budget, since a loaded box starts a race-built binary slowly; the wait ends on the "held" line.
var lockHolderStart = testtime.Budget(60 * time.Second)

// lockHolderRelease is how long the lock may stay held after its holder is killed: the kernel
// drops it on process exit, and a killed process's handles close asynchronously on Windows.
var lockHolderRelease = testtime.Budget(30 * time.Second)

// TestLockHolderHelper is the holder half of TestAcquireLockFreedWhenHolderDies, run in a child
// process. It takes the lock, reports it held, and keeps it until its stdin closes or it is
// killed — it blocks on a read, never on a sleep, so it lives exactly as long as the parent wants.
func TestLockHolderHelper(t *testing.T) {
	path := os.Getenv(lockHolderEnv)
	if path == "" {
		return
	}

	release, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("holder: AcquireLock(%s) = %v, want the lock", path, err)
	}
	defer release()

	fmt.Println(lockHolderReady)
	_, _ = io.Copy(io.Discard, os.Stdin)
}

// lockHolder is a child process holding a lock: the re-run test binary in TestLockHolderHelper.
type lockHolder struct {
	cmd    *exec.Cmd
	stdin  io.Closer
	stderr *bytes.Buffer
	once   sync.Once
}

// startLockHolder starts a child that takes the lock on path and returns once it reports the lock
// held, failing the test when it exits first or does not report within lockHolderStart. The child
// is killed and reaped at cleanup unless the test has already done so.
func startLockHolder(t *testing.T, path string) *lockHolder {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test binary: %v", err)
	}
	cmd := exec.Command(self, "-test.run=^TestLockHolderHelper$")
	cmd.Env = append(os.Environ(), lockHolderEnv+"="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("holder stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("holder stdout: %v", err)
	}
	holder := &lockHolder{cmd: cmd, stdin: stdin, stderr: &bytes.Buffer{}}
	cmd.Stderr = holder.stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the holder: %v", err)
	}
	t.Cleanup(holder.kill)

	ready := make(chan error, 1)
	go func() {
		var seen []string
		lines := bufio.NewScanner(stdout)
		for lines.Scan() {
			if lines.Text() == lockHolderReady {
				ready <- nil
				return
			}
			seen = append(seen, lines.Text())
		}
		ready <- fmt.Errorf("the holder exited before reporting the lock held (read error %v), stdout:\n%s",
			lines.Err(), strings.Join(seen, "\n"))
	}()

	select {
	case err := <-ready:
		if err != nil {
			holder.kill()
			t.Fatalf("%v\nstderr:\n%s", err, holder.stderr)
		}
	case <-time.After(lockHolderStart):
		holder.kill()
		t.Fatalf("the holder did not report the lock held within %s; stderr:\n%s", lockHolderStart, holder.stderr)
	}
	return holder
}

// pid is the holder's process id — the number it records in the lock file.
func (h *lockHolder) pid() int {
	return h.cmd.Process.Pid
}

// kill kills the holder and reaps it. It runs once; later calls do nothing, so the test can kill
// the holder itself and still leave the cleanup registered. Its stderr is safe to read afterwards.
func (h *lockHolder) kill() {
	h.once.Do(func() {
		_ = h.cmd.Process.Kill()
		_ = h.cmd.Wait()
		_ = h.stdin.Close()
	})
}

func TestAcquireLockFreedWhenHolderDies(t *testing.T) {
	t.Parallel()

	path := lockPath(t)
	holder := startLockHolder(t, path)

	// While the holder lives the lock is its own: a newcomer is refused, and both the refusal
	// and a probe name the holder's process, not this one.
	release, err := AcquireLock(path)
	if release != nil {
		release()
		t.Fatalf("AcquireLock returned a release while another process held the lock, want nil")
	}
	var held *LockHeldError
	if !errors.As(err, &held) {
		t.Fatalf("AcquireLock while another process holds the lock = %v, want a *LockHeldError", err)
	}
	if held.PID != holder.pid() {
		t.Errorf("LockHeldError.PID = %d, want the holder's pid %d", held.PID, holder.pid())
	}
	probed, pid, err := ProbeLock(path)
	if err != nil || !probed || pid != holder.pid() {
		t.Errorf("ProbeLock while another process holds the lock = (%v, %d, %v), want (true, %d, nil)",
			probed, pid, err, holder.pid())
	}

	holder.kill()

	// The holder never released anything: only its death can have freed the lock.
	testtime.Eventually(t, lockHolderRelease, func() bool {
		release, err := AcquireLock(path)
		if err != nil {
			return false
		}
		t.Cleanup(release)
		return true
	}, "the lock to be free after its holder was killed")
}

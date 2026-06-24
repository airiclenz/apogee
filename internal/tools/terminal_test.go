package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// fakeConfiner is a caps-injected Confiner for the execution-tool tests. It records each
// Confine call; when unavailable it returns ErrConfinementUnavailable so the demote path is
// exercisable. Its no-op Confine leaves cmd as the real subprocess so a confined run still
// executes /bin/sh in these hermetic tests (the dev host has no landlock, contract §6).
type fakeConfiner struct {
	caps        domain.ConfinementCaps
	unavailable bool

	mu       sync.Mutex
	confined int
}

func (c *fakeConfiner) Capabilities() domain.ConfinementCaps { return c.caps }

func (c *fakeConfiner) Confine(_ context.Context, _ domain.ConfinementBox, _ *exec.Cmd) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unavailable {
		return fmt.Errorf("%w: fake", domain.ErrConfinementUnavailable)
	}
	c.confined++
	return nil
}

func (c *fakeConfiner) confineCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.confined
}

func terminalCall(id, command string) domain.ToolCall {
	return domain.ToolCall{ID: id, Tool: "terminal", Arguments: []byte(fmt.Sprintf(`{"command":%q}`, command))}
}

func TestTerminal_Markers(t *testing.T) {
	t.Parallel()
	term := NewTerminal(t.TempDir())
	if term.Name() != "terminal" {
		t.Errorf("Name() = %q, want terminal", term.Name())
	}
	if term.ReadOnly() {
		t.Error("terminal must be write-capable (ReadOnly()==false)")
	}
	if !domain.IsSubprocessTool(term) {
		t.Error("terminal must be a SubprocessTool")
	}
	if IsWorkspaceScopedWriter(term) {
		t.Error("terminal must NOT carry the workspaceScopedWriter marker (it is OS-confined, not path-bounded)")
	}
}

func TestTerminal_RunsAndCapturesOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell command; covered on unix")
	}
	t.Parallel()
	term := NewTerminal(t.TempDir())
	res, err := term.Execute(context.Background(), terminalCall("c1", "echo hello"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError {
		t.Errorf("clean command produced an error result: %q", res.Content)
	}
	if !strings.Contains(res.Content, "hello") {
		t.Errorf("output = %q, want it to contain %q", res.Content, "hello")
	}
}

func TestTerminal_NonZeroExitIsErrorResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell command; covered on unix")
	}
	t.Parallel()
	term := NewTerminal(t.TempDir())
	res, err := term.Execute(context.Background(), terminalCall("c1", "exit 3"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil (a non-zero exit is a result, not a Go error)", err)
	}
	if !res.IsError {
		t.Error("a non-zero exit must be an IsError result")
	}
	if !strings.Contains(res.Content, "exit code 3") {
		t.Errorf("result = %q, want it to report exit code 3", res.Content)
	}
}

func TestTerminal_EmptyAndUnparseableCommand(t *testing.T) {
	t.Parallel()
	term := NewTerminal(t.TempDir())

	res, err := term.Execute(context.Background(), terminalCall("c1", "   "))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "command is required") {
		t.Errorf("empty command: got %q, want a 'command is required' error result", res.Content)
	}

	// An unbalanced quote is not POSIX-parseable; shlex rejects it before the shell runs.
	res, err = term.Execute(context.Background(), terminalCall("c2", `echo "unterminated`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "could not parse") {
		t.Errorf("unparseable command: got %q, want a parse-error result", res.Content)
	}
}

func TestTerminal_WorkdirEscapeRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	term := NewTerminal(root)
	call := domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"pwd","workdir":"../../etc"}`)}
	res, err := term.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if !res.IsError {
		t.Error("a workdir escaping the root must be rejected as an error result")
	}
}

func TestTerminal_RunsUnderConfine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell command; covered on unix")
	}
	t.Parallel()
	term := NewTerminal(t.TempDir())
	conf := &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}}
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: conf,
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})

	res, err := term.Execute(ctx, terminalCall("c1", "echo confined"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if conf.confineCount() != 1 {
		t.Errorf("Confine called %d times, want 1 (the tool must confine the cmd it builds)", conf.confineCount())
	}
	if res.IsError || !strings.Contains(res.Content, "confined") {
		t.Errorf("confined run result = %q (err=%v)", res.Content, res.IsError)
	}
}

func TestTerminal_ConfinementUnavailablePropagates(t *testing.T) {
	t.Parallel()
	term := NewTerminal(t.TempDir())
	conf := &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}, unavailable: true}
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: conf,
		Box:      domain.ConfinementBox{WorkspaceRoot: t.TempDir()},
	})

	_, err := term.Execute(ctx, terminalCall("c1", "echo should-not-run"))
	if !errors.Is(err, domain.ErrConfinementUnavailable) {
		t.Fatalf("Execute err = %v, want ErrConfinementUnavailable (the tool must NOT run unconfined)", err)
	}
}

func TestTerminal_TimeoutKillsCleanly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell + sleep; covered on unix")
	}
	t.Parallel()
	term := NewTerminal(t.TempDir())
	start := time.Now()
	call := domain.ToolCall{ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"sleep 30","timeout_seconds":1}`)}
	res, err := term.Execute(context.Background(), call)
	if err != nil {
		t.Fatalf("Execute err = %v, want nil (a timeout is a result, not a Go error)", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("timed-out command took %v; the process group was not killed promptly", elapsed)
	}
	if !res.IsError || !strings.Contains(res.Content, "timed out") {
		t.Errorf("timeout result = %q, want it to report a timeout", res.Content)
	}
}

// TestTerminal_CancelKillsChildProcessGroup proves the §2.4 teardown: a ctx-cancelled
// command kills its whole process group, so a grandchild process (here a backgrounded sleep
// that writes its PID to a file) is reaped and not orphaned. It writes the child's own group
// leader PID and asserts the group is gone after cancel.
func TestTerminal_CancelKillsChildProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process groups; covered on unix")
	}
	t.Parallel()
	root := t.TempDir()
	pidFile := filepath.Join(root, "child.pid")
	term := NewTerminal(root)

	// The shell backgrounds a long sleep, records ITS pid, then waits — so a naive
	// leader-only kill would orphan the sleep. The process-group kill must reap it.
	script := fmt.Sprintf(`sleep 30 & echo $! > %s; wait`, strconv.Quote(pidFile))
	call := domain.ToolCall{ID: "c1", Tool: "terminal",
		Arguments: []byte(fmt.Sprintf(`{"command":%q}`, script))}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = term.Execute(ctx, call)
	}()

	// Wait for the child sleep's PID to be recorded, then cancel.
	childPID := waitForPIDFile(t, pidFile)
	cancel()
	<-done

	// Give the kernel a moment to reap, then assert the backgrounded sleep is gone.
	if pidAlive(childPID, 2*time.Second) {
		t.Errorf("backgrounded child PID %d survived ctx cancel; the process group was not killed", childPID)
	}
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil {
			if pid, perr := strconv.Atoi(strings.TrimSpace(string(b))); perr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child PID file %s never appeared", path)
	return 0
}

// pidAlive reports whether pid is still alive within the given window, polling so a
// slightly-late reap does not flake the test.
func pidAlive(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		// Signal 0 probes existence without delivering a signal; ESRCH ⇒ gone.
		if err := syscallKill0(pid); err != nil {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return syscallKill0(pid) == nil
}

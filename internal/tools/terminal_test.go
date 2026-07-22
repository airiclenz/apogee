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
	"github.com/airiclenz/apogee/internal/platform"
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
	// cmd.exe has no such grammar, so on Windows the same line reaches the shell instead
	// (the pre-flight matches the target shell — preflightCommandLine).
	res, err = term.Execute(context.Background(), terminalCall("c2", `echo "unterminated`))
	if err != nil {
		t.Fatalf("Execute err = %v", err)
	}
	if hostShellIsPOSIX() {
		if !res.IsError || !strings.Contains(res.Content, "could not parse") {
			t.Errorf("unparseable command: got %q, want a parse-error result", res.Content)
		}
	} else if strings.Contains(res.Content, "could not parse") {
		t.Errorf("cmd.exe line: got %q, want no POSIX pre-flight rejection", res.Content)
	}
}

// hostShellIsPOSIX reports whether this host's shell takes a real argv (sh -c) rather than a
// verbatim command line (cmd.exe) — the same convention Execute derives its pre-flight from.
func hostShellIsPOSIX() bool { return shellHost.CommandLine("probe") == "" }

// TestTerminal_PreflightMatchesTheTargetShell is the table proof that the pre-flight is a
// POSIX-sh gate and not a universal one: every row is a line cmd.exe reads without
// complaint, and the POSIX splitter's verdict on it must not decide a cmd run.
func TestTerminal_PreflightMatchesTheTargetShell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		command   string
		posixFail bool
	}{
		{name: "apostrophe in a word", command: `echo don't panic`, posixFail: true},
		{name: "quoted path with a trailing backslash", command: `dir "C:\Program Files\"`, posixFail: true},
		{name: "unbalanced double quote", command: `echo "unterminated`, posixFail: true},
		{name: "ordinary line", command: `echo hello`, posixFail: false},
		{name: "balanced quotes", command: `git commit -m "a message"`, posixFail: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := preflightCommandLine(tc.command, false); err != nil {
				t.Errorf("preflightCommandLine(%q, posix=false) = %v, want nil (cmd.exe is not pre-parsed)", tc.command, err)
			}
			err := preflightCommandLine(tc.command, true)
			if tc.posixFail && err == nil {
				t.Errorf("preflightCommandLine(%q, posix=true) = nil, want the POSIX splitter's error", tc.command)
			}
			if !tc.posixFail && err != nil {
				t.Errorf("preflightCommandLine(%q, posix=true) = %v, want nil", tc.command, err)
			}
		})
	}
}

// rawCmdlineHost is platform.Current() with one difference: CommandLine always reports a
// raw command line — the convention that marks a shell the line is delivered to verbatim
// (cmd.exe). It is the injected-Windows-rules seam for the pre-flight, and only for it: the
// argv still comes from the real host, so the command runs in whatever shell the test host
// has, and on POSIX the raw line is never used (setRawCommandLine is a no-op there).
type rawCmdlineHost struct{ platform.Host }

func (h rawCmdlineHost) CommandLine(line string) string {
	if raw := h.Host.CommandLine(line); raw != "" {
		return raw
	}
	return "cmd /c " + line
}

// TestTerminal_CmdLinesAreNotGatedByThePOSIXSplitter drives Execute with the Windows
// raw-command-line convention in force and asserts the two lines the POSIX splitter rejects
// get past the gate and reach spec construction — whatever the shell then makes of them.
//
// It is deliberately NOT parallel: it substitutes the package-level shellHost, and Go
// resumes parallel tests only after the sequential pass over the top-level tests is done.
func TestTerminal_CmdLinesAreNotGatedByThePOSIXSplitter(t *testing.T) {
	saved := shellHost
	shellHost = rawCmdlineHost{Host: saved}
	t.Cleanup(func() { shellHost = saved })

	for _, command := range []string{`echo don't panic`, `dir "C:\Program Files\"`} {
		res, err := executeTerminalLine(t, command)
		if err != nil {
			t.Fatalf("Execute(%q) err = %v, want nil", command, err)
		}
		if strings.Contains(res.Content, "could not parse command line") {
			t.Errorf("Execute(%q) = %q, want no POSIX pre-flight rejection under Windows rules", command, res.Content)
		}
	}
}

// executeTerminalLine runs one command line through a fresh terminal tool rooted at a temp dir.
func executeTerminalLine(t *testing.T, command string) (domain.ToolResult, error) {
	t.Helper()
	return NewTerminal(t.TempDir()).Execute(context.Background(), terminalCall("c1", command))
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
	// Generous: the Windows tree test starts a PowerShell, whose cold start dwarfs a POSIX
	// shell's. The wait ends as soon as the file appears, so the ceiling costs nothing.
	deadline := time.Now().Add(20 * time.Second)
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

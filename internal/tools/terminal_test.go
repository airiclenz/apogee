package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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
	term := NewTerminal(t.TempDir(), nil)
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
	term := NewTerminal(t.TempDir(), nil)
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

// TestTerminal_DropsApogeeCredentialsFromTheChildEnvironment pins the one subtraction the shell
// tool makes from the operator's environment: apogee's own key, which a model-chosen command
// line has no use for and could send anywhere. Everything else — PATH, HOME, the operator's own
// variables — is still inherited, because a stripped environment would break the developer
// tooling the tool exists to run.
func TestTerminal_DropsApogeeCredentialsFromTheChildEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell command; covered on unix")
	}
	// Not parallel: t.Setenv.
	t.Setenv("APOGEE_API_KEY", "sk-secret-value")
	t.Setenv("APOGEE_ENDPOINT", "http://192.0.2.1:1111")

	term := NewTerminal(t.TempDir(), nil)
	res, err := term.Execute(context.Background(), terminalCall("c1", `echo "key=[$APOGEE_API_KEY] endpoint=[$APOGEE_ENDPOINT] path=[${PATH:+set}]"`))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if strings.Contains(res.Content, "sk-secret-value") {
		t.Errorf("the api key reached the shell: %q", res.Content)
	}
	if !strings.Contains(res.Content, "key=[]") {
		t.Errorf("output = %q, want APOGEE_API_KEY empty in the child", res.Content)
	}
	if !strings.Contains(res.Content, "endpoint=[http://192.0.2.1:1111]") {
		t.Errorf("output = %q, want the non-secret variables still inherited", res.Content)
	}
	if !strings.Contains(res.Content, "path=[set]") {
		t.Errorf("output = %q, want PATH still inherited", res.Content)
	}
}

// TestTerminal_DropsTheConfiguredSecretNamesFromTheChildEnvironment pins the caller-named half of
// the same subtraction: a variable the host named — the `api-key-env:` credential an operator
// exported into the shell apogee was started from (ADR 0047) — is dropped from the command line's
// environment too, while the operator's other variables still travel. Asserted on the captured
// spec, so it holds on every platform.
func TestTerminal_DropsTheConfiguredSecretNamesFromTheChildEnvironment(t *testing.T) {
	// Not parallel: t.Setenv, plus the package-level runner swap.
	t.Setenv("APOGEE_TEST_PROVIDER_KEY", "sk-configured-value")
	t.Setenv("APOGEE_TEST_ENDPOINT", "http://192.0.2.1:1111")

	captured := withCapturedTerminalRun(t)
	term := NewTerminal(t.TempDir(), []string{"apogee_test_provider_key"})
	if _, err := term.Execute(context.Background(), terminalCall("c1", "echo hi")); err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if value, ok := envValue(captured.env, "APOGEE_TEST_PROVIDER_KEY"); ok {
		t.Errorf("APOGEE_TEST_PROVIDER_KEY = %q reached the shell environment, want the configured name dropped", value)
	}
	if value, _ := envValue(captured.env, "APOGEE_TEST_ENDPOINT"); value != "http://192.0.2.1:1111" {
		t.Errorf("APOGEE_TEST_ENDPOINT = %q, want it inherited (only the NAMED variables are dropped)", value)
	}
}

// withCapturedTerminalRun swaps the shell runner for one that records the spec and launches
// nothing, so a test can pin the exact environment the tool builds on every platform.
func withCapturedTerminalRun(t *testing.T) *subprocessSpec {
	t.Helper()
	orig := runTerminalSubprocess
	var captured subprocessSpec
	runTerminalSubprocess = func(_ context.Context, spec subprocessSpec) (subprocessResult, error) {
		captured = spec
		return subprocessResult{}, nil
	}
	t.Cleanup(func() { runTerminalSubprocess = orig })
	return &captured
}

// TestTerminal_ScopesTheWorkspaceOffTheChildPATH pins the second subtraction the shell tool makes
// from the operator's environment (the first is apogee's credentials): a PATH entry that resolves
// inside the workspace is dropped, so a `git` or a `curl` the model planted in the box cannot be
// what the command line — or anything it spawns — resolves. Everything else is still inherited.
func TestTerminal_ScopesTheWorkspaceOffTheChildPATH(t *testing.T) {
	// Not parallel: t.Setenv, plus the package-level runner swap.
	root := t.TempDir()
	path, inside, outside := workspacePATH(t, root)
	// The tool RESOLVES its shell on this PATH before it builds the spec (shellArgv), so the
	// fixture carries the host's own shell directory: the assertions below are about which
	// entries survive the scrub, not about a host with no `sh`.
	shell, err := exec.LookPath(shellHost.Shell())
	if err != nil {
		t.Skipf("no platform shell on this host: %v", err)
	}
	t.Setenv("PATH", path+string(os.PathListSeparator)+filepath.Dir(shell))
	t.Setenv("APOGEE_TERMINAL_ENV_PROBE", "kept")

	captured := withCapturedTerminalRun(t)
	if _, err := NewTerminal(root, nil).Execute(context.Background(), terminalCall("c1", "echo hi")); err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	entries := envPathEntries(t, captured.env)
	if slices.Contains(entries, inside) {
		t.Errorf("PATH = %q still names the in-workspace entry %q", entries, inside)
	}
	if !slices.Contains(entries, outside) {
		t.Errorf("PATH = %q dropped the out-of-workspace entry %q; only the workspace is scoped off", entries, outside)
	}
	if slices.Contains(entries, filepath.Join("relative", "bin")) {
		t.Errorf("PATH = %q kept a non-absolute entry, which names a directory inside the child's own cwd", entries)
	}
	if value, _ := envValue(captured.env, "APOGEE_TERMINAL_ENV_PROBE"); value != "kept" {
		t.Errorf("APOGEE_TERMINAL_ENV_PROBE = %q, want it inherited (only PATH is rewritten)", value)
	}
}

func TestTerminal_NonZeroExitIsErrorResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell command; covered on unix")
	}
	t.Parallel()
	term := NewTerminal(t.TempDir(), nil)
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
	term := NewTerminal(t.TempDir(), nil)

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
	return NewTerminal(t.TempDir(), nil).Execute(context.Background(), terminalCall("c1", command))
}

func TestTerminal_WorkdirEscapeRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	term := NewTerminal(root, nil)
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
	term := NewTerminal(t.TempDir(), nil)
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
	term := NewTerminal(t.TempDir(), nil)
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
	term := NewTerminal(t.TempDir(), nil)
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
	term := NewTerminal(root, nil)

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

// TestTerminal_FailFastAbortsTheScriptAtTheFirstFailure pins the incident shape (minus
// confinement): a multi-line script whose first command fails must abort before any
// unguarded later line runs — under `set -e` the write after `false` never happens.
func TestTerminal_FailFastAbortsTheScriptAtTheFirstFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fail-fast preamble; cmd.exe has no set -e analogue")
	}
	t.Parallel()
	root := t.TempDir()
	term := NewTerminal(root, nil)
	res, err := term.Execute(context.Background(), terminalCall("c1", "false\necho reached > blocked.txt"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "exit code") {
		t.Errorf("result = %q (IsError=%v), want a non-zero-exit error result", res.Content, res.IsError)
	}
	if _, statErr := os.Stat(filepath.Join(root, "blocked.txt")); !os.IsNotExist(statErr) {
		t.Errorf("blocked.txt exists (stat err = %v); the script ran past its first failure", statErr)
	}
}

// TestTerminal_PipefailFailsAPipelineWhereSupported asserts the self-detecting half of
// the preamble: where the host sh accepts pipefail, a failing left side of a pipe fails
// the whole call. The preamble always NAMES pipefail now, so it can no longer say whether
// this host honours it — the test asks the host shell itself and skips when it does not
// (test code may spawn; production no longer does).
func TestTerminal_PipefailFailsAPipelineWhereSupported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fail-fast preamble; cmd.exe has no set -e analogue")
	}
	if exec.Command("sh", "-c", "set -o pipefail").Run() != nil {
		t.Skip("host sh does not accept `set -o pipefail`; the preamble's subshell skips it")
	}
	t.Parallel()
	term := NewTerminal(t.TempDir(), nil)
	res, err := term.Execute(context.Background(), terminalCall("c1", "false | cat"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !res.IsError || !strings.Contains(res.Content, "exit code") {
		t.Errorf("result = %q (IsError=%v), want pipefail to surface the pipeline failure", res.Content, res.IsError)
	}
}

// TestTerminal_PreambleLeavesSuccessOutputUntouched pins that the preamble is silent: a
// clean command still exits 0 with exactly its own output, nothing prepended.
func TestTerminal_PreambleLeavesSuccessOutputUntouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell command; covered on unix")
	}
	t.Parallel()
	term := NewTerminal(t.TempDir(), nil)
	res, err := term.Execute(context.Background(), terminalCall("c1", "echo hello"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if res.IsError || res.Content != "hello\n" {
		t.Errorf("result = %q (IsError=%v), want exactly %q with exit 0", res.Content, res.IsError, "hello\n")
	}
}

// TestTerminal_PrependsFailFastPreambleToThePOSIXLine pins where the preamble lands: the
// spec's argv carries the composed preamble ahead of the model's own line, verbatim.
//
// Not parallel: it swaps the package-level runner.
func TestTerminal_PrependsFailFastPreambleToThePOSIXLine(t *testing.T) {
	if !hostShellIsPOSIX() {
		t.Skip("POSIX shell path; the cmd.exe path is pinned by TestTerminal_NoPreambleOnRawCmdLines")
	}
	captured := withCapturedTerminalRun(t)
	if _, err := NewTerminal(t.TempDir(), nil).Execute(context.Background(), terminalCall("c1", "echo hi")); err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	want := platform.FailFastPreamble() + "echo hi"
	if got := captured.argv[len(captured.argv)-1]; got != want {
		t.Errorf("shell line = %q, want %q (preamble + the model's line)", got, want)
	}
}

// TestTerminal_NoPreambleOnRawCmdLines drives Execute under the Windows raw-command-line
// convention and asserts the line reaches spec construction verbatim: cmd.exe has no
// `set -e` analogue, so the fail-fast preamble is POSIX-only by design.
//
// Not parallel: it substitutes the package-level shellHost and the runner.
func TestTerminal_NoPreambleOnRawCmdLines(t *testing.T) {
	saved := shellHost
	shellHost = rawCmdlineHost{Host: saved}
	t.Cleanup(func() { shellHost = saved })

	captured := withCapturedTerminalRun(t)
	if _, err := NewTerminal(t.TempDir(), nil).Execute(context.Background(), terminalCall("c1", "echo hi")); err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if got := captured.argv[len(captured.argv)-1]; got != "echo hi" {
		t.Errorf("shell line = %q, want the verbatim %q (no preamble on the cmd path)", got, "echo hi")
	}
	if strings.Contains(captured.cmdline, "set -e") {
		t.Errorf("cmdline = %q, want no preamble on the verbatim cmd.exe line", captured.cmdline)
	}
}

// TestTerminal_DescriptionDisclosesFailFast pins the first half of the disclosure: the tool's
// own description tells the model the POSIX line runs fail-fast and how to guard a command whose
// non-zero exit is expected. Static, and scoped "On POSIX" so it stays true on a Windows host.
func TestTerminal_DescriptionDisclosesFailFast(t *testing.T) {
	t.Parallel()
	desc := NewTerminal(t.TempDir(), nil).Description()
	for _, want := range []string{"fail-fast", "set -e", "|| true"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description does not name %q: %q", want, desc)
		}
	}
}

// TestSubprocessToolResultFailFastNote pins the second half as a pure table over
// subprocessToolResult: a failed run launched under the preamble says so INSIDE its exit-code
// line, a run without the preamble keeps the plain line, a clean exit gets no line at all, and a
// timeout keeps the plain line — a run its own clock cut short did not stop at a failed command.
func TestSubprocessToolResultFailFastNote(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		res      subprocessResult
		wantLine string
		wantNote bool
	}{
		{"fail-fast failure names the mode", subprocessResult{exitCode: 1, failFast: true},
			"[exit code 1", true},
		{"failure without the preamble keeps the plain line", subprocessResult{exitCode: 1},
			"[exit code 1]", false},
		{"clean exit under the preamble has no exit-code line", subprocessResult{
			combinedOutput: "hello\n", exitCode: 0, failFast: true}, "", false},
		{"a timeout is not a fail-fast stop", subprocessResult{
			exitCode: -1, failFast: true, timedOut: true}, "[exit code -1]", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := subprocessToolResult("c1", tc.res)

			if got := strings.Contains(res.Content, failFastExitNote); got != tc.wantNote {
				t.Errorf("fail-fast note present = %v, want %v (content = %q)", got, tc.wantNote, res.Content)
			}
			if tc.wantLine == "" {
				if strings.Contains(res.Content, "[exit code") {
					t.Errorf("content = %q, want no exit-code line", res.Content)
				}
			} else if !strings.Contains(res.Content, tc.wantLine) {
				t.Errorf("content = %q, want it to contain %q", res.Content, tc.wantLine)
			}
			if tc.res.timedOut && !strings.Contains(res.Content, "command timed out") {
				t.Errorf("content = %q, want the timeout line", res.Content)
			}
		})
	}
}

// TestTerminal_FailFastNoteReachesTheModelEndToEnd drives a real POSIX run of the incident
// shape and asserts both halves the model actually sees: the aborted line never ran, and the
// result says WHY on its own exit-code line rather than leaving an empty failure to guess at.
func TestTerminal_FailFastNoteReachesTheModelEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fail-fast preamble; cmd.exe has no set -e analogue")
	}
	t.Parallel()

	res, err := NewTerminal(t.TempDir(), nil).Execute(context.Background(), terminalCall("c1", "false; echo after"))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}

	if !res.IsError || !strings.Contains(res.Content, failFastExitNote) {
		t.Errorf("result = %q (IsError=%v), want a failure whose exit-code line names fail-fast", res.Content, res.IsError)
	}
	if strings.Contains(res.Content, "after") {
		t.Errorf("result = %q, want the line after the failure never to have run", res.Content)
	}
}

// TestSubprocessToolResultDenialLabel pins the denial-label rendering as a pure table over
// subprocessToolResult: the label lands only on a CONFINED, non-zero-exit result whose output
// carries an OS-denial signature — every other combination stays label-free, and a clean exit
// never gains one however EPERM-shaped its output.
func TestSubprocessToolResultDenialLabel(t *testing.T) {
	t.Parallel()
	box := domain.ConfinementBox{WorkspaceRoot: "/ws", WritablePaths: []string{"/scratch/s1"}}
	cases := []struct {
		name      string
		res       subprocessResult
		wantLabel bool
	}{
		{"confined error with strerror text", subprocessResult{
			combinedOutput: "touch: /etc/f: Operation not permitted", exitCode: 1, confined: true, box: box}, true},
		{"confined error with Go errno text", subprocessResult{
			combinedOutput: "open /etc/f: operation not permitted", exitCode: 1, confined: true, box: box}, true},
		{"confined error with bare EPERM", subprocessResult{
			combinedOutput: "write failed: EPERM", exitCode: 2, confined: true, box: box}, true},
		{"unconfined error with strerror text", subprocessResult{
			combinedOutput: "touch: /etc/f: Operation not permitted", exitCode: 1, confined: false}, false},
		{"confined success with strerror text", subprocessResult{
			combinedOutput: "grep found: Operation not permitted", exitCode: 0, confined: true, box: box}, false},
		{"confined error without a signature", subprocessResult{
			combinedOutput: "no such file or directory", exitCode: 1, confined: true, box: box}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := subprocessToolResult("c1", tc.res)
			if got := strings.Contains(res.Content, confinementDenialLabel(tc.res.box)); got != tc.wantLabel {
				t.Errorf("label present = %v, want %v (content = %q)", got, tc.wantLabel, res.Content)
			}
			if wantErr := tc.res.exitCode != 0; res.IsError != wantErr {
				t.Errorf("IsError = %v, want %v", res.IsError, wantErr)
			}
		})
	}
}

// TestTerminal_ConfinementDenialLabelEndToEnd drives Execute with a fake Confiner and a
// command whose output mimics the incident's OS denial, proving the denial plumbing travels
// from runSubprocess into the rendered result. On the confined run the live kill-on-denial
// watch matches the streamed EPERM text, so the failure carries the definitive STOP label;
// the identical unconfined failure is never watched and carries no confinement label at all.
func TestTerminal_ConfinementDenialLabelEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell command; the label plumbing it pins is platform-independent")
	}
	t.Parallel()
	term := NewTerminal(t.TempDir(), nil)
	denial := `echo "touch: cannot touch '/etc/f': Operation not permitted" >&2; exit 1`

	box := domain.ConfinementBox{WorkspaceRoot: t.TempDir(), WritablePaths: []string{t.TempDir()}}
	ctx := domain.WithConfinement(context.Background(), domain.Confinement{
		Confiner: &fakeConfiner{caps: domain.ConfinementCaps{FSWrite: true}},
		Box:      box,
	})
	res, err := term.Execute(ctx, terminalCall("c1", denial))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !res.IsError || !strings.Contains(res.Content, confinementDenialStopLabel(box)) {
		t.Errorf("confined denial result = %q (IsError=%v), want the stopped-by-confinement label naming %v",
			res.Content, res.IsError, box)
	}

	res, err = term.Execute(context.Background(), terminalCall("c2", denial))
	if err != nil {
		t.Fatalf("Execute err = %v, want nil", err)
	}
	if !res.IsError || strings.Contains(res.Content, "blocked by workspace confinement") {
		t.Errorf("unconfined result = %q (IsError=%v), want the same failure WITHOUT any confinement label", res.Content, res.IsError)
	}
}

// TestSubprocessToolResultDenialStopLabel pins the stop-label rendering as a pure table over
// subprocessToolResult: a denial-stopped non-zero result carries confinementDenialStopLabel
// (and it wins over the weaker "likely" heuristic), while a clean exit stays a success with
// no label however the watch flagged it — the watch's kill racing a process that already
// finished must never turn a success into an error.
func TestSubprocessToolResultDenialStopLabel(t *testing.T) {
	t.Parallel()
	box := domain.ConfinementBox{WorkspaceRoot: "/ws", WritablePaths: []string{"/scratch/s1"}}
	cases := []struct {
		name          string
		res           subprocessResult
		wantStopLabel bool
		wantErr       bool
		wantAnyLikely bool
	}{
		{"stopped kill (signalled exit)", subprocessResult{
			combinedOutput: "mkdir: /tmp/srtest: Operation not permitted", exitCode: -1,
			confined: true, denialStopped: true, box: box}, true, true, false},
		{"stopped but self-exited non-zero", subprocessResult{
			combinedOutput: "mkdir: /tmp/srtest: Operation not permitted", exitCode: 1,
			confined: true, denialStopped: true, box: box}, true, true, false},
		{"watch matched but the run finished cleanly", subprocessResult{
			combinedOutput: "grep found: Operation not permitted", exitCode: 0,
			confined: true, denialStopped: true, box: box}, false, false, false},
		{"confined failure without the watch verdict keeps the likely label", subprocessResult{
			combinedOutput: "touch: /etc/f: Operation not permitted", exitCode: 1,
			confined: true, box: box}, false, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := subprocessToolResult("c1", tc.res)
			if got := strings.Contains(res.Content, confinementDenialStopLabel(tc.res.box)); got != tc.wantStopLabel {
				t.Errorf("stop label present = %v, want %v (content = %q)", got, tc.wantStopLabel, res.Content)
			}
			if got := strings.Contains(res.Content, confinementDenialLabel(tc.res.box)); got != tc.wantAnyLikely {
				t.Errorf("likely label present = %v, want %v (content = %q)", got, tc.wantAnyLikely, res.Content)
			}
			if res.IsError != tc.wantErr {
				t.Errorf("IsError = %v, want %v", res.IsError, tc.wantErr)
			}
		})
	}
}

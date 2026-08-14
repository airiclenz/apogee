package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
)

// Ceilings for a single subprocess call, bounding what one execution tool call can do so
// it cannot exhaust memory or run unbounded.
const (
	// maxSubprocessOutputBytes caps the combined stdout+stderr a subprocess call surfaces
	// to the model — a noisy command cannot flood the context window.
	maxSubprocessOutputBytes = 256 * 1024
	// defaultSubprocessTimeout bounds a subprocess call when the caller names no timeout;
	// the §2.4 teardown reaps the process group when it fires.
	defaultSubprocessTimeout = 120 * time.Second
	// maxSubprocessTimeout is the hard ceiling on a caller-named timeout.
	maxSubprocessTimeout = 600 * time.Second
)

// subprocessSpec is the platform-agnostic description of one subprocess execution: the argv
// to run, the working directory, the per-call timeout, and the optional stdin. The execution
// tools (terminal, python-exec) build a spec and hand it to runSubprocess, which owns the
// confinement handoff and the process-group teardown so each tool stays a thin front-end.
type subprocessSpec struct {
	// argv is the fully-resolved command and arguments (argv[0] is the program). It is
	// never empty when a tool calls runSubprocess.
	argv []string
	// dir is the working directory; empty means the process inherits the caller's.
	dir string
	// timeout bounds the run; zero means defaultSubprocessTimeout.
	timeout time.Duration
	// stdin, when non-empty, is fed to the process on its standard input.
	stdin string
	// env, when non-nil, is the exact environment the process runs with (each entry
	// "KEY=value"); nil means it inherits the caller's environment. EVERY tool that runs
	// something for the MODEL sets it — none of them inherits whole: git and the Go toolchain
	// take an allowlist scoped by platform.Host.ScopeEnv; the shell and interpreter tools take
	// subprocessEnvScopedPath() — the caller's environment minus every credential variable
	// (apogee's own and the host-configured ones), with the child's PATH scoped out of the
	// workspace; and the test runner takes subprocessEnv(), the same minus the credentials,
	// because a test suite needs the toolchain variables its user's shell has but no subprocess
	// of the model's needs apogee's key.
	env []string
	// cmdline, when non-empty, is the verbatim process command line to launch argv with
	// instead of letting os/exec join it (platform.Shell.CommandLine). It is empty on
	// POSIX and for any argv that is a real argv; a tool handing a SHELL LINE to
	// cmd.exe on Windows sets it, because os/exec's argv joining mangles the quotes the
	// shell needs (exec_cmdline_other.go).
	cmdline string
}

// apogeeSecretEnvVars names the environment variables that carry apogee's OWN credentials. A
// subprocess launched for the MODEL — a shell command line, a Python snippet — has no business
// reading the key apogee talks to its inference server with: the model chooses what that
// subprocess does, and an exfiltration from inside the box is one request away (ISSUES.md L3
// accepts that reading is possible; it does not oblige apogee to hand over its own secrets).
//
// The name is a literal rather than internal/config's EnvAPIKey because internal/config imports
// THIS package (its tool-name reconciliation), so the dependency cannot point back — which is
// also why the CONFIGURED names reach the tools as plain strings from the host rather than being
// read from config here. Those names are the reason this list is only HALF the scrub: a server
// entry's key was file-only when the list was written ("APIKey is FILE-ONLY on purpose"), but
// `api-key-env:` (ADR 0047) lets an entry name an ENVIRONMENT VARIABLE instead, and a variable
// the operator exported is inherited by every subprocess unless it is dropped too. The host names
// those variables (HostTools.SecretEnvVars) and isSecretEnv drops them beside this list.
var apogeeSecretEnvVars = []string{"APOGEE_API_KEY"}

// subprocessEnv returns the environment an execution tool's subprocess runs with: everything
// the caller inherited MINUS the credentials it must not see — apogee's own, plus the
// host-configured secretEnv names (nil ⇒ apogee's own alone) — plus each extra "KEY=value" entry
// appended. Appended, so it wins over an inherited spelling of the same key, which is how
// every exec implementation resolves a duplicate.
//
// It is deliberately NOT git's allowlist (safeEnvKeys): the shell and interpreter tools run
// what the operator asked for in the developer environment they expect to be in, and an
// allowlist there would break ordinary tooling. What is removed is only what apogee itself put
// there, and what the operator told apogee its own keys are called.
func subprocessEnv(secretEnv []string, extra ...string) []string {
	inherited := os.Environ()
	env := make([]string, 0, len(inherited)+len(extra))
	for _, entry := range inherited {
		if isSecretEnv(entry, secretEnv) {
			continue
		}
		env = append(env, entry)
	}
	return append(env, extra...)
}

// subprocessEnvScopedPath returns subprocessEnv's environment with one further scrub applied to
// the inherited half: the child's PATH drops every entry that lies inside workspaceRoot and every
// entry that is not an absolute location (platform.Shell.ScopeInheritedEnv).
//
// It is what the tools handing the MODEL a shell or an interpreter take. They inherit the
// operator's environment because that is the developer tooling they exist to run, but a
// workspace-resident PATH entry — an activated .venv, node_modules/.bin — would otherwise let
// bytes the model wrote become the `git`, the `ssh` or the `curl` that the subprocess, or
// anything it spawns, resolves for itself: the plant-then-exec chain apogee refuses at its own
// resolution sites (refuseExecFromWritablePath) and cannot check inside somebody else's process.
//
// The extras are appended AFTER the scoping — they are apogee's own additions rather than
// inherited values, and appending keeps them last-wins in the child, which is how every exec
// implementation resolves a duplicate.
func subprocessEnvScopedPath(workspaceRoot string, secretEnv []string, extra ...string) []string {
	return append(shellHost.ScopeInheritedEnv(workspaceRoot, subprocessEnv(secretEnv)), extra...)
}

// isSecretEnv reports whether a "KEY=value" entry names a credential no subprocess the model
// steers may see: one of apogee's own (isApogeeSecretEnv) or one of the configured names the
// host supplied — the variables its `api-key-env:` key sources read (ADR 0047), which arrive as
// plain strings because internal/config imports this package and the dependency cannot point
// back. They are compared the same case-insensitive way, for the same reason.
//
// An empty or whitespace-only configured name matches nothing: it is a blank entry in somebody's
// configuration, never permission to drop every variable in the environment.
func isSecretEnv(entry string, configured []string) bool {
	if isApogeeSecretEnv(entry) {
		return true
	}
	key, _, ok := strings.Cut(entry, "=")
	if !ok {
		return false
	}
	for _, configuredName := range configured {
		if name := strings.TrimSpace(configuredName); name != "" && strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

// isApogeeSecretEnv reports whether a "KEY=value" entry names one of apogee's own credentials —
// the fixed half of the scrub, which every execution tool drops whatever the host configured.
// The name comparison is case-insensitive because Windows environment names are: APOGEE_API_KEY
// and Apogee_Api_Key are one variable there. On POSIX they are two, and dropping both is the
// safe direction — a lower-cased spelling is one apogee never reads anyway.
func isApogeeSecretEnv(entry string) bool {
	key, _, ok := strings.Cut(entry, "=")
	if !ok {
		return false
	}
	for _, secret := range apogeeSecretEnvVars {
		if strings.EqualFold(key, secret) {
			return true
		}
	}
	return false
}

// subprocessResult is the captured outcome of one subprocess execution.
type subprocessResult struct {
	// combinedOutput is stdout and stderr interleaved (capped), what the model reads.
	combinedOutput string
	// exitCode is the process exit status; 0 on success, the child's code on a clean
	// non-zero exit, and -1 when the process was killed by a signal (e.g. a timeout).
	exitCode int
	// timedOut reports that the run was cut short by its own timeout (vs the model's ctx).
	timedOut bool
	// drainWedged reports that the process had exited but something it left running was still
	// holding the output pipe when processWaitDelay expired, so exec cut the drain short and
	// killed what was left. The captured output may be missing its tail, and the run is not a
	// success however cleanly the leader itself exited.
	drainWedged bool
}

// runSubprocess runs spec as a one-shot subprocess (ADR 0008 — fresh process per call, no
// persistent shell/REPL) and captures its combined output and exit code. It is the single
// place the §2.4 confinement-and-teardown contract is honoured for every execution tool:
//
//   - It builds an idiomatic *exec.Cmd with exec.CommandContext, owning all I/O (the
//     contract's tool-builds-and-runs-the-cmd model, §2.2).
//   - It wires the process-tree teardown (Setpgid + a negative-PID kill on POSIX, a Job
//     Object terminated on cancel on Windows, plus WaitDelay on both) so a cancelled or
//     timed-out command never orphans its children.
//   - If a Confinement handle is on ctx (the dispatch disposition installed it for an
//     Auto/confine subprocess call), it asks the Confiner to wrap the cmd before running.
//     A backend that cannot establish the box returns ErrConfinementUnavailable, which this
//     function propagates verbatim (wrapped) so dispatch can demote the call to Approval —
//     the "confine if you can, gate if you can't" runtime net (carried finding #2). The
//     subprocess is NOT run unconfined when confinement was required and failed — a handle
//     whose Confiner is nil is that same failure, reported rather than run around.
//
// The returned error is non-nil only for ctx cancellation (so the loop rolls the Turn back)
// or a confinement-unavailable demotion; a clean non-zero process exit is a normal result
// (exitCode set), not a Go error — the model reads it and routes around it.
func runSubprocess(ctx context.Context, spec subprocessSpec) (subprocessResult, error) {
	if err := ctx.Err(); err != nil {
		return subprocessResult{}, err
	}
	if len(spec.argv) == 0 {
		return subprocessResult{}, fmt.Errorf("apogee: runSubprocess: empty argv")
	}

	timeout := spec.timeout
	if timeout <= 0 {
		timeout = defaultSubprocessTimeout
	}
	if timeout > maxSubprocessTimeout {
		timeout = maxSubprocessTimeout
	}

	// The run is governed by its own context (a child of the caller's, so a model-side
	// cancel still propagates) carrying the per-call timeout. The §2.4 teardown reaps the
	// process group when either fires.
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, spec.argv[0], spec.argv[1:]...)
	cmd.Dir = spec.dir
	if spec.env != nil {
		cmd.Env = spec.env
	}
	if spec.stdin != "" {
		cmd.Stdin = strings.NewReader(spec.stdin)
	}
	var out cappedBuffer
	out.limit = maxSubprocessOutputBytes
	cmd.Stdout = &out
	cmd.Stderr = &out

	// Wire the process-tree teardown BEFORE confining: the Confiner only appends to
	// SysProcAttr (Setpgid on POSIX, Token on Windows) and never touches cmd.Cancel, so the
	// two compose. The returned handle is what the teardown needs once the process exists —
	// nothing on POSIX, the Job Object assignment on Windows (exec_teardown.go).
	teardown := newProcessTeardown(cmd)
	// The teardown owns an OS resource from the moment it is built (the Windows Job Object
	// handle), so this function owns releasing it: the confine refusal below and a cmd.Start()
	// failure both return without ever reaching Wait, and neither may leak the handle. release
	// is idempotent, so the normal path pays nothing for the guarantee.
	defer teardown.release()
	// A shell line on Windows must reach the shell verbatim; every other platform and
	// every real argv leaves this empty and the cmd untouched.
	setRawCommandLine(cmd, spec.cmdline)

	// Confine the command if the disposition installed a handle. ErrConfinementUnavailable
	// is propagated so dispatch demotes to Approval rather than running unconfined. An
	// installed handle carrying no Confiner is broken wiring, not permission to run free: it
	// fails closed the same way, so the escape surfaces as the truthful demote instead of a
	// silent unconfined run.
	if conf, ok := domain.ConfinementFromContext(ctx); ok {
		if conf.Confiner == nil {
			return subprocessResult{}, fmt.Errorf("confine %s: %w: the installed handle carries no Confiner",
				spec.argv[0], domain.ErrConfinementUnavailable)
		}
		if err := conf.Confiner.Confine(runCtx, conf.Box, cmd); err != nil {
			return subprocessResult{}, fmt.Errorf("confine %s: %w", spec.argv[0], err)
		}
	}

	runErr := runWithTeardown(cmd, teardown)

	// A ctx cancellation is the one case surfaced as a Go error (the loop rolls back).
	if ctx.Err() != nil {
		return subprocessResult{}, ctx.Err()
	}

	res := subprocessResult{combinedOutput: out.String()}
	res.timedOut = runCtx.Err() == context.DeadlineExceeded
	res.exitCode = exitCodeOf(cmd, runErr)
	// exec.ErrWaitDelay is not an *exec.ExitError, so exitCodeOf falls through to the leader's
	// own status — 0 whenever the leader exited cleanly and only its descendants wedged the
	// drain. Reporting that as a success hides exactly the case the operator needs to see:
	// something was still holding the pipe and had to be killed.
	res.drainWedged = errors.Is(runErr, exec.ErrWaitDelay)
	if res.drainWedged && res.exitCode == 0 {
		res.exitCode = -1
	}
	return res, nil
}

// exitCodeOf extracts the process exit code from a finished cmd: the child's code on a clean
// exit (zero or non-zero), and -1 when the process was killed by a signal (a timeout or the
// teardown kill), which exec reports without an ExitCode. A wedged drain (exec.ErrWaitDelay) is
// deliberately NOT decided here — it is not a process status at all, so runSubprocess reads it
// off the run error itself.
func exitCodeOf(cmd *exec.Cmd, runErr error) int {
	if runErr == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return exitErr.ExitCode() // -1 if signalled, the child's code otherwise
	}
	// A non-ExitError (e.g. the program could not be started) — report -1 and let the
	// caller surface the message from combined output / the error itself.
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode()
	}
	return -1
}

// cappedBuffer is an io.Writer that accumulates up to limit bytes and silently discards the
// rest, so a runaway subprocess cannot exhaust memory through its output. The discarded tail
// is summarised by the caller via Truncated.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	discarded int
}

// Write accepts bytes up to the buffer's limit, counting (but not storing) any overflow.
func (b *cappedBuffer) Write(p []byte) (int, error) {
	if remaining := b.limit - b.buf.Len(); remaining > 0 {
		if len(p) <= remaining {
			b.buf.Write(p)
		} else {
			b.buf.Write(p[:remaining])
			b.discarded += len(p) - remaining
		}
	} else {
		b.discarded += len(p)
	}
	// Always report the full length written so the process is never blocked on a short write.
	return len(p), nil
}

// String returns the captured output, with a truncation marker appended when output was
// discarded so the model knows the tail is missing.
func (b *cappedBuffer) String() string {
	s := b.buf.String()
	if b.discarded > 0 {
		s += fmt.Sprintf("\n… [output truncated: %d more bytes]", b.discarded)
	}
	return s
}

// shellHost is the platform shell/path facility the terminal tool wraps a command line with
// (sh -c on POSIX, cmd /c on Windows). It is a package var so a test can substitute a fake.
var shellHost platform.Host = platform.Current()

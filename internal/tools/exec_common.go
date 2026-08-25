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
	// splitStdout asks for the child's standard output to be captured ON ITS OWN
	// (subprocessResult.stdout) instead of interleaved with stderr. A caller that CONSUMES the
	// output as a payload sets it — the autofix formatter reads the reformatted file off
	// stdout, and a diagnostic spliced into the middle of that would be written to the user's
	// file as if it were code. The execution tools leave it false: they SHOW the model what a
	// command printed, and the interleaved order is the truthful one there.
	splitStdout bool
	// cmdline, when non-empty, is the verbatim process command line to launch argv with
	// instead of letting os/exec join it (platform.Shell.CommandLine). It is empty on
	// POSIX and for any argv that is a real argv; a tool handing a SHELL LINE to
	// cmd.exe on Windows sets it, because os/exec's argv joining mangles the quotes the
	// shell needs (exec_cmdline_other.go).
	cmdline string
	// failFast reports that the caller prepended platform.FailFastPreamble to the line it is
	// running, so a non-zero exit may be the preamble aborting the script at its first failed
	// command rather than the line as a whole finishing badly. It rides through onto the
	// result, where subprocessToolResult says so on the exit-code line — the model acts on the
	// last tool result, not on a system-prompt line from a dozen calls earlier. Only the
	// terminal's POSIX branch sets it: python_exec, git and the Console family prepend nothing.
	failFast bool
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
	// combinedOutput is stdout and stderr interleaved (capped), what the model reads. A spec
	// that split the streams (splitStdout) leaves it holding stderr ALONE — that caller took
	// the child's stdout as data, so what remains here is only what the command complained.
	combinedOutput string
	// stdout is the child's standard output alone (capped), captured only when the spec set
	// splitStdout; it is empty for every caller that reads combinedOutput.
	stdout string
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
	// confined reports that the run actually executed inside the confinement fence — a
	// Confinement handle was on ctx and its Confiner wrapped the cmd before it started.
	// subprocessToolResult keys on it to label a likely OS denial (EPERM-shaped output on a
	// failed confined run) so the model learns WHY a write outside the box failed; an
	// unconfined run must never carry that label, however EPERM-shaped its output.
	confined bool
	// box is the confinement policy the run actually executed under, carried through so the
	// denial labels can name the writable roots BY PATH instead of describing them. It is the
	// zero box on an unconfined run, where no label is rendered at all.
	box domain.ConfinementBox
	// denialStopped reports that the live kill-on-denial watch on a CONFINED run matched an
	// OS-denial signature and issued the process-group kill (fix A of the 2026-08-22
	// workspace-clobber incident). subprocessToolResult keys on it for the definitive
	// stopped-by-confinement label — but only on a non-zero exit: a run that still finished
	// cleanly (the match landed after the process was already done, or matched output that
	// was not a fatal denial) keeps its success result untouched.
	denialStopped bool
	// failFast carries the spec's failFast through to the rendering: the run was launched under
	// the fail-fast preamble, so subprocessToolResult can tell the model that a non-zero exit
	// stopped the rest of the line. It says nothing about whether the preamble actually fired —
	// a line whose LAST command failed exits the same way — so the note it drives is worded as
	// the mode that was in force, not as a verdict on which command failed.
	failFast bool
}

// confinementDenialLabel is the line appended to a FAILED confined result whose output looks
// like an OS confinement denial. It NAMES the roots the run may write to, because a model that
// is only told a fence exists has nowhere to put the file: the paths are what let it route the
// write instead of treating the EPERM as a broken command and blindly retrying around it.
func confinementDenialLabel(box domain.ConfinementBox) string {
	return "[likely blocked by workspace confinement: writes are allowed only inside " +
		confinementWritableRoots(box) + "]"
}

// confinementDenialStopLabel is the line appended when the live kill-on-denial watch stopped
// the run itself (subprocessResult.denialStopped, console.Console.DenialStopped): stronger than
// the "likely" label above, because here the harness matched the denial as it streamed and
// killed the process group, so the model is told plainly that the rest of its script did not
// run. It names the writable roots for the same reason that one does — the model's next act is
// to re-aim the write, and it can only do that against real paths. The OS-denial spellings both
// labels key on live in internal/platform (platform.LooksLikeConfinementDenial), which is also
// what the watch scans with.
//
// Both labels sit beside the funnel rather than beside one tool: the one-shot execution tools
// read them off subprocessResult and the Console family reads the stop label off a live
// Console, and there is one wording for the fence however the model met it.
func confinementDenialStopLabel(box domain.ConfinementBox) string {
	return "[blocked by workspace confinement: an operation was denied, so the command was" +
		" stopped; writes are allowed only inside " + confinementWritableRoots(box) + "]"
}

// confinementWritableRoots renders the box's writable roots as the tail both denial labels end
// with: the workspace by path, then every extra writable path the box carries — the session
// scratch dir among them, which Config.ConfinementBox already folds in. A box naming no root at
// all (an unconfined zero box reaching a label, which the callers below never do) falls back to
// the abstract wording rather than pointing the model at an empty path.
func confinementWritableRoots(box domain.ConfinementBox) string {
	roots := make([]string, 0, len(box.WritablePaths)+1)
	if box.WorkspaceRoot != "" {
		roots = append(roots, "the workspace "+box.WorkspaceRoot)
	}
	roots = append(roots, box.WritablePaths...)
	switch len(roots) {
	case 0:
		return "the workspace and the session scratch dir"
	case 1:
		return roots[0]
	default:
		return roots[0] + " and " + strings.Join(roots[1:], ", ")
	}
}

// resolveWorkdirInRoot resolves an execution tool's optional working directory within root
// (path-safe), or returns the root itself when none is given. Every tool taking a `workdir`
// argument resolves it the one way, so a path that escapes the workspace is refused with the
// same sentinel wherever the model tried it.
func resolveWorkdirInRoot(workdir, root string) (string, error) {
	if workdir == "" {
		return root, nil
	}
	return resolveInRoot(workdir, root)
}

// runSubprocess runs spec as a one-shot subprocess (ADR 0008 — fresh process per call, no
// persistent shell/REPL) and captures its combined output and exit code. It is the single
// place the §2.4 confinement-and-teardown contract is honoured for every execution tool:
//
//   - It builds an idiomatic *exec.Cmd with exec.CommandContext, owning all I/O (the
//     contract's tool-builds-and-runs-the-cmd model, §2.2).
//   - It wires the process-tree teardown (Setpgid + a negative-PID kill on POSIX, a Job
//     Object terminated on cancel on Windows, plus WaitDelay on both) so a cancelled or
//     timed-out command takes down every descendant that has not deliberately left the
//     container. The one documented escape is POSIX's: a descendant that calls
//     setsid/setpgid(0,0) is outside the group and outside the kill, so it survives the call
//     unsupervised — still inside whatever fence the Confiner installed, an accepted residual
//     rather than an enforcement gap (setProcessGroupTeardown states it in full). Windows'
//     Job Object denies breakaway and has no counterpart.
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
	// One capped buffer takes everything the child prints, so a runaway command cannot exhaust
	// memory through its output. A spec that split the streams gets a second one: stdout becomes
	// the caller's payload and `out` is left holding the diagnostics alone.
	var out, stdoutOnly cappedBuffer
	out.limit = maxSubprocessOutputBytes
	cmd.Stdout = &out
	cmd.Stderr = &out
	if spec.splitStdout {
		stdoutOnly.limit = maxSubprocessOutputBytes
		cmd.Stdout = &stdoutOnly
	}

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
	confined := false
	var box domain.ConfinementBox
	if conf, ok := domain.ConfinementFromContext(ctx); ok {
		if conf.Confiner == nil {
			return subprocessResult{}, fmt.Errorf("confine %s: %w: the installed handle carries no Confiner",
				spec.argv[0], domain.ErrConfinementUnavailable)
		}
		if err := conf.Confiner.Confine(runCtx, conf.Box, cmd); err != nil {
			return subprocessResult{}, fmt.Errorf("confine %s: %w", spec.argv[0], err)
		}
		confined = true
		// The box the run was fenced by rides along on the result: it is what the denial
		// labels name the writable roots from, and this is the only place it is in hand.
		box = conf.Box
	}

	// A CONFINED run's output is watched live for an OS-denial signature; the first match
	// cancels runCtx, which fires cmd.Cancel — the §2.4 process-group kill — so a script
	// whose command the fence denied is stopped there instead of running its remaining
	// lines against a half-done state (fix A of the 2026-08-22 workspace-clobber
	// incident). `set -e` cannot do this alone: POSIX exempts every command of an AND-OR
	// list but the last, so a denied `mkdir d && cd d` chain does not abort the script and
	// the unguarded lines after it run with the cwd unchanged — the incident's clobber.
	// The watch wraps the SAME capped buffer the streams already feed (one instance on
	// both keeps exec's single interleaved copier); on a split-stdout run only stderr is
	// watched, stdout being the caller's payload. Unconfined runs are never watched.
	var denialWatch *platform.DenialKillWriter
	if confined {
		denialWatch = platform.NewDenialKillWriter(&out, cancel)
		cmd.Stderr = denialWatch
		if !spec.splitStdout {
			cmd.Stdout = denialWatch
		}
	}

	runErr := runWithTeardown(cmd, teardown)

	// A ctx cancellation is the one case surfaced as a Go error (the loop rolls back).
	if ctx.Err() != nil {
		return subprocessResult{}, ctx.Err()
	}

	res := subprocessResult{combinedOutput: out.String(), stdout: stdoutOnly.String(), confined: confined, box: box}
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
	res.denialStopped = denialWatch != nil && denialWatch.Detected()
	res.failFast = spec.failFast
	return res, nil
}

// maxSubprocessErrorExcerptBytes caps how much of a failed command's diagnostics RunHookSubprocess
// quotes back in its error, so a noisy failure cannot drag the whole capped buffer into a log line.
const maxSubprocessErrorExcerptBytes = 256

// RunHookSubprocess runs argv as one subprocess through the SAME funnel every execution tool goes
// through (runSubprocess) and returns what the command wrote to standard output. It is the door a
// HOOK spawns through — a Mechanism runs outside the per-call Resolution and carries a
// domain.SubprocessPermit instead (docs/design/confinement-execution-contract.md §10) — so its
// subprocess gets every protection a tool's does rather than a hand-rolled exec.Command that has
// none: the credential scrub (subprocessEnv — neither apogee's own key nor an operator-declared one
// reaches a child), the §2.4 process-tree teardown, the output cap, the timeout clamp, and the
// confinement handoff, which fails CLOSED: a handle on ctx carrying no Confiner refuses the run
// rather than running unfenced.
//
// The funnel itself stays unexported; this is the whole of its outside surface. The caller
// installs its permit's box on ctx (domain.WithConfinement) before calling — no handle means an
// unfenced run, which is exactly what a permit carrying no box authorises. dir empty runs in
// apogee's own working directory. timeout zero takes the funnel's default and a timeout past the
// ceiling is clamped to it. stdin empty gives the child no input.
//
// secretEnv names the operator-declared credential variables to drop beside apogee's own — the
// same `api-key-env:` names (ADR 0047) the execution tools take from HostTools.SecretEnvVars,
// carried to a hook on mechanisms.Deps.SecretEnvVars and handed in here. A hook's child therefore
// scrubs exactly what a tool's child scrubs; nil names none and leaves the fixed half alone.
//
// The returned output is the child's stdout ALONE, never interleaved with its diagnostics, so a
// caller consuming it as a payload gets exactly the bytes the command produced. err is non-nil for
// a cancelled context, a refused confinement, a timeout, a wedged output drain and any non-zero
// exit: to a caller reading stdout as data every one of those means "no usable output", and the
// message quotes the command's diagnostics to say which.
func RunHookSubprocess(
	ctx context.Context,
	argv []string,
	dir string,
	secretEnv []string,
	timeout time.Duration,
	stdin string,
) (string, error) {
	res, err := runSubprocess(ctx, subprocessSpec{
		argv:        argv,
		dir:         dir,
		timeout:     timeout,
		stdin:       stdin,
		env:         subprocessEnv(secretEnv),
		splitStdout: true,
	})
	if err != nil {
		return "", err
	}

	// argv is known non-empty here: runSubprocess refuses an empty one above.
	switch {
	case res.timedOut:
		return "", fmt.Errorf("apogee: %s timed out%s", argv[0], diagnosticsExcerpt(res.combinedOutput))
	case res.drainWedged:
		return "", fmt.Errorf("apogee: %s left its output pipe held open%s", argv[0], diagnosticsExcerpt(res.combinedOutput))
	case res.exitCode != 0:
		return "", fmt.Errorf("apogee: %s exited %d%s", argv[0], res.exitCode, diagnosticsExcerpt(res.combinedOutput))
	}
	return res.stdout, nil
}

// diagnosticsExcerpt renders a failed command's stderr for an error message: a bounded TAIL —
// a failing command's last line is the one naming the failure — or nothing at all when the
// command was silent. The cut is made byte-wise and then swept of the partial rune it may have
// left at the front, so the excerpt is always valid UTF-8.
func diagnosticsExcerpt(diagnostics string) string {
	trimmed := strings.TrimSpace(diagnostics)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > maxSubprocessErrorExcerptBytes {
		trimmed = "…" + strings.ToValidUTF8(trimmed[len(trimmed)-maxSubprocessErrorExcerptBytes:], "")
	}
	return ": " + trimmed
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

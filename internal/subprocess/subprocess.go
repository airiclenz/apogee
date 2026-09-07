package subprocess

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
)

// Ceilings for a single subprocess call, bounding what one execution tool call can do so
// it cannot exhaust memory or run unbounded.
const (
	// MaxSubprocessOutputBytes caps the combined stdout+stderr a subprocess call surfaces
	// to the model — a noisy command cannot flood the context window.
	MaxSubprocessOutputBytes = 256 * 1024
	// DefaultSubprocessTimeout bounds a subprocess call when the caller names no timeout;
	// the §2.4 teardown reaps the process group when it fires.
	DefaultSubprocessTimeout = 120 * time.Second
	// MaxSubprocessTimeout is the hard ceiling on a caller-named timeout.
	MaxSubprocessTimeout = 600 * time.Second
)

// SubprocessSpec is the platform-agnostic description of one subprocess execution: the argv
// to run, the working directory, the per-call timeout, and the optional stdin. The execution
// tools (terminal, python-exec) build a spec and hand it to RunSubprocess, which owns the
// confinement handoff and the process-group teardown so each tool stays a thin front-end.
type SubprocessSpec struct {
	// Argv is the fully-resolved command and arguments (Argv[0] is the program). It is
	// never empty when a caller reaches RunSubprocess.
	Argv []string
	// Dir is the working directory; empty means the process inherits the caller's.
	Dir string
	// Timeout bounds the run; zero means DefaultSubprocessTimeout.
	Timeout time.Duration
	// Stdin, when non-empty, is fed to the process on its standard input.
	Stdin string
	// Env, when non-nil, is the exact environment the process runs with (each entry
	// "KEY=value"); nil means it inherits the caller's environment. EVERY tool that runs
	// something for the MODEL sets it — none of them inherits whole: git and the Go toolchain
	// take an allowlist scoped by platform.Host.ScopeEnv; the shell and interpreter tools take
	// internal/tools' subprocessEnvScopedPath — the caller's environment minus every credential
	// variable (apogee's own and the host-configured ones), with the child's PATH scoped out of
	// the workspace; and the test runner takes subprocessEnv, the same minus the credentials,
	// because a test suite needs the toolchain variables its user's shell has but no subprocess
	// of the model's needs apogee's key.
	Env []string
	// SplitStdout asks for the child's standard output to be captured ON ITS OWN
	// (SubprocessResult.Stdout) instead of interleaved with stderr. A caller that CONSUMES the
	// output as a payload sets it — a caller splicing a child's stdout into a file it will
	// write needs it clean, since a diagnostic in the middle of that would land in the file as
	// if it were code. The execution tools leave it false: they SHOW the model what a command
	// printed, and the interleaved order is the truthful one there. RunSubprocessTo implies it:
	// there the payload leaves through the caller's writer instead.
	SplitStdout bool
	// Cmdline, when non-empty, is the verbatim process command line to launch Argv with
	// instead of letting os/exec join it (platform.Shell.CommandLine). It is empty on
	// POSIX and for any argv that is a real argv; a caller handing a SHELL LINE to
	// cmd.exe on Windows sets it, because os/exec's argv joining mangles the quotes the
	// shell needs (cmdline_other.go).
	Cmdline string
	// FailFast reports that the caller prepended platform.FailFastPreamble to the line it is
	// running, so a non-zero exit may be the preamble aborting the script at its first failed
	// command rather than the line as a whole finishing badly. It rides through onto the
	// result, where the caller's rendering says so on the exit-code line — the model acts on the
	// last tool result, not on a system-prompt line from a dozen calls earlier. Only the
	// terminal's POSIX branch sets it: python_exec, git and the Console family prepend nothing.
	FailFast bool
}

// SubprocessResult is the captured outcome of one subprocess execution.
type SubprocessResult struct {
	// CombinedOutput is stdout and stderr interleaved (capped), what the model reads. A spec
	// that split the streams (SplitStdout), and every RunSubprocessTo run, leaves it holding
	// stderr ALONE — that caller took the child's stdout as data, so what remains here is only
	// what the command complained.
	CombinedOutput string
	// Stdout is the child's standard output alone (capped), captured only when the spec set
	// SplitStdout; it is empty for every caller that reads CombinedOutput, and for a
	// RunSubprocessTo run, whose stdout went to the caller's own writer.
	Stdout string
	// ExitCode is the process exit status; 0 on success, the child's code on a clean
	// non-zero exit, and -1 when the process was killed by a signal (e.g. a timeout).
	ExitCode int
	// TimedOut reports that the run was cut short by its own timeout (vs the model's ctx).
	TimedOut bool
	// DrainWedged reports that the process had exited but something it left running was still
	// holding the output pipe when platform.ProcessWaitDelay expired, so exec cut the drain
	// short and killed what was left. The captured output may be missing its tail, and the run
	// is not a success however cleanly the leader itself exited.
	DrainWedged bool
	// Confined reports that the run actually executed inside the confinement fence — a
	// Confinement handle was on ctx and its Confiner wrapped the cmd before it started.
	// A caller's denial rendering keys on it to label a likely OS denial (EPERM-shaped output on
	// a failed confined run) so the model learns WHY a write outside the box failed; an
	// unconfined run must never carry that label, however EPERM-shaped its output.
	Confined bool
	// Box is the confinement policy the run actually executed under, carried through so the
	// denial labels can name the writable roots BY PATH instead of describing them. It is the
	// zero box on an unconfined run, where no label is rendered at all.
	Box domain.ConfinementBox
	// DenialStopped reports that the live kill-on-denial watch on a CONFINED run matched an
	// OS-denial signature and issued the process-group kill (fix A of the 2026-08-22
	// workspace-clobber incident). The caller's rendering keys on it for the definitive
	// stopped-by-confinement label — but only on a non-zero exit: a run that still finished
	// cleanly (the match landed after the process was already done, or matched output that
	// was not a fatal denial) keeps its success result untouched.
	DenialStopped bool
	// FailFast carries the spec's FailFast through to the rendering: the run was launched under
	// the fail-fast preamble, so the caller can tell the model that a non-zero exit stopped the
	// rest of the line. It says nothing about whether the preamble actually fired — a line whose
	// LAST command failed exits the same way — so the note it drives is worded as the mode that
	// was in force, not as a verdict on which command failed.
	FailFast bool
}

// NewProcessTeardown builds the per-run process-tree teardown for cmd. It is this package's seam
// onto the platform constructor (platform.NewProcessTeardown, one per build tag) — a package var
// so a test can substitute a fake platform.ProcessTeardown and observe the release lifecycle on
// every OS. Production code never reassigns it.
var NewProcessTeardown = platform.NewProcessTeardown

// RunSubprocess runs spec as a one-shot subprocess (ADR 0008 — fresh process per call, no
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
//     rather than an enforcement gap (platform.NewProcessTeardown states it in full). Windows'
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
// (ExitCode set), not a Go error — the model reads it and routes around it.
func RunSubprocess(ctx context.Context, spec SubprocessSpec) (SubprocessResult, error) {
	return run(ctx, spec, nil)
}

// RunSubprocessTo is RunSubprocess with the child's standard output streamed UNCAPPED to stdout
// instead of accumulated in the output cap. Everything else is identical — the same spec, the same
// confinement handoff, the same §2.4 teardown, the same timeout clamp — and stderr is still capped,
// so a runaway diagnostic stream is bounded exactly as it is on the capped path.
//
// It exists for the caller whose payload is the child's output rather than a report of it: a git
// archive or a blob read spliced into a file has no business being truncated at
// MaxSubprocessOutputBytes, and a caller that hands over a writer has stated where those bytes go
// and taken responsibility for bounding them. The run is therefore split-stdout by construction:
// SubprocessResult.CombinedOutput holds the diagnostics ALONE and SubprocessResult.Stdout is empty,
// whatever spec.SplitStdout says. A nil stdout writer discards the child's output.
//
// Writer errors are the writer's own to notice: an io.Writer that fails mid-stream stops the copy
// os/exec is doing and surfaces on the run the same way any other exec failure does — through the
// exit code and the diagnostics, never as a silent truncation.
func RunSubprocessTo(ctx context.Context, spec SubprocessSpec, stdout io.Writer) (SubprocessResult, error) {
	if stdout == nil {
		stdout = io.Discard
	}
	return run(ctx, spec, stdout)
}

// run is the body both entry points share. streamStdout non-nil is the streaming variant: the
// child's stdout goes there uncapped and never into a capped buffer.
func run(ctx context.Context, spec SubprocessSpec, streamStdout io.Writer) (SubprocessResult, error) {
	if err := ctx.Err(); err != nil {
		return SubprocessResult{}, err
	}
	if len(spec.Argv) == 0 {
		return SubprocessResult{}, fmt.Errorf("apogee: RunSubprocess: empty argv")
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = DefaultSubprocessTimeout
	}
	if timeout > MaxSubprocessTimeout {
		timeout = MaxSubprocessTimeout
	}

	// The run is governed by its own context (a child of the caller's, so a model-side
	// cancel still propagates) carrying the per-call timeout. The §2.4 teardown reaps the
	// process group when either fires.
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Dir
	if spec.Env != nil {
		cmd.Env = spec.Env
	}
	if spec.Stdin != "" {
		cmd.Stdin = strings.NewReader(spec.Stdin)
	}
	// One capped buffer takes everything the child prints, so a runaway command cannot exhaust
	// memory through its output. A spec that split the streams gets a second one: stdout becomes
	// the caller's payload and `out` is left holding the diagnostics alone. The streaming variant
	// is that same split with the caller's writer standing in for the second buffer.
	var out, stdoutOnly CappedBuffer
	out.Limit = MaxSubprocessOutputBytes
	cmd.Stdout = &out
	cmd.Stderr = &out
	splitStdout := spec.SplitStdout || streamStdout != nil
	switch {
	case streamStdout != nil:
		cmd.Stdout = streamStdout
	case spec.SplitStdout:
		stdoutOnly.Limit = MaxSubprocessOutputBytes
		cmd.Stdout = &stdoutOnly
	}

	// Wire the process-tree teardown BEFORE confining: the Confiner only appends to
	// SysProcAttr (Setpgid on POSIX, Token on Windows) and never touches cmd.Cancel, so the
	// two compose. The returned handle is what the teardown needs once the process exists —
	// nothing on POSIX, the Job Object assignment on Windows (internal/platform/teardown.go).
	teardown := NewProcessTeardown(cmd)
	// The teardown owns an OS resource from the moment it is built (the Windows Job Object
	// handle), so this function owns releasing it: the confine refusal below and a cmd.Start()
	// failure both return without ever reaching Wait, and neither may leak the handle. release
	// is idempotent, so the normal path pays nothing for the guarantee.
	defer teardown.Release()
	// A shell line on Windows must reach the shell verbatim; every other platform and
	// every real argv leaves this empty and the cmd untouched.
	setRawCommandLine(cmd, spec.Cmdline)

	// Confine the command if the disposition installed a handle. ErrConfinementUnavailable
	// is propagated so dispatch demotes to Approval rather than running unconfined. An
	// installed handle carrying no Confiner is broken wiring, not permission to run free: it
	// fails closed the same way, so the escape surfaces as the truthful demote instead of a
	// silent unconfined run.
	confined := false
	var box domain.ConfinementBox
	if conf, ok := domain.ConfinementFromContext(ctx); ok {
		if conf.Confiner == nil {
			return SubprocessResult{}, fmt.Errorf("confine %s: %w: the installed handle carries no Confiner",
				spec.Argv[0], domain.ErrConfinementUnavailable)
		}
		if err := conf.Confiner.Confine(runCtx, conf.Box, cmd); err != nil {
			return SubprocessResult{}, fmt.Errorf("confine %s: %w", spec.Argv[0], err)
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
		if !splitStdout {
			cmd.Stdout = denialWatch
		}
	}

	runErr := platform.RunWithTeardown(cmd, teardown)

	// A ctx cancellation is the one case surfaced as a Go error (the loop rolls back).
	if ctx.Err() != nil {
		return SubprocessResult{}, ctx.Err()
	}

	res := SubprocessResult{CombinedOutput: out.String(), Stdout: stdoutOnly.String(), Confined: confined, Box: box}
	res.TimedOut = runCtx.Err() == context.DeadlineExceeded
	res.ExitCode = exitCodeOf(cmd, runErr)
	// exec.ErrWaitDelay is not an *exec.ExitError, so exitCodeOf falls through to the leader's
	// own status — 0 whenever the leader exited cleanly and only its descendants wedged the
	// drain. Reporting that as a success hides exactly the case the operator needs to see:
	// something was still holding the pipe and had to be killed.
	res.DrainWedged = errors.Is(runErr, exec.ErrWaitDelay)
	if res.DrainWedged && res.ExitCode == 0 {
		res.ExitCode = -1
	}
	res.DenialStopped = denialWatch != nil && denialWatch.Detected()
	res.FailFast = spec.FailFast
	return res, nil
}

// exitCodeOf extracts the process exit code from a finished cmd: the child's code on a clean
// exit (zero or non-zero), and -1 when the process was killed by a signal (a timeout or the
// teardown kill), which exec reports without an ExitCode. A wedged drain (exec.ErrWaitDelay) is
// deliberately NOT decided here — it is not a process status at all, so run reads it off the
// run error itself.
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

// CappedBuffer is an io.Writer that accumulates up to Limit bytes and silently discards the
// rest, so a runaway subprocess cannot exhaust memory through its output. The discarded tail
// is summarised by String.
type CappedBuffer struct {
	// Limit is the ceiling in bytes; a zero Limit accumulates nothing and counts everything
	// as discarded, so a caller always sets it before the buffer is written to.
	Limit int

	buf       bytes.Buffer
	discarded int
}

// Write accepts bytes up to the buffer's limit, counting (but not storing) any overflow.
func (b *CappedBuffer) Write(p []byte) (int, error) {
	if remaining := b.Limit - b.buf.Len(); remaining > 0 {
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
func (b *CappedBuffer) String() string {
	s := b.buf.String()
	if b.discarded > 0 {
		s += fmt.Sprintf("\n… [output truncated: %d more bytes]", b.discarded)
	}
	return s
}

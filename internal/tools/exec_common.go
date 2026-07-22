package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	// "KEY=value"); nil means it inherits the caller's environment. A tool that wants a
	// scrubbed, allowlisted environment (e.g. git) sets it; the shell/interpreter tools
	// leave it nil to inherit.
	env []string
	// cmdline, when non-empty, is the verbatim process command line to launch argv with
	// instead of letting os/exec join it (platform.Shell.CommandLine). It is empty on
	// POSIX and for any argv that is a real argv; a tool handing a SHELL LINE to
	// cmd.exe on Windows sets it, because os/exec's argv joining mangles the quotes the
	// shell needs (exec_cmdline_other.go).
	cmdline string
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
//     subprocess is NOT run unconfined when confinement was required and failed.
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
	teardown := setProcessGroupTeardown(cmd)
	// A shell line on Windows must reach the shell verbatim; every other platform and
	// every real argv leaves this empty and the cmd untouched.
	setRawCommandLine(cmd, spec.cmdline)

	// Confine the command if the disposition installed a handle. ErrConfinementUnavailable
	// is propagated so dispatch demotes to Approval rather than running unconfined.
	if conf, ok := domain.ConfinementFromContext(ctx); ok && conf.Confiner != nil {
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
	return res, nil
}

// exitCodeOf extracts the process exit code from a finished cmd: the child's code on a clean
// exit (zero or non-zero), and -1 when the process was killed by a signal (a timeout or the
// teardown kill), which exec reports without an ExitCode.
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

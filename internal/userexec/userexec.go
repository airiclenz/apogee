// Package userexec runs the USER's own argv — a Reaction's `run:` list, a server entry's
// `api-key-cmd:` — under the one exec posture both of those share. It is a leaf: it imports
// internal/security for the program fence and nothing else of apogee's, so any package that holds a
// user-configured command line can call it without pulling a subsystem in.
//
// The posture, and why each half is a refusal of an easier shape:
//
// No shell. The argv is the user's own list, run word for word — no interpolation, no word
// splitting, no glob — so a character in a file path can never mean something. A user who wants a
// pipeline writes ["sh", "-c", "…"] (or a wrapper script) and owns that choice explicitly.
//
// Outside confinement, fenced at the program. The command is the USER's configuration rather than
// anything the model chose, so it runs unsandboxed; what it may not do is execute a file the model
// could have written, which is what security.ResolveProgram refuses (ADR 0073 §6). The box is nil:
// these commands run on apogee's own behalf, before or beside any confinement box, so the workspace
// root the caller holds is the whole fence — and an empty root fences nothing, for the Drivers that
// have no workspace to name.
//
// No terminal. The child gets only the stdin the caller hands it and neither of apogee's standard
// streams: it is running under a TUI that owns the terminal, so a tool that tried to prompt there
// would draw over the frame and read the keystrokes meant for apogee. A backend that has to ask the
// human to unlock must prompt through a GUI agent (pinentry-mac, the Keychain dialog).
//
// Bounded, always. The run ends at the caller's deadline; stderr is capped at MaxStderr and folded
// to a StderrTailRunes-long tail, because it is held only to quote back in a failure line read on
// one row of a TUI; stdout is discarded unless the caller wants it, and then capped at the caller's
// own bound, with the overflow reported as a fact rather than silently cut.
//
// The environment is inherited whole, deliberately: a notifier or a credential tool needs HOME,
// DISPLAY, the D-Bus address and its agents' sockets, and this is the user's own command rather
// than one the model chose (which is what internal/tools scrubs for). The caller's Env is appended
// last, so it wins over an inherited variable of the same name.
//
// internal/keystore's run.go keeps its own runner on purpose: a store tool's exit status is data
// ("no such secret" is the probe's healthy answer) and its argv is apogee's, not the user's — a
// different posture, not a fourth copy of this one.
package userexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/security"
)

const (
	// WaitGrace bounds the wait AFTER the deadline fired. Killing the process ends the process,
	// but a wrapper-shaped command — a shell script re-execing the real tool — can leave a
	// grandchild holding the stderr pipe it inherited, and cmd.Run would then block on the copy
	// forever (internal/keystore's run.go carries the same guard for the same reason).
	WaitGrace = 2 * time.Second

	// MaxStderr bounds what one command may make apogee hold in memory. Stderr is kept only to
	// quote back in a failure line, so it is bounded tightly; a command printing megabytes of it
	// is misbehaving, and reading it to the end is how that becomes an out-of-memory kill.
	MaxStderr = 4 << 10

	// StderrTailRunes is how much of what the command said survives into a failure line. That
	// line is read on one row of a TUI, and the first sentence of a tool's complaint is almost
	// always the part that names the fix.
	StderrTailRunes = 240
)

// Options is what one run varies on. The zero value runs the argv with no stdin, no timeout beyond
// the context's own, stdout discarded and nothing fenced.
type Options struct {
	// WorkspaceRoot is the fence argv[0] is measured against before it runs: a program resolving
	// inside it is refused. Empty fences nothing (security.ResolveProgram's empty-fence rule).
	WorkspaceRoot string

	// Stdin is what the child reads; nil hands it no input at all.
	Stdin io.Reader

	// WantStdout keeps what the child printed, up to StdoutCap bytes; false discards it, for
	// callers where nothing the command prints may reach anyone. StdoutCap is the bound, in
	// bytes, when WantStdout is set: a cap of zero keeps nothing and marks any output as overflow.
	WantStdout bool
	StdoutCap  int

	// Timeout bounds the run on top of the context's own deadline when it is positive; zero
	// leaves the context as the only bound.
	Timeout time.Duration

	// Env is appended to apogee's inherited environment, last, so it wins over an inherited
	// variable of the same name.
	Env []string
}

// Result is what a command that RAN produced — facts, not a sentence, because each caller words
// its own failure line. ExitCode is the status the child exited with (-1 when a signal ended it,
// which is what a deadline kill leaves). TimedOut says the deadline fired, whether the caller's
// context or Options.Timeout set it. StderrTail is what the child complained about, folded onto one
// line and cut to StderrTailRunes — empty when it stayed quiet. Stdout is what it printed when the
// caller wanted it, capped; StdoutTruncated says there was more.
type Result struct {
	ExitCode        int
	TimedOut        bool
	StderrTail      string
	Stdout          string
	StdoutTruncated bool
}

// Run executes argv under the package's posture and reports what the command did.
//
// A non-nil error means the command never ran to a status, in one of three ways: argv[0] resolved
// inside the fence (`refusing to run %q: …`, wrapping security.ErrExecFromWritablePath), it is not
// on this machine's PATH (`%q is not on this machine's PATH`), or it could not be run or waited on
// (`could not run %s: …`, wrapping the underlying error). The Result then carries whatever stderr was
// captured before that, and nothing else. A command that ran — exited, any status, or was killed by
// the deadline — returns a nil error and the facts in Result; the caller reads ExitCode and
// TimedOut to decide what that meant.
func Run(ctx context.Context, argv []string, opts Options) (Result, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return Result{}, errors.New("no command to run")
	}
	program, err := ResolveProgram(argv[0], opts.WorkspaceRoot)
	if err != nil {
		return Result{}, err
	}
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, program, argv[1:]...)
	cmd.Stdin = opts.Stdin
	stdout := &cappedWriter{limit: opts.StdoutCap}
	if opts.WantStdout {
		cmd.Stdout = stdout
	} else {
		cmd.Stdout = io.Discard
	}
	stderr := &cappedWriter{limit: MaxStderr}
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(), opts.Env...)
	cmd.WaitDelay = WaitGrace

	runErr := cmd.Run()
	result := Result{
		TimedOut:        errors.Is(ctx.Err(), context.DeadlineExceeded),
		StderrTail:      stderrTail(stderr.String()),
		Stdout:          stdout.String(),
		StdoutTruncated: stdout.over,
	}
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		return result, nil
	case errors.As(runErr, &exitErr):
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	case result.TimedOut:
		// The deadline killed the child and the wait ended on WaitGrace instead of a status —
		// a wrapper's grandchild held the pipe. That is a run that timed out, not one that
		// never ran.
		result.ExitCode = -1
		return result, nil
	}
	return result, fmt.Errorf("could not run %s: %w", argv[0], runErr)
}

// ResolveProgram turns argv[0] into the absolute program apogee will execute, or the refusal it
// earns: `refusing to run %q: …` wrapping security.ErrExecFromWritablePath when it resolves inside
// workspaceRoot (an empty root fences nothing), or `%q is not on this machine's PATH` when nothing
// answers to the name. It is the judge Run applies before it executes anything, exported so a
// caller that has to pass the SAME verdict without running — internal/config's key resolver,
// judging a memoised `api-key-cmd:` against the workspace a later use names — asks the one fence
// rather than a second spelling of it.
//
// An argv[0] carrying a path separator is made absolute FIRST, against apogee's own working
// directory: that is exactly what exec.Command does with such a name — it skips PATH and hands the
// relative path to the child, which resolves it against the same directory — so making it absolute
// here changes nothing about which file runs and everything about whether the fence can see it. A
// bare name has no such meaning and goes through the PATH lookup unchanged. An absolute form that
// cannot be derived is left relative on purpose: security.ResolveProgram refuses a relative program
// path, which is the same answer security.RefuseExecFromWritablePath gives for that case.
func ResolveProgram(argv0, workspaceRoot string) (string, error) {
	program := argv0
	if filepath.Base(program) != program {
		if absolute, err := filepath.Abs(program); err == nil {
			program = absolute
		}
	}
	resolved, err := security.ResolveProgram(nil, program, workspaceRoot, nil)
	switch {
	case errors.Is(err, security.ErrExecFromWritablePath):
		return "", fmt.Errorf("refusing to run %q: %w", argv0, err)
	case err != nil:
		return "", fmt.Errorf("%q is not on this machine's PATH", argv0)
	}
	return resolved, nil
}

// stderrTail renders what the command complained about as the tail a failure line quotes, or
// nothing at all when it stayed quiet. The text is folded onto one line and cut short because the
// line is read on one row of a TUI, and a page of someone else's output would push apogee's own
// words off the screen.
func stderrTail(text string) string {
	folded := strings.Join(strings.Fields(text), " ")
	if runes := []rune(folded); len(runes) > StderrTailRunes {
		folded = strings.TrimSpace(string(runes[:StderrTailRunes])) + "…"
	}
	return folded
}

// cappedWriter is the bounded sink a child's stream is read into: it keeps the first limit bytes,
// remembers that there were more, and never fails the write. Failing it would kill the command with
// a broken pipe and report THAT instead of what the command actually said or the oversized output,
// which is the one fact the user needs.
type cappedWriter struct {
	limit int
	buf   []byte
	over  bool
}

// Write keeps what still fits and notes anything beyond it, always reporting a FULL write.
// Reporting the short write it really made would be read as io.ErrShortWrite by the copier os/exec
// runs behind the pipe, which would close the pipe and kill the command with SIGPIPE — reporting a
// signal instead of the exit status the user needs to see.
func (w *cappedWriter) Write(p []byte) (int, error) {
	switch room := w.limit - len(w.buf); {
	case room <= 0:
		w.over = w.over || len(p) > 0
	case len(p) > room:
		w.buf = append(w.buf, p[:room]...)
		w.over = true
	default:
		w.buf = append(w.buf, p...)
	}
	return len(p), nil
}

// String is what was kept.
func (w *cappedWriter) String() string {
	return string(w.buf)
}

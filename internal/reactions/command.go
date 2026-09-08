package reactions

import (
	"bytes"
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

// The exec contract a Hook's `command:` runs under is the `api-key-cmd:` resolver's contract
// (internal/config's keyresolve.go, runKeyCommand/resolveKeyProgram), copied here because this
// package depends on internal/domain and internal/security alone. internal/keystore's run.go
// carries the same copy for the same reason; unifying the three is deliberately out of scope.
//
// No shell. The argv is the user's own list, run word for word — no interpolation, no word
// splitting, no glob — so a character in a file path can never mean something. A user who wants a
// pipeline writes ["sh", "-c", "…"] and owns that choice explicitly.
//
// Outside confinement, fenced at the program. A Hook is the USER's configuration rather than
// anything the model chose, so it runs unsandboxed like the key command does; what it may not do
// is execute a file the model could have written, which is what security.ResolveProgram refuses
// (ADR 0073 §6).
//
// No stdout, no terminal. Stdin is the payload JSON and nothing else; stdout is discarded, because
// nothing a Hook prints may reach the model, the conversation or the Session record; stderr is
// kept only to quote back in the failure line the Driver reports.
//
// The environment is inherited whole, deliberately: a notifier needs HOME, DISPLAY, the D-Bus
// address and its agents' sockets, and this is the user's own command rather than one the model
// chose (which is what internal/tools scrubs for). The APOGEE_HOOK_* facts are appended last, so
// they win over an inherited variable of the same name.
const (
	// waitGrace bounds the wait AFTER the deadline fired. Killing the process ends the process,
	// but a wrapper-shaped command — a shell script re-execing the real tool — can leave a
	// grandchild holding the stderr pipe it inherited, and cmd.Run would then block on the copy
	// forever (keyresolve.go and keystore/run.go carry the same guard).
	waitGrace = 2 * time.Second

	// maxStderr bounds what one command may make apogee hold in memory. Stderr is kept only to
	// quote back in a failure line, so it is bounded tightly; a command printing megabytes of it
	// is misbehaving, and reading it to the end is how that becomes an out-of-memory kill.
	maxStderr = 4 << 10

	// maxErrorStderr is how much of what the command said survives into the failure line. That
	// line is read on one row of a TUI, and the first sentence of a tool's complaint is almost
	// always the part that names the fix.
	maxErrorStderr = 240
)

// commandExecutor runs a Hook's `command:` argv. It holds only the workspace root, because that is
// the whole fence: everything else about one run comes from the Hook and the firing.
//
// It is safe for concurrent use — it keeps no per-run state — which the Executor contract requires,
// since one executor serves every Hook and each Hook has a worker of its own.
type commandExecutor struct {
	workspaceRoot string
}

// Run executes the Hook's argv with the payload on stdin, and reports what went wrong in the words
// the Driver puts in front of the user. The Runner prefixes the Hook's name and event, so the
// message here says only what happened: `exit 3: …`, `timed out after 30s`, or the refusal that
// stopped the program from being run at all.
//
// The context is already bounded by the Hook's Timeout by the time it arrives, and is cancelled
// when the Runner is closing; both end the child, and only the deadline is reported — a
// cancellation is apogee's own shutdown and the Runner drops it.
func (c commandExecutor) Run(ctx context.Context, h Hook, p Payload) error {
	if len(h.Command) == 0 {
		return errors.New("no command to run")
	}
	body, err := encodePayload(p)
	if err != nil {
		return err
	}
	program, err := c.resolveProgram(h.Command[0])
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, program, h.Command[1:]...)
	cmd.Stdin = bytes.NewReader(body)
	cmd.Stdout = io.Discard
	stderr := &cappedWriter{limit: maxStderr}
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(), p.Env()...)
	cmd.WaitDelay = waitGrace

	runErr := cmd.Run()
	said := stderrTail(stderr.String())
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("timed out after %s%s", h.Timeout, said)
	case runErr == nil:
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return fmt.Errorf("exit %d%s", exitErr.ExitCode(), said)
	}
	return fmt.Errorf("could not run %s: %w%s", h.Command[0], runErr, said)
}

// resolveProgram turns the Hook's argv[0] into the absolute program apogee will execute, or the
// refusal it earns. It is resolveKeyProgram's rule, verbatim in behaviour.
//
// An argv[0] carrying a path separator is made absolute FIRST, against apogee's own working
// directory: that is exactly what exec.Command does with such a name — it skips PATH and hands the
// relative path to the child, which resolves it against the same directory — so making it absolute
// here changes nothing about which file runs and everything about whether the fence can see it. A
// bare name has no such meaning and goes through the PATH lookup unchanged. An absolute form that
// cannot be derived is left relative on purpose: ResolveProgram refuses a relative program path.
func (c commandExecutor) resolveProgram(argv0 string) (string, error) {
	program := argv0
	if filepath.Base(program) != program {
		if absolute, err := filepath.Abs(program); err == nil {
			program = absolute
		}
	}
	resolved, err := security.ResolveProgram(nil, program, c.workspaceRoot, nil)
	switch {
	case errors.Is(err, security.ErrExecFromWritablePath):
		return "", fmt.Errorf("refusing to run %q: %w", argv0, err)
	case err != nil:
		return "", fmt.Errorf("%q is not on this machine's PATH", argv0)
	}
	return resolved, nil
}

// stderrTail renders what the command complained about as a tail for the failure line, or nothing
// at all when it stayed quiet. The text is folded onto one line and cut short because the line is
// read on one row of a TUI, and a page of someone else's output would push apogee's own words off
// the screen.
func stderrTail(text string) string {
	folded := strings.Join(strings.Fields(text), " ")
	if folded == "" {
		return ""
	}
	if runes := []rune(folded); len(runes) > maxErrorStderr {
		folded = strings.TrimSpace(string(runes[:maxErrorStderr])) + "…"
	}
	return ": " + folded
}

// cappedWriter is the bounded sink the command's stderr is read into: it keeps the first limit
// bytes, drops the rest, and never fails the write. Failing it would kill the command with a
// broken pipe and report THAT instead of what the command actually said, which is the one fact
// the user needs. Nothing here needs to know that the output overflowed, because the tail quoted
// into the failure line is cut far shorter than the cap anyway.
type cappedWriter struct {
	limit int
	buf   []byte
}

// Write keeps what still fits, discards the rest, and always reports a FULL write. Reporting the
// short write it really made would be read as io.ErrShortWrite by the copier os/exec runs behind
// the stderr pipe, which would close the pipe and kill the command with SIGPIPE — reporting a
// signal instead of the exit status the user needs to see.
func (w *cappedWriter) Write(p []byte) (int, error) {
	written := len(p)
	if room := w.limit - len(w.buf); room > 0 {
		if written > room {
			p = p[:room]
		}
		w.buf = append(w.buf, p...)
	}
	return written, nil
}

// String is what was kept.
func (w *cappedWriter) String() string {
	return string(w.buf)
}

package keystore

// The exec contract every store command runs under — the resolver's contract (internal/config's
// keyresolve.go), because these commands are the same kind of thing: a credential tool, run by
// apogee on the user's behalf, from a process that owns the terminal.
//
// No shell. The argv is built here, word by word, from values apogee decided; there is nothing for a
// shell to add but the chance that a character in a server name means something.
//
// No terminal, and stdin is ours. The child is running under a TUI holding the screen, so a tool
// that tried to prompt there would draw over the frame and read the keystrokes meant for apogee — a
// store that must ask the human to unlock has to do it through its own GUI agent. Stdin belongs to
// the write: it is how the secret reaches the tool without passing through an argument vector.
//
// Bounded, always. A store tool can block for a very long time — a locked keychain, a bus autolaunch
// waiting on a session that will never come — and an unbounded wait on the startup path is
// indistinguishable from a hung apogee.
//
// The environment is inherited whole, deliberately: `security` and `secret-tool` need HOME, DISPLAY,
// the D-Bus address and their agents' sockets, and these are apogee's own fixed invocations rather
// than anything a model chose (which is what internal/tools scrubs for).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// writeTimeout bounds one write into the store. It is generous for the same reason the resolver's
// command timeout is: the write may be the moment a locked keychain puts an unlock dialog in front
// of a human, and a human reaching for a password takes tens of seconds. What it stops is the write
// that never answers at all.
const writeTimeout = 60 * time.Second

// probeTimeout bounds the question the Linux probe asks the secret service. It is short on purpose:
// this one runs at startup on every Linux machine, answered or not, so a bus that never replies must
// degrade to "no store" quickly rather than hold the session's first frame.
const probeTimeout = 5 * time.Second

// waitGrace bounds the wait AFTER a timeout fired. Killing the process ends the process, but a
// wrapper-shaped tool can leave a grandchild holding the stderr pipe it inherited, and cmd.Run would
// then block on the copy forever (keyresolve.go and autofix.go carry the same guard).
const waitGrace = 2 * time.Second

// maxToolStderr bounds what a store tool may make apogee hold in memory. Stderr is kept only to
// quote back in a refusal, so it is bounded tightly; a tool that prints megabytes of it is
// misbehaving, and reading to the end is how that becomes an out-of-memory kill.
const maxToolStderr = 4 << 10

// maxErrorStderr is how much of what the tool said survives into the error message. It is read on
// one line of a TUI, and the first sentence of a tool's complaint is almost always the part that
// names the fix.
const maxErrorStderr = 240

// toolResult is what one run of a store tool produced: what it complained about, and the status it
// exited with. There is no stdout field — the only command here whose standard output could carry a
// secret is the probe's lookup, and its answer is irrelevant to the question being asked, so it is
// discarded rather than held in apogee's memory.
type toolResult struct {
	stderr string
	code   int
}

// runner runs one store-tool command line with the given standard input, and reports what came back.
//
// The error means the command could not be run or never finished — a missing program, a timeout. A
// command that RAN and exited non-zero is not an error at this level: "no such secret" is exactly
// that, and the probe reads it as the healthy answer, so the status is data (toolResult.code) and
// each caller decides what it means.
type runner func(ctx context.Context, argv []string, stdin string) (toolResult, error)

// runTool is the production runner: the contract at the top of this file, executed.
func runTool(ctx context.Context, argv []string, stdin string) (toolResult, error) {
	if len(argv) == 0 {
		return toolResult{}, errors.New("no command to run")
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Stdout = io.Discard
	stderr := &cappedBuffer{limit: maxToolStderr}
	cmd.Stderr = stderr
	cmd.WaitDelay = waitGrace

	runErr := cmd.Run()
	outcome := toolResult{stderr: stderr.String()}

	program := filepath.Base(argv[0])
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return outcome, fmt.Errorf("%s did not answer in time — a store that has to ask you to unlock must "+
			"prompt through its own GUI agent, since this runs with no terminal of its own", program)
	}
	if runErr == nil {
		return outcome, nil
	}

	var exited *exec.ExitError
	if errors.As(runErr, &exited) {
		outcome.code = exited.ExitCode()
		return outcome, nil
	}
	return outcome, fmt.Errorf("%s could not be run: %w", program, runErr)
}

// said renders what a tool complained about as a tail for an error message, or nothing at all when
// it stayed quiet. The text is folded onto one line and cut short because the message is read on one
// line of a TUI, and a page of a tool's output would push apogee's own words off the screen.
func said(text string) string {
	folded := strings.Join(strings.Fields(text), " ")
	if folded == "" {
		return ""
	}
	if runes := []rune(folded); len(runes) > maxErrorStderr {
		folded = strings.TrimSpace(string(runes[:maxErrorStderr])) + "…"
	}
	return " — it said: " + folded
}

// cappedBuffer is the bounded sink a tool's stderr is read into: it keeps the first limit bytes,
// drops the rest, and never fails a write. Failing one would kill the tool with a broken pipe and
// report THAT instead of what the tool was trying to say.
type cappedBuffer struct {
	limit int
	buf   []byte
}

// Write keeps what still fits and discards the rest, always reporting a full write.
func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - len(b.buf); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		b.buf = append(b.buf, p[:room]...)
	}
	return len(p), nil
}

// String is what was kept.
func (b *cappedBuffer) String() string {
	return string(b.buf)
}

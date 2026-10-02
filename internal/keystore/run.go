package keystore

// The exec contract every store command runs under is internal/userexec's — the one the
// `api-key-cmd:` resolver and a Reaction's `run:` argv share, because these commands are the same
// kind of thing: a credential tool, run by apogee on the user's behalf, from a process that owns
// the terminal. userexec.Run carries it out: no shell, no terminal, bounded by the caller's
// deadline, stderr capped, and the tool's whole process tree torn down when that deadline fires —
// a wrapper-shaped tool's grandchild dies with it rather than outliving the write.
//
// What keystore adds is its reading of the result, because its posture differs where it matters.
// The argv is apogee's, not the user's: the program was fenced against the workspace when the store
// was probed (fenceProgram), so the run passes no root of its own. A non-zero exit is DATA here ("no
// such secret" is the probe's healthy answer) rather than the failure the user reads, and userexec
// reports it as a fact, so each caller below decides what it means. And the sentences are keystore's
// own: a deadline names the GUI agent a locked store must prompt through, and a tool that could not
// start is named by its base name.
//
// Stdin belongs to the write: it is how the secret reaches the tool without passing through an
// argument vector. Stdout is discarded — see toolResult.
//
// The environment is inherited whole, deliberately: `security` and `secret-tool` need HOME, DISPLAY,
// the D-Bus address and their agents' sockets, and these are apogee's own fixed invocations rather
// than anything a model chose (which is what internal/tools scrubs for).

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/userexec"
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

// maxErrorStderr is how much of what the tool said survives into the error message. It is read on
// one line of a TUI, and the first sentence of a tool's complaint is almost always the part that
// names the fix.
const maxErrorStderr = 240

// toolResult is what one run of a store tool produced: what it complained about — raw, capped at
// userexec.MaxStderr bytes, and whether the tool said more than that — and the status it exited
// with. The complaint is kept raw rather than folded because a store tool can echo the secret it
// was handed, and redaction has to run on the text before folding and cutting can reshape it.
// There is no stdout field — the only command here whose standard output could carry a secret is
// the probe's lookup, and its answer is irrelevant to the question being asked, so it is discarded
// rather than held in apogee's memory.
type toolResult struct {
	stderr       string
	stderrCapped bool
	code         int
}

// runner runs one store-tool command line with the given standard input, and reports what came back.
//
// The error means the command could not be run or never finished — a missing program, a timeout. A
// command that RAN and exited non-zero is not an error at this level: "no such secret" is exactly
// that, and the probe reads it as the healthy answer, so the status is data (toolResult.code) and
// each caller decides what it means.
type runner func(ctx context.Context, argv []string, stdin string) (toolResult, error)

// runTool is the production runner: the contract at the top of this file, executed through
// userexec.Run. The deadline is checked first because userexec reports a run the deadline killed as
// a run (nil error, TimedOut) — and that is the failure a locked store produces.
func runTool(ctx context.Context, argv []string, stdin string) (toolResult, error) {
	if len(argv) == 0 {
		return toolResult{}, errors.New("no command to run")
	}

	var opts userexec.Options
	if stdin != "" {
		opts.Stdin = strings.NewReader(stdin)
	}
	result, runErr := userexec.Run(ctx, argv, opts)
	outcome := toolResult{stderr: result.Stderr, stderrCapped: result.StderrCapped, code: result.ExitCode}

	program := filepath.Base(argv[0])
	if result.TimedOut {
		return outcome, fmt.Errorf("%s did not answer in time — a store that has to ask you to unlock must "+
			"prompt through its own GUI agent, since this runs with no terminal of its own", program)
	}
	if runErr == nil {
		return outcome, nil
	}

	// userexec words its own "could not run <argv0>: <cause>"; this sentence names the program by
	// its base name instead, so the cause is lifted out of that wrapper rather than quoted twice.
	if cause := errors.Unwrap(runErr); cause != nil {
		runErr = cause
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

// trimCappedKeyTail drops a secret the cap cut in half.
//
// userexec.MaxStderr is a BYTE cut: it falls wherever 4096 bytes into a tool's complaint happens to
// land, and when that is inside the key the tool echoed, the capture keeps the key's first bytes as
// a fragment. Redaction cannot take that back — it replaces whole occurrences of the secret, and
// half a secret is not one — so the fragment would ride into the refusal message, which is the
// readable place (terminal, session log, pasted bug report) the migration exists to get the key out
// of. Half a key is worth having, too: it names the issuer and the shape, and shortens a guess.
//
// So when, and only when, the tool said more than the cap kept (toolResult.stderrCapped) — the one
// condition under which the text may have been cut mid-word — the longest tail that spells the
// beginning of the key is dropped. Both spellings are checked for the reason redactKey checks both:
// on macOS the key travels quoted, so what the cut leaves behind is the beginning of the quoted
// word. A tail that is the WHOLE key is left to redactKey, which marks its place — that reads better
// than a sentence ending nowhere.
func trimCappedKeyTail(outcome toolResult, key string) string {
	text := outcome.stderr
	if key == "" || !outcome.stderrCapped {
		return text
	}

	cut := 0
	for _, spelling := range []string{key, securityWord(key)} {
		for n := min(len(spelling)-1, len(text)); n > cut; n-- {
			if strings.HasSuffix(text, spelling[:n]) {
				cut = n
				break
			}
		}
	}
	return text[:len(text)-cut]
}

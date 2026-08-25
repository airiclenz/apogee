package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/console"
	"github.com/airiclenz/apogee/internal/domain"
)

// The Console family's shared floor (ADR 0059): the id argument three of the four tools take,
// the registry lookup and the refusal it renders, the wait-window ceilings, and the one
// collector-and-renderer that turns "what the process said" into "what the model reads".
// console_open.go and console_send.go are thin fronts over it, as console_read.go and
// console_close.go are.

// Wait-window ceilings for one Console call, in milliseconds. A window is how long a call is
// willing to sit collecting output before it answers, and each tool's default is what its own
// question is worth: opening a program asks the short "did it start, and what did it say",
// sending it a line asks the longer "what did that produce", and reading asks "what has happened
// since I last looked" — which the buffer can answer with no window at all, so read's default is
// none and its window is something the model asks for when it means to wait.
const (
	consoleOpenWaitDefaultMS = 500
	consoleOpenWaitMaxMS     = 10_000
	consoleSendWaitDefaultMS = 1_000
	consoleSendWaitMaxMS     = 30_000
	consoleReadWaitDefaultMS = 0
	consoleReadWaitMaxMS     = 30_000
)

// consoleAliveStatus is the verdict on a Console whose process is still running — the word every
// read and send ends with until the program behind it exits.
const consoleAliveStatus = "alive"

// consoleDroppedFormat prefixes a result whose ring buffer overflowed, so the model learns its
// output has a hole in it instead of reading a spliced stream as though it were continuous.
const consoleDroppedFormat = "[… %d bytes of earlier output dropped …]"

// consoleID is a Console id as it arrives in a tool call: a JSON number, or the numeric STRING a
// model that quotes its arguments sends instead. Both spell the same id, and refusing the quoted
// one would cost a Turn to teach JSON rather than to drive the console.
type consoleID int

// UnmarshalJSON decodes a Console id from a JSON number or from a string holding one.
func (id *consoleID) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if unquoted, err := strconv.Unquote(text); err == nil {
		text = strings.TrimSpace(unquoted)
	}
	parsed, err := strconv.Atoi(text)
	if err != nil {
		return fmt.Errorf("id must be a console id (a number), got %s", string(data))
	}
	*id = consoleID(parsed)
	return nil
}

// lookupConsole returns the Console the call named, or the refusal the model reads instead. The
// refusal names the ids that ARE open, because "no console 7" on its own tells a model that has
// lost track of its consoles nothing it can act on.
//
// A context carrying no registry — missing engine wiring rather than a model mistake — reads as
// an engine holding no consoles: the model is told the id is unknown instead of being handed a
// Go error that would roll its whole Turn back.
func lookupConsole(ctx context.Context, callID string, id int) (*console.Console, domain.ToolResult, bool) {
	registry := console.FromContext(ctx)
	if registry != nil {
		if found, ok := registry.Get(id); ok {
			return found, domain.ToolResult{}, true
		}
	}
	refusal := fmt.Sprintf("no console %d (open consoles: %s)", id, openConsoleIDs(registry))
	return nil, errorResult(callID, refusal), false
}

// openConsoleIDs renders the ids a registry holds open the way a refusal names them, or "none"
// when it holds nothing (including the no-registry case, which holds nothing by definition).
func openConsoleIDs(registry *console.Registry) string {
	if registry == nil {
		return "none"
	}
	ids := registry.OpenIDs()
	if len(ids) == 0 {
		return "none"
	}
	rendered := make([]string, 0, len(ids))
	for _, id := range ids {
		rendered = append(rendered, strconv.Itoa(id))
	}
	return strings.Join(rendered, ", ")
}

// consoleWait turns a wait_ms argument into the window a call collects output for: absent (the
// JSON zero) takes the tool's default, a negative value collects nothing, and a value past the
// tool's ceiling is clamped rather than refused — a model asking for a longer window than it may
// have still gets the longest one it may have.
func consoleWait(waitMS, defaultMS, maxMS int) time.Duration {
	switch {
	case waitMS == 0:
		waitMS = defaultMS
	case waitMS < 0:
		waitMS = 0
	}
	if waitMS > maxMS {
		waitMS = maxMS
	}
	return time.Duration(waitMS) * time.Millisecond
}

// consoleInputBytes renders what console_send actually writes to a Console's terminal: the input
// with a newline appended, which is what "typing it" means, unless the call asked for the bytes
// verbatim. Raw is how a control character is sent on its own — a JSON \u0003 escape is
// Ctrl-C, and appending a newline to it would send a second keystroke nobody asked for.
func consoleInputBytes(input string, raw bool) []byte {
	if raw {
		return []byte(input)
	}
	return []byte(input + "\n")
}

// consoleTail renders what a Console has to say, returning as soon as SOME output arrives — the
// polling shape console_read wants, where the question is "has anything happened yet".
func consoleTail(ctx context.Context, c *console.Console, wait time.Duration) string {
	output, dropped := c.Read(wait)
	return renderConsoleTail(c, output, dropped, true, confinementBox(ctx))
}

// consoleWindowTail renders what a Console produces over the WHOLE window rather than stopping
// at the first byte, ending early when the process exits. It is what console_send needs: a
// terminal echoes the line it was sent before the program answers it, so a collector returning
// at the first output would hand the model back its own keystrokes and nothing else.
func consoleWindowTail(ctx context.Context, c *console.Console, wait time.Duration) string {
	output, dropped := collectConsoleWindow(c, wait)
	return renderConsoleTail(c, output, dropped, true, confinementBox(ctx))
}

// consoleOpenTail is consoleWindowTail without the "alive" line: console_open's first line has
// already said the Console is open, so the only liveness fact left worth adding is the one that
// contradicts it — a program that exited before anyone could speak to it.
func consoleOpenTail(ctx context.Context, c *console.Console, wait time.Duration) string {
	output, dropped := collectConsoleWindow(c, wait)
	return renderConsoleTail(c, output, dropped, false, confinementBox(ctx))
}

// consoleExitPollInterval is how often collectConsoleWindow re-asks whether a Console's exit has
// been recorded once its terminal has gone quiet. It is short enough to be invisible in a result
// and long enough not to spin: the wait it bounds is the millisecond gap between a process's last
// byte and its reaper writing down how it ended.
const consoleExitPollInterval = 5 * time.Millisecond

// collectConsoleWindow collects everything a Console produces until the window closes or its
// process exits, whichever comes first, reporting the bytes its buffer dropped over the same
// span. Each read parks until output arrives, so a quiet Console costs a waiting goroutine rather
// than a poll loop.
func collectConsoleWindow(c *console.Console, wait time.Duration) (string, int) {
	deadline := time.Now().Add(wait)
	var collected strings.Builder
	dropped := 0
	for {
		chunk, lost := c.Read(time.Until(deadline))
		collected.WriteString(chunk)
		dropped += lost
		switch {
		case !c.Alive() || time.Until(deadline) <= 0:
			return collected.String(), dropped
		case chunk == "":
			// The terminal went quiet while the process is still recorded as running: its
			// output has ENDED, so the only thing this window still owes the model is how
			// the process ended, which its reaper writes down a moment later.
			awaitConsoleExit(c, deadline)
			return collected.String(), dropped
		}
	}
}

// awaitConsoleExit waits, inside what is left of the window, for a Console's exit to be recorded,
// so a program that was over before the call returned is reported as exited rather than as alive.
// The wait is bounded by the window the caller already asked for: a program that closed its
// terminal and kept running costs that window and nothing more.
func awaitConsoleExit(c *console.Console, deadline time.Time) {
	for time.Now().Before(deadline) {
		if !c.Alive() {
			return
		}
		time.Sleep(consoleExitPollInterval)
	}
}

// renderConsoleTail assembles one Console result: the fence label when the kill-on-denial watch
// stopped this Console, the dropped-bytes note when its buffer overflowed, the output itself,
// and a closing line saying whether the process is still running. The output is capped at the
// ceiling every execution tool's is (maxSubprocessOutputBytes) with the same truncation marker,
// so one flooding program cannot fill the model's context from a single read.
//
// withAlive is false only for console_open, whose first line already says the Console is open.
//
// box is the confinement policy the Console was opened under, whose roots the fence label names
// by path; nil is the unconfined case, where DenialStopped is always false and the label cannot
// fire at all.
func renderConsoleTail(c *console.Console, output string, dropped int, withAlive bool, box *domain.ConfinementBox) string {
	lines := make([]string, 0, 4)
	if c.DenialStopped() {
		var fence domain.ConfinementBox
		if box != nil {
			fence = *box
		}
		lines = append(lines, confinementDenialStopLabel(fence))
	}
	if dropped > 0 {
		lines = append(lines, fmt.Sprintf(consoleDroppedFormat, dropped))
	}
	if text := strings.TrimRight(capConsoleOutput(output), "\r\n"); text != "" {
		lines = append(lines, text)
	}
	if status := consoleStatus(c); withAlive || status != consoleAliveStatus {
		lines = append(lines, status)
	}
	return strings.Join(lines, "\n")
}

// consoleStatus is the one-line verdict on a Console's process: still running, exited with a
// code, or killed by a signal — the teardown's own kill, or the denial watch's.
func consoleStatus(c *console.Console) string {
	if c.Alive() {
		return consoleAliveStatus
	}
	if code := c.ExitCode(); code >= 0 {
		return fmt.Sprintf("exited with code %d", code)
	}
	return "killed"
}

// capConsoleOutput bounds one call's output at the same ceiling a one-shot subprocess call has,
// through the same capped buffer, so a Console that floods its terminal costs the model exactly
// what a runaway terminal command does — and says so in the same words.
func capConsoleOutput(output string) string {
	if len(output) <= maxSubprocessOutputBytes {
		return output
	}
	var capped cappedBuffer
	capped.limit = maxSubprocessOutputBytes
	_, _ = capped.Write([]byte(output))
	return capped.String()
}

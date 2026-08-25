package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

var consoleSendSpec = toolSpec{
	name: "console_send",
	description: "Send input to a console opened with console_open, as if typed at its keyboard, and return what" +
		" the program printed in reply. A newline is appended unless raw is true; control characters may be" +
		" sent as JSON escapes (\\u0003 is Ctrl-C). Use console_read to keep watching a console that is still" +
		" producing output.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["id", "input"],
  "properties": {
    "id": {"type": "integer", "description": "The console id returned by console_open"},
    "input": {"type": "string", "description": "The text to type. A newline is appended unless raw is true; an empty string presses Enter."},
    "raw": {"type": "boolean", "description": "Send the bytes exactly as given, with no trailing newline (how a lone control character is sent)"},
    "wait_ms": {"type": "integer", "description": "Optional milliseconds to collect output after sending (default 1000, max 30000)"}
  }
}`),
}

type consoleSendArgs struct {
	ID consoleID `json:"id"`
	// Input is a pointer so an ABSENT input and an EMPTY one stay distinguishable: the first is
	// a malformed call, and the second is pressing Enter at a prompt — a real thing to do to a
	// console, and one an empty-means-missing check would refuse.
	Input  *string `json:"input"`
	Raw    bool    `json:"raw"`
	WaitMS int     `json:"wait_ms"`
}

// ConsoleSend types into a Console opened by console_open and reports what came back (ADR 0059).
//
// It carries the SubprocessTool marker although it spawns nothing, and that is deliberate:
// sending a line to a live shell IS command execution — the shell runs it — and the marker is
// what makes the disposition confine-or-gate the call instead of waving it through as an
// in-process write (ADR 0059 §2). The Resolution is taken per send, so a Console opened in one
// mode is never a standing permission in another; a mode change or a `/confine` change reaches
// the next send, never the live process (§4).
//
// It is DEFAULT-OFF beside the rest of the family (ADR 0057).
type ConsoleSend struct {
	toolSpec
}

// NewConsoleSend returns a console_send tool. It takes no root and no credential names: it starts
// nothing and resolves no path — the Console it types into was fenced when console_open opened it.
func NewConsoleSend() *ConsoleSend {
	return &ConsoleSend{toolSpec: consoleSendSpec}
}

// ReadOnly reports that console_send is write-capable (false): the line it sends is a line the
// program behind the Console executes.
func (t *ConsoleSend) ReadOnly() bool { return false }

// Subprocess reports the Subprocess marker even though console_send starts no process — see the
// type comment: the bytes it writes are executed by one, and the marker is what confines or gates
// them.
func (t *ConsoleSend) Subprocess() bool { return true }

// DefaultOff reports that console_send ships registered but off the default menu (ADR 0057).
func (t *ConsoleSend) DefaultOff() bool { return true }

// ApprovalScope names the Console this call reaches, which the call's own arguments do not: `id`
// is a bare number in the pane, and "→ console 3" is the sentence the human deciding actually
// reads.
//
// It is derived from the CALL alone, never from the registry. The approval path hands a tool no
// context, so the seam holding the Consoles — and with it the command line console 3 is running —
// is out of reach here BY DESIGN: domain.ApprovalScoper requires a cheap, non-blocking line
// derived from the arguments, not the tool's work. A call whose arguments name no usable id gets
// no line at all, leaving the prompt exactly as it was.
func (t *ConsoleSend) ApprovalScope(call domain.ToolCall) string {
	args, _, ok := decodeToolArgs[consoleSendArgs](call)
	if !ok || args.ID <= 0 {
		return ""
	}
	return fmt.Sprintf("→ console %d", args.ID)
}

// Execute writes the input to the Console's terminal and collects what the program produced over
// the wait window, ending early if the program exits.
//
// An unknown id, a missing input and a terminal that refused the write are all error RESULTS —
// each is something the model can act on, and the unknown-id refusal names the ids that are open.
// Only ctx cancellation is a Go error: nothing here can start a process, so there is no
// confinement demotion to make.
func (t *ConsoleSend) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[consoleSendArgs](call)
	if !ok {
		return fail, nil
	}
	if args.Input == nil {
		return errorResult(call.ID, "input is required"), nil
	}

	target, fail, ok := lookupConsole(ctx, call.ID, int(args.ID))
	if !ok {
		return fail, nil
	}

	if _, err := target.Write(consoleInputBytes(*args.Input, args.Raw)); err != nil {
		return errorResult(call.ID, fmt.Sprintf("could not write to console %d: %v", args.ID, err)), nil
	}

	wait := consoleWait(args.WaitMS, consoleSendWaitDefaultMS, consoleSendWaitMaxMS)
	return okResult(call.ID, consoleWindowTail(target, wait)), nil
}

var (
	_ domain.Tool           = (*ConsoleSend)(nil)
	_ domain.SubprocessTool = (*ConsoleSend)(nil)
	_ domain.DefaultOffTool = (*ConsoleSend)(nil)
	_ domain.ApprovalScoper = (*ConsoleSend)(nil)
)

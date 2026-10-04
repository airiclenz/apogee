package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/airiclenz/apogee/internal/console"
	"github.com/airiclenz/apogee/internal/domain"
)

var consoleSendSpec = toolSpec{
	name: "console_send",
	description: "Send input to a console opened with console_open, as if typed at its keyboard, and return what" +
		" the program printed in reply. Enter is pressed after the input unless raw is true; control characters may be" +
		" sent as JSON escapes (\\u0003 is Ctrl-C). Use console_read to keep watching a console that is still" +
		" producing output.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["id", "input"],
  "properties": {
    "id": {"type": "integer", "description": "The console id returned by console_open"},
    "input": {"type": "string", "description": "The text to type. Enter is pressed after it unless raw is true; an empty string presses Enter alone."},
    "raw": {"type": "boolean", "description": "Send the bytes exactly as given, without pressing Enter (how a lone control character is sent)"},
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
// A send made under a confinement box to a Console that was not opened confined has no fence behind
// the shell it would reach: the Console was opened unconfined (in Ask-Before, before a switch to
// Auto), on Windows as on POSIX. On every host such a send is refused with
// domain.ErrConfinementUnavailable and the dispatch demotes it to Approval — each send on its own
// (ADR 0059 Bounds and its 2026-10-04 Amendment).
//
// It is DEFAULT-OFF beside the rest of the family (ADR 0057).
type ConsoleSend struct {
	toolSpec
	// host is the operating system the tool types through: its platform rules supply the bytes
	// the Enter key sends, and whether a Console could have been opened confined at all — which
	// picks the wording of the demotion error, never whether a send is demoted.
	host execHost
}

// NewConsoleSend returns a console_send tool on the real operating system (defaultExecHost). It
// takes no root and no credential names: it starts nothing and resolves no path — a Console
// opened confined was fenced when console_open opened it, and a send under a box to one that was
// not is demoted by Execute instead. builtinTools builds it on the execution tools' one host
// through newConsoleSend.
func NewConsoleSend() *ConsoleSend {
	return newConsoleSend(defaultExecHost())
}

// newConsoleSend is NewConsoleSend with the host the tool types through supplied — one execHost
// shared by the execution tools in production, a host carrying fakes in a test.
func newConsoleSend(host execHost) *ConsoleSend {
	return &ConsoleSend{toolSpec: consoleSendSpec, host: host}
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
// Only two things are Go errors, both BEFORE the write: ctx cancellation, and the confinement
// demotion — a send under a confinement box to a Console that was not opened confined
// (consoleSendUnfenced) returns an error wrapping domain.ErrConfinementUnavailable, on every
// host, so the dispatch gates it through Approval rather than typing into an unfenced shell
// (ADR 0059, Amendment 2026-10-04). The error's text is what the model reads beside the refusal:
// where the platform can confine a Console it says to close this one and reopen it fenced, and
// where it cannot it says so. A cancel during the wait window ends it promptly with a nil error,
// returning the output collected so far and consoleCutShortNote — the input was already typed,
// and that is finished work the model must be told about (ADR 0088).
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

	if t.consoleSendUnfenced(ctx, target) {
		return domain.ToolResult{}, t.unfencedSendError(int(args.ID))
	}

	if _, err := target.Write(consoleInputBytes(*args.Input, args.Raw, t.host.shell.Enter())); err != nil {
		return errorResult(call.ID, fmt.Sprintf("could not write to console %d: %v", args.ID, err)), nil
	}

	wait := consoleWait(args.WaitMS, consoleSendWaitDefaultMS, consoleSendWaitMaxMS)
	return okResult(call.ID, consoleWindowTail(ctx, target, wait)), nil
}

// consoleSendUnfenced reports that a send to target must be demoted rather than typed: the call
// runs under a confinement box and target was not opened confined, so no fence stands behind the
// shell the line would reach. It holds on every host — whether the host's Console open can
// confine (platform.Terminal.ConsoleConfines) changes only what unfencedSendError says, to the
// model and on the Approval prompt.
func (t *ConsoleSend) consoleSendUnfenced(ctx context.Context, target *console.Console) bool {
	return confinementBox(ctx) != nil && !target.Confined
}

const (
	// consoleUnfencedSendReasonFormat is the Approval prompt reason for a demoted send on a host
	// whose Console open can confine: the cause is this Console, not the host, so the prompt names
	// it. The %d is the console id.
	consoleUnfencedSendReasonFormat = "send to console %d, which was opened unconfined"
	// consoleUnfencedSendRemedy is that prompt's way out. The fence is one reopen away and the
	// model already holds that advice (the error's detail), so denying is the fix — not lifting
	// confinement for the session.
	consoleUnfencedSendRemedy = "deny it — the agent is told to close the console and reopen it fenced"
)

// unfencedSendError is the demotion error for a send to console id: it wraps
// domain.ErrConfinementUnavailable so the dispatch gates the send, and its text names the way to
// a fenced send. Where a Console open can confine (every shipped platform, Windows included),
// the way is to close this Console and reopen it, which fences the new one, and the error is a
// domain.ConfineDemoteError so the Approval prompt names this Console and that fix; on a host
// whose rules say a Console open cannot confine, there is none to name and the prompt keeps the
// host-incapacity wording.
func (t *ConsoleSend) unfencedSendError(id int) error {
	if !t.host.shell.ConsoleConfines() {
		return fmt.Errorf(
			"%w: console %d was opened unconfined and this platform cannot confine a console",
			domain.ErrConfinementUnavailable, id,
		)
	}
	return &domain.ConfineDemoteError{
		Detail: fmt.Sprintf(
			"console %d was opened unconfined; close it with console_close and reopen it with"+
				" console_open to run it fenced",
			id,
		),
		Reason: fmt.Sprintf(consoleUnfencedSendReasonFormat, id),
		Remedy: consoleUnfencedSendRemedy,
	}
}

var (
	_ domain.Tool           = (*ConsoleSend)(nil)
	_ domain.SubprocessTool = (*ConsoleSend)(nil)
	_ domain.DefaultOffTool = (*ConsoleSend)(nil)
	_ domain.ApprovalScoper = (*ConsoleSend)(nil)
)

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/airiclenz/apogee/internal/console"
	"github.com/airiclenz/apogee/internal/domain"
)

var consoleCloseSpec = toolSpec{
	name: "console_close",
	description: "Close a console opened with console_open: its program and everything that program started are" +
		" stopped, its id is retired, and whatever it printed but nobody read comes back with how it ended." +
		" Read-only: never prompts. Close a console as soon as its program has nothing left to do — only a" +
		" few can be open at once.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["id"],
  "properties": {
    "id": {"type": "integer", "description": "The console id returned by console_open"}
  }
}`),
}

type consoleCloseArgs struct {
	ID consoleID `json:"id"`
}

// ConsoleClose ends a Console: it stops the process, hands back the tail nobody read and retires
// the id (ADR 0059).
//
// It is READ-ONLY beside console_read, which reads oddly for a tool that kills a process and is
// nonetheless right: the blast radius the class measures is what a call leaves BEHIND, and
// closing a Console leaves strictly less behind than opening one did. Stopping something the
// model itself started is the one act in the family that never needs supervision — a mode that
// would refuse it is a mode where a forgotten shell can only accumulate — so Plan admits it with
// no engine code of its own (planAdmits, internal/agent/resolution.go).
//
// It is DEFAULT-OFF beside the rest of the family (ADR 0057).
type ConsoleClose struct {
	toolSpec
}

// NewConsoleClose returns a console_close tool. It takes no root and no credential names: the
// Console it ends was fenced when console_open opened it.
func NewConsoleClose() *ConsoleClose {
	return &ConsoleClose{toolSpec: consoleCloseSpec}
}

// ReadOnly reports the read-only floor (true) — see the type comment: ending a process the model
// started leaves less behind than starting it did.
func (t *ConsoleClose) ReadOnly() bool { return true }

// DefaultOff reports that console_close ships registered but off the default menu (ADR 0057).
func (t *ConsoleClose) DefaultOff() bool { return true }

// Execute kills the Console's process group, waits for it to be reaped, and returns the output
// nobody had read yet together with how the process ended.
//
// The tail is taken AFTER the teardown rather than before it, which is what makes it the whole
// tail: the registry's close waits for the process's output to finish draining into the buffer,
// so a program that printed a parting line — or one the kill itself ended — is read out here
// instead of being lost between the drain and the signal.
//
// An unknown or already-closed id is an error RESULT naming the ids that are open, per the
// family's contract; only ctx cancellation is a Go error.
func (t *ConsoleClose) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[consoleCloseArgs](call)
	if !ok {
		return fail, nil
	}

	registry := console.FromContext(ctx)
	target, fail, ok := lookupConsole(ctx, call.ID, int(args.ID))
	if !ok {
		return fail, nil
	}

	// lookupConsole reports a Console only when it FOUND one in this context's registry, so
	// registry is the one holding target and is never nil on this path.
	closeErr := registry.Close(target.ID)
	tail := consoleTail(ctx, target, 0)
	if closeErr != nil {
		// The Console is gone from the registry whatever the teardown said, so this is not a
		// call to retry: an error here is the pseudo-terminal refusing to be released, not a
		// program still running. It rides ALONGSIDE the tail rather than replacing it — the
		// tail is the last thing this Console will ever say — and it is not swallowed, because
		// a host leaking terminals is something the transcript should show.
		tail = fmt.Sprintf("console %d was stopped, but releasing its terminal failed: %v\n%s",
			target.ID, closeErr, tail)
	}
	return okResult(call.ID, tail), nil
}

var (
	_ domain.Tool           = (*ConsoleClose)(nil)
	_ domain.ReadOnlyTool   = (*ConsoleClose)(nil)
	_ domain.DefaultOffTool = (*ConsoleClose)(nil)
)

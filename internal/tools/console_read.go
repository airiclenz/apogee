package tools

import (
	"context"
	"encoding/json"

	"github.com/airiclenz/apogee/internal/domain"
)

var consoleReadSpec = toolSpec{
	name: "console_read",
	description: "Read what a console opened with console_open has printed since it was last read or sent to," +
		" without typing anything into it. Read-only: never prompts. Use it to poll a running program — set" +
		" wait_ms to wait for new output instead of calling this in a loop.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["id"],
  "properties": {
    "id": {"type": "integer", "description": "The console id returned by console_open"},
    "wait_ms": {"type": "integer", "description": "Optional milliseconds to wait for NEW output, returning as soon as some arrives (default 0: return whatever is waiting right now; max 30000)"}
  }
}`),
}

type consoleReadArgs struct {
	ID     consoleID `json:"id"`
	WaitMS int       `json:"wait_ms"`
}

// ConsoleRead reports what a Console has printed since the model last heard from it (ADR 0059).
//
// It is the family's READ-ONLY half together with console_close: it types nothing, starts
// nothing, and touches no file, so it takes the read-only floor and Plan mode admits it with no
// engine code of its own (planAdmits, internal/agent/resolution.go) — a model planning in Plan
// can watch a dev server that an earlier mode started, which is exactly the read a plan is made
// of. Its write-capable siblings console_open and console_send carry the Subprocess marker and
// Plan refuses them (ADR 0059 §2).
//
// It is DEFAULT-OFF beside the rest of the family (ADR 0057).
type ConsoleRead struct {
	toolSpec
}

// NewConsoleRead returns a console_read tool. It takes no root and no credential names: it reads
// a buffer the Console already filled, and reaches neither the filesystem nor a new process.
func NewConsoleRead() *ConsoleRead {
	return &ConsoleRead{toolSpec: consoleReadSpec}
}

// ReadOnly reports that console_read only reads (true): the bytes it returns were produced
// before the call, by a program the Console was already running.
func (t *ConsoleRead) ReadOnly() bool { return true }

// DefaultOff reports that console_read ships registered but off the default menu (ADR 0057).
func (t *ConsoleRead) DefaultOff() bool { return true }

// Execute returns the Console's unread output and whether its process is still running.
//
// An unknown id is an error RESULT naming the ids that are open — the one thing a model that has
// lost track of its consoles can act on. Only ctx cancellation is a Go error: reading a buffer
// starts no process, so there is no confinement demotion to make.
func (t *ConsoleRead) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[consoleReadArgs](call)
	if !ok {
		return fail, nil
	}

	target, fail, ok := lookupConsole(ctx, call.ID, int(args.ID))
	if !ok {
		return fail, nil
	}

	wait := consoleWait(args.WaitMS, consoleReadWaitDefaultMS, consoleReadWaitMaxMS)
	return okResult(call.ID, consoleTail(ctx, target, wait)), nil
}

var (
	_ domain.Tool           = (*ConsoleRead)(nil)
	_ domain.ReadOnlyTool   = (*ConsoleRead)(nil)
	_ domain.DefaultOffTool = (*ConsoleRead)(nil)
)

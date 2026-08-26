package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/airiclenz/apogee/internal/console"
	"github.com/airiclenz/apogee/internal/domain"
)

var consoleOpenSpec = toolSpec{
	name: "console_open",
	description: "Open a PERSISTENT interactive program — a REPL, a dev server, a database shell — under a" +
		" pseudo-terminal and leave it running across Turns. Returns a console id: type into it with" +
		" console_send, poll it with console_read, and end it with console_close. Use terminal instead for a" +
		" one-shot command that runs to completion on its own.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["command"],
  "properties": {
    "command": {"type": "string", "description": "The command line to start, run through the platform shell (POSIX sh) inside a pseudo-terminal. Supports pipes, redirection, and globs."},
    "workdir": {"type": "string", "description": "Optional working directory (relative to the workspace root or absolute)"},
    "wait_ms": {"type": "integer", "description": "Optional milliseconds to collect the program's first output before returning (default 500, max 10000)"}
  }
}`),
}

type consoleOpenArgs struct {
	Command string `json:"command"`
	Workdir string `json:"workdir"`
	WaitMS  int    `json:"wait_ms"`
}

// consoleTermVar is the terminal type every Console's program is told it is talking to. `dumb`
// asks for the plainest output a program knows how to produce: the Console strips the escape
// sequences it gets anyway (console.stripEscapes), and a program that never emits them in the
// first place spends none of the model's context on cursor motion it cannot see.
const consoleTermVar = "TERM=dumb"

// openConsole starts the Console a call assembled (a package var so a test can capture the exact
// argv, environment and confinement this tool builds — the seam runTerminalSubprocess already is
// for the one-shot tools).
var openConsole = func(registry *console.Registry, spec console.OpenSpec) (*console.Console, error) {
	return registry.Open(spec)
}

// ConsoleOpen starts a persistent interactive program under a pseudo-terminal and hands the model
// an id to drive it by (ADR 0059). Where terminal is one process per call — ADR 0008's stateless
// floor — a Console is LIVE HOST STATE held on the engine: it outlives the Turn that opened it,
// keeps its shell's cwd, its REPL's bindings and its dev server's port, and dies with the process
// or the delegation that owns it, never with a session snapshot.
//
// It is a SubprocessTool (domain.SubprocessTool): the dispatch disposition runs it under
// Confiner.Confine in Auto with confine-to-workspace on and gates it through Approval when
// fs-confinement is unavailable, exactly as terminal is treated — a shell that stays open is at
// least as much command execution as one that does not. The Resolution is taken per call, so the
// mode a Console was opened under never becomes a standing permission: every console_send is
// resolved again on its own (ADR 0059 §4).
//
// It is DEFAULT-OFF (domain.DefaultOffTool, ADR 0057): the family costs four slots in every
// model's tool list, and a model that never needs an interactive program should not pay for them.
// A `tools.enabled:` entry or a profile roster axis lifts it.
//
// Like terminal, the program runs in the operator's own environment minus apogee's credentials
// and with its PATH scoped out of the workspace (subprocessEnvScopedPath), and its working
// directory is path-scoped to root. Unlike terminal there is NO fail-fast preamble: `set -e` ends
// a script at its first failure, and a shell that exits the moment a typed command fails is not a
// console. The live kill-on-denial watch still applies to a confined Console, which is what stops
// a fenced program that was denied instead of leaving it running blind (ADR 0056 §2).
type ConsoleOpen struct {
	toolSpec
	root string
	// secretEnv names the host-configured credential variables to drop from the child's
	// environment beside apogee's own (HostTools.SecretEnvVars); nil drops apogee's own alone.
	secretEnv []string
}

// NewConsoleOpen returns a console_open tool whose working directory resolves within root and
// whose child environment drops the secretEnv variables on top of apogee's own credentials (nil ⇒
// apogee's own alone) — the same construction terminal takes, for the same reasons.
func NewConsoleOpen(root string, secretEnv []string) *ConsoleOpen {
	return &ConsoleOpen{toolSpec: consoleOpenSpec, root: root, secretEnv: secretEnv}
}

// ReadOnly reports that console_open is write-capable (false): the program it starts is a shell,
// so the loop must gate or confine it rather than running it freely.
func (t *ConsoleOpen) ReadOnly() bool { return false }

// Subprocess reports that console_open launches an OS subprocess — the marker the disposition
// keys on to confine it in Auto rather than gating it (domain.SubprocessTool).
func (t *ConsoleOpen) Subprocess() bool { return true }

// DefaultOff reports that console_open ships registered but off the default menu (ADR 0057): the
// Console family is enabled by configuration for the models that want it, not by every roster.
func (t *ConsoleOpen) DefaultOff() bool { return true }

// Execute starts the command under a pseudo-terminal and registers it as a Console, returning the
// id and whatever the program printed inside the wait window.
//
// A command line the shell could not parse, a working directory that escapes the root, a shell
// that resolves inside the workspace, the open-console cap and a platform with no pseudo-terminal
// backend are all surfaced to the model as error RESULTS — every one of them is something it can
// act on. Only ctx cancellation and a confinement-unavailable demotion are Go errors, the
// terminal convention: the second is what makes the disposition gate the call instead of leaving
// an unfenced shell running past the Turn that asked for it.
func (t *ConsoleOpen) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[consoleOpenArgs](call)
	if !ok {
		return fail, nil
	}
	if strings.TrimSpace(args.Command) == "" {
		return errorResult(call.ID, "command is required"), nil
	}
	// The pre-flight is derived from the raw command line the platform would hand the shell,
	// exactly as terminal's is: empty means a real argv (POSIX sh -c) and a POSIX splitter is
	// the right second opinion; non-empty means cmd.exe, which has none worth giving.
	if err := preflightCommandLine(args.Command, shellHost.CommandLine(args.Command) == ""); err != nil {
		return errorResult(call.ID, "could not parse command line: "+err.Error()), nil
	}

	dir, err := resolveWorkdirInRoot(args.Workdir, t.root)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	argv, fail, ok := t.consoleArgv(ctx, call.ID, args.Command)
	if !ok {
		return fail, nil
	}

	registry := console.FromContext(ctx)
	if registry == nil {
		return errorResult(call.ID, "consoles are not available in this session"), nil
	}

	prepare, confined, err := consolePrepare(ctx, argv[0])
	if err != nil {
		return domain.ToolResult{}, err
	}

	opened, err := openConsole(registry, console.OpenSpec{
		// A delegation owns what it opened: when the sub-agent ends, its Consoles are closed
		// and the parent's are not (ADR 0059 §6). Empty at the top level.
		Owner:    domain.SpawnCallIDFromContext(ctx),
		Command:  args.Command,
		Argv:     argv,
		Dir:      dir,
		Env:      subprocessEnvScopedPath(t.root, t.secretEnv, consoleTermVar),
		Confined: confined,
		Prepare:  prepare,
	})
	if err != nil {
		if errors.Is(err, domain.ErrConfinementUnavailable) {
			// Fail closed the way runSubprocess does: the loop demotes the call to Approval
			// rather than leaving an unfenced shell alive past this Turn.
			return domain.ToolResult{}, err
		}
		return errorResult(call.ID, "could not open a console: "+err.Error()), nil
	}

	header := fmt.Sprintf("console %d opened: %s", opened.ID, args.Command)
	tail := consoleOpenTail(ctx, opened, consoleWait(args.WaitMS, consoleOpenWaitDefaultMS, consoleOpenWaitMaxMS))
	if tail == "" {
		return okResult(call.ID, header), nil
	}
	return okResult(call.ID, header+"\n"+tail), nil
}

// consoleArgv resolves the argv the Console actually runs: the platform shell wrapped around the
// model's command line, with the shell resolved to an absolute program.
//
// The resolution and the fence are the shared ones (shellArgv, exec_common.go): the platform
// hands back a BARE "sh", and the fence measures argv[0] against the writable box — a bare name
// would be measured against apogee's own working directory, which is the workspace itself, and
// every open would be refused. Resolving first also puts the fence where it belongs: on the
// program PATH actually leads to, so an `sh` planted inside the workspace is refused by name
// rather than executed.
func (t *ConsoleOpen) consoleArgv(ctx context.Context, callID, command string) ([]string, domain.ToolResult, bool) {
	argv, err := shellArgv(ctx, t.root, command)
	if err != nil {
		return nil, errorResult(callID, err.Error()), false
	}
	return argv, domain.ToolResult{}, true
}

// consolePrepare returns the hook that fences a Console's command before it starts, and whether
// the Console will run confined. No handle on ctx means an unconfined run — the
// `confine-to-workspace: false` opt-in and the gated-then-approved case, where the Resolution
// already decided — and nil Prepare is how the process layer spells "nothing to prepare".
//
// A handle carrying no Confiner is broken wiring rather than permission to run free: it fails
// CLOSED as ErrConfinementUnavailable, the same refusal runSubprocess makes, so the escape
// surfaces as a truthful demote instead of an unfenced shell nobody gated.
func consolePrepare(ctx context.Context, program string) (func(*exec.Cmd) error, bool, error) {
	handle, ok := domain.ConfinementFromContext(ctx)
	if !ok {
		return nil, false, nil
	}
	if handle.Confiner == nil {
		return nil, false, fmt.Errorf("confine %s: %w: the installed handle carries no Confiner",
			program, domain.ErrConfinementUnavailable)
	}
	return func(cmd *exec.Cmd) error {
		if err := handle.Confiner.Confine(ctx, handle.Box, cmd); err != nil {
			return fmt.Errorf("confine %s: %w", program, err)
		}
		return nil
	}, true, nil
}

var (
	_ domain.Tool           = (*ConsoleOpen)(nil)
	_ domain.SubprocessTool = (*ConsoleOpen)(nil)
	_ domain.DefaultOffTool = (*ConsoleOpen)(nil)
)

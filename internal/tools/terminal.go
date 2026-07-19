package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/shlex"

	"github.com/airiclenz/apogee/internal/domain"
)

var terminalSpec = toolSpec{
	name:        "terminal",
	description: "Run a shell command line and capture its output and exit code. One-shot (a fresh process per call); supports pipes, redirection, and globbing through the platform shell.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["command"],
  "properties": {
    "command": {"type": "string", "description": "The shell command line to run (POSIX sh on Unix, cmd on Windows). Supports pipes, redirection, and globs."},
    "workdir": {"type": "string", "description": "Optional working directory (relative to the workspace root or absolute)"},
    "timeout_seconds": {"type": "integer", "description": "Optional timeout in seconds (default 120, max 600)"}
  }
}`),
}

type terminalArgs struct {
	Command        string `json:"command"`
	Workdir        string `json:"workdir"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// Terminal runs a one-shot shell command line through the platform shell (sh -c on POSIX,
// cmd /c on Windows) and captures its combined output and exit code. It is a SubprocessTool
// (domain.SubprocessTool): the dispatch disposition runs it under Confiner.Confine in Auto
// with confine-to-workspace on, and gates it through Approval when fs-confinement is
// unavailable ("confine if you can, gate if you can't"). It is stateless across Turns
// (ADR 0008) — a fresh process per call, no persistent shell — and is path-scoped to root
// for its working directory.
type Terminal struct {
	toolSpec
	root string
}

// NewTerminal returns a terminal tool whose working directory resolves within root.
func NewTerminal(root string) *Terminal { return &Terminal{toolSpec: terminalSpec, root: root} }

// ReadOnly reports that terminal is write-capable (false) — a shell command can write, so
// the loop must gate/confine it rather than running it freely.
func (t *Terminal) ReadOnly() bool { return false }

// Subprocess reports that terminal launches an OS subprocess — the marker the disposition
// keys on to confine it in Auto rather than gating it (domain.SubprocessTool).
func (t *Terminal) Subprocess() bool { return true }

// Execute runs the command line through the platform shell, honouring ctx cancellation and
// the confinement handle the disposition installed (if any). An unparseable command line, a
// working directory that escapes the root, or a non-zero exit are surfaced to the model as
// results; only ctx cancellation or a confinement-unavailable demotion is a Go error.
func (t *Terminal) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[terminalArgs](call)
	if !ok {
		return fail, nil
	}
	if strings.TrimSpace(args.Command) == "" {
		return errorResult(call.ID, "command is required"), nil
	}
	// Validate the command line is POSIX-parseable (balanced quotes, etc.) before handing
	// it to the shell, so an obviously malformed command fails with a clear message rather
	// than a confusing shell error. shlex is the POSIX command-line splitter (plan §3a).
	if _, err := shlex.Split(args.Command); err != nil {
		return errorResult(call.ID, "could not parse command line: "+err.Error()), nil
	}

	dir, err := t.resolveWorkdir(args.Workdir)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	spec := subprocessSpec{
		argv:    shellHost.Command(args.Command),
		dir:     dir,
		timeout: time.Duration(args.TimeoutSeconds) * time.Second,
	}
	res, err := runSubprocess(ctx, spec)
	if err != nil {
		return domain.ToolResult{}, err
	}
	return subprocessToolResult(call.ID, res), nil
}

// resolveWorkdir resolves the optional working directory within the root (path-safe), or
// returns the root itself when none is given.
func (t *Terminal) resolveWorkdir(workdir string) (string, error) {
	if workdir == "" {
		return t.root, nil
	}
	return resolveInRoot(workdir, t.root)
}

// subprocessToolResult renders a captured subprocess outcome as a ToolResult. A non-zero
// exit is an error result (so the model sees the command failed) carrying the captured
// output and exit code; a clean exit is a success result with the output.
func subprocessToolResult(callID string, res subprocessResult) domain.ToolResult {
	var b strings.Builder
	if res.timedOut {
		b.WriteString("command timed out\n")
	}
	b.WriteString(res.combinedOutput)
	if res.exitCode != 0 {
		fmt.Fprintf(&b, "\n[exit code %d]", res.exitCode)
		return errorResult(callID, b.String())
	}
	return okResult(callID, b.String())
}

var (
	_ domain.Tool           = (*Terminal)(nil)
	_ domain.SubprocessTool = (*Terminal)(nil)
)

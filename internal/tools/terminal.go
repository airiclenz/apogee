package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/shlex"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/platform"
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
// for its working directory. The command line runs in the operator's own environment, minus
// apogee's own credentials and with its PATH scoped out of the workspace
// (subprocessEnvScopedPath): a shell line the model chose has no use for the key apogee
// authenticates to its inference server with, and must not resolve its programs out of the box
// the model can write.
//
// On the POSIX path every script runs under a fail-fast preamble
// (platform.FailFastPreamble: `set -e`, plus `set -o pipefail` where the host sh accepts
// it), so a failed plain command aborts the whole script instead of letting an unguarded
// later line run against a half-done state. `set -e` does NOT cover a failure inside an
// AND-OR list other than its last command (POSIX exempts them), so a denied
// `mkdir d && cd d && …` chain still falls through to the lines after it — the 2026-08-22
// incident's shape; that gap is closed by the live kill-on-denial watch every CONFINED
// run is wired through (platform.DenialKillWriter in runSubprocess), which kills the
// process group at the first OS-denial signature. Windows is asymmetric by necessity:
// cmd.exe has no `set -e` analogue (`if errorlevel` is per-line, not a mode), so cmd
// lines pass through verbatim with no fail-fast floor — and no denial watch either (its
// denials print "Access is denied.", which the POSIX signature set deliberately skips).
type Terminal struct {
	toolSpec
	root string
	// secretEnv names the host-configured credential variables to drop from the child's
	// environment beside apogee's own (HostTools.SecretEnvVars); nil drops apogee's own alone.
	secretEnv []string
}

// NewTerminal returns a terminal tool whose working directory resolves within root and whose
// child environment drops the secretEnv variables on top of apogee's own credentials (nil ⇒
// apogee's own alone — the scrub as it was before the host could name any).
func NewTerminal(root string, secretEnv []string) *Terminal {
	return &Terminal{toolSpec: terminalSpec, root: root, secretEnv: secretEnv}
}

// ReadOnly reports that terminal is write-capable (false) — a shell command can write, so
// the loop must gate/confine it rather than running it freely.
func (t *Terminal) ReadOnly() bool { return false }

// Subprocess reports that terminal launches an OS subprocess — the marker the disposition
// keys on to confine it in Auto rather than gating it (domain.SubprocessTool).
func (t *Terminal) Subprocess() bool { return true }

// runTerminalSubprocess runs the shell command (a package var so a test can capture the exact
// argv and environment this tool builds without launching one — the shape python_exec and
// run_tests already use).
var runTerminalSubprocess = runSubprocess

// Execute runs the command line through the platform shell, honouring ctx cancellation and
// the confinement handle the disposition installed (if any). A command line the target shell
// could not parse (preflightCommandLine — POSIX sh only), a working directory that escapes
// the root, or a non-zero exit are surfaced to the model as results; only ctx cancellation
// or a confinement-unavailable demotion is a Go error.
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
	// The line is handed to the shell verbatim where the platform needs it (Windows:
	// os/exec's argv joining would escape the model's quotes into cmd.exe's face). That
	// raw command line is also what says WHICH shell is about to read the line, so the
	// pre-flight below is derived from it rather than from a second OS switch.
	cmdline := shellHost.CommandLine(args.Command)
	if err := preflightCommandLine(args.Command, cmdline == ""); err != nil {
		return errorResult(call.ID, "could not parse command line: "+err.Error()), nil
	}

	dir, err := resolveWorkdirInRoot(args.Workdir, t.root)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	// Fail fast, POSIX only (cmdline == "" is the same convention the pre-flight keys
	// on): the preamble goes in AFTER the pre-flight parsed the model's own line, so a
	// parse verdict is about what the model wrote, and cmd.exe — which has no `set -e`
	// analogue — gets the line verbatim.
	command := args.Command
	if cmdline == "" {
		command = platform.FailFastPreamble() + command
	}

	spec := subprocessSpec{
		argv:    shellHost.Command(command),
		cmdline: cmdline,
		dir:     dir,
		timeout: time.Duration(args.TimeoutSeconds) * time.Second,
		// The command line runs in the operator's own environment — minus the credential
		// variables, which a model-chosen command line has no use for and could exfiltrate,
		// and minus the PATH entries that resolve inside the workspace, which would let the
		// model plant the programs its own command line then executes.
		env: subprocessEnvScopedPath(t.root, t.secretEnv),
	}
	res, err := runTerminalSubprocess(ctx, spec)
	if err != nil {
		return domain.ToolResult{}, err
	}
	return subprocessToolResult(call.ID, res), nil
}

// preflightCommandLine reports why the target shell could not parse command, so an
// obviously malformed line fails with a clear message rather than a confusing shell error.
//
// The gate is POSIX-only, and posix says which shell the line is bound for: it is derived
// from platform.Shell.CommandLine, which is empty exactly where the platform hands the
// shell a real argv (sh -c) and non-empty where the line is delivered verbatim to cmd.exe
// (exec_cmdline_other.go). shlex is the POSIX splitter — it is a parser for a DIFFERENT
// language than cmd's, and running it over a cmd line rejects ordinary, valid input:
// `echo don't panic` reads as an unterminated single quote, and `dir "C:\Program Files\"`
// as an escaped quote that never closes. cmd.exe has no stable quoting grammar worth
// pre-parsing (its rules differ per built-in, and a trailing backslash, a caret, a `%VAR%`
// and an unbalanced quote are all legal), so there is deliberately no cmd pre-flight: cmd
// reports its own errors, which is strictly better than a wrong second opinion.
func preflightCommandLine(command string, posix bool) error {
	if !posix {
		return nil
	}
	_, err := shlex.Split(command)
	return err
}

// subprocessToolResult renders a captured subprocess outcome as a ToolResult. A non-zero
// exit is an error result (so the model sees the command failed) carrying the captured
// output and exit code; a clean exit is a success result with the output. An error result
// the kill-on-denial watch stopped carries confinementDenialStopLabel; any other error
// result from a CONFINED run whose output looks like an OS denial carries
// confinementDenialLabel — both best-effort, never forced onto a clean exit.
func subprocessToolResult(callID string, res subprocessResult) domain.ToolResult {
	var b strings.Builder
	if res.timedOut {
		b.WriteString("command timed out\n")
	}
	if res.drainWedged {
		// The exit code alone cannot say this: the leader may have exited 0 and left the
		// pipe held by something else, which runSubprocess reports as -1 rather than as a
		// success. Name the reason so the reader is not left guessing at the code.
		b.WriteString("output was cut short: something the command left running still held the pipe and was killed\n")
	}
	b.WriteString(res.combinedOutput)
	if res.exitCode != 0 {
		fmt.Fprintf(&b, "\n[exit code %d]", res.exitCode)
		switch {
		case res.denialStopped:
			b.WriteString("\n" + confinementDenialStopLabel)
		case res.confined && platform.LooksLikeConfinementDenial(res.combinedOutput):
			b.WriteString("\n" + confinementDenialLabel)
		}
		return errorResult(callID, b.String())
	}
	return okResult(callID, b.String())
}

var (
	_ domain.Tool           = (*Terminal)(nil)
	_ domain.SubprocessTool = (*Terminal)(nil)
)

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
	"github.com/airiclenz/apogee/internal/subprocess"
)

var terminalSpec = toolSpec{
	name:        "terminal",
	description: "Run a shell command line and capture its output and exit code. One-shot (a fresh process per call); supports pipes, redirection, and globbing through the platform shell. On POSIX the line runs fail-fast (`set -e`, and `pipefail` where the shell supports it): the first command that exits non-zero stops the rest of the line, so guard expected non-zero exits (`grep … || true`). The shell is POSIX sh (dash on Debian-family hosts — no bash arrays, [[ ]] or process substitution); use python_exec for anything bash-only.",
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
// later line run against a half-done state. The preamble is DISCLOSED rather than silent:
// the tool description says the POSIX line runs fail-fast and how to guard an expected
// non-zero exit, and a failed run's exit-code line repeats it at the point of failure
// (failFastExitNote; under bash the note also names the command that stopped the line,
// failFastStoppedLine) — a script that aborted early prints nothing about having done so, and
// the reviewed 2026-08-25 session shows a model spending six calls re-running one. `set -e` does NOT cover a failure inside an
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
	// host is the operating system the tool launches through: the platform shell it wraps the
	// line with and scopes the child's environment through (execHost).
	host execHost
}

// NewTerminal returns a terminal tool whose working directory resolves within root and whose
// child environment drops the secretEnv variables on top of apogee's own credentials (nil ⇒
// apogee's own alone — the scrub as it was before the host could name any). It runs on the real
// operating system (defaultExecHost); builtinTools builds the five execution tools on one host
// through newTerminal.
func NewTerminal(root string, secretEnv []string) *Terminal {
	return newTerminal(root, secretEnv, defaultExecHost())
}

// newTerminal is NewTerminal with the host the tool launches through supplied — one execHost
// shared by the execution tools in production, a host carrying fakes in a test.
func newTerminal(root string, secretEnv []string, host execHost) *Terminal {
	return &Terminal{toolSpec: terminalSpec, root: root, secretEnv: secretEnv, host: host}
}

// ReadOnly reports that terminal is write-capable (false) — a shell command can write, so
// the loop must gate/confine it rather than running it freely.
func (t *Terminal) ReadOnly() bool { return false }

// Subprocess reports that terminal launches an OS subprocess — the marker the disposition
// keys on to confine it in Auto rather than gating it (domain.SubprocessTool).
func (t *Terminal) Subprocess() bool { return true }

// ShellCommandKeys declares `command` as the shell command line this tool hands to the shell —
// the marker (domain.ShellCommandTool) that lets a write-shaped dangerous-action rule judge what
// the line writes rather than every word it names.
func (t *Terminal) ShellCommandKeys() []string { return []string{"command"} }

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
	cmdline := t.host.shell.CommandLine(args.Command)
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
	failFast := cmdline == ""
	if failFast {
		command = platform.FailFastPreamble() + command
	}

	// The shell is resolved to an absolute program and fenced before it becomes argv[0]
	// (shellArgv): a refusal names the resolved path, so the operator reads which PATH entry
	// to fix and the model reads a refusal rather than "not available". The Windows raw
	// command line is unaffected — argv[0] is now the absolute cmd.exe and the verbatim line
	// is still what cmd reads (internal/subprocess/cmdline_other.go).
	argv, err := t.host.shellArgv(ctx, t.root, command)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	spec := subprocess.SubprocessSpec{
		Argv:     argv,
		Cmdline:  cmdline,
		Dir:      dir,
		Timeout:  time.Duration(args.TimeoutSeconds) * time.Second,
		FailFast: failFast,
		// The command line runs in the operator's own environment — minus the credential
		// variables, which a model-chosen command line has no use for and could exfiltrate,
		// and minus the PATH entries that resolve inside the workspace, which would let the
		// model plant the programs its own command line then executes.
		Env: t.host.subprocessEnvScopedPath(t.root, t.secretEnv),
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
// (internal/subprocess/cmdline_other.go). shlex is the POSIX splitter — it is a parser for a DIFFERENT
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

// failFastExitNote is what the exit-code line adds when the run was launched under the
// fail-fast preamble (subprocess.SubprocessResult.FailFast): the non-zero code may be `set -e` stopping
// the line at its first failed command, which is invisible in the output — the aborted lines
// simply never printed. Naming it at the point of failure is what a small model acts on; a
// sentence in the tool description a dozen calls back is not (ratified design call 5).
const failFastExitNote = " — fail-fast: the line stopped at the first command that failed;" +
	" guard expected non-zero exits with `|| true`"

// failFastStoppedLine is the line that names the command `set -e` stopped at, rendered above
// the exit-code line from the `failed at: <cmd>` line bash's ERR trap in the preamble left as
// the LAST line of the output (platform.FailFastStopPrefix; only bash installs that trap, so
// under dash the generic note stands alone). It goes on its own line BEFORE the bracketed
// marker, never inside it: a `]` in the quoted command (`[ -f missing ]`, `ls x[1]`) would
// otherwise break the marker the TUI reads the exit code from (internal/tui exitCodeMarker).
const failFastStoppedLine = "fail-fast: the line stopped at `%s`"

// isFailFastStop reports whether a failed run's exit was `set -e` stopping the line at a failed
// command, which is when the fail-fast note applies. A timeout, a signalled exit (-1) and a run
// the kill-on-denial watch stopped were not stopped by `set -e`, whatever the preamble said:
// blaming fail-fast for a confinement kill would send the model guarding a command that was
// refused, not failed.
func isFailFastStop(res subprocess.SubprocessResult) bool {
	return res.FailFast && !res.TimedOut && !res.DenialStopped && res.ExitCode > 0
}

// splitFailFastStop takes the preamble's `failed at: <cmd>` line off the end of output. It
// returns the command it named and the output without that line, or ok=false — leaving output
// untouched — when the last line is not the trap's: the trap prints at the moment the script
// stops, so its line is the last one written, and anything after it (a background child still
// writing, a cap notice) means the run did not end the way the trap describes.
func splitFailFastStop(output string) (command, rest string, ok bool) {
	trimmed := strings.TrimSuffix(output, "\n")
	start := strings.LastIndexByte(trimmed, '\n') + 1
	last := trimmed[start:]
	if !strings.HasPrefix(last, platform.FailFastStopPrefix) {
		return "", output, false
	}
	command = strings.TrimPrefix(last, platform.FailFastStopPrefix)
	if command == "" {
		return "", output, false
	}
	return command, trimmed[:start], true
}

// cwdLinePrefix opens the first line of every result a subprocess tool renders from a run
// that has a working directory: `cwd: /path/to/dir`. The line says what the command's own
// relative paths were relative to — the workspace root, or the `workdir` the call named — so a
// model reading `./build/out` in the output knows where that is without a second call, and a
// model that changed directories inside the line is reminded it did not change where the NEXT
// call starts. StripCwdLine is its one reader on the host side; the model reads it as text.
const cwdLinePrefix = "cwd: "

// StripCwdLine takes the `cwd:` line (cwdLinePrefix) off the front of a terminal or python_exec
// result's content and returns the rest, or content unchanged when no such line opens it. It is
// the ONE strip every host-side consumer of that content shares — the TUI's success detail, the
// TUI's failure body and headless narration — so the three cannot drift into different readings
// of where the output begins: the line is written for the model, and a card or a narration line
// that already names the command has nothing to gain from repeating the directory above its
// first line of output.
func StripCwdLine(content string) string {
	if !strings.HasPrefix(content, cwdLinePrefix) {
		return content
	}
	if _, rest, found := strings.Cut(content, "\n"); found {
		return rest
	}
	return ""
}

// shellHintLine is the line a failed result gains when its output carries one of the tell-tale
// complaints sh makes about a bash-only construct (bashismSignatures): the model wrote bash,
// and the description's disclosure of the shell is a dozen calls back. It is rendered on its
// own line ABOVE the exit-code marker, never inside the brackets, for the reason
// failFastStoppedLine is (the TUI reads the code out of the last bracketed line).
const shellHintLine = "hint: the shell is sh, not bash"

// bashismSignatures are the stderr fragments dash (and any POSIX sh) prints when handed a
// bash-only construct: `${var//x/y}` and `${!ref}` ("Bad substitution"), `<(cmd)` and `arr=(a b)`
// (`Syntax error: "(" unexpected`) and `shopt` itself. Each is matched as a substring of the
// captured output, so the exact wording of the surrounding line does not matter.
var bashismSignatures = []string{
	"Bad substitution",
	`Syntax error: "(" unexpected`,
	"shopt: not found",
}

// looksLikeBashism reports whether a failed run's output carries one of bashismSignatures.
func looksLikeBashism(output string) bool {
	for _, signature := range bashismSignatures {
		if strings.Contains(output, signature) {
			return true
		}
	}
	return false
}

// subprocessToolResult renders a captured subprocess outcome as a ToolResult. A result from a
// run that had a working directory (subprocess.SubprocessResult.Dir) opens with the `cwd:` line
// (cwdLinePrefix); one built with no dir opens with the output itself. A non-zero
// exit is an error result (so the model sees the command failed) carrying the captured
// output and exit code; a clean exit is a success result with the output. A failed run whose
// output carries a bash-ism complaint (looksLikeBashism) says the shell is sh on the line above
// the exit-code line (shellHintLine). A failed run that
// `set -e` stopped (isFailFastStop) says so inside the exit-code line (failFastExitNote) and,
// where the preamble's bash-only trap named the command, on the line above it
// (failFastStoppedLine) — a timeout, a signalled exit or a denial kill gets neither. An error
// result the kill-on-denial watch stopped carries confinementDenialStopLabel; any other error
// result from a CONFINED run whose output looks like an OS denial carries
// confinementDenialLabel — both best-effort, never forced onto a clean exit, and both still
// follow on their own line after the exit-code line. Both are rendered from the box the run
// was fenced by (subprocess.SubprocessResult.Box), so the model reads the writable roots by path.
func subprocessToolResult(callID string, res subprocess.SubprocessResult) domain.ToolResult {
	var b strings.Builder
	if res.Dir != "" {
		b.WriteString(cwdLinePrefix + res.Dir + "\n")
	}
	if res.TimedOut {
		b.WriteString("command timed out\n")
	}
	if res.DrainWedged {
		// The exit code alone cannot say this: the leader may have exited 0 and left the
		// pipe held by something else, which runSubprocess reports as -1 rather than as a
		// success. Name the reason so the reader is not left guessing at the code.
		b.WriteString("output was cut short: something the command left running still held the pipe and was killed\n")
	}
	output, note, stoppedAt := res.CombinedOutput, "", ""
	if isFailFastStop(res) {
		note = failFastExitNote
		if command, rest, ok := splitFailFastStop(output); ok {
			output, stoppedAt = rest, command
		}
	}
	b.WriteString(output)
	if res.ExitCode != 0 {
		if looksLikeBashism(res.CombinedOutput) {
			b.WriteString("\n" + shellHintLine)
		}
		if stoppedAt != "" {
			fmt.Fprintf(&b, "\n"+failFastStoppedLine, stoppedAt)
		}
		fmt.Fprintf(&b, "\n[exit code %d%s]", res.ExitCode, note)
		switch {
		case res.DenialStopped:
			b.WriteString("\n" + confinementDenialStopLabel(res.Box))
		case res.Confined && platform.LooksLikeConfinementDenial(res.CombinedOutput):
			b.WriteString("\n" + confinementDenialLabel(res.Box))
		}
		return errorResult(callID, b.String())
	}
	return okResult(callID, b.String())
}

var (
	_ domain.Tool           = (*Terminal)(nil)
	_ domain.SubprocessTool = (*Terminal)(nil)
)

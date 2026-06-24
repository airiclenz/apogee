package tools

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

var pythonExecSchema = json.RawMessage(`{
  "type": "object",
  "required": ["code"],
  "properties": {
    "code": {"type": "string", "description": "The Python source to run. It is fed to the interpreter on standard input (a fresh interpreter per call)."},
    "workdir": {"type": "string", "description": "Optional working directory (relative to the workspace root or absolute)"},
    "timeout_seconds": {"type": "integer", "description": "Optional timeout in seconds (default 120, max 600)"}
  }
}`)

type pythonExecArgs struct {
	Code           string `json:"code"`
	Workdir        string `json:"workdir"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// pythonCandidates are the interpreter names probed on PATH, in preference order. A detected
// interpreter is used; none found is a graceful "unavailable" result, never a hard dep (§3a).
var pythonCandidates = []string{"python3", "python"}

// lookInterpreter resolves the first available interpreter on PATH (a package var so a test
// can inject a fake resolver). It returns the absolute path and ok=false when none is found.
var lookInterpreter = func(candidates []string) (string, bool) {
	for _, name := range candidates {
		if path, err := exec.LookPath(name); err == nil {
			return path, true
		}
	}
	return "", false
}

// PythonExec runs a one-shot Python script through a detected interpreter (python3, then
// python), feeding the source on stdin so no temp file is left behind. It is a SubprocessTool
// (domain.SubprocessTool): the disposition runs it under Confiner.Confine in Auto and gates
// it when fs-confinement is unavailable. It degrades gracefully when no interpreter is present
// — a clear "python not available" result, never a hard dependency (§3a). It is stateless
// across Turns (ADR 0008): a fresh interpreter per call, no persistent REPL.
type PythonExec struct{ root string }

// NewPythonExec returns a python-exec tool whose working directory resolves within root.
func NewPythonExec(root string) *PythonExec { return &PythonExec{root: root} }

// Name returns the stable identifier the model calls.
func (t *PythonExec) Name() string { return "python_exec" }

// Description returns the model-facing summary of the tool.
func (t *PythonExec) Description() string {
	return "Run a Python script through the system interpreter and capture its output and exit code. One-shot (a fresh interpreter per call); the script is fed on standard input. Reports clearly when no Python interpreter is available."
}

// Schema returns the JSON schema of the tool's arguments.
func (t *PythonExec) Schema() json.RawMessage { return pythonExecSchema }

// ReadOnly reports that python-exec is write-capable (false) — a script can write, so the
// loop must gate/confine it rather than running it freely.
func (t *PythonExec) ReadOnly() bool { return false }

// Subprocess reports that python-exec launches an OS subprocess — the marker the disposition
// keys on to confine it in Auto rather than gating it (domain.SubprocessTool).
func (t *PythonExec) Subprocess() bool { return true }

// Execute runs the script through a detected interpreter, honouring ctx cancellation and the
// confinement handle the disposition installed (if any). A missing interpreter, an
// out-of-root working directory, or a non-zero exit are surfaced as results; only ctx
// cancellation or a confinement-unavailable demotion is a Go error.
func (t *PythonExec) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	var args pythonExecArgs
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return errorResult(call.ID, "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(args.Code) == "" {
		return errorResult(call.ID, "code is required"), nil
	}

	interp, ok := lookInterpreter(pythonCandidates)
	if !ok {
		// Graceful degradation (§3a): no interpreter is an unavailable result, not a crash
		// and not a hard dependency.
		return errorResult(call.ID, "python not available: no Python interpreter found on PATH (looked for "+strings.Join(pythonCandidates, ", ")+")"), nil
	}

	dir, err := t.resolveWorkdir(args.Workdir)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	// "-" tells the interpreter to read the program from stdin, so no temp file is created
	// (statelessness, ADR 0008) and the script is never written to the filesystem.
	spec := subprocessSpec{
		argv:    []string{interp, "-"},
		dir:     dir,
		timeout: time.Duration(args.TimeoutSeconds) * time.Second,
		stdin:   args.Code,
	}
	res, err := runSubprocess(ctx, spec)
	if err != nil {
		return domain.ToolResult{}, err
	}
	return subprocessToolResult(call.ID, res), nil
}

// resolveWorkdir resolves the optional working directory within the root (path-safe), or
// returns the root itself when none is given.
func (t *PythonExec) resolveWorkdir(workdir string) (string, error) {
	if workdir == "" {
		return t.root, nil
	}
	return resolveInRoot(workdir, t.root)
}

var (
	_ domain.Tool           = (*PythonExec)(nil)
	_ domain.SubprocessTool = (*PythonExec)(nil)
)

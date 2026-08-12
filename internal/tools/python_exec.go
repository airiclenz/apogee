package tools

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

var pythonExecSpec = toolSpec{
	name:        "python_exec",
	description: "Run a Python script through the system interpreter and capture its output and exit code. One-shot (a fresh interpreter per call); the script is fed on standard input. The workspace is deliberately NOT on sys.path, so the standard library always wins: importing a project module needs an explicit line in the code, e.g. `import sys; sys.path.append('.')`. Reports clearly when no Python interpreter is available.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["code"],
  "properties": {
    "code": {"type": "string", "description": "The Python source to run. It is fed to the interpreter on standard input (a fresh interpreter per call). The workspace is not on sys.path: to import a project module, add it in the code (import sys; sys.path.append('.'))."},
    "workdir": {"type": "string", "description": "Optional working directory (relative to the workspace root or absolute)"},
    "timeout_seconds": {"type": "integer", "description": "Optional timeout in seconds (default 120, max 600)"}
  }
}`),
}

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

// The load-path policy: the workspace never precedes the standard library on sys.path.
//
// A program read from stdin ("-") makes CPython front sys.path with the process's working
// directory, which for this tool is the workspace root. A repo-root json.py / socket.py /
// subprocess.py then OWNS the corresponding import in a snippet whose approved `code` shows
// nothing of the sort, and confinement is no mitigation — the box leaves read and exec open, so
// the Auto path runs the repo's json.py too.
//
// Two switches remove that entry and nothing else does. PYTHONSAFEPATH is the modern one and is
// what the tool prefers; it landed in 3.11 and every older interpreter ignores it SILENTLY, so
// the version is measured rather than assumed (python3 on a stock macOS is frequently 3.9) and
// an older one is given -I instead. Neither -s, -S, -E nor -P is used: the first three do not
// address the load path at all, and -P shares PYTHONSAFEPATH's 3.11 floor.
//
// Nothing is injected into the snippet and PYTHONPATH is never set: a project import is the
// snippet's own explicit sys.path line, which puts the addition in the `code` the operator
// approves (ratified 2026-08-11). One residual is deliberate and bounded: a PYTHONPATH the
// OPERATOR exported is still honoured on the 3.11+ path (-I drops it on the older one), so an
// operator who has put the workspace on PYTHONPATH themselves has armed that shadowing — an
// operator-armed switch, which this plan's threat model leaves to the operator.
const (
	// pythonSafePathVar keeps the script's own directory — for a stdin program, the working
	// directory, i.e. the workspace — off sys.path on CPython 3.11 and newer.
	pythonSafePathVar = "PYTHONSAFEPATH=1"
	// pythonIsolationFlag is what a pre-3.11 interpreter gets instead. -I (isolated mode)
	// removes the same entry and is the only pre-3.11 switch that does. It is deliberately
	// BROADER than PYTHONSAFEPATH — it also ignores every PYTHON* environment variable and
	// the user site-directory — so the older path is stricter than the modern one rather than
	// weaker, and the contract the model is given is identical either way, because
	// sys.path.append works normally under -I (ratified 2026-08-12).
	pythonIsolationFlag = "-I"
	// pythonSafePathMajor and pythonSafePathMinor are the first release honouring
	// PYTHONSAFEPATH.
	pythonSafePathMajor = 3
	pythonSafePathMinor = 11
	// pythonVersionProbeTimeout bounds the version probe; printing two integers is
	// instantaneous, so anything near this ceiling is a wedged interpreter, and the probe
	// answers "unknown" (which isolates) rather than holding the call open.
	pythonVersionProbeTimeout = 15 * time.Second
	// pythonVersionProgram prints the interpreter's own major.minor. `sys` is a BUILT-IN
	// module, resolved before any sys.path entry is consulted, so the probe cannot itself be
	// shadowed by the workspace it exists to protect against; print() with a single argument
	// also parses under Python 2, so an ancient interpreter reports itself rather than
	// erroring into "unknown".
	pythonVersionProgram = "import sys; print('%d.%d' % sys.version_info[:2])"
)

// interpreterVersion reports interp's own (major, minor) version, with ok=false when the probe
// could not be run or its output could not be read. It is a package var so a test can pin
// either side of the 3.11 boundary without depending on the Python the host happens to ship.
//
// The probe runs in the INTERPRETER's own directory rather than the workspace: the exec fence
// has already refused an interpreter resolving inside a model-writable path, so that directory
// holds bytes the model cannot have authored — and the working directory is precisely what
// would otherwise front sys.path for the -c program.
var interpreterVersion = func(ctx context.Context, interp string) (major, minor int, ok bool) {
	res, err := runSubprocess(ctx, subprocessSpec{
		argv:    []string{interp, "-c", pythonVersionProgram},
		dir:     filepath.Dir(interp),
		timeout: pythonVersionProbeTimeout,
		env:     subprocessEnv(),
	})
	if err != nil || res.exitCode != 0 {
		return 0, 0, false
	}
	return parsePythonVersion(res.combinedOutput)
}

// parsePythonVersion reads a "major.minor" pair out of a probe's combined output. It takes the
// LAST field rather than the first: stdout and stderr are interleaved, so a deprecation warning
// or a site-package notice can precede the line the probe printed.
func parsePythonVersion(out string) (major, minor int, ok bool) {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return 0, 0, false
	}
	majorText, minorText, cut := strings.Cut(fields[len(fields)-1], ".")
	if !cut {
		return 0, 0, false
	}
	var err error
	if major, err = strconv.Atoi(majorText); err != nil {
		return 0, 0, false
	}
	if minor, err = strconv.Atoi(minorText); err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// honoursSafePath reports whether an interpreter of this version honours PYTHONSAFEPATH. An
// UNKNOWN version (ok=false) answers false, so the caller isolates: the flag is the
// conservative choice, and a version that could not be read is more likely a surprising
// interpreter than a modern one. The cost of that direction is bounded and loud — an
// interpreter old enough to reject -I (3.3 and earlier) fails with its own "Unknown option"
// message rather than silently running with the workspace ahead of the stdlib.
func honoursSafePath(major, minor int, ok bool) bool {
	if !ok {
		return false
	}
	return major > pythonSafePathMajor || (major == pythonSafePathMajor && minor >= pythonSafePathMinor)
}

// pythonArgv builds the interpreter argv. "-" tells the interpreter to read the program from
// stdin, so no temp file is created (statelessness, ADR 0008) and the script is never written to
// the filesystem; isolate adds the pre-3.11 stand-in for PYTHONSAFEPATH.
func pythonArgv(interp string, isolate bool) []string {
	if isolate {
		return []string{interp, pythonIsolationFlag, "-"}
	}
	return []string{interp, "-"}
}

// runPythonSubprocess runs the interpreter (a package var so a test can capture the exact argv
// and environment this tool builds without launching one).
var runPythonSubprocess = runSubprocess

// PythonExec runs a one-shot Python script through a detected interpreter (python3, then
// python), feeding the source on stdin so no temp file is left behind. It is a SubprocessTool
// (domain.SubprocessTool): the disposition runs it under Confiner.Confine in Auto and gates
// it when fs-confinement is unavailable. It degrades gracefully when no interpreter is present
// — a clear "python not available" result, never a hard dependency (§3a). It is stateless
// across Turns (ADR 0008): a fresh interpreter per call, no persistent REPL.
//
// The snippet runs in the workspace but never IMPORTS from it: the load-path policy above keeps
// the working directory off sys.path, so the standard library a snippet asks for is the one it
// gets. The interpreter inherits the caller's environment minus apogee's own credentials
// (subprocessEnv), which the model's snippet has no use for.
type PythonExec struct {
	toolSpec
	root string
}

// NewPythonExec returns a python-exec tool whose working directory resolves within root.
func NewPythonExec(root string) *PythonExec { return &PythonExec{toolSpec: pythonExecSpec, root: root} }

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

	args, fail, ok := decodeToolArgs[pythonExecArgs](call)
	if !ok {
		return fail, nil
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
	// An interpreter the model can write is not an interpreter: the exec fence refuses it and
	// says so in its own words, naming the resolved path. This is deliberately NOT the
	// graceful "python not available" above — that message would send the operator installing
	// a Python they already have, when the cause is an in-workspace one (typically an
	// activated .venv) sitting ahead of the system entries on PATH.
	if err := refuseExecFromWritablePath(interp, t.root, confinementBox(ctx)); err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	dir, err := t.resolveWorkdir(args.Workdir)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	// The workspace must not precede the standard library on sys.path: PYTHONSAFEPATH is set
	// unconditionally (it is what a 3.11+ interpreter honours, and is inert — indeed ignored
	// outright — under -I), and an interpreter that does not honour it is isolated instead.
	// Both are decided here rather than in the snippet, so nothing is injected into the `code`
	// the operator approved.
	spec := subprocessSpec{
		argv:    pythonArgv(interp, !honoursSafePath(interpreterVersion(ctx, interp))),
		dir:     dir,
		timeout: time.Duration(args.TimeoutSeconds) * time.Second,
		stdin:   args.Code,
		env:     subprocessEnv(pythonSafePathVar),
	}
	res, err := runPythonSubprocess(ctx, spec)
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

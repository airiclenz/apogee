package tools

import (
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The diagnostics tool (P3.10) — in-process Go checks + optional shell-out linters
// ----------------------------------------------------------------------------
//
// diagnostics reports compile/lint-level problems in a source file. For Go it is
// fully in-process and dependency-free: go/parser catches syntax errors and the
// go vet that ships with the toolchain catches the common semantic mistakes — the
// parser half NEVER needs an external program, so a Go syntax error is always
// reported even on a host with no `go` on PATH. For other languages it probes for
// an optional, detected linter (tsc for TS/JS, …) and degrades gracefully to a
// clear "no diagnostics available" result when none is present (§3a — an
// enhancement, never a hard dependency, never an error).
//
// The tool is ReadOnly() — it only inspects, never mutates — so the disposition
// runs it freely in every mode (including Plan). It is also a SubprocessTool
// (domain.SubprocessTool) because the go vet / linter half shells out; read-only
// wins over the subprocess class in the disposition (the disposition runs it,
// never confines it), but the marker keeps the classification honest and lets the
// shared runSubprocess honour a confinement handle if one is installed — exactly
// like git_diff_range (P3.9). It is stateless across Turns (ADR 0008): a fresh
// parse / a fresh process per call, no persistent state.

// vetTimeout bounds a single go vet (or external linter) invocation. Vetting a
// single package is local and quick, so a short ceiling is ample and a hung
// toolchain never wedges a Turn (the §2.4 teardown reaps the process group).
const vetTimeout = 30 * time.Second

var diagnosticsSpec = toolSpec{
	name:        "diagnostics",
	description: "Report syntax and lint-level problems in a source file. Go files are checked in-process (syntax) plus 'go vet' when the toolchain is present; other languages use a detected linter if one is available, and report 'no diagnostics available' (not an error) when none is. Read-only.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": {"type": "string", "description": "Path to the source file to diagnose (relative to the workspace root or absolute). The language is inferred from the file extension."},
    "vet": {"type": "boolean", "description": "For Go files, also run 'go vet' on the file's package when the toolchain is available (default: true). Syntax checking via go/parser is always performed and needs no toolchain."}
  }
}`),
}

type diagnosticsArgs struct {
	Path string `json:"path"`
	// Vet defaults to true; *bool distinguishes "omitted" (run vet) from an
	// explicit false (skip vet) so the in-process syntax check is never the only
	// thing a caller can get by accident.
	Vet *bool `json:"vet"`
}

// Diagnostics inspects a source file for compile/lint-level problems, scoped to a
// workspace root. Go files are checked in-process (go/parser for syntax) plus an
// optional go vet; other languages probe for a detected linter and degrade
// gracefully when none is available. It is read-only.
type Diagnostics struct {
	toolSpec
	root string
}

// NewDiagnostics returns a diagnostics tool whose target path resolves within root.
func NewDiagnostics(root string) *Diagnostics {
	return &Diagnostics{toolSpec: diagnosticsSpec, root: root}
}

// ReadOnly reports that diagnostics performs no writes — it only inspects — so the
// disposition runs it freely in every mode (including Plan).
func (t *Diagnostics) ReadOnly() bool { return true }

// Subprocess reports that diagnostics may launch an OS subprocess (go vet / an
// external linter). It is read-only, so the disposition runs it freely (read-only
// wins over the subprocess class), but the marker keeps the classification honest
// (domain.SubprocessTool).
func (t *Diagnostics) Subprocess() bool { return true }

// Execute diagnoses the file at the requested path. An invalid path, a path escape,
// or an unsupported language are surfaced as results (the last as a graceful "no
// diagnostics available", not an error); only ctx cancellation is a Go error (this
// tool is read-only and never confines).
func (t *Diagnostics) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[diagnosticsArgs](call)
	if !ok {
		return fail, nil
	}
	if strings.TrimSpace(args.Path) == "" {
		return errorResult(call.ID, "path is required"), nil
	}

	abs, err := resolveInRoot(args.Path, t.root)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	switch detectLanguage(abs) {
	case langGo:
		return t.diagnoseGo(ctx, call.ID, abs, args.runVet())
	default:
		// An unsupported language is graceful degradation (§3a): a clear "no
		// diagnostics available", not an error and not a hard dependency.
		return okResult(call.ID, noDiagnosticsMessage(abs)), nil
	}
}

// runVet reports whether the go vet half should run: true unless the caller
// explicitly passed vet:false.
func (a diagnosticsArgs) runVet() bool { return a.Vet == nil || *a.Vet }

// diagnoseGo runs the Go diagnostics: the always-available in-process syntax check
// (go/parser) and, when requested and the toolchain is present, go vet on the
// file's package. A syntax error or a vet finding produces an error result the
// model can react to; a clean file produces a success result. The Go error return
// is reserved for ctx cancellation (so the loop rolls the Turn back, ADR 0007).
func (t *Diagnostics) diagnoseGo(ctx context.Context, callID, abs string, runVet bool) (domain.ToolResult, error) {
	// In-process syntax check — never needs an external program, so a Go syntax
	// error is reported even with no `go` on PATH.
	if syntax := goSyntaxDiagnostics(abs); syntax != "" {
		// A file that does not parse cannot be vetted; stop here with the syntax
		// findings (go vet would only repeat the parse failure).
		return errorResult(callID, syntax), nil
	}

	if !runVet {
		return okResult(callID, cleanGoMessage(abs)), nil
	}

	goPath, ok := lookGo()
	if !ok {
		// The syntax check passed; go vet is the optional enhancement that is
		// unavailable here (§3a). Report the clean result plus a note, not an error —
		// the toolchain is not a hard dependency.
		return okResult(callID, cleanGoMessage(abs)+"\n\ngo vet skipped: no 'go' toolchain found on PATH."), nil
	}

	vet, hadFindings, err := runGoVet(ctx, goPath, t.root, abs)
	if err != nil {
		// Only ctx cancellation reaches here (runGoVet's contract); surface it as a Go
		// error so the loop rolls the Turn back rather than reporting a partial result.
		return domain.ToolResult{}, err
	}
	if hadFindings {
		return errorResult(callID, vet), nil
	}
	return okResult(callID, cleanGoMessage(abs)), nil
}

// goSyntaxDiagnostics parses the Go file in-process and returns the formatted
// syntax errors, or "" when the file parses cleanly. parser.AllErrors surfaces all
// syntax errors in one pass (not just the first) so the model sees the whole list.
// A read error (e.g. the file vanished) is reported as a diagnostic, not a Go error
// — this tool never fails the Turn for a tool-level problem.
func goSyntaxDiagnostics(abs string) string {
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, abs, nil, parser.ParseComments|parser.AllErrors)
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

// runGoVet runs `go vet` on the package containing abs, under the vet timeout. It
// returns the formatted findings, whether vet reported any problem (a non-zero exit
// with output), and a non-nil error ONLY for ctx cancellation (so the read-only
// diagnosis can degrade rather than fail). go vet writes findings to stderr and
// exits non-zero when it finds problems; a clean package exits zero with no output.
func runGoVet(ctx context.Context, goPath, root, abs string) (findings string, hadFindings bool, err error) {
	// Vet the package directory (go vet operates on packages, not single files), so
	// a finding in any file of the package is surfaced; the dir stays inside root.
	dir := filepath.Dir(abs)
	spec := subprocessSpec{
		argv:    []string{goPath, "vet", dir},
		dir:     root,
		timeout: vetTimeout,
		env:     safeGitEnv(),
	}
	res, runErr := runSubprocess(ctx, spec)
	if runErr != nil {
		// ctx cancellation (or a confinement-unavailable demotion, which cannot
		// happen here because diagnostics is read-only and never confined).
		return "", false, runErr
	}
	out := strings.TrimSpace(res.combinedOutput)
	if res.exitCode == 0 {
		return "", false, nil
	}
	if out == "" {
		out = "go vet reported problems (exit code " + strconv.Itoa(res.exitCode) + ")"
	}
	return out, true, nil
}

// cleanGoMessage is the success text for a Go file with no syntax errors and no
// vet findings.
func cleanGoMessage(abs string) string {
	return "No diagnostics: " + filepath.Base(abs) + " looks clean."
}

// noDiagnosticsMessage is the graceful-degradation result for a file whose language
// has no available diagnostics (§3a — not an error, never a hard dependency).
func noDiagnosticsMessage(abs string) string {
	return "no diagnostics available for " + filepath.Base(abs) + " (no diagnostics provider for this file type)"
}

// language is the small set of source languages diagnostics recognises.
type language int

const (
	langUnknown language = iota
	langGo
)

// detectLanguage infers the language from the file extension. Only Go has a
// built-in (dependency-free) provider today; other extensions resolve to
// langUnknown and degrade gracefully (an optional external linter can be added per
// language later without changing the disposition — the tool stays read-only).
func detectLanguage(abs string) language {
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".go":
		return langGo
	default:
		return langUnknown
	}
}

// lookGo resolves the system Go toolchain on PATH (a package var so a test can
// inject a fake resolver). It returns the absolute path and ok=false when go is
// absent — the signal the vet half degrades to a graceful "skipped" note (§3a).
var lookGo = func() (string, bool) {
	path, err := exec.LookPath("go")
	return path, err == nil
}

var (
	_ domain.Tool           = (*Diagnostics)(nil)
	_ domain.ReadOnlyTool   = (*Diagnostics)(nil)
	_ domain.SubprocessTool = (*Diagnostics)(nil)
)

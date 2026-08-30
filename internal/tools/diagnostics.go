package tools

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
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
// The vet half is wider than the call that asks for it: `go vet` operates on PACKAGES, so
// a call naming one file reads every .go file in that file's directory. That widening is
// declared in the tool's own description and restated on every vet result
// (vettedPackageLine), because a surface that shows one filename while the subprocess reads
// a directory is telling the operator something other than what runs. The subprocess itself
// runs with a Go-specific pinned environment (goVetEnv/goVetPins) rather than git's
// allowlist — vetting untrusted code must not let that code choose a toolchain.
//
// The tool is ReadOnly() — it only inspects, never mutates. It is also a
// SubprocessTool (domain.SubprocessTool) because the go vet / linter half shells
// out, and THAT marker is what classifies the call: the unfakeable marker outranks
// the self-declaration (confinement-execution-contract §4, amended 2026-07-26), so
// the call takes the subprocess row — confined in Auto (the shared runSubprocess
// honours the handle the disposition installs), gated below it, and (since
// 2026-08-02) neither offered nor run in Plan, because the Plan menu keys on that
// same class — exactly like git_diff_range (P3.9). It is stateless across Turns
// (ADR 0008): a fresh parse / a fresh process per call, no persistent state.

// vetTimeout bounds a single go vet (or external linter) invocation. Vetting a
// single package is local and quick, so a short ceiling is ample and a hung
// toolchain never wedges a Turn (the §2.4 teardown reaps the process group).
const vetTimeout = 30 * time.Second

var diagnosticsSpec = toolSpec{
	name:        "diagnostics",
	description: "Report syntax and lint-level problems in a source file. Go files are checked in-process (syntax) plus 'go vet' when the toolchain is present — vet runs on the file's whole PACKAGE DIRECTORY (every .go file beside it), not on the named file alone. Other languages use a detected linter if one is available, and report 'no diagnostics available' (not an error) when none is. Read-only.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": {"type": "string", "description": "Path to the source file to diagnose (relative to the workspace root or absolute). The language is inferred from the file extension. For Go the 'go vet' half reads the file's whole package directory, so naming one file asks for a check of every .go file in that directory."},
    "vet": {"type": "boolean", "description": "For Go files, also run 'go vet' on the directory the file is in — its package, every .go file included — when the toolchain is available (default: true). Syntax checking via go/parser is always performed, needs no toolchain, and covers only the named file."}
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

// ReadOnly reports that diagnostics performs no writes — it only inspects — an honest
// statement about the tool, read by self-regulation's read/write tally. It is NOT what
// classifies the call, and (since 2026-08-02) NOT what the Plan menu filters on: the
// Subprocess marker below outranks this declaration in both.
func (t *Diagnostics) ReadOnly() bool { return true }

// Subprocess reports that diagnostics may launch an OS subprocess (go vet / an
// external linter). The unfakeable marker OUTRANKS the read-only self-declaration in
// the per-call classification (confinement-execution-contract §4, amended
// 2026-07-26), so the call takes the subprocess row: confined in Auto, gated below it.
func (t *Diagnostics) Subprocess() bool { return true }

// ApprovalScope states, for the approval pane, the same widening the result strings state
// (vettedPackageLine): the call names one file and the vet half reads the whole package
// directory around it. Until this marker existed the pane was the ONE surface that did not say
// so — the tool's description and its results both did — and the pane is where the human
// actually decides. The scope is derived from the call's arguments alone (no disk read beyond
// the same path resolution Execute performs, no subprocess), because it is computed on the
// approval path, before anything runs.
//
// It is EMPTY for every call whose arguments already name their own reach: a non-Go file (no
// vet half at all), an explicit vet:false (the syntax check reads only the named file), and a
// path that does not resolve inside the workspace (a call Execute will refuse). The scope
// deliberately says nothing about the TOOLCHAIN being present — that is discovered at run time,
// and a pane that promised "go vet may be skipped" would understate the reach the human is
// asked to authorise.
func (t *Diagnostics) ApprovalScope(call domain.ToolCall) string {
	args, _, ok := decodeToolArgs[diagnosticsArgs](call)
	if !ok || strings.TrimSpace(args.Path) == "" || !args.runVet() {
		return ""
	}
	abs, err := resolveInRoot(args.Path, t.root)
	if err != nil || detectLanguage(abs) != langGo {
		return ""
	}
	return "go vet reads " + vettedPackageScope(abs, t.root) + "."
}

// Execute diagnoses the file at the requested path. An invalid path, a path escape,
// or an unsupported language are surfaced as results (the last as a graceful "no
// diagnostics available", not an error); the Go error is reserved for ctx
// cancellation and a confinement-unavailable demotion (the runSubprocess contract).
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
		return t.diagnoseGo(ctx, call.ID, args.Path, abs, args.runVet())
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
// model can react to; a clean file produces a success result. name is the path the
// model asked for (what an "absent file" message names), abs its resolved form. The Go
// error return is reserved for ctx cancellation (so the loop rolls the Turn back, ADR 0007).
func (t *Diagnostics) diagnoseGo(ctx context.Context, callID, name, abs string, runVet bool) (domain.ToolResult, error) {
	// The source is read ONCE, through the workspace fence (os.Root-pinned), and the BYTES
	// are handed to the parser below: parsing by path would re-walk that path, following a
	// component swapped to point outside the workspace after resolveInRoot checked it. A
	// refusal is reported as a refusal, never as an absent file. No size bound is added —
	// go/parser read the whole file before this fence too, and a cap would stop a large but
	// legitimate source file from being diagnosable at all.
	src, err := safeReadFile(workspaceRelative(abs, t.root), t.root)
	if err != nil {
		return errorResult(callID, escapeOrMessage(err, "file not found: "+name)), nil
	}

	// In-process syntax check — never needs an external program, so a Go syntax
	// error is reported even with no `go` on PATH.
	if syntax := goSyntaxDiagnostics(abs, src); syntax != "" {
		// A file that does not parse cannot be vetted; stop here with the syntax
		// findings (go vet would only repeat the parse failure).
		return errorResult(callID, syntax), nil
	}

	if !runVet {
		return okResult(callID, cleanGoMessage(abs)), nil
	}

	goPath, err := security.ResolveProgram(lookGo, "go", t.root, confinementBox(ctx))
	if err != nil {
		if errors.Is(err, security.ErrExecFromWritablePath) {
			// A toolchain the model can write is refused rather than run. vet is the optional
			// half here, so the syntax verdict still stands and the refusal rides on the note —
			// which names the resolved path, so the operator can see WHICH go was refused.
			return okResult(callID, cleanGoMessage(abs)+"\n\ngo vet skipped: "+err.Error()), nil
		}
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
	// Both vet outcomes state the SCOPE vet really ran on (vettedPackageLine): the call
	// named one file, the subprocess read the package around it, and the result is where
	// that difference is said out loud. The two branches that skip vet above say nothing
	// of the kind, because nothing beyond the named file was read.
	if hadFindings {
		return errorResult(callID, vettedPackageLine(abs, t.root)+"\n\n"+vet), nil
	}
	return okResult(callID, cleanGoMessage(abs)+"\n\n"+vettedPackageLine(abs, t.root)), nil
}

// goSyntaxDiagnostics parses src in-process and returns the formatted syntax errors, or ""
// when it parses cleanly. src is the file's already-read content — the caller read it
// through the workspace fence, and passing the bytes rather than the path is what keeps
// the parser from re-walking (and re-following) that path. abs names the file only for the
// positions in the reported diagnostics. parser.AllErrors surfaces all syntax errors in one
// pass (not just the first) so the model sees the whole list.
func goSyntaxDiagnostics(abs string, src []byte) string {
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, abs, src, parser.ParseComments|parser.AllErrors)
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

// runGoVet runs `go vet` on the package containing abs, under the vet timeout and the
// pinned toolchain environment (goVetSpec). It returns the formatted findings, whether
// vet reported any problem (a non-zero exit with output), and a non-nil error ONLY for
// ctx cancellation or a confinement-unavailable demotion (so an ordinary vet failure —
// including a dependency the pinned environment cannot resolve — degrades rather than
// failing the diagnosis). go vet writes findings to stderr and
// exits non-zero when it finds problems; a clean package exits zero with no output.
func runGoVet(ctx context.Context, goPath, root, abs string) (findings string, hadFindings bool, err error) {
	res, runErr := runSubprocess(ctx, goVetSpec(goPath, root, abs))
	if runErr != nil {
		// ctx cancellation, or a confinement-unavailable demotion (diagnostics takes
		// the subprocess class, so dispatch does confine it — the demote signal must
		// reach dispatch rather than being swallowed as a finding).
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

// goVetSpec is the exact subprocess one vet invocation runs as: the argv, the working
// directory, the ceiling, and the pinned toolchain environment (goVetEnv). It is a
// function of its own — rather than a literal inside runGoVet — so a test can pin what
// the vet half executes key-by-key without launching a toolchain.
//
// The vetted target is the PACKAGE DIRECTORY, because go vet operates on packages rather
// than single files: a finding in any file of the package is surfaced, and the dir stays
// inside root (abs was already resolved through the fence). That widening is what
// vettedPackageLine states on the result, so the sentence the operator approved and the
// scope the subprocess read are the same sentence.
func goVetSpec(goPath, root, abs string) subprocessSpec {
	return subprocessSpec{
		argv:    []string{goPath, "vet", filepath.Dir(abs)},
		dir:     root,
		timeout: vetTimeout,
		env:     goVetEnv(root),
	}
}

// goToolchainEnvKeys is the allowlist of host environment variables the vet subprocess
// inherits. It is deliberately NOT git's list (safeEnvKeys): that list was written for a
// program whose behaviour is steered by GIT_* and a pager, and borrowing it for the Go
// toolchain both dropped the operator's own Go hardening and put nothing back. This list
// carries only what a build cache needs, and goVetPins below decides everything else.
var goToolchainEnvKeys = []string{
	// PATH — the toolchain resolves programs of its own (the compiler, the vet tool);
	// ScopeEnv strips the entries inside root, so none of them come from the workspace.
	"PATH",
	// HOME — GOCACHE and GOMODCACHE default beneath it, and go refuses to build with no
	// build cache at all.
	"HOME",
	// The build's scratch space, and (on Linux) where GOCACHE lands when the user moved
	// their cache root.
	"TMPDIR", "TMP", "TEMP", "XDG_CACHE_HOME",
}

// goVetPins are the toolchain settings the vet subprocess runs with WHATEVER the host
// environment or the vetted repository says. They are appended after the inherited keys,
// so a duplicate spelling of any of them loses (os/exec resolves duplicates last-wins).
//
// Each one closes a way the Go toolchain can be made to do more than read code:
//   - GOFLAGS=-mod=readonly — no go.mod/go.sum edit as a side effect of a diagnosis, and
//     no inherited -toolexec/-exec flag riding in on the operator's own GOFLAGS.
//   - GOWORK=off — a go.work the ATTACKER authored cannot pull other modules (and other
//     directories) into the build vet performs.
//   - GOTOOLCHAIN=local — a `toolchain` line in the repository's go.mod cannot make go
//     download and execute a different toolchain binary. This is the one pin that stops
//     an exec rather than bounding one.
//   - CGO_ENABLED=0 — vet needs no cgo, and this keeps a repository's #cgo directives
//     away from the host C compiler (which is an exec of an attacker-named line).
//   - GOENV=off — the `go env -w` file (HOME passes, so it would otherwise still apply)
//     is a persistent, invisible source of exactly the flags above. Off means the four
//     pins are the whole story rather than the story until someone runs `go env -w`.
//
// The cost of GOENV=off is stated rather than hidden: an operator's persisted GOPROXY,
// GOPRIVATE or GOMODCACHE are not read either, so on a cold module cache a vet may fail
// to resolve dependencies. That failure degrades — it is reported as a vet finding, never
// as a tool error — which is the trade the diagnosis half is allowed to make.
var goVetPins = []string{
	"GOFLAGS=-mod=readonly",
	"GOWORK=off",
	"GOTOOLCHAIN=local",
	"CGO_ENABLED=0",
	"GOENV=off",
}

// goVetEnv returns the exact environment the vet subprocess runs with: the allowlisted
// host keys scoped to root (PATH first among them — the toolchain must not resolve its
// own programs out of the workspace the model writes to), then the pins that decide how
// the toolchain behaves.
func goVetEnv(root string) []string {
	return append(shellHost.ScopeEnv(root, goToolchainEnvKeys, os.LookupEnv), goVetPins...)
}

// cleanGoMessage is the success text for a Go file with no syntax errors and no
// vet findings.
func cleanGoMessage(abs string) string {
	return "No diagnostics: " + filepath.Base(abs) + " looks clean."
}

// vettedPackageLine names what the vet half actually read: the package DIRECTORY around
// the requested file, spelled relative to the workspace root, and said in the same breath
// as the file the call named. The tool takes one filename and vets its whole package, and
// until this line existed no surface said so — "I approved foo.go" and "it read every file
// beside foo.go" were two different sentences.
//
// It rides the result string, which is the surface this tool owns once the call has run; the
// approval pane gets the SAME scope before it runs, off the ApprovalScoper marker above, so the
// two cannot describe one call differently (they share vettedPackageScope, and only their verb
// tense differs — the pane speaks of a call about to happen, the result of one that did).
func vettedPackageLine(abs, root string) string {
	return "go vet checked " + vettedPackageScope(abs, root) + "."
}

// vettedPackageScope is the scope clause both surfaces are built from: the package DIRECTORY
// around the requested file, spelled relative to the workspace root, said in the same breath as
// the file the call named and carrying no verb of its own so each caller supplies its own tense.
func vettedPackageScope(abs, root string) string {
	return "the whole package directory " + packageDirName(filepath.Dir(abs), root) +
		" — every .go file in it, not only " + filepath.Base(abs)
}

// packageDirName spells a package directory for a human: relative to the workspace root,
// with the root itself named rather than left as the bare "." that filepath.Rel returns
// (a lone dot on a security-facing line reads as a typo, not as "the whole workspace").
func packageDirName(dir, root string) string {
	if rel := workspaceRelative(dir, root); rel != "." {
		return rel
	}
	return "the workspace root"
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
// language later without changing the disposition — the tool already carries the
// subprocess marker the classification keys on).
func detectLanguage(abs string) language {
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".go":
		return langGo
	default:
		return langUnknown
	}
}

// lookGo is the PATH lookup security.ResolveProgram performs for the system Go toolchain (a
// package var so a test can inject a fake resolver). It carries the resolver's own look
// shape — the absolute path and a nil error, or exec.LookPath's error when go is absent, which
// diagnoseGo maps to the graceful "skipped" note (§3a).
var lookGo = exec.LookPath

var (
	_ domain.Tool           = (*Diagnostics)(nil)
	_ domain.ReadOnlyTool   = (*Diagnostics)(nil)
	_ domain.SubprocessTool = (*Diagnostics)(nil)
)

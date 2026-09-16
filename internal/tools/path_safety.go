package tools

import (
	"context"
	"os"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// Path-safety is consolidated into the shared internal/security guard (P3.6 / D6):
// one symlink-aware, traversal-rejecting boundary every guarded tool inherits, in
// every mode. These package-local aliases keep the built-in tools (and their tests)
// calling the same names while the implementation lives in one place. Behaviour is
// unchanged — security.ResolveInRoot is the verbatim move of the former local code.

// ErrPathEscape is returned when a tool argument resolves to a path outside the
// sandbox root. It is the security guard's sentinel, re-exported here so existing
// errors.Is(err, ErrPathEscape) checks in the tools and their tests keep matching.
// Its counterpart security.ErrRootInaccessible — the ROOT itself would not open, which
// says nothing about the argument — is matched by its own qualified name at the few
// sites that distinguish the two, always ahead of this one.
var ErrPathEscape = security.ErrPathEscape

// resolveInRoot resolves input within root via the shared path-safety guard, returning
// ErrPathEscape for a path that escapes the workspace (symlinks followed).
func resolveInRoot(input, root string) (string, error) {
	return security.ResolveInRoot(input, root)
}

// confinementBox returns the box a confined call runs inside, or nil when no Confinement
// handle rides on ctx — the gated/unconfined case, where the workspace root is the whole
// fence. It is the small read the exec sites need to pass the box on to
// security.ResolveProgram, which resolves an executable and fences it in one step: every tool
// that reaches PATH — git, python_exec, run_tests, diagnostics — goes through it, so bytes the
// model was allowed to write can never become argv[0].
func confinementBox(ctx context.Context) *domain.ConfinementBox {
	if conf, ok := domain.ConfinementFromContext(ctx); ok {
		return &conf.Box
	}
	return nil
}

// safeReadFile reads input within root through the shared TOCTOU-safe guard, with the
// workspace fence enforced at READ time so an escaping symlink component is refused
// rather than followed (security review H1). It replaces the former resolveInRoot+
// os.ReadFile pair for the write tools' read-modify-write step.
func safeReadFile(input, root string) ([]byte, error) {
	return security.SafeReadFile(root, input)
}

// safeOpen opens input for reading within root through the shared TOCTOU-safe guard, with
// the workspace fence enforced at OPEN time (os.Root-pinned). The returned handle pins the
// file's identity: what is statted and read through it is the file that was opened,
// regardless of any rename after. The caller owns Close and any size policy.
func safeOpen(input, root string) (*os.File, error) {
	return security.SafeOpen(root, input)
}

// statInRoot stats path within root through ONE pinned descriptor: the file is opened
// through the workspace fence (os.Root-pinned) and the FileInfo is an fstat of THAT
// descriptor, so what is described is what was opened. It replaces the resolveInRoot +
// os.Stat pair, whose second half re-walked the path string and would follow a component
// swapped to point outside the workspace after the check passed (the H1 check-then-use gap).
// A directory opens successfully and is reported by its FileInfo, so each caller keeps its
// own "not a file" / "not a directory" wording.
func statInRoot(path, root string) (os.FileInfo, error) {
	f, err := safeOpen(path, root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return f.Stat()
}

// ----------------------------------------------------------------------------
// The approved escape (ADR 0049)
// ----------------------------------------------------------------------------
//
// A workspace-scoped write whose target lies OUTSIDE the workspace is GATED, and the approval
// pane shows the operator the resolved path before they answer (confinement-execution-contract
// §4). When the answer is yes — or the mode is the one whose contract is that the VM is the box —
// dispatch stamps the execution context with a domain.WriteEscapePermit naming exactly that
// resolved path. All this package does with it is read the permitted target (below) and pin the
// write family's OWN read-back and pre-flight stat to it — the writeScope's permit and the
// writeTarget.pin that routes on it (write_target.go). There is no per-tool logic and no per-tool
// decision — the permit either governs this call's target or it does not, and every verb asks the
// same question in the same place.
//
// The floor is unconditional: a call with no permit passes "" and behaves byte-for-byte as it did
// before ADR 0049, and no READ tool takes a permit at all.

// writeEscapeTarget answers the ONE resolved absolute path this execution may write outside the
// workspace fence, or "" when there is none — the string internal/security's mutating primitives
// take as their permitted target. It sits beside confinementBox because it answers the same shape
// of question: what did the engine authorise for THIS execution, read at the site that needs it
// rather than threaded through every tool.
func writeEscapeTarget(ctx context.Context) string {
	permit, ok := domain.WriteEscapePermitFrom(ctx)
	if !ok {
		return ""
	}
	return permit.Real
}

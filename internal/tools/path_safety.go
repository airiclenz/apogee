package tools

import (
	"context"
	"os"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// Path-safety is consolidated into the shared internal/security guard (P3.6 / D6):
// one symlink-aware, traversal-rejecting boundary every guarded tool inherits, in
// every mode. The built-in tools call it by its own names — security.ResolveInRoot for
// the containment judgement, security.SafeReadFile and security.SafeOpen for the
// TOCTOU-safe reads — so the rule lives in one place and is named as one thing. What
// stays here is only what this package adds on top: the sentinel every tool matches, the
// confinement read the exec sites need, and the one-descriptor stat.

// ErrPathEscape is returned when a tool argument resolves to a path outside the
// sandbox root. It is the security guard's sentinel, re-exported here so existing
// errors.Is(err, ErrPathEscape) checks in the tools and their tests keep matching.
// Its counterpart security.ErrRootInaccessible — the ROOT itself would not open, which
// says nothing about the argument — is matched by its own qualified name at the few
// sites that distinguish the two, always ahead of this one.
var ErrPathEscape = security.ErrPathEscape

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

// statInRoot stats path within root through ONE pinned descriptor: the file is opened
// through the workspace fence (os.Root-pinned) and the FileInfo is an fstat of THAT
// descriptor, so what is described is what was opened. It replaces the former resolveInRoot +
// os.Stat pair, whose second half re-walked the path string and would follow a component
// swapped to point outside the workspace after the check passed (the H1 check-then-use gap).
// A directory opens successfully and is reported by its FileInfo, so each caller keeps its
// own "not a file" / "not a directory" wording.
func statInRoot(path, root string) (os.FileInfo, error) {
	f, err := security.SafeOpen(root, path)
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
// resolved path. All this package does with it is read it off the context into the
// security.Fence its writeScope holds (writeScopeOf, write_target.go) and pin the write family's
// OWN read-back and pre-flight stat to it — the writeTarget.pin that asks the Fence whether it
// Governs the call's argument. There is no per-tool logic and no per-tool decision — the Fence
// either governs this call's target or it does not, and every verb asks the same question in the
// same place.
//
// The floor is unconditional: a call with no permit holds a Fence with the zero Permit and behaves
// byte-for-byte as it did before ADR 0049, and no READ tool takes a permit at all.

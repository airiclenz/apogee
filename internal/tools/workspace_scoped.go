package tools

import (
	"path/filepath"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// ----------------------------------------------------------------------------
// The workspaceScopedWriter marker (confinement-execution-contract §3)
// ----------------------------------------------------------------------------
//
// workspaceScopedWriter is the unexported marker carried ONLY by Apogee's own write
// tools. Its unexported method means no type outside this package — and no third-party
// tool in another module (Go's internal/ rule) — can satisfy it, so the dispatch
// disposition may trust it as "Apogee's own path-safety-bounded write" (ADR 0012 D1).
// Such a tool needs no OS confinement: the same trusted boundary that auto-approves it
// in Allow-Edits is what bounds it in Auto.
//
// It rides the tool VALUE (a method set), so it survives registry.Subset for free — a
// sub-agent one level down inherits it with no threading (contract §3.4).
type workspaceScopedWriter interface {
	domain.Tool

	// workspaceWriteTarget resolves the absolute path this call would write, so dispatch
	// can classify in- vs out-of-workspace before Execute (contract §4). ok is false when
	// the call writes nothing inspectable (then dispatch treats it as in-bounds). It
	// performs no write — pure path resolution, reusing the path-safety resolver's logic
	// WITHOUT enforcing containment, so an out-of-workspace target resolves rather than
	// erroring (that classification is what dispatch needs).
	workspaceWriteTarget(call domain.ToolCall) (abs string, ok bool)
}

// IsWorkspaceScopedWriter reports whether t is one of Apogee's own workspace-scoped
// write tools — the signal dispatch keys on to auto-approve an in-workspace write in
// Allow-Edits/Auto with no OS confinement and no Approval (ADR 0012 D1/D5).
func IsWorkspaceScopedWriter(t domain.Tool) bool {
	_, ok := t.(workspaceScopedWriter)
	return ok
}

// WorkspaceWriteTarget exposes the marker's target-path resolution to dispatch without
// exporting the marker interface itself. Returns ("", false) for any tool that is not a
// workspace-scoped writer (or a writer whose call has no inspectable target).
func WorkspaceWriteTarget(t domain.Tool, call domain.ToolCall) (string, bool) {
	w, ok := t.(workspaceScopedWriter)
	if !ok {
		return "", false
	}
	return w.workspaceWriteTarget(call)
}

// resolveTargetUnbounded resolves input (relative to root, or absolute) to an absolute
// real path WITHOUT the containment check ResolveInRoot enforces — the path-resolution
// half of the path-safety logic, used only to classify a write target as in- or
// out-of-workspace. An empty input yields ok=false (nothing inspectable to classify).
func resolveTargetUnbounded(input, root string) (string, bool) {
	if input == "" {
		return "", false
	}
	if filepath.IsAbs(input) {
		return security.EvalRealPath(input), true
	}
	return security.EvalRealPath(filepath.Join(root, input)), true
}

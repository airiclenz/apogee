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
	workspaceWriteTarget(call domain.ToolCall) (target writeTarget, ok bool)
}

// writeTarget is one call's write destination in the two spellings the security surfaces
// need, produced together because only the resolver sees both: Named is the absolute path
// the model's own argument NAMES (root-joined and cleaned, nothing followed), Real is that
// path with every symlink resolved — where the write is classified to land.
//
// They differ exactly when the argument travels through something that is not the directory
// it appears to be. That difference is the fact the operator is otherwise never told: the
// pane, the tool card and the write's own result all quote the argument, so a `docs/` that
// is a symlink out of the workspace reads as an in-workspace write right up to the moment it
// lands elsewhere. Keeping both spellings on one value is what lets the disclosure be the
// SAME resolution the gate decided from (ResolvedWriteTarget), rather than a second opinion
// computed on the display side.
type writeTarget struct {
	Named string
	Real  string
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
	target, ok := writeTargetOf(t, call)
	if !ok {
		return "", false
	}
	return target.Real, true
}

// ResolvedWriteTarget is the DISCLOSURE half of the same seam: the absolute path this call's
// write really lands on, returned ONLY when it differs from the path the argument names, and
// "" whenever the two agree or the tool writes nothing inspectable. Empty therefore means
// "the argument names its own target" — the ordinary case — so a surface that renders this
// unconditionally says nothing extra about an ordinary write and names the redirection on the
// one that is not (the `→ resolves to …` line the approval pane, the tool card and the write's
// result string share).
//
// It is deliberately the SAME resolution WorkspaceWriteTarget hands dispatch, symlinked final
// name included. That makes the surfaces agree with the gate by construction: the operator is
// shown the very path the blast-radius classification judged, never a second reading of the
// same call. Where the two could be read apart — a final name that is itself a symlink, which
// SafeWriteFile REPLACES rather than follows — this errs towards disclosing more than the
// write will touch, which is the safe direction for a security surface and the one the gate
// already took.
func ResolvedWriteTarget(t domain.Tool, call domain.ToolCall) string {
	target, ok := writeTargetOf(t, call)
	if !ok || target.Real == target.Named {
		return ""
	}
	return target.Real
}

// resolvedTargetNote is the RESULT-STRING half of the same disclosure: the tail a write tool
// appends to the sentence it reports, naming where the write really landed when that is not
// where the argument said. It is "" for an ordinary write, so a result the model reads — and
// the transcript prints — grows nothing on the common path.
//
// A tool holds its root and its own decoded argument, so this takes the two directly rather
// than re-deriving them through the marker: same resolution, one less round trip through the
// call's JSON. The sentence a surface renders is its own (the pane and the card share the
// TUI's); what all three must not do is disagree about the path, which is why they all read
// resolveTargetUnbounded.
func resolvedTargetNote(input, root string) string {
	target, ok := resolveTargetUnbounded(input, root)
	if !ok || target.Real == target.Named {
		return ""
	}
	return " → resolves to " + target.Real
}

// writeTargetOf is the marker lookup both accessors above share: it resolves through the
// unexported seam without exporting the marker interface itself, and yields ok=false for any
// tool that is not a workspace-scoped writer (or a writer whose call has no inspectable
// target).
func writeTargetOf(t domain.Tool, call domain.ToolCall) (writeTarget, bool) {
	w, ok := t.(workspaceScopedWriter)
	if !ok {
		return writeTarget{}, false
	}
	return w.workspaceWriteTarget(call)
}

// pathArgWriteTarget is the shared body of every workspaceScopedWriter's
// workspaceWriteTarget: decode the call's "path" argument and resolve it against root
// WITHOUT the containment check, because dispatch needs an out-of-workspace target to
// resolve rather than error (contract §3). It decodes into a minimal one-field struct
// rather than each tool's args type — every write tool spells the argument "path", which
// two tests keep true from the tools' own surfaces rather than from a literal:
// TestWriteToolsDeclarePathArgument pins each writer's declared schema and args struct to
// that key, and TestWriteTargetsAgreeOnPath drives this body with calls marshalled from
// those very structs, so a rename on either side fails before it can blind the fence.
// Ignoring the rest of the call's arguments is deliberate: classification asks only where
// the write would land, so a malformed sibling argument still yields a target for dispatch
// to judge, and Execute is where that argument is decoded properly and rejected.
func pathArgWriteTarget(call domain.ToolCall, root string) (writeTarget, bool) {
	var args struct {
		Path string `json:"path"`
	}
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return writeTarget{}, false
	}
	return resolveTargetUnbounded(args.Path, root)
}

// destinationArgWriteTarget is pathArgWriteTarget's twin for the two-path writers (copy_file,
// move_file): what they WRITE is the destination, so that — not the source — is the target
// dispatch classifies. The source is left out deliberately rather than forgotten: it is read
// (or, for a move, removed) through the same os.Root-pinned fence at operation time, which
// refuses an out-of-workspace source outright, so there is no reachable call in which the source
// lands outside the blast radius the destination already described.
//
// Like pathArgWriteTarget it decodes a minimal one-field struct rather than fileOpsArgs, and for
// the same reason: classification asks only where the write would land, so a malformed sibling
// argument still yields a target for dispatch to judge. TestWriteToolsDeclarePathArgument and
// TestWriteTargetsAgreeOnPath drive both bodies from the tools' own surfaces, so a renamed json
// tag fails there instead of blinding the fence.
func destinationArgWriteTarget(call domain.ToolCall, root string) (writeTarget, bool) {
	var args struct {
		Destination string `json:"destination"`
	}
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return writeTarget{}, false
	}
	return resolveTargetUnbounded(args.Destination, root)
}

// resolveTargetUnbounded resolves input (relative to root, or absolute) to an absolute
// real path WITHOUT the containment check ResolveInRoot enforces — the path-resolution
// half of the path-safety logic, used only to classify a write target as in- or
// out-of-workspace. An empty input yields ok=false (nothing inspectable to classify).
//
// It returns the path in both spellings (writeTarget): the root-joined name the argument
// asked for, and that name resolved. This is the ONE place that sees the two, which is why
// the disclosure is derived here rather than recomputed by each surface that renders it.
func resolveTargetUnbounded(input, root string) (writeTarget, bool) {
	if input == "" {
		return writeTarget{}, false
	}
	named := filepath.Clean(input)
	if !filepath.IsAbs(input) {
		named = filepath.Join(root, input)
	}
	return writeTarget{Named: named, Real: security.EvalRealPath(named)}, true
}

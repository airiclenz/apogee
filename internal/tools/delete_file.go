package tools

import (
	"context"
	"encoding/json"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// delete_file (2026-08-10) is the file-operation family's remove-bytes half, beside the
// move-bytes half copy_file and move_file carry (file_ops.go). It names ONE file, so its
// marker resolves `path` like every other single-file writer, and its dangerous-action
// classification is the one ADR 0012 already assigns: NO new default rule. That ruleset is
// precision-over-recall — membership is *almost-never-legitimate* AND *catastrophic* — and deleting
// a single file inside the workspace is neither: it is an ordinary refactoring step, the exact
// near-miss the shipped rules go out of their way NOT to fire on ("rm -rf ./build" is documented as
// allowed). What DOES wire delete_file into the guard is structural and already in place: `path` is
// not a payloadKey (internal/security/dangerous.go), so a delete_file call's target is inspectable
// text and the shipped credential/persistence rules hard-refuse it in every mode, ahead of the
// ladder — belt to the fence's braces, since the os.Root would refuse those paths anyway.
// TestDeleteFile_DangerousActionClassification pins both halves.
//
// Git-aware since 2026-08-22: when the workspace is a git worktree and the named file is TRACKED,
// the removal is followed by a best-effort stage, so the deletion lands in the index the way
// `git rm` would leave it. The contract — silent skip when there is nothing to stage, a note
// instead of an error when staging fails, and the undo interplay (/undo restores worktree bytes
// and leaves the staged deletion standing) — is stated once, on stageGitPaths in git_stage.go.

var deleteFileSpec = toolSpec{
	name: "delete_file",
	// The description says "permanently" because that is the one fact the model cannot recover
	// from getting wrong: there is no trash and no undo, and the file is gone from the workspace
	// even though a committed copy may survive in git.
	description: "Permanently delete a file within the workspace. Files only — a directory is refused. There is no undo, so name the path exactly. A file tracked in git has its deletion staged automatically.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": {"type": "string", "description": "File path to delete"}
  }
}`),
}

// deleteFileArgs is delete_file's argument shape. It spells its target `path` — the key every
// single-file writer uses and pathArgWriteTarget decodes — so the blast-radius fence sees the file
// this call would remove (TestWriteToolsDeclarePathArgument pins the two surfaces together).
type deleteFileArgs struct {
	Path string `json:"path"`
}

// DeleteFile removes one workspace file. It is a write tool — the loop routes it through Approval
// in Ask-Before before Execute is called — and the most destructive of the family, which is why it
// refuses everything it is not certain about rather than guessing.
type DeleteFile struct {
	toolSpec
	root string
	// host is the execHost whose look resolves the git the deletion is staged through
	// (stageGitPaths) — the one host builtinTools builds, so a test hands the tool a fake look.
	host execHost
}

// NewDeleteFile returns a delete_file tool that resolves paths within root and stages through the
// git the real operating system resolves (defaultExecHost); builtinTools builds it on the shared
// host through newDeleteFile.
func NewDeleteFile(root string) *DeleteFile { return newDeleteFile(root, defaultExecHost()) }

// newDeleteFile is NewDeleteFile with the host whose look resolves git supplied.
func newDeleteFile(root string, host execHost) *DeleteFile {
	return &DeleteFile{toolSpec: deleteFileSpec, root: root, host: host}
}

// ReadOnly reports that delete_file is write-capable — it returns false, the signal that the loop
// must gate it through Approval in Ask-Before (domain.ReadOnlyTool).
func (t *DeleteFile) ReadOnly() bool { return false }

// workspaceWriteTarget resolves the absolute path this call would remove so dispatch can classify
// in- vs out-of-workspace before Execute (the workspaceScopedWriter marker,
// confinement-execution-contract §3). A removal is a write to the directory that held the file, and
// the file's own path is what states that blast radius. It performs no removal.
func (t *DeleteFile) workspaceWriteTarget(call domain.ToolCall) (writeTarget, bool) {
	return pathArgWriteTarget(call, t.root)
}

// Execute removes the named file, honouring ctx cancellation. Bad arguments, a missing file, a
// directory target and a path escaping the root are all reported as IsError results the model can
// react to. The removal itself goes through the pinned os.Root, so the fence is re-decided at
// REMOVE time rather than trusted from the check above.
func (t *DeleteFile) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[deleteFileArgs](call)
	if !ok {
		return fail, nil
	}
	// The one resolution of this call's path (writeScope.target): the pre-flight stat, the
	// disclosure and the journaled removal below all go through this value, so they describe
	// the same file as the one dispatch classified. An empty path is its refusal.
	target, err := writeScopeOf(ctx, t.root).target(args.Path)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	// The name must exist inside the fence and must not be a directory, because directory
	// removal is a different blast radius and a different tool. The stat goes through the
	// workspace fence, so an escaping path is refused here with the uniform escape wording
	// rather than reported as an ordinary absence. Like checkFileOpsPaths this is for the
	// MESSAGE first — Fence.Remove re-decides containment at operation time — but the directory
	// refusal also carries weight of its own, since os.Remove would happily unlink an EMPTY
	// directory. A name swapped between the two steps can therefore cost at most one empty
	// directory inside the workspace: still within the blast radius the call already declared,
	// and not worth a second fenced primitive to close.
	info, err := target.stat()
	if err != nil {
		return errorResult(call.ID, target.notFound(err, "file not found: ")), nil
	}
	if info.IsDir() {
		return errorResult(call.ID, "not a file: "+args.Path+" (directories are not supported)"), nil
	}
	// Read before the removal: afterwards the name is gone and resolves to itself through its
	// parent, so the one delete worth disclosing — a name that pointed somewhere else — would
	// report nothing. Fence.Remove unlinks THE NAME, so this discloses more than the call touches
	// (the link's target survives), which is the direction a security surface errs in and the one
	// the gate already took (writeTarget.note, ResolvedWriteTarget).
	resolved := target.note()
	// The funnel reads the bytes before the unlink, for the plainest reason in the family:
	// afterwards there are none. That pre-image IS the file (writeTarget.journaled, ADR 0051) —
	// the journal's copy is the only one left once Fence.Remove returns, and it is what `/undo`
	// writes back, with the mode the file carried rather than a default one. The path goes
	// post-absent, which is what makes the undo a restore rather than a rewrite.
	err = target.journaled(postAbsent, func(fence security.Fence) (bool, error) {
		if err := fence.Remove(args.Path); err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	// Staging runs only after the unlink stands, and only ever adds to what the call reports: the
	// probe reads the INDEX, so the path it takes is the one that was just removed from disk.
	staged := stageGitPaths(ctx, t.root, t.host.look, " (deletion staged in git)", args.Path)
	return okResult(call.ID, "deleted "+args.Path+resolved+staged), nil
}

var (
	_ domain.Tool           = (*DeleteFile)(nil)
	_ workspaceScopedWriter = (*DeleteFile)(nil)
)

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/security"
)

// The file-operation tools (2026-08-10) move bytes that already exist rather than bytes the
// model produced: copy_file and move_file take a source and a destination instead of a path and
// a payload. That shape is the whole reason they are their own file — everything else about them
// follows the write-tool conventions: they resolve both ends through the workspace fence at
// OPERATION time (internal/security's os.Root-pinned primitives, never a re-walked path string),
// they report an expected failure as an IsError result the model can react to, and they carry the
// workspaceScopedWriter marker so the dispatch disposition path-bounds rather than confines them
// (ADR 0012 D1, confinement-execution-contract §3).
//
// Their marker resolves the DESTINATION, not the source: that is where the write lands. The
// source needs no separate classification because it is fenced anyway — by the workspace root for
// move_file, and for copy_file (2026-08-12) by the READ scope, so an ABSOLUTE source the workspace
// refuses may still match a configured read-only root (the skills library) and be read through an
// os.Root pinned at THAT root. A copy's source is a read, which is why it is the one sanctioned use
// of a readScope on a write tool (path_safety.go). Nothing about the write moves with it: the
// destination is workspace-fenced at both ends of the call — classification and operation — so
// there is still no reachable state in which this family WRITES a file the destination
// classification did not account for.
//
// delete_file (2026-08-10) joins them as the family's remove-bytes half. It names ONE file, so its
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

var copyFileSpec = toolSpec{
	name: "copy_file",
	// Both descriptions state the destination is the new file's own PATH, not a directory to
	// drop the file into: a model carrying `cp foo bar/` habits would otherwise create a file
	// literally named "bar" and believe it had filled a directory.
	description: "Copy a file to a new path within the workspace, preserving its permissions. The destination is the full path of the new file, not a directory to copy into. Refuses to replace an existing destination unless overwrite is true. The source may also be an absolute path under a configured read-only root (such as the skills library); the destination must stay within the workspace.",
	schema:      fileOpsSchema("File path to copy from", "File path to copy to"),
}

var moveFileSpec = toolSpec{
	name:        "move_file",
	description: "Move or rename a file within the workspace. The destination is the full path of the moved file, not a directory to move into. Refuses to replace an existing destination unless overwrite is true.",
	schema:      fileOpsSchema("File path to move from", "File path to move to"),
}

// fileOpsSchema builds the argument schema both tools share, differing only in how each
// describes its two paths. Keeping one builder is what keeps the two schemas from drifting into
// different argument names, which the blast-radius fence reads (destinationArgWriteTarget).
func fileOpsSchema(sourceDesc, destinationDesc string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
  "type": "object",
  "required": ["source", "destination"],
  "properties": {
    "source": {"type": "string", "description": %q},
    "destination": {"type": "string", "description": %q},
    "overwrite": {"type": "boolean", "description": "Replace the destination if it already exists (default false)"}
  }
}`, sourceDesc, destinationDesc))
}

// fileOpsArgs is the argument shape both tools decode. They share it because they share the
// contract it describes — the fence, the overwrite refusal and the tests that pin them all read
// one set of names.
type fileOpsArgs struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Overwrite   bool   `json:"overwrite"`
}

// CopyFile copies a file onto a workspace path, preserving the source's mode. Its destination is
// always workspace-fenced; its SOURCE resolves over a readScope, so an ABSOLUTE path under a
// configured read-only root is a legal source — a copy's source is a read. It is a write tool —
// the loop routes it through Approval in Ask-Before before Execute is called.
type CopyFile struct {
	toolSpec
	root  string
	scope readScope
}

// NewCopyFile returns a copy_file tool that writes within root and resolves its SOURCE within root
// plus — for ABSOLUTE paths only — any extra read-only root extraReadRoots reports at call time
// (the same live seam NewReadFile takes). A nil extraReadRoots means workspace-only:
// byte-identical to the fence before extra roots existed.
func NewCopyFile(root string, extraReadRoots func() []string) *CopyFile {
	return &CopyFile{
		toolSpec: copyFileSpec,
		root:     root,
		scope:    readScope{root: root, extra: extraReadRoots},
	}
}

// ReadOnly reports that copy_file is write-capable — it returns false, the signal that the loop
// must gate it through Approval in Ask-Before (domain.ReadOnlyTool).
func (t *CopyFile) ReadOnly() bool { return false }

// ReadSourceKeys declares `source` as a read-only source path (domain.ReadSourceTool), so the
// dangerous-action guard's write-shaped rules judge the DESTINATION alone — copy_file reads its
// source and writes only its destination, and copying a resource OUT of the home skill library
// (an extra read root under ~/.apogee) is the ordinary skill-materialization step. MoveFile
// deliberately makes no such declaration: its source is deleted, a write by another name.
func (t *CopyFile) ReadSourceKeys() []string { return []string{"source"} }

// workspaceWriteTarget resolves the absolute path this call would write — its DESTINATION — so
// dispatch can classify in- vs out-of-workspace before Execute (the workspaceScopedWriter
// marker, confinement-execution-contract §3). It performs no write; see
// destinationArgWriteTarget for why the source needs no classification of its own.
func (t *CopyFile) workspaceWriteTarget(call domain.ToolCall) (writeTarget, bool) {
	return destinationArgWriteTarget(call, t.root)
}

// Execute copies the source file onto the destination path, honouring ctx cancellation. Bad
// arguments, a missing or non-file source, an occupied destination the call did not ask to
// overwrite, and a path escaping ITS OWN root are all reported as IsError results. The copy is
// atomic at the destination name: it either fully lands or the destination is untouched.
//
// The source's root is chosen ONCE per call — the readScope's workspace-first, absolute-only order
// — and pins both the pre-flight stat and the copy itself, so the two never disagree about which
// file is being described. A source under no root at all resolves to the workspace, which then
// refuses it with the one uniform escape message. The destination is pinned to the workspace root,
// the only end this call writes.
func (t *CopyFile) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[fileOpsArgs](call)
	if !ok {
		return fail, nil
	}
	sourceRoot := t.scope.readRoot(args.Source)
	if refusal := checkFileOpsPathsFrom(args, sourceRoot, t.root); refusal != "" {
		return errorResult(call.ID, refusal), nil
	}
	if err := security.SafeCopyFileFrom(sourceRoot, args.Source, t.root, args.Destination); err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	return okResult(call.ID, fmt.Sprintf("copied %s to %s", args.Source, args.Destination)), nil
}

// MoveFile moves or renames a workspace file. It is a write tool — the loop routes it through
// Approval in Ask-Before before Execute is called.
type MoveFile struct {
	toolSpec
	root string
}

// NewMoveFile returns a move_file tool that resolves paths within root.
func NewMoveFile(root string) *MoveFile { return &MoveFile{toolSpec: moveFileSpec, root: root} }

// ReadOnly reports that move_file is write-capable — it returns false, the signal that the loop
// must gate it through Approval in Ask-Before (domain.ReadOnlyTool).
func (t *MoveFile) ReadOnly() bool { return false }

// workspaceWriteTarget resolves the absolute path this call would write — its DESTINATION — so
// dispatch can classify in- vs out-of-workspace before Execute (the workspaceScopedWriter
// marker, confinement-execution-contract §3). The removal of the source is part of the same
// fenced operation, so it needs no classification of its own (destinationArgWriteTarget).
func (t *MoveFile) workspaceWriteTarget(call domain.ToolCall) (writeTarget, bool) {
	return destinationArgWriteTarget(call, t.root)
}

// Execute moves the source file to the destination path, honouring ctx cancellation. The same
// refusals copy_file gives apply; a rename that the filesystem cannot perform (the classic being
// a destination on a different filesystem, reachable inside the workspace through a mount point)
// falls back to copy-then-remove, which is the same move at a higher cost.
func (t *MoveFile) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[fileOpsArgs](call)
	if !ok {
		return fail, nil
	}
	if refusal := checkFileOpsPaths(args, t.root); refusal != "" {
		return errorResult(call.ID, refusal), nil
	}
	if err := t.move(args); err != "" {
		return errorResult(call.ID, err), nil
	}
	return okResult(call.ID, fmt.Sprintf("moved %s to %s", args.Source, args.Destination)), nil
}

// move performs the rename, falling back to copy-then-remove, and returns the model-facing
// failure (empty on success). A fence refusal is NEVER retried: the fallback would refuse it
// again, and reporting the escape once is what tells the model the truth about why.
func (t *MoveFile) move(args fileOpsArgs) string {
	err := security.SafeRename(t.root, args.Source, args.Destination)
	if err == nil {
		return ""
	}
	if errors.Is(err, ErrPathEscape) {
		return err.Error()
	}
	if copyErr := security.SafeCopyFile(t.root, args.Source, args.Destination); copyErr != nil {
		return copyErr.Error()
	}
	if removeErr := security.SafeRemove(t.root, args.Source); removeErr != nil {
		// The destination now holds the file and the source still does. Say so: a bare error
		// would leave the model guessing which half of the move happened.
		return fmt.Sprintf("copied %s to %s but could not remove the source: %v",
			args.Source, args.Destination, removeErr)
	}
	return ""
}

// checkFileOpsPaths is checkFileOpsPathsFrom's EQUAL-ROOTS case: one root fences both ends, which
// is what move_file needs — its removal of the source is itself a write, so the source may never
// come from anywhere the destination fence does not already cover.
func checkFileOpsPaths(args fileOpsArgs, root string) string {
	return checkFileOpsPathsFrom(args, root, root)
}

// checkFileOpsPathsFrom validates the pair the file-operation tools need before either touches the
// filesystem, and returns the model-facing refusal (empty when the operation may proceed): a source
// under sourceRoot that exists and is a regular FILE, and a destination under destinationRoot the
// operation is allowed to land on — absent, or an existing file the call explicitly asked to
// overwrite.
//
// The two roots differ for copy_file alone, whose source may have matched a configured read-only
// root (a copy's source is a read) while its destination stays workspace-fenced; move_file passes
// the workspace root for both. Everything else is identical for the two tools, refusal wording
// included, so they can never drift into different answers for the same mistake.
//
// Every stat goes through the fence of ITS OWN root, so a path that escapes is refused here with
// the uniform escape message rather than being reported as an ordinary absence. These checks are
// for the MESSAGE, never for the safety: the fenced primitives that follow re-decide containment at
// operation time, so a name swapped after this returns cannot widen anything — it can only turn
// a friendly refusal into a blunter one.
func checkFileOpsPathsFrom(args fileOpsArgs, sourceRoot, destinationRoot string) string {
	if args.Source == "" {
		return "source is required"
	}
	if args.Destination == "" {
		return "destination is required"
	}

	source, err := statInRoot(args.Source, sourceRoot)
	if err != nil {
		return escapeOrMessage(err, "file not found: "+args.Source)
	}
	if source.IsDir() {
		return "not a file: " + args.Source + " (directories are not supported)"
	}

	destination, err := statInRoot(args.Destination, destinationRoot)
	switch {
	case errors.Is(err, ErrPathEscape):
		return err.Error()
	case err != nil:
		return "" // no destination there yet — free to land
	case destination.IsDir():
		return "destination is a directory: " + args.Destination + " (give the full path of the new file)"
	case !args.Overwrite:
		return "destination already exists: " + args.Destination + " (pass overwrite: true to replace it)"
	}
	return ""
}

var deleteFileSpec = toolSpec{
	name: "delete_file",
	// The description says "permanently" because that is the one fact the model cannot recover
	// from getting wrong: there is no trash and no undo, and the file is gone from the workspace
	// even though a committed copy may survive in git.
	description: "Permanently delete a file within the workspace. Files only — a directory is refused. There is no undo, so name the path exactly.",
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
}

// NewDeleteFile returns a delete_file tool that resolves paths within root.
func NewDeleteFile(root string) *DeleteFile { return &DeleteFile{toolSpec: deleteFileSpec, root: root} }

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
	if refusal := checkDeletePath(args.Path, t.root); refusal != "" {
		return errorResult(call.ID, refusal), nil
	}
	if err := security.SafeRemove(t.root, args.Path); err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	return okResult(call.ID, "deleted "+args.Path), nil
}

// checkDeletePath validates the one path delete_file takes and returns the model-facing refusal
// (empty when the removal may proceed): the name must exist inside the fence and must not be a
// directory, because directory removal is a different blast radius and a different tool.
//
// The stat goes through the workspace fence, so an escaping path is refused here with the uniform
// escape wording rather than reported as an ordinary absence. Like checkFileOpsPaths this is for
// the MESSAGE first — SafeRemove re-decides containment at operation time — but the directory
// refusal also carries weight of its own, since os.Remove would happily unlink an EMPTY directory.
// A name swapped between the two steps can therefore cost at most one empty directory inside the
// workspace: still within the blast radius the call already declared, and not worth a second
// fenced primitive to close.
func checkDeletePath(path, root string) string {
	if path == "" {
		return "path is required"
	}

	info, err := statInRoot(path, root)
	if err != nil {
		return escapeOrMessage(err, "file not found: "+path)
	}
	if info.IsDir() {
		return "not a file: " + path + " (directories are not supported)"
	}
	return ""
}

var (
	_ domain.Tool           = (*CopyFile)(nil)
	_ workspaceScopedWriter = (*CopyFile)(nil)
	_ domain.ReadSourceTool = (*CopyFile)(nil)
	_ domain.Tool           = (*MoveFile)(nil)
	_ workspaceScopedWriter = (*MoveFile)(nil)
	_ domain.Tool           = (*DeleteFile)(nil)
	_ workspaceScopedWriter = (*DeleteFile)(nil)
)

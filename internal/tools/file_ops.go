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
// source needs no separate classification because it is fenced anyway — a source outside the
// workspace is refused by the same os.Root the copy reads through, so there is no reachable
// state in which a call touches a file the destination classification did not account for.

var copyFileSpec = toolSpec{
	name: "copy_file",
	// Both descriptions state the destination is the new file's own PATH, not a directory to
	// drop the file into: a model carrying `cp foo bar/` habits would otherwise create a file
	// literally named "bar" and believe it had filled a directory.
	description: "Copy a file to a new path within the workspace, preserving its permissions. The destination is the full path of the new file, not a directory to copy into. Refuses to replace an existing destination unless overwrite is true.",
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

// CopyFile copies a workspace file to another workspace path, preserving the source's mode. It
// is a write tool — the loop routes it through Approval in Ask-Before before Execute is called.
type CopyFile struct {
	toolSpec
	root string
}

// NewCopyFile returns a copy_file tool that resolves paths within root.
func NewCopyFile(root string) *CopyFile { return &CopyFile{toolSpec: copyFileSpec, root: root} }

// ReadOnly reports that copy_file is write-capable — it returns false, the signal that the loop
// must gate it through Approval in Ask-Before (domain.ReadOnlyTool).
func (t *CopyFile) ReadOnly() bool { return false }

// workspaceWriteTarget resolves the absolute path this call would write — its DESTINATION — so
// dispatch can classify in- vs out-of-workspace before Execute (the workspaceScopedWriter
// marker, confinement-execution-contract §3). It performs no write; see
// destinationArgWriteTarget for why the source needs no classification of its own.
func (t *CopyFile) workspaceWriteTarget(call domain.ToolCall) (string, bool) {
	return destinationArgWriteTarget(call, t.root)
}

// Execute copies the source file onto the destination path, honouring ctx cancellation. Bad
// arguments, a missing or non-file source, an occupied destination the call did not ask to
// overwrite, and a path escaping the root are all reported as IsError results. The copy is
// atomic at the destination name: it either fully lands or the destination is untouched.
func (t *CopyFile) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
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
	if err := security.SafeCopyFile(t.root, args.Source, args.Destination); err != nil {
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
func (t *MoveFile) workspaceWriteTarget(call domain.ToolCall) (string, bool) {
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

// checkFileOpsPaths validates the pair both tools need before either touches the filesystem, and
// returns the model-facing refusal (empty when the operation may proceed): a source that exists
// and is a regular FILE, and a destination the operation is allowed to land on — absent, or an
// existing file the call explicitly asked to overwrite.
//
// Every stat goes through the workspace fence, so a path that escapes is refused here with the
// uniform escape message rather than being reported as an ordinary absence. These checks are for
// the MESSAGE, never for the safety: the fenced primitives that follow re-decide containment at
// operation time, so a name swapped after this returns cannot widen anything — it can only turn
// a friendly refusal into a blunter one.
func checkFileOpsPaths(args fileOpsArgs, root string) string {
	if args.Source == "" {
		return "source is required"
	}
	if args.Destination == "" {
		return "destination is required"
	}

	source, err := statInRoot(args.Source, root)
	if err != nil {
		return escapeOrMessage(err, "file not found: "+args.Source)
	}
	if source.IsDir() {
		return "not a file: " + args.Source + " (directories are not supported)"
	}

	destination, err := statInRoot(args.Destination, root)
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

var (
	_ domain.Tool           = (*CopyFile)(nil)
	_ workspaceScopedWriter = (*CopyFile)(nil)
	_ domain.Tool           = (*MoveFile)(nil)
	_ workspaceScopedWriter = (*MoveFile)(nil)
)

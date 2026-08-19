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
// of a readScope on a write tool (path_read.go). Nothing about the write moves with it: the
// destination is workspace-fenced at both ends of the call — classification and operation — so
// there is still no reachable state in which this family WRITES a file the destination
// classification did not account for.

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
	if refusal := checkFileOpsPathsFrom(ctx, args, sourceRoot, t.root); refusal != "" {
		return errorResult(call.ID, refusal), nil
	}
	// Where this copy REALLY lands, read BEFORE it lands (resolvedTargetNote): the destination
	// name is renamed over, so a symlink AT that name is replaced rather than followed and
	// afterwards the name would resolve to itself — the one call worth disclosing would report
	// nothing. The SOURCE gets no note of its own: it is a read, and this is the writers'
	// disclosure (workspace_scoped.go).
	resolved := resolvedTargetNote(args.Destination, t.root)
	// The DESTINATION is the only end a copy mutates, so it is the only end journalled (ADR
	// 0051): pre-image bytes when overwrite:true clobbers a file, pre-absent when the copy
	// creates one — which is what makes an undo restore the first and remove the second. The
	// source is a read and records nothing.
	pre := capturePreImage(ctx, args.Destination, t.root)

	if err := security.SafeCopyFileFrom(sourceRoot, args.Source, t.root, args.Destination, writeEscapeTarget(ctx)); err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	pre.commitReadBack(ctx)
	return okResult(call.ID, fmt.Sprintf("copied %s to %s%s", args.Source, args.Destination, resolved)), nil
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
	if refusal := checkFileOpsPaths(ctx, args, t.root); refusal != "" {
		return errorResult(call.ID, refusal), nil
	}
	// Read before the move for the reason write_file reads before its write: the rename replaces
	// the destination NAME, so once it lands that name resolves to itself and the redirection the
	// operator needed to see would be gone from the sentence (resolvedTargetNote).
	resolved := resolvedTargetNote(args.Destination, t.root)

	if err := t.move(ctx, args); err != "" {
		return errorResult(call.ID, err), nil
	}
	return okResult(call.ID, fmt.Sprintf("moved %s to %s%s", args.Source, args.Destination, resolved)), nil
}

// move performs the rename, falling back to copy-then-remove, and returns the model-facing
// failure (empty on success). An UNPERMITTED fence refusal is NEVER retried: the fallback would
// refuse it again, and reporting the escape once is what tells the model the truth about why.
//
// A symlinked-parent refusal is terminal for the same reason and one more. SafeRename validates
// BOTH chains — the source's and the destination's — before it renames anything, so this error
// means one of them crosses an in-root link; retrying it as copy-then-remove would refuse the
// same chain in different words, or worse, refuse only ONE half and leave the move split (the
// copy landing while the removal is refused, duplicating the file). Passing it straight through
// also means the fallback below runs only when both chains already cleared that gate.
//
// An APPROVED escape (ADR 0049) is the one case where a fence refusal from the rename is
// EXPECTED and the fallback is the real route: one rename is one syscall through one pinned root,
// so it can never span the workspace fence and a permitted target outside it (SafeRename takes no
// permit for exactly that reason). The pair does span it — SafeCopyFileFrom carries the permit to
// the DESTINATION, SafeRemove unlinks the source under the workspace fence with no permit at all —
// which is also what keeps move_file's undisclosed source unconditionally in-workspace: the Gate
// showed the operator a destination, never a source, so nothing may leave through that half.
//
// A move is TWO journal records (ADR 0051) because it changes two files: the source, whose
// pre-image bytes are the only copy of it left once it is gone, and the destination, which the
// move either creates or clobbers. Both pre-images are read BEFORE anything moves — afterwards
// the source does not exist and the destination holds the source's bytes, so neither is
// recoverable from the filesystem a move leaves behind — and each is committed only once ITS OWN
// half landed. That is what makes the two routes journal identically, and it is what keeps the
// split failure honest: a copy that landed while the removal was refused records the destination
// alone, because the source really is still there.
func (t *MoveFile) move(ctx context.Context, args fileOpsArgs) string {
	sourcePre := capturePreImage(ctx, args.Source, t.root)
	destinationPre := capturePreImage(ctx, args.Destination, t.root)

	permitted := writeEscapeTarget(ctx)
	err := security.SafeRename(t.root, args.Source, args.Destination)
	if err == nil {
		sourcePre.commit(nil, false)
		destinationPre.commitReadBack(ctx)
		return ""
	}
	if errors.Is(err, security.ErrSymlinkedParent) {
		return err.Error()
	}
	// A root that would not open is terminal too, and it comes FIRST because it is not a fence
	// refusal at all: the copy-then-remove fallback runs through that same unopenable root, so
	// retrying it would only restate the failure in the escape's words.
	if errors.Is(err, security.ErrRootInaccessible) {
		return err.Error()
	}
	if errors.Is(err, ErrPathEscape) && permitted == "" {
		return err.Error()
	}
	if copyErr := security.SafeCopyFile(t.root, args.Source, args.Destination, permitted); copyErr != nil {
		return copyErr.Error()
	}
	if removeErr := security.SafeRemove(t.root, args.Source, ""); removeErr != nil {
		// The destination now holds the file and the source still does. Say so: a bare error
		// would leave the model guessing which half of the move happened — and journal the half
		// that DID happen, so an undo can still take the destination back.
		destinationPre.commitReadBack(ctx)
		return fmt.Sprintf("copied %s to %s but could not remove the source: %v",
			args.Source, args.Destination, removeErr)
	}
	sourcePre.commit(nil, false)
	destinationPre.commitReadBack(ctx)
	return ""
}

// checkFileOpsPaths is checkFileOpsPathsFrom's EQUAL-ROOTS case: one root fences both ends, which
// is what move_file needs — its removal of the source is itself a write, so the source may never
// come from anywhere the destination fence does not already cover.
func checkFileOpsPaths(ctx context.Context, args fileOpsArgs, root string) string {
	return checkFileOpsPathsFrom(ctx, args, root, root)
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
//
// The two halves read ctx differently, which is the ADR 0049 asymmetry in one place: the
// DESTINATION is stat'd through statWriteTarget, so an approved escape's pre-flight looks where the
// copy or move will actually land, while the SOURCE keeps the plain workspace-rooted stat and no
// permit ever reaches it — copy_file's source is already fenced by its read scope, and move_file's
// source is the one path the Gate never disclosed.
func checkFileOpsPathsFrom(ctx context.Context, args fileOpsArgs, sourceRoot, destinationRoot string) string {
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

	destination, err := statWriteTarget(ctx, args.Destination, destinationRoot)
	switch {
	// A root that will not open leads, ahead of both the escape arm and the free-to-land arm:
	// a stat that never reached the destination says nothing about whether something is there.
	case errors.Is(err, security.ErrRootInaccessible):
		return err.Error()
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
	_ domain.ReadSourceTool = (*CopyFile)(nil)
	_ domain.Tool           = (*MoveFile)(nil)
	_ workspaceScopedWriter = (*MoveFile)(nil)
)

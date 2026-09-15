package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
	description: "Copy a file to a new path within the workspace, preserving its permissions. The destination is the full path of the new file, not a directory to copy into. A directory source is copied recursively (its regular files, under the same relative paths) onto the destination as the new directory's own path. Refuses to replace an existing destination unless overwrite is true. The source may also be an absolute path under a configured read-only root (such as the skills library); the destination must stay within the workspace.",
	schema:      fileOpsSchema("File path to copy from", "File path to copy to"),
}

var moveFileSpec = toolSpec{
	name:        "move_file",
	description: "Move or rename a file within the workspace. The destination is the full path of the moved file, not a directory to move into. Refuses to replace an existing destination unless overwrite is true. When the source file is tracked in git, the rename is staged automatically (the effect of git mv).",
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

// CopyFile copies a file onto a workspace path, preserving the source's mode — or, since
// 2026-09-15, a whole directory (copyDirectory). Its destination is always workspace-fenced; its
// SOURCE resolves over a readScope, so an ABSOLUTE path under a configured read-only root is a
// legal source — a copy's source is a read. It is a write tool — the loop routes it through
// Approval in Ask-Before before Execute is called.
type CopyFile struct {
	toolSpec
	root  string
	scope readScope
}

// NewCopyFile returns a copy_file tool that writes within root and resolves its SOURCE within root
// plus — for ABSOLUTE paths only — any extra read-only root mounts reports at call time, plus any
// virtual mount it names (the same live seam NewReadFile takes). A zero ReadMounts means
// workspace-only: byte-identical to the fence before either mount seam existed.
func NewCopyFile(root string, mounts ReadMounts) *CopyFile {
	return &CopyFile{
		toolSpec: copyFileSpec,
		root:     root,
		scope:    mounts.scope(root),
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
// arguments, a missing source, an occupied destination the call did not ask to overwrite, and a
// path escaping ITS OWN root are all reported as IsError results. The copy is atomic at the
// destination name: it either fully lands or the destination is untouched. A source that is a
// DIRECTORY takes the recursive branch (copyDirectory) before the shared per-file check runs —
// that check's "not a file" arm stays for move_file, whose rename of a directory would run
// SafeRename unjournalled.
//
// The source's root AND the spelling of the source path are chosen ONCE per call, together
// (readScope.locate, the workspace-first absolute-only order): the argument AS GIVEN under the
// workspace, the RESOLVED real path under an extra root — so a symlink spelling of a mounted skill
// file copies exactly as list_dir lists it (audit 2026-08-28 F-13). That one answer pins both the
// pre-flight stat and the copy itself, so the two never disagree about which file is being
// described. A source under no root at all is read at the workspace under its own name, which then
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
	if v, ok := t.scope.virtualLocate(args.Source); ok {
		return t.copyFromMount(ctx, call, v, args)
	}

	sourceRoot, source, err := t.scope.locate(args.Source)
	if err != nil {
		sourceRoot, source = t.root, args.Source
	}
	if args.Source != "" {
		if info, statErr := statInRoot(source, sourceRoot); statErr == nil && info.IsDir() {
			return t.copyDirectory(ctx, call, args, sourceRoot, source)
		}
	}
	if refusal := checkFileOpsPathsFrom(ctx, args, source, sourceRoot, t.root); refusal != "" {
		return errorResult(call.ID, refusal), nil
	}
	// Where this copy REALLY lands, read BEFORE it lands (resolvedTargetNote): the destination
	// name is renamed over, so a symlink AT that name is replaced rather than followed and
	// afterwards the name would resolve to itself — the one call worth disclosing would report
	// nothing. The SOURCE gets no note of its own: it is a read, and this is the writers'
	// disclosure (workspace_scoped.go).
	resolved := resolvedTargetNote(args.Destination, t.root)
	// The DESTINATION is the only end a copy mutates, so it is the only path handed to the funnel
	// (journaledMutation, ADR 0051): pre-image bytes when overwrite:true clobbers a file,
	// pre-absent when the copy creates one — which is what makes an undo restore the first and
	// remove the second. The source is a read and records nothing.
	err = journaledMutation(
		ctx,
		[]mutationPath{{input: args.Destination, root: t.root, post: postReadBack}},
		func(escape string) ([]bool, error) {
			if err := security.SafeCopyFileFrom(sourceRoot, source, t.root, args.Destination, escape); err != nil {
				return nil, err
			}
			return []bool{true}, nil
		})
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	return okResult(call.ID, fmt.Sprintf("copied %s to %s%s", args.Source, args.Destination, resolved)), nil
}

// maxCopyDirectoryFiles bounds how many files one directory copy may land. Every destination is
// journalled before the first byte moves (journaledMutation captures a pre-image per path), so an
// unbounded tree — a `node_modules`, a build output — would hold that many records in memory and
// take that many undo steps; a model that hits the cap is told to copy the subtrees separately.
const maxCopyDirectoryFiles = 2000

// copyDirectory is Execute's DIRECTORY branch (2026-09-15): the source tree is enumerated
// through the fence of its own root (directoryFiles), every destination path is handed to ONE
// journaledMutation — so an undo takes the whole copy back as one step and a clobbered file's
// pre-image is captured before anything lands — and each file is copied with SafeCopyFileFrom,
// the same primitive a single-file copy uses, so the symlink-spelling rules, the source's mode
// and the staged-and-renamed destination all hold per file. The result names the file count.
//
// The destination is judged by the shared destination check with the directory reading of its
// wording: an existing directory is refused unless overwrite is true, in which case the copy
// lands INTO it, replacing the files it already holds under the same names and leaving the rest.
//
// A destination that resolves OUTSIDE the workspace is refused here with one sentence naming the
// reason, before the funnel: the write permit an approved escape carries is exact-path
// (security's openPermittedRoot compares the resolved name to the ONE approved target), so a
// directory copy — many paths — could never land under it and every child would be refused with
// its own ErrPathEscape. One refusal that says why is what the model can act on.
func (t *CopyFile) copyDirectory(
	ctx context.Context,
	call domain.ToolCall,
	args fileOpsArgs,
	sourceRoot, source string,
) (domain.ToolResult, error) {
	if args.Destination == "" {
		return errorResult(call.ID, "destination is required"), nil
	}
	if _, err := resolveInRoot(args.Destination, t.root); errors.Is(err, ErrPathEscape) {
		return errorResult(call.ID, "cannot copy a directory outside the workspace: "+args.Destination+
			" (an approved write lands on one path, and a directory copy writes many — copy the files one at a time)"), nil
	}
	if refusal := checkFileOpsDestination(ctx, args, t.root, true); refusal != "" {
		return errorResult(call.ID, refusal), nil
	}

	files, err := directoryFiles(source, sourceRoot)
	if err != nil {
		return errorResult(call.ID, escapeOrMessage(err, "directory not found: "+args.Source)), nil
	}
	if len(files) == 0 {
		return errorResult(call.ID, "nothing to copy: "+args.Source+" holds no regular files"), nil
	}
	if len(files) > maxCopyDirectoryFiles {
		return errorResult(call.ID, fmt.Sprintf("directory too large to copy: %s holds more than %d files (copy its subdirectories separately)",
			args.Source, maxCopyDirectoryFiles)), nil
	}

	resolved := resolvedTargetNote(args.Destination, t.root)
	paths := make([]mutationPath, len(files))
	for i, rel := range files {
		paths[i] = mutationPath{input: filepath.Join(args.Destination, rel), root: t.root, post: postReadBack}
	}
	err = journaledMutation(ctx, paths, func(escape string) ([]bool, error) {
		landed := make([]bool, len(files))
		for i, rel := range files {
			if err := ctx.Err(); err != nil {
				return landed, err
			}
			if err := security.SafeCopyFileFrom(sourceRoot, filepath.Join(source, rel), t.root,
				filepath.Join(args.Destination, rel), escape); err != nil {
				// Say how far the copy got: the files before this one are in place (and
				// journalled), so the model knows what to retry and an undo knows what to take back.
				return landed, fmt.Errorf("copied %d of %d files, then %s: %w", i, len(files), rel, err)
			}
			landed[i] = true
		}
		return landed, nil
	})
	if err != nil {
		// A cancellation mid-tree is the loop's to roll back, as it is at Execute's door; the
		// files that landed before it are journalled, so an undo still takes them back.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return domain.ToolResult{}, ctxErr
		}
		return errorResult(call.ID, err.Error()), nil
	}
	return okResult(call.ID, fmt.Sprintf("copied %s to %s (%d files)%s", args.Source, args.Destination, len(files), resolved)), nil
}

// directoryFiles enumerates the regular files under dir — spelled as the copy will name it under
// root — as slash-free relative paths in a stable (sorted) order, EVERY directory opened through
// root's fence (safeOpen) so a subtree that leaves the root is refused rather than walked. It
// never descends through a symlink and lists only real regular files: a linked directory could
// loop, and a link, a device or a socket is not something a copy reproduces — which is the same
// reading find_files gives a tree, whose walk names real files alone.
func directoryFiles(dir, root string) ([]string, error) {
	var files []string
	var walk func(rel string) error
	walk = func(rel string) error {
		d, err := safeOpen(filepath.Join(dir, rel), root)
		if err != nil {
			return err
		}
		entries, err := d.ReadDir(-1)
		_ = d.Close()
		if err != nil {
			return err
		}
		// A directory HANDLE yields entries in filesystem order; the copy's journal and its
		// result should not depend on it.
		slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
		for _, entry := range entries {
			child := filepath.Join(rel, entry.Name())
			switch {
			case entry.Type().IsDir():
				if err := walk(child); err != nil {
					return err
				}
			case entry.Type().IsRegular():
				files = append(files, child)
			}
		}
		return nil
	}
	if err := walk(""); err != nil {
		return nil, err
	}
	return files, nil
}

// copyFromMount is Execute's virtual-mount branch: a copy whose SOURCE lives in a tree with no
// host path (path_virtual.go), which is how a shipped skill's bundled file is materialized into
// the workspace. The bytes are read through the mount's bounded read and written through the
// workspace fence, so the sanctioned crossing stays exactly what it is for a disk mount — the
// source is a READ, the destination is the only end this call writes.
//
// Everything the model sees is the disk copy's: the same destination refusals
// (checkFileOpsDestination), the same success sentence naming the spellings it wrote, and the same
// journal record on the destination alone (safeWriteFile captures it), so an undo takes the copy
// back exactly as it takes back a copy from disk.
func (t *CopyFile) copyFromMount(ctx context.Context, call domain.ToolCall, v virtualTarget, args fileOpsArgs) (domain.ToolResult, error) {
	if args.Destination == "" {
		return errorResult(call.ID, "destination is required"), nil
	}

	info, err := v.stat()
	if err != nil {
		return errorResult(call.ID, escapeOrMessage(err, "file not found: "+args.Source)), nil
	}
	if info.IsDir() {
		return errorResult(call.ID, "not a file: "+args.Source+" (directories under a shipped mount are not supported)"), nil
	}
	data, failure := v.readBounded()
	if failure != "" {
		return errorResult(call.ID, failure), nil
	}
	if refusal := checkFileOpsDestination(ctx, args, t.root, false); refusal != "" {
		return errorResult(call.ID, refusal), nil
	}

	resolved := resolvedTargetNote(args.Destination, t.root)
	if err := safeWriteFile(ctx, args.Destination, t.root, data, copiedFilePerm); err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	return okResult(call.ID, fmt.Sprintf("copied %s to %s%s", args.Source, args.Destination, resolved)), nil
}

// copiedFilePerm is the mode a file copied OUT OF a virtual mount lands under. A mount carries no
// host mode to preserve — the bytes were compiled into the binary — so the copy takes the same
// ordinary file mode every other tool-authored write does.
const copiedFilePerm = 0o644

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
//
// A move whose SOURCE is tracked in git also stages the rename, which is the index half of
// `git mv` — the model gets rename tracking without a second tool call, and the appended note
// tells it the index changed (git_stage.go, which also owns the /undo interplay). Staging is read
// from the ONE place that means the whole move landed: move returns an empty string on both of
// its routes and a sentence on every failure, so a split failure — the copy landed, the source
// did not go — never reaches it. There is no completed rename to stage there.
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
	// args.Source is paths[0] by the helper's contract — the pre-move path whose trackedness
	// decides whether anything is staged. The destination rides along in the same pathspec pair,
	// which is also what stages an OVERWRITTEN tracked destination's replacement.
	staged := stageGitPaths(ctx, t.root, " (rename staged in git)", args.Source, args.Destination)
	return okResult(call.ID, fmt.Sprintf("moved %s to %s%s%s",
		args.Source, args.Destination, resolved, staged)), nil
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
// move either creates or clobbers. Both are handed to the funnel as one mutation, so both
// pre-images are read BEFORE anything moves — afterwards the source does not exist and the
// destination holds the source's bytes, so neither is recoverable from the filesystem a move
// leaves behind — and each is committed only once the route below REPORTS its own half landed.
// That is what makes the two routes journal identically, and it is what keeps the split failure
// honest: a copy that landed while the removal was refused reports [false, true], recording the
// destination alone, because the source really is still there.
func (t *MoveFile) move(ctx context.Context, args fileOpsArgs) string {
	err := journaledMutation(
		ctx,
		[]mutationPath{
			{input: args.Source, root: t.root, post: postAbsent},
			{input: args.Destination, root: t.root, post: postReadBack},
		},
		func(permitted string) ([]bool, error) {
			err := security.SafeRename(t.root, args.Source, args.Destination)
			if err == nil {
				return []bool{true, true}, nil
			}
			if errors.Is(err, security.ErrSymlinkedParent) {
				return nil, err
			}
			// A root that would not open is terminal too, and it comes FIRST because it is not a
			// fence refusal at all: the copy-then-remove fallback runs through that same
			// unopenable root, so retrying it would only restate the failure in the escape's
			// words.
			if errors.Is(err, security.ErrRootInaccessible) {
				return nil, err
			}
			if errors.Is(err, ErrPathEscape) && permitted == "" {
				return nil, err
			}
			if copyErr := security.SafeCopyFile(t.root, args.Source, args.Destination, permitted); copyErr != nil {
				return nil, copyErr
			}
			if removeErr := security.SafeRemove(t.root, args.Source, ""); removeErr != nil {
				// The destination now holds the file and the source still does. Say so: a bare
				// error would leave the model guessing which half of the move happened — and
				// report the half that DID happen, so an undo can still take the destination back.
				return []bool{false, true}, fmt.Errorf("copied %s to %s but could not remove the source: %w",
					args.Source, args.Destination, removeErr)
			}
			return []bool{true, true}, nil
		})
	if err != nil {
		return err.Error()
	}
	return ""
}

// checkFileOpsPaths is checkFileOpsPathsFrom's EQUAL-ROOTS case: one root fences both ends, which
// is what move_file needs — its removal of the source is itself a write, so the source may never
// come from anywhere the destination fence does not already cover.
func checkFileOpsPaths(ctx context.Context, args fileOpsArgs, root string) string {
	return checkFileOpsPathsFrom(ctx, args, args.Source, root, root)
}

// checkFileOpsPathsFrom validates the pair the file-operation tools need before either touches the
// filesystem, and returns the model-facing refusal (empty when the operation may proceed): a source
// under sourceRoot that exists and is a regular FILE, and a destination under destinationRoot the
// operation is allowed to land on — absent, or an existing file the call explicitly asked to
// overwrite. The "not a file" arm is move_file's: a directory move would run SafeRename
// unjournalled, so it is refused here, while copy_file branches on a directory source BEFORE
// reaching this check (copyDirectory).
//
// The two roots differ for copy_file alone, whose source may have matched a configured read-only
// root (a copy's source is a read) while its destination stays workspace-fenced; move_file passes
// the workspace root for both. sourcePath travels with sourceRoot for the same reason and from the
// same one answer (readScope.locate): under an extra root it is the RESOLVED source, not the
// argument's spelling. The REFUSALS still name args.Source — the spelling the model wrote is the
// one it can act on. Everything else is identical for the two tools, refusal wording included, so
// they can never drift into different answers for the same mistake.
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
func checkFileOpsPathsFrom(
	ctx context.Context,
	args fileOpsArgs,
	sourcePath, sourceRoot, destinationRoot string,
) string {
	if args.Source == "" {
		return "source is required"
	}
	if args.Destination == "" {
		return "destination is required"
	}

	source, err := statInRoot(sourcePath, sourceRoot)
	if err != nil {
		return escapeOrMessage(err, "file not found: "+args.Source)
	}
	if source.IsDir() {
		return "not a file: " + args.Source + " (directories are not supported)"
	}

	return checkFileOpsDestination(ctx, args, destinationRoot, false)
}

// checkFileOpsDestination is the DESTINATION half of that check, on its own because copy_file's
// virtual-mount branch has no source to stat through a fence — the mount is the fence — while its
// destination is judged by exactly these rules. Splitting it is what keeps the two copy routes
// giving one answer, refusal wording included, to the same destination mistake.
//
// sourceIsDir is the directory copy's reading of an occupied destination (copyDirectory): an
// existing directory there is where the copy would land INTO, so it is refused for want of
// overwrite rather than for being a directory, and an existing file cannot become one.
func checkFileOpsDestination(ctx context.Context, args fileOpsArgs, destinationRoot string, sourceIsDir bool) string {
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
	case sourceIsDir && destination.IsDir() && !args.Overwrite:
		return "destination already exists: " + args.Destination + " (give the full path of the new directory, or pass overwrite: true to copy into it, replacing the files it already holds)"
	case sourceIsDir && destination.IsDir():
		return ""
	case sourceIsDir:
		return "destination is a file: " + args.Destination + " (a directory copies onto a directory path)"
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

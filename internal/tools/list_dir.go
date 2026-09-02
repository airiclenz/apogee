package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

var listDirSpec = toolSpec{
	name:        "list_dir",
	description: "List the entries of a directory, optionally recursing into subdirectories; absolute paths under a configured read-only root (such as the skills library) are also readable.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": {"type": "string", "description": "Directory path to list, relative to the workspace root or absolute"},
    "recursive": {"type": "boolean", "description": "List subdirectories recursively (default false)"},
    "max_depth": {"type": "integer", "description": "Maximum recursion depth (default 3)"},
    "offset": {"type": "integer", "description": "Number of entries to skip for pagination (default 0)"}
  }
}`),
}

type listDirArgs struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
	MaxDepth  int    `json:"max_depth"`
	Offset    int    `json:"offset"`
}

// ListDir lists the entries of a directory, optionally recursing. It is a read-only
// tool scoped to a sandbox root plus any extra read-only roots the host mounted (readScope).
type ListDir struct {
	toolSpec
	scope readScope
}

// NewListDir returns a list_dir tool that resolves paths within root, and — for ABSOLUTE
// paths only — within any extra read-only root mounts reports at call time, plus any virtual
// mount it names. A zero ReadMounts means workspace-only: byte-identical to the fence before
// either mount seam existed.
func NewListDir(root string, mounts ReadMounts) *ListDir {
	return &ListDir{toolSpec: listDirSpec, scope: mounts.scope(root)}
}

// ReadOnly reports that list_dir performs no writes (domain.ReadOnlyTool).
func (t *ListDir) ReadOnly() bool { return true }

// Execute lists the directory named in call.Arguments, honouring ctx cancellation.
// Hidden entries (dot-prefixed) and node_modules are skipped, recursion is bounded by
// max_depth, and the entry count is capped; a missing or non-directory path is an
// IsError result.
//
// The path is resolved over the workspace root first, then — for an ABSOLUTE path only —
// over the configured extra read-only roots, and the WHOLE walk is pinned to the root that
// accepted it: a listing that began in an extra root measures its children against that
// root, never against the workspace.
func (t *ListDir) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[listDirArgs](call)
	if !ok {
		return fail, nil
	}
	if args.Path == "" {
		return errorResult(call.ID, "path is required"), nil
	}

	if v, ok := t.scope.virtualLocate(args.Path); ok {
		return t.listVirtual(ctx, call, v, args)
	}

	root, dir, err := t.scope.resolve(args.Path)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	// The walk enumerates NAMES and opens every directory through the fence, so the resolved
	// absolute path is only ever used to derive the root-relative name it starts from.
	rel := workspaceRelative(dir, root)

	handle, err := safeOpen(rel, root)
	if err != nil {
		return errorResult(call.ID, directoryNotFoundMessage(err, root, rel, args.Path)), nil
	}
	defer func() { _ = handle.Close() }()

	info, err := handle.Stat()
	if err != nil {
		// The directory opened and then would not describe itself; the fence has already had
		// its say, so this refusal is the absent one and carries near misses like the other.
		return errorResult(call.ID, directoryNotFoundMessage(nil, root, rel, args.Path)), nil
	}
	if !info.IsDir() {
		return errorResult(call.ID, "not a directory: "+args.Path), nil
	}

	maxDepth := defaultDirDepth
	if args.MaxDepth > 0 {
		maxDepth = args.MaxDepth
	}
	if maxDepth > maxDirDepthLimit {
		maxDepth = maxDirDepthLimit
	}

	entries, err := t.collectEntries(ctx, handle, root, rel, args.Recursive, maxDepth, 0)
	if err != nil {
		return domain.ToolResult{}, err // only ctx cancellation propagates as a Go error
	}

	text, listed := renderEntries(entries, args.Offset)
	return okSummary(call.ID, text, listed), nil
}

// directoryNotFoundMessage renders list_dir's absent-directory refusal for the model: the
// unchanged "directory not found: <path as the model spelled it>" wording, plus the near
// misses that name has among its parent's entries. root is the root the path was accepted
// under, rel its root-relative name, and given the model's own spelling.
//
// err is the failure being rendered, or nil where the site cannot produce a fenced one (the
// stat of an already-opened handle). A fence refusal keeps its own uniform wording and NEVER
// gains suggestions — a "did you mean" would read as absence and hide the refusal — which is
// escapeOrMessage's rule; passing it an EMPTY absent string asks exactly that question without
// keeping a second copy of the sentinel checks here.
func directoryNotFoundMessage(err error, root, rel, given string) string {
	if err != nil {
		if refusal := escapeOrMessage(err, ""); refusal != "" {
			return refusal
		}
	}
	return notFoundMessage("directory not found: ", given, suggestSiblings(root, rel, given))
}

// collectEntries walks the already-opened directory dir to the given depth, returning
// indented entry names. root is the root the listed path was accepted under and rel is dir's
// name relative to it, which is how each subdirectory is re-opened — through that root's
// fence, never by an absolute path this walk handed itself. It stops at maxDirEntries and
// checks ctx between directories so a large tree honours cancellation.
func (t *ListDir) collectEntries(ctx context.Context, dir *os.File, root, rel string, recursive bool, maxDepth, depth int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	items, err := dir.ReadDir(-1)
	if err != nil {
		return nil, nil // an unreadable subdirectory is silently skipped, as in the oracle
	}
	// A directory HANDLE yields entries in filesystem order; os.ReadDir — how this walk read
	// a directory before it opened through the fence — sorts them by name, and that order is
	// part of what the tool reports.
	slices.SortFunc(items, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })

	entries := make([]string, 0, len(items))
	indent := strings.Repeat("  ", depth)
	for _, item := range items {
		if len(entries) >= maxDirEntries {
			break
		}
		name := item.Name()
		if skipDirEntry(name) {
			continue
		}

		// The name is data inside a one-entry-per-line grammar: a directory entry carrying a
		// line break would otherwise forge rows the model reads as further entries.
		row := escapeRowBreaks(name)

		if item.IsDir() {
			entries = append(entries, indent+row+"/")
			if recursive && depth+1 < maxDepth {
				children, err := t.collectSubdir(ctx, root, filepath.Join(rel, name), recursive, maxDepth, depth+1)
				if err != nil {
					return nil, err
				}
				entries = append(entries, children...)
			}
		} else {
			entries = append(entries, indent+row)
		}
	}
	return entries, nil
}

// collectSubdir opens the subdirectory named by the root-relative rel THROUGH root's
// fence and collects its entries. A subdirectory the fence refuses — one that became a
// symlink out of that root after the walk named it — is skipped exactly like an
// unreadable one: list_dir has always reported what it can read, so an entry it cannot read
// is silently absent rather than an error.
func (t *ListDir) collectSubdir(ctx context.Context, root, rel string, recursive bool, maxDepth, depth int) ([]string, error) {
	sub, err := safeOpen(rel, root)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = sub.Close() }()
	return t.collectEntries(ctx, sub, root, rel, recursive, maxDepth, depth)
}

// skipDirEntry reports whether a directory entry is one list_dir never shows: dot-prefixed
// (hidden) or node_modules. It is shared by the disk walk and the virtual one so a mount cannot
// come to list what the workspace hides.
func skipDirEntry(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules"
}

// listVirtual answers a listing of a VIRTUAL mount (path_virtual.go) — a tree with no host path,
// hence no os.Root to pin and no fence to enforce: the mount IS the boundary, and fs.ReadDir over
// it can no more leave it than a walk can leave an os.Root. Everything a human or a model sees is
// the disk listing's: the same absent/not-a-directory refusals quoting the address they spelled,
// the same hidden-entry rule (skipDirEntry), the same caps, the same rendering.
//
// The one thing it does NOT carry is the near-miss suggestions a fenced refusal offers: those are
// read off a host directory, and a mount has none.
func (t *ListDir) listVirtual(ctx context.Context, call domain.ToolCall, v virtualTarget, args listDirArgs) (domain.ToolResult, error) {
	info, err := v.stat()
	if err != nil {
		return errorResult(call.ID, escapeOrMessage(err, "directory not found: "+args.Path)), nil
	}
	if !info.IsDir() {
		return errorResult(call.ID, "not a directory: "+args.Path), nil
	}

	tree, err := v.sub()
	if err != nil {
		return errorResult(call.ID, escapeOrMessage(err, "directory not found: "+args.Path)), nil
	}

	maxDepth := defaultDirDepth
	if args.MaxDepth > 0 {
		maxDepth = args.MaxDepth
	}
	if maxDepth > maxDirDepthLimit {
		maxDepth = maxDirDepthLimit
	}

	entries, err := collectVirtualEntries(ctx, tree, ".", args.Recursive, maxDepth, 0, nil)
	if err != nil {
		return domain.ToolResult{}, err // only ctx cancellation propagates as a Go error
	}

	text, listed := renderEntries(entries, args.Offset)
	return okSummary(call.ID, text, listed), nil
}

// collectVirtualEntries is collectEntries over an fs.FS: the same ordering (fs.ReadDir sorts by
// name, which is the order the disk walk restores by hand), the same indentation, the same
// row-break escaping and the same maxDirEntries cap, accumulated into entries so the cap counts
// across the whole recursion exactly as it does on disk.
func collectVirtualEntries(ctx context.Context, tree fs.FS, dir string, recursive bool, maxDepth, depth int, entries []string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items, err := fs.ReadDir(tree, dir)
	if err != nil {
		return entries, nil // an unreadable subdirectory is silently skipped, as on disk
	}

	indent := strings.Repeat("  ", depth)
	for _, item := range items {
		if len(entries) >= maxDirEntries {
			break
		}
		name := item.Name()
		if skipDirEntry(name) {
			continue
		}

		// The name is data inside a one-entry-per-line grammar: a directory entry carrying a
		// line break would otherwise forge rows the model reads as further entries.
		row := escapeRowBreaks(name)

		if !item.IsDir() {
			entries = append(entries, indent+row)
			continue
		}
		entries = append(entries, indent+row+"/")
		if recursive && depth+1 < maxDepth {
			entries, err = collectVirtualEntries(ctx, tree, path.Join(dir, name), recursive, maxDepth, depth+1, entries)
			if err != nil {
				return nil, err
			}
		}
	}
	return entries, nil
}

// renderEntries paginates from offset and prepends a header naming the total count. It
// returns the two counts the header states as a domain.ListedEntries — the total and the
// pagination offset actually applied, which is the offset after clamping to the total.
func renderEntries(entries []string, offset int) (string, domain.ListedEntries) {
	total := len(entries)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	shown := entries[offset:]

	truncated := ""
	if total >= maxDirEntries {
		truncated = fmt.Sprintf("\n[...truncated at %d entries]", maxDirEntries)
	}
	skipped := ""
	if offset > 0 {
		skipped = fmt.Sprintf(", skipped first %d", offset)
	}
	header := fmt.Sprintf("[%d entries total%s]", total, skipped)
	listed := domain.ListedEntries{Total: total, Skipped: offset}
	return header + "\n" + strings.Join(shown, "\n") + truncated, listed
}

var _ domain.ReadOnlyTool = (*ListDir)(nil)

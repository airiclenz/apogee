package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
// paths only — within any extra read-only root extraReadRoots reports at call time. A nil
// extraReadRoots means workspace-only: byte-identical to the fence before extra roots existed.
func NewListDir(root string, extraReadRoots func() []string) *ListDir {
	return &ListDir{toolSpec: listDirSpec, scope: readScope{root: root, extra: extraReadRoots}}
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

	root, dir, err := t.scope.resolve(args.Path)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}
	// The walk enumerates NAMES and opens every directory through the fence, so the resolved
	// absolute path is only ever used to derive the root-relative name it starts from.
	rel := workspaceRelative(dir, root)

	handle, err := safeOpen(rel, root)
	if err != nil {
		return errorResult(call.ID, escapeOrMessage(err, "directory not found: "+args.Path)), nil
	}
	defer handle.Close()

	info, err := handle.Stat()
	if err != nil {
		return errorResult(call.ID, "directory not found: "+args.Path), nil
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
		if strings.HasPrefix(name, ".") || name == "node_modules" {
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
	defer sub.Close()
	return t.collectEntries(ctx, sub, root, rel, recursive, maxDepth, depth)
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

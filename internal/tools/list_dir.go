package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

var listDirSchema = json.RawMessage(`{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": {"type": "string", "description": "Directory path to list, relative to the workspace root or absolute"},
    "recursive": {"type": "boolean", "description": "List subdirectories recursively (default false)"},
    "max_depth": {"type": "integer", "description": "Maximum recursion depth (default 3)"},
    "offset": {"type": "integer", "description": "Number of entries to skip for pagination (default 0)"}
  }
}`)

type listDirArgs struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
	MaxDepth  int    `json:"max_depth"`
	Offset    int    `json:"offset"`
}

// ListDir lists the entries of a directory, optionally recursing. It is a read-only
// tool scoped to a sandbox root.
type ListDir struct{ root string }

// NewListDir returns a list_dir tool that resolves paths within root.
func NewListDir(root string) *ListDir { return &ListDir{root: root} }

// Name returns the stable identifier the model calls.
func (t *ListDir) Name() string { return "list_dir" }

// Description returns the model-facing summary of the tool.
func (t *ListDir) Description() string {
	return "List the entries of a directory, optionally recursing into subdirectories."
}

// Schema returns the JSON schema of the tool's arguments.
func (t *ListDir) Schema() json.RawMessage { return listDirSchema }

// ReadOnly reports that list_dir performs no writes (domain.ReadOnlyTool).
func (t *ListDir) ReadOnly() bool { return true }

// Execute lists the directory named in call.Arguments, honouring ctx cancellation.
// Hidden entries (dot-prefixed) and node_modules are skipped, recursion is bounded by
// max_depth, and the entry count is capped; a missing or non-directory path is an
// IsError result.
func (t *ListDir) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	var args listDirArgs
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return errorResult(call.ID, "invalid arguments: "+err.Error()), nil
	}
	if args.Path == "" {
		return errorResult(call.ID, "path is required"), nil
	}

	dir, err := resolveInRoot(args.Path, t.root)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	info, err := os.Stat(dir)
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

	entries, err := collectEntries(ctx, dir, args.Recursive, maxDepth, 0)
	if err != nil {
		return domain.ToolResult{}, err // only ctx cancellation propagates as a Go error
	}

	return okResult(call.ID, renderEntries(entries, args.Offset)), nil
}

// collectEntries walks dir to the given depth, returning indented entry names. It
// stops at maxDirEntries and checks ctx between directories so a large tree honours
// cancellation.
func collectEntries(ctx context.Context, dir string, recursive bool, maxDepth, depth int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // an unreadable subdirectory is silently skipped, as in the oracle
	}

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

		if item.IsDir() {
			entries = append(entries, indent+name+"/")
			if recursive && depth+1 < maxDepth {
				children, err := collectEntries(ctx, filepath.Join(dir, name), recursive, maxDepth, depth+1)
				if err != nil {
					return nil, err
				}
				entries = append(entries, children...)
			}
		} else {
			entries = append(entries, indent+name)
		}
	}
	return entries, nil
}

// renderEntries paginates from offset and prepends a header naming the total count.
func renderEntries(entries []string, offset int) string {
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
	return header + "\n" + strings.Join(shown, "\n") + truncated
}

var _ domain.ReadOnlyTool = (*ListDir)(nil)

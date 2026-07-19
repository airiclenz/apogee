package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

var viewDiffSpec = toolSpec{
	name:        "view_diff",
	description: "Show a line-by-line diff between a file's current content and a proposed new content.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["path", "newContent"],
  "properties": {
    "path": {"type": "string", "description": "The file path to diff against, relative to the workspace root or absolute"},
    "newContent": {"type": "string", "description": "The proposed new content to compare with the file's current content"}
  }
}`),
}

type viewDiffArgs struct {
	Path       string `json:"path"`
	NewContent string `json:"newContent"`
}

// ViewDiff shows a line-by-line diff between a file's current content and a proposed new
// content — the read-only preview affordance the model uses before committing an edit. It
// computes the diff with a small in-package Myers LCS (no external program, §3a) so the
// output is stable and deterministic. It is read-only and carries no writer marker.
type ViewDiff struct {
	toolSpec
	root string
}

// NewViewDiff returns a view_diff tool that resolves paths within root.
func NewViewDiff(root string) *ViewDiff { return &ViewDiff{toolSpec: viewDiffSpec, root: root} }

// ReadOnly reports that view_diff performs no writes (domain.ReadOnlyTool) — it runs in
// Plan and never gates.
func (t *ViewDiff) ReadOnly() bool { return true }

// Execute reads the file named in call.Arguments and returns a deterministic unified-style
// line diff against newContent, honouring ctx cancellation. A missing file or a path
// escape is reported as an IsError result; identical content reports "No changes".
func (t *ViewDiff) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[viewDiffArgs](call)
	if !ok {
		return fail, nil
	}
	if args.Path == "" {
		return errorResult(call.ID, "path is required"), nil
	}

	path, err := resolveInRoot(args.Path, t.root)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	current, err := os.ReadFile(path)
	if err != nil {
		return errorResult(call.ID, "file not found: "+args.Path), nil
	}

	diff := unifiedLineDiff(string(current), args.NewContent)
	if diff == "" {
		return okResult(call.ID, "No changes detected"), nil
	}
	return okResult(call.ID, diff), nil
}

// unifiedLineDiff returns a unified-style line diff of old vs new, prefixing each line
// with "  ", "- ", or "+ " for context, removal, and addition respectively. It returns
// the empty string when the two are identical. The line ordering is fully determined by
// the Myers LCS below, so the output is stable across runs.
func unifiedLineDiff(oldText, newText string) string {
	if oldText == newText {
		return ""
	}

	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")

	var b strings.Builder
	for _, op := range diffLines(oldLines, newLines) {
		b.WriteString(op.tag)
		b.WriteString(op.line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// diffOp is one line of a diff: tag is "  " (context), "- " (removal), or "+ " (addition).
type diffOp struct {
	tag  string
	line string
}

// diffLines computes a line-level diff of a vs b via a longest-common-subsequence table
// (Myers' problem reduced to LCS). It emits removals before additions at each divergence,
// so the result is deterministic. The table is O(len(a)*len(b)) in space — acceptable for
// the file sizes the read tools already bound (maxFileReadBytes).
func diffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)

	// lcs[i][j] = length of the LCS of a[i:] and b[j:].
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	ops := make([]diffOp, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{tag: "  ", line: a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{tag: "- ", line: a[i]})
			i++
		default:
			ops = append(ops, diffOp{tag: "+ ", line: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{tag: "- ", line: a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{tag: "+ ", line: b[j]})
	}
	return ops
}

var _ domain.ReadOnlyTool = (*ViewDiff)(nil)

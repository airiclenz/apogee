package tools

import (
	"context"
	"encoding/json"
	"fmt"
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
// line diff against newContent, honouring ctx cancellation. A missing file, an oversized
// file or content, or a path escape is reported as an IsError result; identical content
// reports "No changes". A real diff carries its diffstat as a domain.DiffStat summary; the
// no-changes sentinel carries none, because there is no diff to describe.
//
// Both sides are bounded before any table is built: the old side is read through the same
// one-handle bounded read the read tools use (readWorkspaceFileBounded — the workspace fence
// at OPEN time plus maxFileReadBytes on the descriptor that is read, no check/use gap), and
// newContent is held to maxFileContentBytes, the ceiling the write tools apply to model-authored
// content. The quadratic table itself is capped by maxDiffTableCells (see unifiedLineDiff), so
// a preview of a large generated file degrades to a diffstat instead of exhausting memory.
//
// Reading through the pinned root carries the same narrowing read_file recorded: an in-workspace
// symlink whose target is spelled as an ABSOLUTE path is refused, because the root resolves
// relative components only. The fence is tighten-only, so the narrowing is kept.
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
	if len(args.NewContent) > maxFileContentBytes {
		return errorResult(call.ID, fmt.Sprintf("newContent too large: %d bytes (max %d)",
			len(args.NewContent), maxFileContentBytes)), nil
	}

	current, failMessage := readWorkspaceFileBounded(args.Path, t.root)
	if failMessage != "" {
		return errorResult(call.ID, failMessage), nil
	}

	diff, stat := unifiedLineDiff(string(current), args.NewContent)
	if diff == "" {
		return okResult(call.ID, "No changes detected"), nil
	}
	return okSummary(call.ID, diff, stat), nil
}

// unifiedLineDiff returns a unified-style line diff of old vs new, prefixing each line
// with "  ", "- ", or "+ " for context, removal, and addition respectively. It returns
// the empty string when the two are identical. The line ordering is fully determined by
// the Myers LCS below, so the output is stable across runs.
//
// The domain.DiffStat it returns alongside is counted from the OPERATIONS it renders from,
// never from the rendered text, so a host reads the added/removed counts as data instead of
// re-counting leading "+"/"-" over lines that may have been capped or elided on the way.
// Identical input yields the zero stat with the empty string.
//
// A pair whose LCS table would exceed maxDiffTableCells is NOT rendered: the line counts are
// measured before either side is split, and an over-budget pair degrades to the diffstat-only
// result overBudgetDiff returns. Nothing proportional to the table is allocated on that path —
// the point of the cap.
func unifiedLineDiff(oldText, newText string) (string, domain.DiffStat) {
	if oldText == newText {
		return "", domain.DiffStat{}
	}
	oldCount, newCount := lineCount(oldText), lineCount(newText)
	if int64(oldCount)*int64(newCount) > maxDiffTableCells {
		return overBudgetDiff(oldText, newText, oldCount, newCount)
	}

	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")

	var stat domain.DiffStat
	var b strings.Builder
	for _, op := range diffLines(oldLines, newLines) {
		switch op.tag {
		case tagAdded:
			stat.Added++
		case tagRemoved:
			stat.Removed++
		}
		b.WriteString(op.tag)
		b.WriteString(op.line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n"), stat
}

// overBudgetDiff renders the outcome for a pair too large to diff: the LCS table it would take
// is refused, and what comes back is the diffstat alone, with a sentence saying so. It is a
// success result, not an error — the counts are a real answer to "how much changes here?", and
// the model needs to read that the line-by-line rendering is what was withheld.
//
// The counts are approximate BY CONSTRUCTION and the sentence says so: everything between the
// identical head and the identical tail counts as changed, because establishing which inner
// lines survive is exactly the LCS work the cap exists to avoid. That over-counts a pair whose
// middles partly match and is never an under-count, which is the safe direction for a warning.
// Both trims scan the raw strings, so nothing proportional to the line count is allocated.
func overBudgetDiff(oldText, newText string, oldCount, newCount int) (string, domain.DiffStat) {
	head := commonLinePrefix(oldText, newText)
	tail := commonLineSuffix(oldText, newText)
	if shorter := min(oldCount, newCount); head+tail > shorter {
		tail = shorter - head // the head and the tail met in the middle: count each line once
	}

	stat := domain.DiffStat{Added: newCount - head - tail, Removed: oldCount - head - tail}
	return fmt.Sprintf(
		"Diff too large to render: %d x %d lines exceeds the %d-cell diff budget. "+
			"Diffstat only: +%d -%d, counted by trimming the identical head and tail, "+
			"so the counts are an upper bound rather than a rendered diff.",
		oldCount, newCount, maxDiffTableCells, stat.Added, stat.Removed), stat
}

// lineCount reports how many lines s splits into — the length strings.Split(s, "\n") would
// yield, without allocating the slice. A trailing newline therefore counts the empty final
// line, exactly as the split does.
func lineCount(s string) int { return strings.Count(s, "\n") + 1 }

// commonLinePrefix reports how many leading lines a and b share. It stops at the first
// differing line, and at the point where either side runs out of lines.
func commonLinePrefix(a, b string) int {
	for n := 0; ; n++ {
		lineA, restA, moreA := strings.Cut(a, "\n")
		lineB, restB, moreB := strings.Cut(b, "\n")
		if lineA != lineB {
			return n
		}
		if !moreA || !moreB {
			return n + 1
		}
		a, b = restA, restB
	}
}

// commonLineSuffix reports how many trailing lines a and b share — the mirror of
// commonLinePrefix, walking each string backwards from its last newline.
func commonLineSuffix(a, b string) int {
	for n := 0; ; n++ {
		lineA, restA, moreA := cutLast(a)
		lineB, restB, moreB := cutLast(b)
		if lineA != lineB {
			return n
		}
		if !moreA || !moreB {
			return n + 1
		}
		a, b = restA, restB
	}
}

// cutLast splits s around its LAST newline, returning the text after it, the text before it,
// and whether a newline was found — strings.Cut read from the other end.
func cutLast(s string) (last, rest string, found bool) {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:], s[:i], true
	}
	return s, "", false
}

// The three line tags a diff op can carry: context, removal, and addition.
const (
	tagContext = "  "
	tagRemoved = "- "
	tagAdded   = "+ "
)

// diffOp is one line of a diff: tag is tagContext, tagRemoved, or tagAdded.
type diffOp struct {
	tag  string
	line string
}

// diffLines computes a line-level diff of a vs b via a longest-common-subsequence table
// (Myers' problem reduced to LCS). It emits removals before additions at each divergence,
// so the result is deterministic.
//
// The table is O(len(a)*len(b)) in space, which the byte bounds on either side do NOT contain:
// 10 MiB of short lines is a quarter-million lines a side, and the table for that pair is
// measured in hundreds of gigabytes. Its only bound is maxDiffTableCells, enforced by the sole
// caller BEFORE the split — call this with an over-budget pair and it will try to allocate it.
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
			ops = append(ops, diffOp{tag: tagContext, line: a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{tag: tagRemoved, line: a[i]})
			i++
		default:
			ops = append(ops, diffOp{tag: tagAdded, line: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{tag: tagRemoved, line: a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{tag: tagAdded, line: b[j]})
	}
	return ops
}

var _ domain.ReadOnlyTool = (*ViewDiff)(nil)

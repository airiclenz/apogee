package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

var readFileSpec = toolSpec{
	name:        "read_file",
	description: "Read the contents of a file by path, optionally restricted to a line range, and optionally locating the line numbers where a substring occurs; absolute paths under a configured read-only root (such as the skills library) are also readable.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": {"type": "string", "description": "File path to read, relative to the workspace root or absolute"},
    "start_line": {"type": "integer", "description": "Optional 1-based start line"},
    "end_line": {"type": "integer", "description": "Optional 1-based end line (inclusive)"},
    "max_lines": {"type": "integer", "description": "Maximum number of lines to return"},
    "locate": {"type": "string", "description": "Optional substring to locate; the result reports the absolute 1-based line numbers where it occurs. The whole file is always scanned, even when a line range narrows the returned content."}
  }
}`),
}

type readFileArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	MaxLines  int    `json:"max_lines"`
	Locate    string `json:"locate"`
}

// ReadFile reads a file's contents, optionally restricted to a line range and optionally
// reporting where a substring occurs. It is a read-only tool scoped to a sandbox root plus
// any extra read-only roots the host mounted (readScope).
type ReadFile struct {
	toolSpec
	scope readScope
}

// NewReadFile returns a read_file tool that resolves paths within root, and — for ABSOLUTE
// paths only — within any extra read-only root extraReadRoots reports at call time. A nil
// extraReadRoots means workspace-only: byte-identical to the fence before extra roots existed.
func NewReadFile(root string, extraReadRoots func() []string) *ReadFile {
	return &ReadFile{toolSpec: readFileSpec, scope: readScope{root: root, extra: extraReadRoots}}
}

// ReadOnly reports that read_file performs no writes (domain.ReadOnlyTool).
func (t *ReadFile) ReadOnly() bool { return true }

// Execute reads the file named in call.Arguments and returns its content, honouring
// ctx cancellation. Bad arguments, a missing file, an oversized file, or a path that
// escapes the root are reported as IsError results, not Go errors.
//
// The fence is enforced at OPEN time through an os.Root pinned at the root the path was
// accepted under — the workspace, or, for an absolute path the workspace refuses, the first
// configured extra read-only root that contains it — so a path component swapped to point
// outside that root, including a concurrent swap by a confined subprocess mid-call, is
// refused rather than followed (security review H1). A path under no root is refused with
// the workspace's own uniform escape message, whatever the extra roots happen to be.
// The open, the size check and the read share ONE descriptor (readWorkspaceFileBounded),
// so there is no check/use gap between them: a rename mid-call changes nothing the call
// sees, and a file grown past the cap mid-read is refused (see the SCOPE note in
// internal/security/safeio.go).
//
// The pinned root resolves RELATIVE components only, so an in-root symlink whose target is
// spelled as an absolute path is refused even when that target is inside the root. That is
// narrower than the former resolveInRoot + unfenced stat + unfenced read trio, which read
// such a link; the fence is tighten-only, so the narrowing is kept and recorded in the
// CHANGELOG (Unreleased → Security). Relative in-root symlinks read as they did before.
//
// A read that DOES follow a link discloses it: the result text ends with the same
// ` → resolves to <path>` tail the write tools append when the argument named one path and the
// operation landed on another (resolvedTargetNote). An ordinary read grows nothing.
func (t *ReadFile) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	args, fail, ok := decodeToolArgs[readFileArgs](call)
	if !ok {
		return fail, nil
	}
	if args.Path == "" {
		return errorResult(call.ID, "path is required"), nil
	}

	content, failMessage := t.scope.readBounded(args.Path)
	if failMessage != "" {
		return errorResult(call.ID, failMessage), nil
	}

	// Where these bytes REALLY came from, when that is not where the argument said
	// (resolvedTargetNote — the writers' disclosure tail, appended to the rendered text so the
	// model and the transcript read the same sentence the write tools give). A read FOLLOWS a
	// symlink rather than replacing it, so the note is the only place the redirection is said
	// out loud: without it the header quotes an argument that named one file while the body
	// carries another's bytes.
	//
	// The root is the one that served the READ, not the workspace assumed (readScope.readRoot):
	// an absolute path accepted by a configured read-only root is resolved under that root. The
	// live roots are evaluated a second time here, which cannot disagree with the read's own
	// evaluation about anything this note says: a relative path never consults the extra roots
	// at all (readScope.extraRoots), and for an absolute path the root argument is unused —
	// resolveTargetUnbounded cleans such a path on its own.
	text, span := renderFile(args.Path, string(content), args)
	return okSummary(call.ID, text+resolvedTargetNote(args.Path, t.scope.readRoot(args.Path)), span), nil
}

// renderFile selects the requested line range and prepends a header naming the file
// and the lines shown, mirroring the oracle's read output. It returns the same three
// numbers the header states as a domain.ReadSpan, so a host reads the span as data
// instead of parsing it back out of the sentence.
//
// When args.Locate is set, the WHOLE file is scanned — not just the selected range — and a
// single "Located …" line naming the absolute 1-based line numbers is emitted between the
// header and the content, so a match outside a narrowed span is still reported. The span
// carries those same numbers as data. An empty Locate means none was requested and the
// output is byte-identical to a plain read.
func renderFile(displayPath, content string, args readFileArgs) (string, domain.ReadSpan) {
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	start := 0
	if args.StartLine > 0 {
		start = args.StartLine - 1
	}
	end := totalLines
	if args.EndLine > 0 && args.EndLine < end {
		end = args.EndLine
	}
	if start > totalLines {
		start = totalLines
	}
	if end < start {
		end = start
	}
	selected := lines[start:end]

	truncated := ""
	if args.MaxLines > 0 && len(selected) > args.MaxLines {
		selected = selected[:args.MaxLines]
		truncated = fmt.Sprintf("\n[...truncated at %d lines]", args.MaxLines)
	}

	header := fmt.Sprintf("[File: %s, %d lines total, showing lines %d-%d]",
		displayPath, totalLines, start+1, start+len(selected))
	span := domain.ReadSpan{Start: start + 1, End: start + len(selected), Total: totalLines}
	body := strings.Join(selected, "\n") + truncated

	if args.Locate == "" {
		return header + "\n" + body, span
	}

	span.Locate = args.Locate
	for i, line := range lines {
		if strings.Contains(line, args.Locate) {
			span.LocatedOn = append(span.LocatedOn, i+1)
		}
	}
	return header + "\n" + locateReport(span.Locate, span.LocatedOn) + "\n" + body, span
}

// locateReport words the one-line locate result: the 1-based line numbers the term was
// found on, or "on no lines" when it occurs nowhere. The sentence is BUILT from the same
// numbers the summary carries, so the two can never disagree.
func locateReport(locate string, locatedOn []int) string {
	if len(locatedOn) == 0 {
		return fmt.Sprintf("Located %q on no lines", locate)
	}

	numbers := make([]string, len(locatedOn))
	for i, n := range locatedOn {
		numbers[i] = strconv.Itoa(n)
	}
	return fmt.Sprintf("Located %q on lines: %s", locate, strings.Join(numbers, ", "))
}

// Ensure ReadFile satisfies the domain.Tool contract at compile time. The same guard
// is repeated for each tool so a signature drift fails the build here, not at wiring.
var _ domain.ReadOnlyTool = (*ReadFile)(nil)

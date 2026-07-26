package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

var readFileSpec = toolSpec{
	name:        "read_file",
	description: "Read the contents of a file by path, optionally restricted to a line range.",
	schema: json.RawMessage(`{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": {"type": "string", "description": "File path to read, relative to the workspace root or absolute"},
    "start_line": {"type": "integer", "description": "Optional 1-based start line"},
    "end_line": {"type": "integer", "description": "Optional 1-based end line (inclusive)"},
    "max_lines": {"type": "integer", "description": "Maximum number of lines to return"}
  }
}`),
}

type readFileArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	MaxLines  int    `json:"max_lines"`
}

// ReadFile reads a file's contents, optionally restricted to a line range. It is a
// read-only tool scoped to a sandbox root.
type ReadFile struct {
	toolSpec
	root string
}

// NewReadFile returns a read_file tool that resolves paths within root.
func NewReadFile(root string) *ReadFile { return &ReadFile{toolSpec: readFileSpec, root: root} }

// ReadOnly reports that read_file performs no writes (domain.ReadOnlyTool).
func (t *ReadFile) ReadOnly() bool { return true }

// Execute reads the file named in call.Arguments and returns its content, honouring
// ctx cancellation. Bad arguments, a missing file, an oversized file, or a path that
// escapes the root are reported as IsError results, not Go errors.
//
// The workspace fence is enforced at OPEN time through an os.Root pinned at t.root, so a
// path component swapped to point outside the root — including a concurrent swap by a
// confined subprocess mid-call — is refused rather than followed (security review H1).
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

	content, failMessage := readWorkspaceFileBounded(args.Path, t.root)
	if failMessage != "" {
		return errorResult(call.ID, failMessage), nil
	}

	text, span := renderFile(args.Path, string(content), args)
	return okSummary(call.ID, text, span), nil
}

// renderFile selects the requested line range and prepends a header naming the file
// and the lines shown, mirroring the oracle's read output. It returns the same three
// numbers the header states as a domain.ReadSpan, so a host reads the span as data
// instead of parsing it back out of the sentence.
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
	return header + "\n" + strings.Join(selected, "\n") + truncated, span
}

// Ensure ReadFile satisfies the domain.Tool contract at compile time. The same guard
// is repeated for each tool so a signature drift fails the build here, not at wiring.
var _ domain.ReadOnlyTool = (*ReadFile)(nil)

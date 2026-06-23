package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

var readFileSchema = json.RawMessage(`{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": {"type": "string", "description": "File path to read, relative to the workspace root or absolute"},
    "start_line": {"type": "integer", "description": "Optional 1-based start line"},
    "end_line": {"type": "integer", "description": "Optional 1-based end line (inclusive)"},
    "max_lines": {"type": "integer", "description": "Maximum number of lines to return"}
  }
}`)

type readFileArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	MaxLines  int    `json:"max_lines"`
}

// ReadFile reads a file's contents, optionally restricted to a line range. It is a
// read-only tool scoped to a sandbox root.
type ReadFile struct{ root string }

// NewReadFile returns a read_file tool that resolves paths within root.
func NewReadFile(root string) *ReadFile { return &ReadFile{root: root} }

// Name returns the stable identifier the model calls.
func (t *ReadFile) Name() string { return "read_file" }

// Description returns the model-facing summary of the tool.
func (t *ReadFile) Description() string {
	return "Read the contents of a file by path, optionally restricted to a line range."
}

// Schema returns the JSON schema of the tool's arguments.
func (t *ReadFile) Schema() json.RawMessage { return readFileSchema }

// ReadOnly reports that read_file performs no writes (domain.ReadOnlyTool).
func (t *ReadFile) ReadOnly() bool { return true }

// Execute reads the file named in call.Arguments and returns its content, honouring
// ctx cancellation. Bad arguments, a missing file, an oversized file, or a path that
// escapes the root are reported as IsError results, not Go errors.
func (t *ReadFile) Execute(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return domain.ToolResult{}, err
	}

	var args readFileArgs
	if err := decodeArgs(call.Arguments, &args); err != nil {
		return errorResult(call.ID, "invalid arguments: "+err.Error()), nil
	}
	if args.Path == "" {
		return errorResult(call.ID, "path is required"), nil
	}

	path, err := resolveInRoot(args.Path, t.root)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return errorResult(call.ID, "file not found: "+args.Path), nil
	}
	if info.IsDir() {
		return errorResult(call.ID, "not a file: "+args.Path), nil
	}
	if info.Size() > maxFileReadBytes {
		return errorResult(call.ID, fmt.Sprintf("file too large: %d bytes (max %d)", info.Size(), maxFileReadBytes)), nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return errorResult(call.ID, err.Error()), nil
	}

	return okResult(call.ID, renderFile(args.Path, string(content), args)), nil
}

// renderFile selects the requested line range and prepends a header naming the file
// and the lines shown, mirroring the oracle's read output.
func renderFile(displayPath, content string, args readFileArgs) string {
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
	return header + "\n" + strings.Join(selected, "\n") + truncated
}

// Ensure ReadFile satisfies the domain.Tool contract at compile time. The same guard
// is repeated for each tool so a signature drift fails the build here, not at wiring.
var _ domain.ReadOnlyTool = (*ReadFile)(nil)
